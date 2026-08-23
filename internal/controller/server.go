package controller

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Nischoy-ai/topo/internal/audit"
	"github.com/Nischoy-ai/topo/internal/enrollment"
	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/model"
)

// enrollmentTokenTTL bounds a minted enrollment token's lifetime.
const enrollmentTokenTTL = time.Hour

// maxEnrollRequestBytes bounds the POST /v1/enroll request body.
const maxEnrollRequestBytes = 16 << 10

// maxEnrollmentTokenRequestBytes bounds the small fixed-shape token issuance
// request, which carries only one collector ID.
const maxEnrollmentTokenRequestBytes = 4 << 10

// maxHeartbeatRequestBytes bounds the POST /v1/heartbeats request body,
// which carries only a schema version and two short IDs.
const maxHeartbeatRequestBytes = 4 << 10

// maxJobRequestBytes bounds POST /v1/jobs and POST /v1/jobs/{id}/result
// request bodies, both small fixed-shape objects.
const maxJobRequestBytes = 4 << 10

// maxCertificateRevocationRequestBytes bounds the small fixed-shape
// certificate revocation request.
const maxCertificateRevocationRequestBytes = 4 << 10

type collectorIdentityContextKey struct{}

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

	// certificateMu linearizes rotation authorization with revocation writes.
	// A rotation already holding the read lock completes before a competing
	// revocation; a revocation holding the write lock makes every later
	// rotation observe the old certificate as revoked.
	certificateMu sync.RWMutex
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
	mux.HandleFunc("GET /v1/assets", s.adminAuth(s.assets))
	mux.HandleFunc("GET /v1/relationships", s.adminAuth(s.relationships))
	mux.HandleFunc("GET /v1/audit", s.adminAuth(s.auditLog))
	mux.HandleFunc("GET /v1/observations", s.adminAuth(s.observations))
	mux.HandleFunc("POST /v1/observations", s.collectorAuth(s.ingest))
	mux.HandleFunc("POST /v1/enrollment-tokens", s.adminAuth(s.createEnrollmentToken))
	mux.HandleFunc("POST /v1/enroll", s.enroll)
	mux.HandleFunc("POST /v1/rotate", s.rotate)
	mux.HandleFunc("POST /v1/certificate-revocations", s.adminAuth(s.createCertificateRevocation))
	mux.HandleFunc("GET /v1/certificate-revocations", s.adminAuth(s.listCertificateRevocations))
	mux.HandleFunc("POST /v1/heartbeats", s.collectorAuth(s.heartbeat))
	mux.HandleFunc("GET /v1/collectors", s.adminAuth(s.collectors))
	mux.HandleFunc("POST /v1/jobs", s.adminAuth(s.createJob))
	mux.HandleFunc("GET /v1/jobs", s.collectorAuth(s.pollJobs))
	mux.HandleFunc("GET /v1/jobs/{id}", s.adminAuth(s.jobStatus))
	mux.HandleFunc("POST /v1/jobs/{id}/result", s.collectorAuth(s.reportJobResult))
	mux.HandleFunc("POST /v1/schedules", s.adminAuth(s.createOrUpdateSchedule))
	mux.HandleFunc("GET /v1/schedules", s.adminAuth(s.listSchedules))
	mux.HandleFunc("DELETE /v1/schedules/{collector_id}", s.adminAuth(s.deleteSchedule))
	return securityHeaders(mux)
}

// collectorIdentity determines the calling collector's ID for
// identity-bound endpoints (POST /v1/heartbeats, GET /v1/jobs,
// POST /v1/jobs/{id}/result). A verified mTLS peer certificate's subject
// always wins over fallback (whatever the caller claims via a request
// body field or query parameter), exactly as for POST /v1/rotate, so a
// collector can never act as a different one. collectorAuth installs this
// context value only after proving the certificate is not revoked. fallback
// is used for bearer-key authorization, including when a revoked certificate
// was presented alongside that independent compatibility credential.
func collectorIdentity(r *http.Request, fallback string) string {
	if identity, ok := r.Context().Value(collectorIdentityContextKey{}).(string); ok {
		return identity
	}
	return fallback
}

// hasVerifiedCollectorCertificate reports whether the TLS listener supplied
// a peer certificate. In production, crypto/tls populates PeerCertificates
// only after verifying the presented chain against topo serve's ClientCAs.
func hasVerifiedCollectorCertificate(r *http.Request) bool {
	return r.TLS != nil && len(r.TLS.PeerCertificates) > 0
}

