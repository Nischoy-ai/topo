package controller

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Nischoy-ai/topo/internal/store"
)

func requestWithCollectorCertificate(method, target string, body []byte) *http.Request {
	r := httptest.NewRequest(method, target, bytes.NewReader(body))
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{
		Subject: pkix.Name{CommonName: "collector-1"},
	}}}
	return r
}

func TestCollectorCertificateIsForbiddenFromEveryAdminEndpoint(t *testing.T) {
	h := New(store.NewMemory(), nil, "secret").Handler()
	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/assets"},
		{http.MethodGet, "/v1/relationships"},
		{http.MethodGet, "/v1/audit"},
		{http.MethodGet, "/v1/observations"},
		{http.MethodPost, "/v1/enrollment-tokens"},
		{http.MethodGet, "/v1/collectors"},
		{http.MethodPost, "/v1/jobs"},
		{http.MethodGet, "/v1/jobs/job-1"},
		{http.MethodPost, "/v1/schedules"},
		{http.MethodGet, "/v1/schedules"},
		{http.MethodDelete, "/v1/schedules/collector-1"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			r := requestWithCollectorCertificate(tt.method, tt.path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestCollectorCertificateCanUseEveryCollectorEndpoint(t *testing.T) {
	h := New(store.NewMemory(), nil, "secret").Handler()
	tests := []struct {
		name   string
		method string
		path   string
		body   []byte
		want   int
	}{
		{
			name:   "deliver observation",
			method: http.MethodPost,
			path:   "/v1/observations",
			body:   []byte(`{"schema_version":"v1alpha1","observation_id":"o1","collector_id":"collector-1","observed_at":"2026-08-21T00:00:00Z","assets":[]}`),
			want:   http.StatusAccepted,
		},
		{
			name:   "send heartbeat",
			method: http.MethodPost,
			path:   "/v1/heartbeats",
			body:   []byte(`{"schema_version":"v1alpha1","collector_id":"collector-1","site_id":"site-a"}`),
			want:   http.StatusAccepted,
		},
		{name: "poll jobs", method: http.MethodGet, path: "/v1/jobs", want: http.StatusOK},
		{
			name:   "report job result",
			method: http.MethodPost,
			path:   "/v1/jobs/missing/result",
			body:   []byte(`{"collector_id":"collector-1","success":true}`),
			want:   http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := requestWithCollectorCertificate(tt.method, tt.path, tt.body)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, tt.want, w.Body.String())
			}
		})
	}
}

func TestObservationOverMTLSUsesPeerCertificateIdentity(t *testing.T) {
	s := New(store.NewMemory(), nil, "secret")
	r := requestWithCollectorCertificate(
		http.MethodPost,
		"/v1/observations",
		[]byte(`{"schema_version":"v1alpha1","observation_id":"o1","collector_id":"someone-else","observed_at":"2026-08-21T00:00:00Z","assets":[]}`),
	)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", w.Code, w.Body.String())
	}

	observations, err := s.Store.ListObservations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 || observations[0].CollectorID != "collector-1" {
		t.Fatalf("observations = %+v, want the peer certificate identity collector-1", observations)
	}
}

func TestEvaluationModeLeavesAdminEndpointsOpen(t *testing.T) {
	h := New(store.NewMemory(), nil, "").Handler()
	r := requestWithCollectorCertificate(http.MethodGet, "/v1/assets", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}
