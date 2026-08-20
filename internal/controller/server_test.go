package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/model"
)

func TestIngestAndListAssets(t *testing.T) {
	s := New(store.NewMemory(), nil, "secret")
	h := s.Handler()
	body := []byte(`{"schema_version":"v1alpha1","observation_id":"o1","site_id":"s","collector_id":"c","plugin":"test","observed_at":"` + time.Now().UTC().Format(time.RFC3339) + `","assets":[{"type":"host","native_id":"h1","name":"host"}]}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/observations", bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatalf("ingest status %d: %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest(http.MethodGet, "/v1/assets", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"count":1`)) {
		t.Fatalf("list response: %d %s", w.Code, w.Body.String())
	}
}
func TestIngestAndListRelationships(t *testing.T) {
	s := New(store.NewMemory(), nil, "secret")
	h := s.Handler()
	body := []byte(`{"schema_version":"v1alpha1","observation_id":"o1","site_id":"s","collector_id":"c","plugin":"test","observed_at":"` + time.Now().UTC().Format(time.RFC3339) + `","assets":[{"type":"host","native_id":"h1","name":"host"},{"type":"network_interface","native_id":"h1:interface:1","name":"eth0"}],"relationships":[{"type":"host_has_interface","from_native_id":"h1","to_native_id":"h1:interface:1"}]}`)
	r := httptest.NewRequest(http.MethodPost, "/v1/observations", bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatalf("ingest status %d: %s", w.Code, w.Body.String())
	}
	r = httptest.NewRequest(http.MethodGet, "/v1/relationships", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"count":1`)) {
		t.Fatalf("list response: %d %s", w.Code, w.Body.String())
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"host_has_interface"`)) {
		t.Fatalf("relationship missing from response: %s", w.Body.String())
	}
}
func TestAuthRequired(t *testing.T) {
	h := New(store.NewMemory(), nil, "secret").Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/assets", nil))
	if w.Code != 401 {
		t.Fatalf("got %d", w.Code)
	}
}
func TestRejectsWrongSchema(t *testing.T) {
	_ = model.SchemaVersion
	h := New(store.NewMemory(), nil, "").Handler()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/observations", bytes.NewBufferString(`{"schema_version":"v0","observation_id":"x","observed_at":"2026-01-01T00:00:00Z","assets":[]}`)))
	if w.Code != 422 {
		t.Fatalf("got %d", w.Code)
	}
}
