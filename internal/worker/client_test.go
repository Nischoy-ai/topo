package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientUsesOnlyFixedAuthenticatedResources(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer worker-token" {
			t.Errorf("request = %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case registerPath:
			writeServiceNowResult(w, RegisterResponse{WorkerID: "worker-1"})
		case heartbeatPath:
			writeServiceNowResult(w, HeartbeatResponse{CancelAttemptIDs: []string{}})
		case claimPath:
			writeServiceNowResult(w, ClaimResponse{})
		case taskPathPrefix + "task-1/renew":
			writeServiceNowResult(w, RenewResponse{LeaseExpiresAt: time.Now().Add(time.Minute)})
		case taskPathPrefix + "task-1/credential":
			writeServiceNowResult(w, SSHCredential{Username: "topo", Password: "test-password"})
		case taskPathPrefix + "task-1/results":
			writeServiceNowResult(w, ResultChunkResponse{Accepted: true})
		case taskPathPrefix + "task-1/complete":
			writeServiceNowResult(w, CompleteResponse{TaskState: "complete", RunState: "complete"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "worker-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := client.Register(ctx, RegisterRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Heartbeat(ctx, HeartbeatRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Claim(ctx, ClaimRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Renew(ctx, "task-1", RenewRequest{}); err != nil {
		t.Fatal(err)
	}
	if credential, err := client.Credential(ctx, "task-1", CredentialRequest{}); err != nil || credential.Username != "topo" {
		t.Fatalf("credential = %#v, %v", credential, err)
	}
	if _, err := client.SubmitResult(ctx, "task-1", ResultChunkRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Complete(ctx, "task-1", CompleteRequest{}); err != nil {
		t.Fatal(err)
	}
}

func TestClientRejectsInjectedTaskAuthority(t *testing.T) {
	t.Parallel()
	for _, injected := range []string{`"command":"whoami"`, `"port":2222`, `"url":"https://target.invalid"`} {
		injected := injected
		t.Run(injected, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"result":{"task":{"task_id":"task-1","run_id":"run-1","attempt_id":"attempt-1","lease_token":"token-1","lease_expires_at":"2099-01-01T00:00:00Z","operation":"local.v1","profile_id":"local","profile_revision":1,"deadline":"2099-01-01T00:00:00Z",` + injected + `}}}`))
			}))
			defer server.Close()
			client, err := NewClient(server.URL, "worker-token", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Claim(context.Background(), ClaimRequest{}); err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestClientRejectsUnknownOperation(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeServiceNowResult(w, ClaimResponse{Task: &Task{TaskID: "task-1", RunID: "run-1", AttemptID: "attempt-1", LeaseToken: "token-1", LeaseExpiresAt: time.Now().Add(time.Minute), Operation: "shell.v1", ProfileID: "profile-1", ProfileRevision: 1, Deadline: time.Now().Add(time.Minute)}})
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "worker-token", server.Client())
	if _, err := client.Claim(context.Background(), ClaimRequest{}); err == nil || !strings.Contains(err.Error(), "unsupported operation") {
		t.Fatalf("error = %v", err)
	}
}

func TestClientAcceptsServiceNowIntegralGlideNumbers(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"task":{"task_id":"task-1","run_id":"run-1","attempt_id":"attempt-1","lease_token":"token-1","lease_expires_at":"2099-01-01T00:00:00Z","operation":"ssh_linux.v1","profile_id":"ssh-linux","profile_revision":1.0,"credential_binding_id":"binding-1","target_partition":{"key":"` + strings.Repeat("a", 64) + `","ordinal":0.0,"count":1.0,"cidrs":["127.0.0.1/32"]},"deadline":"2099-01-01T00:00:00Z"}}}`))
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "worker-token", server.Client())
	response, err := client.Claim(context.Background(), ClaimRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if response.Task == nil || response.Task.ProfileRevision != 1 || response.Task.TargetPartition == nil || response.Task.TargetPartition.Ordinal != 0 || response.Task.TargetPartition.Count != 1 {
		t.Fatalf("task = %#v", response.Task)
	}
}

func TestClientRejectsFractionalServiceNowGlideNumber(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"task":{"task_id":"task-1","run_id":"run-1","attempt_id":"attempt-1","lease_token":"token-1","lease_expires_at":"2099-01-01T00:00:00Z","operation":"ssh_linux.v1","profile_id":"ssh-linux","profile_revision":1.0,"credential_binding_id":"binding-1","target_partition":{"key":"` + strings.Repeat("a", 64) + `","ordinal":0.5,"count":1.0,"cidrs":["127.0.0.1/32"]},"deadline":"2099-01-01T00:00:00Z"}}}`))
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "worker-token", server.Client())
	if _, err := client.Claim(context.Background(), ClaimRequest{}); err == nil || !strings.Contains(err.Error(), "target_partition.ordinal must be an integer") {
		t.Fatalf("error = %v", err)
	}
}

func TestClientValidatesImmutableTargetPartition(t *testing.T) {
	t.Parallel()
	validTask := Task{TaskID: "task-1", RunID: "run-1", AttemptID: "attempt-1", LeaseToken: "token-1", LeaseExpiresAt: time.Now().Add(time.Minute), Operation: OperationSSHLinuxV1, ProfileID: "profile-1", ProfileRevision: 1, CredentialBindingID: "binding-1", Deadline: time.Now().Add(2 * time.Minute), TargetPartition: &TargetPartition{Key: strings.Repeat("a", 64), Ordinal: 0, Count: 1, CIDRs: []string{"192.0.2.1/32"}}}
	if err := validateTask(validTask); err != nil {
		t.Fatal(err)
	}
	tests := []*TargetPartition{
		{Key: "bad key", Ordinal: 0, Count: 1},
		{Key: strings.Repeat("a", 64), Ordinal: 1, Count: 1, CIDRs: []string{"192.0.2.0/24"}},
		{Key: strings.Repeat("a", 64), Ordinal: 0, Count: 1},
		{Key: strings.Repeat("a", 64), Ordinal: 0, Count: 1, CIDRs: []string{"192.0.2.1/24"}},
		{Key: strings.Repeat("a", 64), Ordinal: 0, Count: 1, CIDRs: []string{"not-a-cidr"}},
	}
	for _, partition := range tests {
		task := validTask
		task.TargetPartition = partition
		if err := validateTask(task); err == nil {
			t.Fatalf("partition %#v was accepted", partition)
		}
	}
	local := validTask
	local.Operation = OperationLocalV1
	local.CredentialBindingID = ""
	if err := validateTask(local); err != nil {
		t.Fatalf("simulator partition metadata was rejected: %v", err)
	}
	if _, err := (Executor{Policy: Policy{WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true}}).Execute(t.Context(), local); err == nil || !strings.Contains(err.Error(), "does not accept") {
		t.Fatalf("production local executor accepted remote partition: %v", err)
	}
}

func TestCredentialRequiresNoStoreAndNeverReflectsSecret(t *testing.T) {
	t.Parallel()
	const secret = "do-not-copy-this-secret"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/credential") {
			writeServiceNowResult(w, SSHCredential{Username: "topo", Password: secret})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "worker-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	credential, err := client.Credential(t.Context(), "task-1", CredentialRequest{})
	if err == nil || credential.Password != "" || strings.Contains(err.Error(), secret) {
		t.Fatalf("credential=%#v error=%v", credential, err)
	}
}

func TestClientRejectsUnknownServiceNowEnvelopeField(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"worker_id":"worker-1"},"unexpected":true}`))
	}))
	defer server.Close()
	client, _ := NewClient(server.URL, "worker-token", server.Client())
	if _, err := client.Register(context.Background(), RegisterRequest{}); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v", err)
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
	httpClient := redirect.Client()
	httpClient.CheckRedirect = nil
	client, err := NewClient(redirect.URL, "worker-token", httpClient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Register(context.Background(), RegisterRequest{}); err == nil {
		t.Fatal("redirect response was accepted")
	}
	if targetCalls.Load() != 0 {
		t.Fatalf("redirect target called %d times", targetCalls.Load())
	}
}

func TestNewClientValidatesOriginAndToken(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"http://example.com", "https://user@example.com", "https://example.com/path", "https://example.com?x=1", "https://example.com/#fragment"} {
		if _, err := NewClient(raw, "token", nil); err == nil {
			t.Errorf("NewClient(%q) succeeded", raw)
		}
	}
	for _, token := range []string{"", "has space", "line\nbreak"} {
		if _, err := NewClient("https://example.com", token, nil); err == nil {
			t.Errorf("NewClient accepted token %q", token)
		}
	}
}

func TestClientDoesNotCopyRemoteResponseIntoErrors(t *testing.T) {
	t.Parallel()
	const reflectedSecret = "lease-token-must-not-be-logged"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"` + reflectedSecret + `"}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "worker-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Register(context.Background(), RegisterRequest{})
	if err == nil || strings.Contains(err.Error(), reflectedSecret) {
		t.Fatalf("error leaked untrusted response content: %v", err)
	}
}

func writeServiceNowResult(w http.ResponseWriter, value any) {
	_ = json.NewEncoder(w).Encode(struct {
		Result any `json:"result"`
	}{Result: value})
}
