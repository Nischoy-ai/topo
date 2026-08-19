package controller

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Nischoy-ai/topo/internal/enrollment"
	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/model"
)

// enrollmentTokenTTL bounds a minted enrollment token's lifetime.
const enrollmentTokenTTL = time.Hour

// maxEnrollRequestBytes bounds the POST /v1/enroll request body.
const maxEnrollRequestBytes = 16 << 10

// maxHeartbeatRequestBytes bounds the POST /v1/heartbeats request body,
// which carries only a schema version and two short IDs.
const maxHeartbeatRequestBytes = 4 << 10

// maxJobRequestBytes bounds POST /v1/jobs and POST /v1/jobs/{id}/result
// request bodies, both small fixed-shape objects.
const maxJobRequestBytes = 4 << 10

type Server struct {
	Store  store.Repository
	Logger *slog.Logger
	APIKey string

	// CA and Tokens enable collector enrollment (POST /v1/enrollment-tokens
	// and POST /v1/enroll) when both are set. Leaving either nil disables
	// enrollment entirely; existing deployments that never set them see no
	// behavior change.
	CA     *enrollment.CA
	Tokens *enrollment.TokenStore

	// Heartbeats tracks collector liveness (POST /v1/heartbeats,
	// GET /v1/collectors). Unlike CA/Tokens this is always populated by
	// New: it requires no additional infrastructure to enable, only the
	// same auth already required for observation delivery.
	Heartbeats *HeartbeatStore

	// Jobs queues collector-initiated-poll jobs (POST /v1/jobs,
	// GET /v1/jobs, GET /v1/jobs/{id}, POST /v1/jobs/{id}/result).
	// Always populated by New, for the same reason as Heartbeats.
	Jobs    *JobStore
	started time.Time
}

func New(repo store.Repository, logger *slog.Logger, apiKey string) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		Store:      repo,
		Logger:     logger,
		APIKey:     apiKey,
		Heartbeats: NewHeartbeatStore(DefaultHeartbeatStaleAfter),
		Jobs:       NewJobStore(),
		started:    time.Now(),
	}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/assets", s.auth(s.assets))
	mux.HandleFunc("GET /v1/observations", s.auth(s.observations))
	mux.HandleFunc("POST /v1/observations", s.auth(s.ingest))
	mux.HandleFunc("POST /v1/enrollment-tokens", s.auth(s.createEnrollmentToken))
	mux.HandleFunc("POST /v1/enroll", s.enroll)
	mux.HandleFunc("POST /v1/rotate", s.rotate)
	mux.HandleFunc("POST /v1/heartbeats", s.auth(s.heartbeat))
	mux.HandleFunc("GET /v1/collectors", s.auth(s.collectors))
	mux.HandleFunc("POST /v1/jobs", s.auth(s.createJob))
	mux.HandleFunc("GET /v1/jobs", s.auth(s.pollJobs))
	mux.HandleFunc("GET /v1/jobs/{id}", s.auth(s.jobStatus))
	mux.HandleFunc("POST /v1/jobs/{id}/result", s.auth(s.reportJobResult))
	return securityHeaders(mux)
}

