package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/credentialref"
)

// Explicit live acceptance consumes one operator-dispatched Windows task in a
// dedicated pool. It probes the production broker, then reports a test failure
// without connecting to the target or publishing a CI. Normal CI skips it.
func TestRealWindowsManagedBrokerAuthority(t *testing.T) {
	instance := os.Getenv("TOPO_REAL_MANAGED_INSTANCE")
	if instance == "" {
		t.Skip("real managed instance not explicitly configured")
	}
	originHash := sha256.Sum256([]byte(instance))
	if hex.EncodeToString(originHash[:]) != os.Getenv("TOPO_REAL_MANAGED_ORIGIN_SHA256") {
		t.Fatal("acceptance origin is not approved")
	}
	token, err := credentialref.Resolve(os.Getenv("TOPO_REAL_MANAGED_TOKEN_REF"))
	if err != nil {
		t.Fatal("worker token reference unavailable")
	}
	startup, err := LoadWinRMStartupConfig(os.Getenv("TOPO_REAL_MANAGED_ALLOWLIST"), os.Getenv("TOPO_REAL_MANAGED_CA_CERTS"))
	if err != nil {
		t.Fatal("local Windows authority unavailable")
	}
	policy := Policy{WorkerPool: os.Getenv("TOPO_REAL_MANAGED_POOL"), SiteID: os.Getenv("TOPO_REAL_MANAGED_SITE"), AllowWinRMWindows: true, WinRMAllowlist: startup.Allowlist, WinRMTrustDigest: startup.TrustDigest, MaxConcurrency: 1}
	digest, err := policy.Digest()
	if err != nil {
		t.Fatal("acceptance policy invalid")
	}
	client, err := NewClient(instance, string(token), nil)
	if err != nil {
		t.Fatal("acceptance client invalid")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	// The same worker token must not have generic user or credential-table access.
	// Only status is inspected; arbitrary response bodies never enter diagnostics.
	for _, table := range []string{"sys_user", "x_664635_topo_ssh_credential"} {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, instance+"/api/now/table/"+table+"?sysparm_limit=1&sysparm_fields=sys_id", nil)
		if err != nil {
			t.Fatal("boundary request construction failed")
		}
		request.Header.Set("Authorization", "Bearer "+string(token))
		response, err := client.httpClient.Do(request)
		if err != nil {
			t.Fatal("live worker Table API boundary request failed")
		}
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusForbidden {
			t.Fatalf("worker generic %s Table API access was not denied", table)
		}
	}
	boot, err := randomID()
	if err != nil {
		t.Fatal("boot identity unavailable")
	}
	reg, err := client.Register(ctx, RegisterRequest{SchemaVersion: ContractVersion, BootID: boot, WorkerPool: policy.WorkerPool, SiteID: policy.SiteID, Version: "windows-broker-acceptance", Capabilities: policy.Capabilities(), PolicyDigest: digest, MaxConcurrency: 1, StartedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal("live worker registration failed")
	}
	t.Log("registered; waiting for one operator-dispatched Windows task")
	var task *Task
	for task == nil {
		claim, claimErr := client.Claim(ctx, ClaimRequest{SchemaVersion: ContractVersion, WorkerID: reg.WorkerID, BootID: boot, Capabilities: policy.Capabilities()})
		if claimErr != nil {
			t.Fatal("live claim failed")
		}
		task = claim.Task
		if task == nil {
			select {
			case <-ctx.Done():
				t.Fatal("operator dispatch wait expired")
			case <-time.After(time.Second):
			}
		}
	}
	request := CredentialRequest{SchemaVersion: ContractVersion, WorkerID: reg.WorkerID, BootID: boot, AttemptID: task.AttemptID, LeaseToken: task.LeaseToken}
	completion := CompleteRequest{SchemaVersion: ContractVersion, WorkerID: reg.WorkerID, BootID: boot, AttemptID: task.AttemptID, LeaseToken: task.LeaseToken, Failure: &Failure{Code: "acceptance_probe", Message: "broker-only acceptance probe; no target connection or result"}}
	completed := false
	defer func() {
		if !completed {
			cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
			defer stop()
			if _, cleanupErr := client.Complete(cleanup, task.TaskID, completion); cleanupErr != nil {
				t.Error("probe cleanup failed; inspect task lease expiry")
			}
		}
	}()
	if task.Operation != OperationWinRMWindowsV1 || task.TargetPartition == nil || len(task.TargetPartition.CIDRs) != 1 {
		t.Fatal("unexpected live task operation")
	}
	prefix, err := netip.ParsePrefix(task.TargetPartition.CIDRs[0])
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 || !targetAllowed(prefix.Addr(), policy.WinRMAllowlist) {
		t.Fatal("live task target is outside local Windows authority")
	}
	// Local denial must precede credentials and connections.
	denied := Executor{Policy: Policy{WorkerPool: policy.WorkerPool, SiteID: policy.SiteID, AllowLocal: true}}
	source := &testCredentialSource{}
	if _, err := denied.ExecuteWithCredentials(ctx, *task, source); err == nil || source.calls != 0 {
		t.Fatal("local operation denial accessed credentials")
	}
	for _, field := range []string{"worker", "boot", "attempt", "token"} {
		bad := request
		switch field {
		case "worker":
			bad.WorkerID = "unregistered-worker"
		case "boot":
			bad.BootID = "wrong-boot"
		case "attempt":
			bad.AttemptID = "wrong-attempt"
		case "token":
			bad.LeaseToken = "wrong-token"
		}
		_, err := client.Credential(ctx, task.TaskID, bad)
		var status *HTTPError
		if !errors.As(err, &status) || status.StatusCode != 403 {
			t.Fatalf("live %s mismatch did not return 403", field)
		}
		if strings.Contains(err.Error(), string(token)) || strings.Contains(err.Error(), task.LeaseToken) {
			t.Fatal("secret leaked in live denial")
		}
	}
	credential, err := client.Credential(ctx, task.TaskID, request)
	if err != nil || credential.Username == "" || credential.Password == "" {
		t.Fatal("live attempt-bound Password2 resolution failed")
	}
	// Client.Credential enforces no-store and bounded response decoding.
	t.Logf("task %s: four live identity/attempt denials, successful no-store broker response; no target connection/IRE", task.TaskID)
	result, err := client.Complete(ctx, task.TaskID, completion)
	if err != nil || result.TaskState != "failed" {
		t.Fatal("live probe completion failed")
	}
	completed = true
	_, err = client.Credential(ctx, task.TaskID, request)
	var status *HTTPError
	if !errors.As(err, &status) || status.StatusCode != 409 {
		t.Fatal("terminal live task retained credential authority")
	}
}
