package controller

import (
	"encoding/base64"
	"encoding/json"
	"errors"
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

type Server struct {
	Store  store.Repository
	Logger *slog.Logger
	APIKey string

	// CA and Tokens enable collector enrollment (POST /v1/enrollment-tokens
	// and POST /v1/enroll) when both are set. Leaving either nil disables
	// enrollment entirely; existing deployments that never set them see no
	// behavior change.
	CA      *enrollment.CA
	Tokens  *enrollment.TokenStore
	started time.Time
}

func New(repo store.Repository, logger *slog.Logger, apiKey string) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{Store: repo, Logger: logger, APIKey: apiKey, started: time.Now()}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/assets", s.auth(s.assets))
	mux.HandleFunc("GET /v1/observations", s.auth(s.observations))
	mux.HandleFunc("POST /v1/observations", s.auth(s.ingest))
	mux.HandleFunc("POST /v1/enrollment-tokens", s.auth(s.createEnrollmentToken))
	mux.HandleFunc("POST /v1/enroll", s.enroll)
	return securityHeaders(mux)
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
