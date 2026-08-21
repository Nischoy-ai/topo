package controller

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/model"
)

func TestHeartbeatStoreRecordThenList(t *testing.T) {
	s := NewHeartbeatStore(time.Minute)
	s.Record("collector-1", "site-a")
	s.Record("collector-2", "site-b")

	items := s.List()
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	// Sorted by collector ID.
	if items[0].CollectorID != "collector-1" || items[0].SiteID != "site-a" {
		t.Fatalf("items[0] = %+v", items[0])
	}
	if items[1].CollectorID != "collector-2" || items[1].SiteID != "site-b" {
		t.Fatalf("items[1] = %+v", items[1])
	}
	if !items[0].Alive || !items[1].Alive {
		t.Fatal("a heartbeat recorded just now should be alive")
	}
}

func TestHeartbeatStoreRecordOverwritesPrevious(t *testing.T) {
	s := NewHeartbeatStore(time.Minute)
	s.Record("collector-1", "site-a")
	s.Record("collector-1", "site-b")

	items := s.List()
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].SiteID != "site-b" {
		t.Fatalf("site = %q, want site-b (the most recent heartbeat)", items[0].SiteID)
	}
}

func TestHeartbeatStoreReportsStale(t *testing.T) {
	s := NewHeartbeatStore(time.Millisecond)
	s.Record("collector-1", "site-a")
	time.Sleep(5 * time.Millisecond)

	items := s.List()
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].Alive {
		t.Fatal("expected a heartbeat older than staleAfter to be reported as not alive")
	}
}

func TestHeartbeatStoreListEmpty(t *testing.T) {
	s := NewHeartbeatStore(time.Minute)
	items := s.List()
	if len(items) != 0 {
		t.Fatalf("items = %d, want 0", len(items))
	}
}

func heartbeatBody(t *testing.T, collectorID, siteID string) []byte {
	t.Helper()
	body, err := json.Marshal(model.HeartbeatRequest{SchemaVersion: model.SchemaVersion, CollectorID: collectorID, SiteID: siteID})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestHeartbeatEndToEndOverBearerKey(t *testing.T) {
	s := New(store.NewMemory(), nil, "secret")
	h := s.Handler()

	r := httptest.NewRequest(http.MethodPost, "/v1/heartbeats", bytes.NewReader(heartbeatBody(t, "collector-1", "site-a")))
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	r = httptest.NewRequest(http.MethodGet, "/v1/collectors", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var decoded struct {
		Items []model.CollectorStatus `json:"items"`
		Count int                     `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Count != 1 || decoded.Items[0].CollectorID != "collector-1" || decoded.Items[0].SiteID != "site-a" {
		t.Fatalf("collectors response = %+v", decoded)
	}
	if !decoded.Items[0].Alive {
		t.Fatal("expected the just-recorded collector to be alive")
	}
}

func TestHeartbeatRejectsWithoutAuth(t *testing.T) {
	h := New(store.NewMemory(), nil, "secret").Handler()
	r := httptest.NewRequest(http.MethodPost, "/v1/heartbeats", bytes.NewReader(heartbeatBody(t, "collector-1", "site-a")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestHeartbeatRejectsWrongSchemaVersion(t *testing.T) {
	h := New(store.NewMemory(), nil, "").Handler()
	r := httptest.NewRequest(http.MethodPost, "/v1/heartbeats", bytes.NewBufferString(`{"schema_version":"v0","collector_id":"c","site_id":"s"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHeartbeatRejectsInvalidCollectorID(t *testing.T) {
	h := New(store.NewMemory(), nil, "").Handler()
	r := httptest.NewRequest(http.MethodPost, "/v1/heartbeats", bytes.NewReader(heartbeatBody(t, "", "site-a")))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// TestHeartbeatOverMTLSUsesPeerCertificateIdentity proves the same identity
// rule as POST /v1/rotate: a heartbeat authenticated by a verified client
// certificate is recorded under that certificate's collector ID, not
// whatever collector_id the request body claims — so a collector cannot
// heartbeat, and therefore appear alive, as a different collector.
func TestHeartbeatOverMTLSUsesPeerCertificateIdentity(t *testing.T) {
	server, clientCert, caPool := newMTLSTestServer(t)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caPool,
	}}}

	resp, err := client.Post(server.URL+"/v1/heartbeats", "application/json", bytes.NewReader(heartbeatBody(t, "someone-elses-identity", "site-a")))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/v1/collectors", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp2, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var decoded struct {
		Items []model.CollectorStatus `json:"items"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Items) != 1 || decoded.Items[0].CollectorID != "collector-1" {
		t.Fatalf("collectors = %+v, want a single entry for collector-1 (the peer certificate's identity)", decoded.Items)
	}
}