// collectorIdentity determines the calling collector's ID for
// identity-bound endpoints (POST /v1/heartbeats, GET /v1/jobs,
// POST /v1/jobs/{id}/result). A verified mTLS peer certificate's subject
// always wins over fallback (whatever the caller claims via a request
// body field or query parameter), exactly as for POST /v1/rotate, so a
// collector can never act as a different one. fallback is used only when
// there is no verified peer certificate, since a bearer-key-authenticated
// request carries no identity of its own.
func collectorIdentity(r *http.Request, fallback string) string {
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		return r.TLS.PeerCertificates[0].Subject.CommonName
	}
	return fallback
}
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// A non-empty PeerCertificates here means the TLS handshake already
		// verified a client certificate against the server's configured
		// ClientCAs (Go's crypto/tls rejects the connection during the
		// handshake otherwise) — an enrolled collector authenticating over
		// outbound mTLS, an alternative to the bearer API key rather than a
		// replacement for it.
		if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
			next(w, r)
			return
		}
		if s.APIKey != "" && r.Header.Get("Authorization") != "Bearer "+s.APIKey {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "schema_version": model.SchemaVersion, "uptime_seconds": int(time.Since(s.started).Seconds())})
}
func (s *Server) assets(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.ListAssets(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": v, "count": len(v)})
}
func (s *Server) observations(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.ListObservations(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": v, "count": len(v)})
}
func (s *Server) ingest(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	var e model.ObservationEnvelope
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		writeError(w, 400, "invalid observation: "+err.Error())
		return
	}
	if err := validateObservation(e); err != nil {
		writeError(w, 422, err.Error())
		return
	}
	if err := s.Store.SaveObservation(r.Context(), e); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 202, map[string]string{"observation_id": e.ObservationID, "status": "accepted"})
}
func (s *Server) createEnrollmentToken(w http.ResponseWriter, _ *http.Request) {
	if s.CA == nil || s.Tokens == nil {
		writeError(w, http.StatusNotImplemented, "collector enrollment is not enabled")
		return
	}
	token, expiresAt, err := s.Tokens.Issue(enrollmentTokenTTL)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, enrollment.TokenResponse{Token: token, ExpiresAt: expiresAt})
}

func (s *Server) enroll(w http.ResponseWriter, r *http.Request) {
	if s.CA == nil || s.Tokens == nil {
		writeError(w, http.StatusNotImplemented, "collector enrollment is not enabled")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxEnrollRequestBytes)
	var req enrollment.EnrollRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, 400, "invalid enrollment request: "+err.Error())
		return
	}
	if !enrollment.ValidCollectorID(req.CollectorID) {
		writeError(w, 400, "collector_id is empty, too long, or contains control characters")
		return
	}
	csrDER, err := base64.StdEncoding.DecodeString(req.CSR)
	if err != nil {
		writeError(w, 400, "csr is not valid base64")
		return
	}
	csr, err := enrollment.ParseCSR(csrDER)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}

	// The token is redeemed only after the CSR itself is structurally
	// valid, so a malformed request never burns a valid token.
	if err := s.Tokens.Redeem(req.Token); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	certPEM, err := s.CA.Sign(csr, req.CollectorID, enrollment.DefaultCertificateTTL)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, enrollment.EnrollResponse{
		CertificatePEM:   string(certPEM),
		CACertificatePEM: string(s.CA.CACertPEM()),
		ExpiresAt:        time.Now().Add(enrollment.DefaultCertificateTTL),
	})
}

// rotate issues a collector a fresh certificate ahead of its current one
// expiring, authenticated by that current certificate rather than a new
// enrollment token. Deliberately not wrapped in s.auth(): the bearer API
// key must never be accepted here, since it is shared across every
// collector and accepting it would let any holder mint a certificate for
// any collector ID, defeating per-collector identity. Only a client
// certificate the TLS handshake has already verified against s.CA — which
// requires the controller to be running `topo serve -mtls` — can reach
// this far; the collector ID comes from that certificate's subject, not
// from the request body, so a collector can only ever rotate its own
// certificate.
func (s *Server) rotate(w http.ResponseWriter, r *http.Request) {
	if s.CA == nil {
		writeError(w, http.StatusNotImplemented, "collector enrollment is not enabled")
		return
	}
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		writeError(w, http.StatusUnauthorized, "certificate rotation requires an already-verified client certificate")
		return
	}
	collectorID := r.TLS.PeerCertificates[0].Subject.CommonName
	if !enrollment.ValidCollectorID(collectorID) {
		writeError(w, http.StatusUnauthorized, "peer certificate has an invalid collector ID")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxEnrollRequestBytes)
	var req enrollment.RotateRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, 400, "invalid rotation request: "+err.Error())
		return
	}
	csrDER, err := base64.StdEncoding.DecodeString(req.CSR)
	if err != nil {
		writeError(w, 400, "csr is not valid base64")
		return
	}
	csr, err := enrollment.ParseCSR(csrDER)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}

	certPEM, err := s.CA.Sign(csr, collectorID, enrollment.DefaultCertificateTTL)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, enrollment.EnrollResponse{
		CertificatePEM:   string(certPEM),
		CACertificatePEM: string(s.CA.CACertPEM()),
		ExpiresAt:        time.Now().Add(enrollment.DefaultCertificateTTL),
	})
}