// collectorAuth protects the collector data plane. An enrolled certificate
// is preferred because it carries a per-collector identity, but only after a
// fail-closed revocation lookup. The shared bearer key remains accepted as an
// independent credential so existing agents and evaluation deployments do not
// break while certificate enrollment stays opt-in.
func (s *Server) collectorAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if hasVerifiedCollectorCertificate(r) {
			cert := r.TLS.PeerCertificates[0]
			s.certificateMu.RLock()
			revoked, err := s.Store.IsCertificateRevoked(r.Context(), enrollment.CertificateSerial(cert))
			s.certificateMu.RUnlock()
			if err != nil {
				s.Logger.Error("check certificate revocation failed", "err", err)
				writeError(w, http.StatusServiceUnavailable, "certificate revocation check unavailable")
				return
			}
			if revoked {
				// The shared bearer key remains an independent compatibility
				// credential. Do not bind identity to the revoked certificate
				// when that credential is what authorizes the request.
				if s.APIKey != "" && r.Header.Get("Authorization") == "Bearer "+s.APIKey {
					next(w, r)
					return
				}
				writeError(w, http.StatusUnauthorized, "certificate revoked")
				return
			}
			ctx := context.WithValue(r.Context(), collectorIdentityContextKey{}, cert.Subject.CommonName)
			next(w, r.WithContext(ctx))
			return
		}
		if s.APIKey != "" && r.Header.Get("Authorization") != "Bearer "+s.APIKey {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

type certificateRevocationRequest struct {
	SerialNumber string `json:"serial_number"`
	Reason       string `json:"reason"`
}

func validRevocationReason(reason string) bool {
	if reason == "" || len(reason) > 512 || strings.TrimSpace(reason) != reason {
		return false
	}
	for _, r := range reason {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (s *Server) createCertificateRevocation(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCertificateRevocationRequestBytes)
	var req certificateRevocationRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid certificate revocation request: "+err.Error())
		return
	}
	serial, err := enrollment.NormalizeCertificateSerial(req.SerialNumber)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !validRevocationReason(req.Reason) {
		writeError(w, http.StatusBadRequest, "reason must be 1 to 512 bytes, have no surrounding whitespace, and contain no control characters")
		return
	}

	revocation := store.CertificateRevocation{
		SerialNumber: serial,
		Reason:       req.Reason,
		RevokedAt:    time.Now().UTC(),
	}
	s.certificateMu.Lock()
	created, err := s.Store.RevokeCertificate(r.Context(), revocation)
	if err == nil && created {
		s.recordAudit(r.Context(), "certificate_revoked", "api-key", map[string]string{
			"serial_number": serial,
			"reason":        req.Reason,
		})
	}
	s.certificateMu.Unlock()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !created {
		items, listErr := s.Store.ListCertificateRevocations(r.Context())
		if listErr != nil {
			writeError(w, http.StatusInternalServerError, listErr.Error())
			return
		}
		for _, existing := range items {
			if existing.SerialNumber == serial {
				writeJSON(w, http.StatusOK, existing)
				return
			}
		}
		writeError(w, http.StatusInternalServerError, "certificate revocation was not found after idempotent write")
		return
	}
	writeJSON(w, http.StatusCreated, revocation)
}

func (s *Server) listCertificateRevocations(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListCertificateRevocations(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

// adminAuth protects operator reads and control-plane mutations. Collector
// certificates deliberately do not confer administrative authority: with an
// API key configured, the operator bearer credential is required even when a
// client certificate is also present. Leaving APIKey empty preserves Topo's
// existing unauthenticated evaluation mode.
func (s *Server) adminAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.APIKey == "" {
			next(w, r)
			return
		}
		if r.Header.Get("Authorization") == "Bearer "+s.APIKey {
			next(w, r)
			return
		}
		if hasVerifiedCollectorCertificate(r) {
			writeError(w, http.StatusForbidden, "collector certificate is not authorized for administrative endpoints")
			return
		}
		writeError(w, http.StatusUnauthorized, "unauthorized")
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
func (s *Server) relationships(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.ListRelationships(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": v, "count": len(v)})
}

// auditLog returns the full hash-chained audit trail in sequence order,
// exactly as stored — it does not re-verify the chain itself. An operator
// or external tool wanting proof the log has not been tampered with should
// fetch this and run audit.VerifyChain over the result.
func (s *Server) auditLog(w http.ResponseWriter, r *http.Request) {
	v, err := s.Store.ListAuditEntries(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": v, "count": len(v)})
}

// recordAudit appends an audit event for an action that has already
// completed successfully. It is deliberately best-effort: an audit-storage
// failure (for example, a full disk under -db-driver sqlite) is logged but
// does not undo or fail the action that already happened, since the
// action's own effects (a minted token, an issued certificate, a queued
// job) are not stored in s.Store and cannot be rolled back through it.
func (s *Server) recordAudit(ctx context.Context, action, actor string, detail map[string]string) {
	if _, err := s.Store.AppendAuditEvent(ctx, audit.Event{Action: action, Actor: actor, Detail: detail}); err != nil {
		s.Logger.Error("append audit event failed", "action", action, "actor", actor, "err", err)
	}
}

// tokenFingerprint is a one-way, non-reversible reference to an enrollment
// token safe to place in the audit log: it lets an operator correlate an
// audit entry with a specific token (for example, while investigating a
// suspected leak) without the log itself ever holding secret material that
// would grant access if the log were read by the wrong party.
func tokenFingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
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
	// Match heartbeat and job-result identity binding: when mTLS proves a
	// collector identity, never trust a different collector_id in the body.
	e.CollectorID = collectorIdentity(r, e.CollectorID)
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
func (s *Server) createEnrollmentToken(w http.ResponseWriter, r *http.Request) {
	if s.CA == nil || s.Tokens == nil {
		writeError(w, http.StatusNotImplemented, "collector enrollment is not enabled")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxEnrollmentTokenRequestBytes)
	var req enrollment.TokenRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid enrollment token request: "+err.Error())
		return
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid enrollment token request: request body must contain one JSON object")
		return
	}
	if !enrollment.ValidCollectorID(req.CollectorID) {
		writeError(w, http.StatusBadRequest, "collector_id is empty, too long, or contains control characters")
		return
	}
	token, expiresAt, err := s.Tokens.Issue(req.CollectorID, enrollmentTokenTTL)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	s.recordAudit(r.Context(), "enrollment_token_issued", "api-key", map[string]string{
		"collector_id":      req.CollectorID,
		"token_fingerprint": tokenFingerprint(token),
		"expires_at":        expiresAt.UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusCreated, enrollment.TokenResponse{Token: token, CollectorID: req.CollectorID, ExpiresAt: expiresAt})
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
	if err := s.Tokens.Redeem(req.Token, req.CollectorID); err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	certPEM, err := s.CA.Sign(csr, req.CollectorID, enrollment.DefaultCertificateTTL)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	serial, err := enrollment.CertificateSerialFromPEM(certPEM)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	expiresAt := time.Now().Add(enrollment.DefaultCertificateTTL)
	s.recordAudit(r.Context(), "collector_enrolled", req.CollectorID, map[string]string{
		"serial_number": serial,
		"expires_at":    expiresAt.UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, enrollment.EnrollResponse{
		CertificatePEM:   string(certPEM),
		CACertificatePEM: string(s.CA.CACertPEM()),
		SerialNumber:     serial,
		ExpiresAt:        expiresAt,
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

	oldSerial := enrollment.CertificateSerial(r.TLS.PeerCertificates[0])
	// Keep authorization, signing, and the response on one side of a
	// competing revocation write. Whichever operation takes the lock first
	// defines the observable ordering of the race.
	s.certificateMu.RLock()
	defer s.certificateMu.RUnlock()
	revoked, err := s.Store.IsCertificateRevoked(r.Context(), oldSerial)
	if err != nil {
		s.Logger.Error("check certificate revocation before rotation failed", "err", err)
		writeError(w, http.StatusServiceUnavailable, "certificate revocation check unavailable")
		return
	}
	if revoked {
		writeError(w, http.StatusUnauthorized, "certificate revoked")
		return
	}

	certPEM, err := s.CA.Sign(csr, collectorID, enrollment.DefaultCertificateTTL)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	newSerial, err := enrollment.CertificateSerialFromPEM(certPEM)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	expiresAt := time.Now().Add(enrollment.DefaultCertificateTTL)
	s.recordAudit(r.Context(), "certificate_rotated", collectorID, map[string]string{
		"previous_serial_number": oldSerial,
		"serial_number":          newSerial,
		"expires_at":             expiresAt.UTC().Format(time.RFC3339),
	})
	writeJSON(w, http.StatusOK, enrollment.EnrollResponse{
		CertificatePEM:   string(certPEM),
		CACertificatePEM: string(s.CA.CACertPEM()),
		SerialNumber:     newSerial,
		ExpiresAt:        expiresAt,
	})
}

// heartbeat records a lightweight liveness signal, distinct from
// observation delivery, so GET /v1/collectors can tell a collector is
// alive between discovery scans. Wrapped in collectorAuth, so it accepts
// either the bearer API key or a verified mTLS client certificate.
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
// collector's next GET /v1/jobs poll. This is an operator control-plane
// action protected by adminAuth; an enrolled collector certificate alone
// cannot queue work.
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
	s.recordAudit(r.Context(), "job_created", "api-key", map[string]string{
		"collector_id": req.CollectorID,
		"job_id":       job.JobID,
		"job_type":     string(req.Type),
	})
	writeJSON(w, http.StatusCreated, job)
}

// pollJobs returns and marks-dispatched every pending job queued for the
// calling collector. Unlike heartbeats, there is no request body (it is a
// GET), so the collector ID comes from a query parameter when there is no
// stronger mTLS identity to use instead. Before polling, it also checks
// whether the collector has a recurring schedule that is now due — see
// maybeEnqueueScheduledJob — so a scheduled job appears in the same poll
// exactly as if it had been queued manually.
func (s *Server) pollJobs(w http.ResponseWriter, r *http.Request) {
	collectorID := collectorIdentity(r, r.URL.Query().Get("collector_id"))
	if !enrollment.ValidCollectorID(collectorID) {
		writeError(w, 400, "collector_id is empty, too long, or contains control characters")
		return
	}
	s.maybeEnqueueScheduledJob(r.Context(), collectorID)
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
