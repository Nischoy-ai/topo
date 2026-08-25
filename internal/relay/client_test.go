package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientPollAndReportUseFixedAuthenticatedEndpoints(t *testing.T) {
	t.Parallel()
	var polls, reports atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case pollPath:
			polls.Add(1)
			var request PollRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode poll: %v", err)
			}
			if request.RelayID != "relay-1" {
				t.Errorf("relay_id = %q", request.RelayID)
			}
			_ = json.NewEncoder(w).Encode(pollResponse{Jobs: []Job{{JobID: "job-1", Type: JobTypeDiscover, ProfileID: "local"}}})
		case resultPath:
			reports.Add(1)
			var result JobResult
			if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
				t.Errorf("decode result: %v", err)
			}
			if result.JobID != "job-1" || !result.Success {
				t.Errorf("result = %#v", result)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "secret-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := client.Poll(context.Background(), PollRequest{SchemaVersion: ContractVersion, RelayID: "relay-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ProfileID != "local" {
		t.Fatalf("jobs = %#v", jobs)
	}
	if err := client.Report(context.Background(), JobResult{JobID: "job-1", Success: true}); err != nil {
		t.Fatal(err)
	}
	if polls.Load() != 1 || reports.Load() != 1 {
		t.Fatalf("polls=%d reports=%d", polls.Load(), reports.Load())
	}
}

func TestClientRejectsInjectedJobFields(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jobs":[{"job_id":"job-1","type":"discover","profile_id":"linux","targets":["attacker:22"]}]}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Poll(context.Background(), PollRequest{}); err == nil {
		t.Fatal("Poll accepted a ServiceNow-supplied targets field")
	}
}

func TestClientNeverFollowsRedirectOrReplaysToken(t *testing.T) {
	t.Parallel()
	var targetCalls atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		if r.Header.Get("Authorization") != "" {
			t.Error("redirect target received Authorization")
		}
	}))
	defer target.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	clientHTTP := redirect.Client()
	clientHTTP.CheckRedirect = nil // NewClient must override this unsafe default.
	client, err := NewClient(redirect.URL, "secret-token", clientHTTP)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Poll(context.Background(), PollRequest{}); err == nil {
		t.Fatal("Poll accepted redirect response")
	}
	if targetCalls.Load() != 0 {
		t.Fatalf("redirect target called %d times", targetCalls.Load())
	}
}

func TestNewClientValidatesInstanceURL(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"http://example.com", "https://user@example.com", "https://example.com/path", "https://example.com?x=1"} {
		if _, err := NewClient(raw, "token", nil); err == nil {
			t.Errorf("NewClient(%q) succeeded", raw)
		}
	}
	if _, err := NewClient("https://example.com", "", nil); err == nil {
		t.Error("NewClient accepted empty token")
	}
}

func TestClientRejectsMoreThanOneClaimedJob(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(pollResponse{Jobs: []Job{
			{JobID: "one", Type: JobTypeDiscover, ProfileID: "p", RequestedAt: time.Now()},
			{JobID: "two", Type: JobTypeDiscover, ProfileID: "p", RequestedAt: time.Now()},
		}})
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "token", server.Client())
	if _, err := client.Poll(context.Background(), PollRequest{}); err == nil {
		t.Fatal("Poll accepted more than one job")
	}
}
