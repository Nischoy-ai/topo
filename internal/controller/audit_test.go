package controller

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nischoy-ai/topo/internal/audit"
	"github.com/Nischoy-ai/topo/internal/enrollment"
	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/model"
)

func getAuditEntries(t *testing.T, h http.Handler, bearer string) []audit.Entry {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/v1/audit", nil)
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /v1/audit status %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Items []audit.Entry `json:"items"`
		Count int           `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Count != len(body.Items) {
		t.Fatalf("count %d does not match len(items) %d", body.Count, len(body.Items))
	}
	return body.Items
}

func TestAuditLogRequiresAuth(t *testing.T) {
	h := New(store.NewMemory(), nil, "secret").Handler()
	r := httptest.NewRequest(http.MethodGet, "/v1/audit", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestEnrollmentTokenIssuanceIsAudited(t *testing.T) {
	s := newEnrollmentTestServer(t)
	h := s.Handler()
	token := mintToken(t, h)

	entries := getAuditEntries(t, h, "secret")
	if len(entries) != 1 {
		t.Fatalf("got %d audit entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Action != "enrollment_token_issued" {
		t.Fatalf("action = %q, want enrollment_token_issued", e.Action)
	}
	if e.Detail["token_fingerprint"] == "" {
		t.Fatal("expected a non-empty token_fingerprint in the audit detail")
	}
	if e.Detail["expires_at"] == "" {
		t.Fatal("expected a non-empty expires_at in the audit detail")
	}

	// The raw token is a bearer credential; it must never appear anywhere
	// in the audit trail, which is retained indefinitely and may be read
	// by parties who should not be able to redeem a still-valid token.
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), token) {
		t.Fatal("raw enrollment token leaked into the audit log")
	}
}

func TestEnrollIsAudited(t *testing.T) {
	s := newEnrollmentTestServer(t)
	h := s.Handler()
	token := mintToken(t, h)

	body, _ := json.Marshal(enrollment.EnrollRequest{Token: token, CollectorID: "collector-1", CSR: testCSRBase64(t, "collector-1")})
	r := httptest.NewRequest(http.MethodPost, "/v1/enroll", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("enroll status %d: %s", w.Code, w.Body.String())
	}

	entries := getAuditEntries(t, h, "secret")
	if len(entries) != 2 {
		t.Fatalf("got %d audit entries, want 2 (token issuance + enrollment)", len(entries))
	}
	if entries[0].Action != "enrollment_token_issued" {
		t.Fatalf("entries[0].Action = %q, want enrollment_token_issued", entries[0].Action)
	}
	if entries[1].Action != "collector_enrolled" || entries[1].Actor != "collector-1" {
		t.Fatalf("entries[1] = %#v, want action=collector_enrolled actor=collector-1", entries[1])
	}
	if err := audit.VerifyChain(entries); err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
}

func TestRotateIsAudited(t *testing.T) {
	httpServer, clientCert, caPool := newMTLSTestServer(t)
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{clientCert}, RootCAs: caPool}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}

	csr := testCSRBase64(t, "collector-1")
	reqBody, err := json.Marshal(enrollment.RotateRequest{CSR: csr})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(httpServer.URL+"/v1/rotate", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rotate status = %d", resp.StatusCode)
	}

	// GET /v1/audit is an operator endpoint, so the collector certificate
	// used for rotation is not sufficient by itself.
	auditReq, err := http.NewRequest(http.MethodGet, httpServer.URL+"/v1/audit", nil)
	if err != nil {
		t.Fatal(err)
	}
	auditReq.Header.Set("Authorization", "Bearer secret")
	auditResp, err := client.Do(auditReq)
	if err != nil {
		t.Fatal(err)
	}
	defer auditResp.Body.Close()
	var body struct {
		Items []audit.Entry `json:"items"`
	}
	if err := json.NewDecoder(auditResp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 1 || body.Items[0].Action != "certificate_rotated" || body.Items[0].Actor != "collector-1" {
		t.Fatalf("got audit entries %#v, want a single certificate_rotated entry for collector-1", body.Items)
	}
	if body.Items[0].Detail["previous_serial_number"] == "" || body.Items[0].Detail["serial_number"] == "" {
		t.Fatalf("rotation audit entry does not identify both certificate serials: %#v", body.Items[0].Detail)
	}
}

func TestJobCreationIsAudited(t *testing.T) {
	s := New(store.NewMemory(), nil, "secret")
	h := s.Handler()

	reqBody, _ := json.Marshal(model.JobRequest{CollectorID: "collector-1", Type: model.JobTypeDiscover})
	r := httptest.NewRequest(http.MethodPost, "/v1/jobs", bytes.NewReader(reqBody))
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("create job status %d: %s", w.Code, w.Body.String())
	}
	var job model.Job
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}

	entries := getAuditEntries(t, h, "secret")
	if len(entries) != 1 {
		t.Fatalf("got %d audit entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Action != "job_created" {
		t.Fatalf("action = %q, want job_created", e.Action)
	}
	if e.Detail["collector_id"] != "collector-1" || e.Detail["job_type"] != "discover" || e.Detail["job_id"] != job.JobID {
		t.Fatalf("unexpected audit detail: %#v (job_id = %q)", e.Detail, job.JobID)
	}
}
