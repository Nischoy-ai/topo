package controller

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/model"
)

func TestJobsEndToEndOverBearerKey(t *testing.T) {
	h := New(store.NewMemory(), nil, "secret").Handler()

	createBody, err := json.Marshal(model.JobRequest{CollectorID: "collector-1", Type: model.JobTypeDiscover})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/jobs", bytes.NewReader(createBody))
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", w.Code, w.Body.String())
	}
	var job model.Job
	if err := json.Unmarshal(w.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.JobID == "" {
		t.Fatal("expected a job ID")
	}

	// Poll as the target collector.
	r = httptest.NewRequest(http.MethodGet, "/v1/jobs?collector_id=collector-1", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("poll status = %d: %s", w.Code, w.Body.String())
	}
	var polled struct {
		Items []model.Job `json:"items"`
		Count int         `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &polled); err != nil {
		t.Fatal(err)
	}
	if polled.Count != 1 || polled.Items[0].JobID != job.JobID {
		t.Fatalf("polled = %+v", polled)
	}

	// A second poll must not redeliver it.
	r = httptest.NewRequest(http.MethodGet, "/v1/jobs?collector_id=collector-1", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var secondPoll struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &secondPoll); err != nil {
		t.Fatal(err)
	}
	if secondPoll.Count != 0 {
		t.Fatalf("second poll count = %d, want 0", secondPoll.Count)
	}

	// Report a result.
	resultBody, err := json.Marshal(model.JobResult{CollectorID: "collector-1", Success: true})
	if err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest(http.MethodPost, "/v1/jobs/"+job.JobID+"/result", bytes.NewReader(resultBody))
	r.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("result status = %d: %s", w.Code, w.Body.String())
	}

	// Check status.
	r = httptest.NewRequest(http.MethodGet, "/v1/jobs/"+job.JobID, nil)
	r.Header.Set("Authorization", "Bearer secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status lookup = %d: %s", w.Code, w.Body.String())
	}
	var status model.JobStatus
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Status != "succeeded" {
		t.Fatalf("status = %+v", status)
	}
}

func TestCreateJobRejectsUnknownType(t *testing.T) {
	h := New(store.NewMemory(), nil, "secret").Handler()
	body, err := json.Marshal(model.JobRequest{CollectorID: "collector-1", Type: "reboot"})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/jobs", bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestCreateJobRejectsInvalidCollectorID(t *testing.T) {
	h := New(store.NewMemory(), nil, "secret").Handler()
	body, err := json.Marshal(model.JobRequest{CollectorID: "", Type: model.JobTypeDiscover})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/jobs", bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestJobsRejectWithoutAuth(t *testing.T) {
	h := New(store.NewMemory(), nil, "secret").Handler()

	cases := []*http.Request{
		httptest.NewRequest(http.MethodPost, "/v1/jobs", bytes.NewBufferString(`{}`)),
		httptest.NewRequest(http.MethodGet, "/v1/jobs?collector_id=c", nil),
		httptest.NewRequest(http.MethodGet, "/v1/jobs/x", nil),
		httptest.NewRequest(http.MethodPost, "/v1/jobs/x/result", bytes.NewBufferString(`{}`)),
	}
	for _, r := range cases {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: status = %d, want 401", r.Method, r.URL.Path, w.Code)
		}
	}
}

func TestJobStatusUnknownReturns404(t *testing.T) {
	h := New(store.NewMemory(), nil, "secret").Handler()
	r := httptest.NewRequest(http.MethodGet, "/v1/jobs/nonexistent", nil)
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestReportJobResultRejectsWrongCollector(t *testing.T) {
	s := New(store.NewMemory(), nil, "secret")
	h := s.Handler()
	job, err := s.Jobs.Enqueue("collector-1", model.JobTypeDiscover)
	if err != nil {
		t.Fatal(err)
	}
	s.Jobs.Poll("collector-1")

	body, err := json.Marshal(model.JobResult{CollectorID: "collector-2", Success: true})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/jobs/"+job.JobID+"/result", bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// TestJobsOverMTLSUseVerifiedPeerCertificateIdentity proves the same
// identity rule as POST /v1/rotate and POST /v1/heartbeats: a poll or
// result reported over mTLS is bound to the verified peer certificate's
// identity, not to a query parameter or request body field the caller
// could otherwise set arbitrarily.
func TestJobsOverMTLSUseVerifiedPeerCertificateIdentity(t *testing.T) {
	server, clientCert, caPool := newMTLSTestServer(t)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caPool,
	}}}

	// Poll claiming a different collector_id via the query string: the
	// verified certificate's identity ("collector-1") must win, so this
	// must return no jobs (none were queued for "collector-1").
	resp, err := client.Get(server.URL + "/v1/jobs?collector_id=someone-elses-identity")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var polled struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&polled); err != nil {
		t.Fatal(err)
	}
	if polled.Count != 0 {
		t.Fatalf("count = %d, want 0 (nothing queued for collector-1)", polled.Count)
	}
}

func TestReportJobResultOverMTLSUsesPeerCertificateIdentity(t *testing.T) {
	server, clientCert, caPool := newMTLSTestServer(t)
	// newMTLSTestServer's underlying *Server is only reachable via the
	// httptest.Server it returns, so drive everything through HTTP,
	// including queuing the job itself via a bearer-authenticated request
	// made over the same mTLS listener (VerifyClientCertIfGiven allows a
	// request with no client certificate through to the TLS layer; the
	// enrollment test server's bearer key is "secret").
	adminClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: caPool}}}
	body, err := json.Marshal(model.JobRequest{CollectorID: "collector-1", Type: model.JobTypeDiscover})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/jobs", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	resp, err := adminClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	var job model.Job
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}

	collectorClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caPool,
	}}}
	pollResp, err := collectorClient.Get(server.URL + "/v1/jobs")
	if err != nil {
		t.Fatal(err)
	}
	defer pollResp.Body.Close()
	var polled struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(pollResp.Body).Decode(&polled); err != nil {
		t.Fatal(err)
	}
	if polled.Count != 1 {
		t.Fatalf("polled count = %d, want 1", polled.Count)
	}

	// Report the result claiming a different collector_id in the body:
	// the verified peer certificate's real identity ("collector-1") must
	// still be what gets checked against the job's actual owner.
	resultBody, err := json.Marshal(model.JobResult{CollectorID: "someone-elses-identity", Success: true})
	if err != nil {
		t.Fatal(err)
	}
	resultResp, err := collectorClient.Post(server.URL+"/v1/jobs/"+job.JobID+"/result", "application/json", bytes.NewReader(resultBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resultResp.Body.Close()
	if resultResp.StatusCode != http.StatusOK {
		t.Fatalf("result status = %d, want 200 (peer certificate identity should have been used, not the body's claim)", resultResp.StatusCode)
	}
}