// heartbeat records a lightweight liveness signal, distinct from
// observation delivery, so GET /v1/collectors can tell a collector is
// alive between discovery scans. Wrapped in s.auth(), so it accepts either
// the bearer API key or a verified mTLS client certificate.
func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxHeartbeatRequestBytes)
	var req model.HeartbeatRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, 400, "invalid heartbeat: "+err.Error())
		return
	}
	if req.SchemaVersion != model.SchemaVersion {
		writeError(w, 400, "unsupported schema_version")
		return
	}

	collectorID := collectorIdentity(r, req.CollectorID)
	if !enrollment.ValidCollectorID(collectorID) {
		writeError(w, 400, "collector_id is empty, too long, or contains control characters")
		return
	}

	s.Heartbeats.Record(collectorID, req.SiteID)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "ok"})
}

func (s *Server) collectors(w http.ResponseWriter, _ *http.Request) {
	v := s.Heartbeats.List()
	writeJSON(w, 200, map[string]any{"items": v, "count": len(v)})
}

// createJob queues jobType for collectorID, to be picked up on that
// collector's next GET /v1/jobs poll. Wrapped in s.auth() like every
// other admin-style action in this project (POST /v1/enrollment-tokens
// included) — the shared bearer key or any verified collector
// certificate is treated as sufficient, matching existing precedent
// rather than introducing a separate admin-only credential here.
func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJobRequestBytes)
	var req model.JobRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, 400, "invalid job request: "+err.Error())
		return
	}
	if !enrollment.ValidCollectorID(req.CollectorID) {
		writeError(w, 400, "collector_id is empty, too long, or contains control characters")
		return
	}
	if req.Type != model.JobTypeDiscover {
		writeError(w, 400, fmt.Sprintf("unsupported job type %q", req.Type))
		return
	}
	job, err := s.Jobs.Enqueue(req.CollectorID, req.Type)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

// pollJobs returns and marks-dispatched every pending job queued for the
// calling collector. Unlike heartbeats, there is no request body (it is a
// GET), so the collector ID comes from a query parameter when there is no
// stronger mTLS identity to use instead.
func (s *Server) pollJobs(w http.ResponseWriter, r *http.Request) {
	collectorID := collectorIdentity(r, r.URL.Query().Get("collector_id"))
	if !enrollment.ValidCollectorID(collectorID) {
		writeError(w, 400, "collector_id is empty, too long, or contains control characters")
		return
	}
	jobs := s.Jobs.Poll(collectorID)
	writeJSON(w, 200, map[string]any{"items": jobs, "count": len(jobs)})
}

// jobStatus is a read-only lookup, unlike pollJobs: it never marks a job
// dispatched, so an operator can check on a job without racing the
// collector's own poll.
func (s *Server) jobStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.Jobs.Status(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, 200, status)
}

func (s *Server) reportJobResult(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJobRequestBytes)
	var req model.JobResult
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, 400, "invalid job result: "+err.Error())
		return
	}
	collectorID := collectorIdentity(r, req.CollectorID)
	if !enrollment.ValidCollectorID(collectorID) {
		writeError(w, 400, "collector_id is empty, too long, or contains control characters")
		return
	}
	if err := s.Jobs.Result(r.PathValue("id"), collectorID, req); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, ErrJobNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func validateObservation(e model.ObservationEnvelope) error {
	if e.SchemaVersion != model.SchemaVersion {
		return errors.New("unsupported schema_version")
	}
	if strings.TrimSpace(e.ObservationID) == "" {
		return errors.New("observation_id is required")
	}
	if e.ObservedAt.IsZero() {
		return errors.New("observed_at is required")
	}
	for _, a := range e.Assets {
		if a.Type == "" || a.NativeID == "" {
			return errors.New("each asset requires type and native_id")
		}
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
