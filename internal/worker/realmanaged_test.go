package worker

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/credentialref"
	"github.com/Nischoy-ai/topo/pkg/discovery"
	"github.com/Nischoy-ai/topo/pkg/discovery/winrm"
	"github.com/Nischoy-ai/topo/pkg/model"
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
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
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
	if os.Getenv("TOPO_REAL_MANAGED_LEASE_FAULT") == "native_binding" {
		// Operator-only fault injection into this disposable leased task. The
		// worker request and stored Password2 credential remain unchanged.
		for _, field := range []string{"protocol", "profile", "revision", "scope"} {
			t.Logf("task %s run %s: inject native %s mismatch", task.TaskID, task.RunID, field)
			for _, wantDenied := range []bool{true, false} {
				for {
					value, callErr := client.Credential(ctx, task.TaskID, request)
					var status *HTTPError
					denied := errors.As(callErr, &status) && status.StatusCode == 409
					valid := callErr == nil && value.Username == credential.Username && value.Password == credential.Password
					if callErr != nil && !denied {
						t.Fatal("unexpected native binding broker status")
					}
					if (wantDenied && denied) || (!wantDenied && valid) {
						break
					}
					if _, renewErr := client.Renew(ctx, task.TaskID, RenewRequest(request)); renewErr != nil {
						t.Fatal("native binding probe renewal failed")
					}
					select {
					case <-ctx.Done():
						t.Fatal("native binding injection wait expired")
					case <-time.After(2 * time.Second):
					}
				}
				if wantDenied {
					t.Logf("native %s mismatch denied with 409; restore original task field", field)
				}
			}
			t.Logf("native %s restored; matching broker resolution succeeded", field)
		}
		if _, completeErr := client.Complete(ctx, task.TaskID, completion); completeErr != nil {
			t.Fatal("native binding probe cleanup failed")
		}
		completed = true
		t.Log("four isolated live binding mismatches denied; restored task completed without target collection or IRE")
		return
	}
	leaseFault := os.Getenv("TOPO_REAL_MANAGED_LEASE_FAULT")
	if leaseFault == "cancel" || leaseFault == "expire" || leaseFault == "cancel_expire" {
		t.Logf("task %s run %s: valid credential resolved; waiting for %s", task.TaskID, task.RunID, leaseFault)
		if leaseFault == "expire" {
			timer := time.NewTimer(time.Until(task.LeaseExpiresAt) + time.Second)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				t.Fatal("lease wait expired")
			case <-timer.C:
			}
		} else {
			for {
				_, pollErr := client.Credential(ctx, task.TaskID, request)
				var status *HTTPError
				if errors.As(pollErr, &status) && status.StatusCode == 409 {
					break
				}
				if pollErr != nil {
					t.Fatal("unexpected cancellation broker status")
				}
				select {
				case <-ctx.Done():
					t.Fatal("operator cancellation wait expired")
				case <-time.After(time.Second):
				}
			}
		}
		if leaseFault == "cancel_expire" {
			timer := time.NewTimer(time.Until(task.LeaseExpiresAt) + time.Second)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				t.Fatal("cancelled lease expiry wait expired")
			case <-timer.C:
			}
		}
		_, deniedErr := client.Credential(ctx, task.TaskID, request)
		var deniedStatus *HTTPError
		if !errors.As(deniedErr, &deniedStatus) || deniedStatus.StatusCode != 409 {
			t.Fatal("revoked lease retained credential authority")
		}
		if leaseFault == "cancel" {
			renewed, renewErr := client.Renew(ctx, task.TaskID, RenewRequest(request))
			if renewErr != nil || !renewed.Cancelled {
				t.Fatal("cancelled lease renewal did not signal cancellation")
			}
			completion.Failure = &Failure{Code: "cancelled", Message: "task was cancelled by operator"}
			result, completeErr := client.Complete(ctx, task.TaskID, completion)
			if completeErr != nil || result.TaskState != "cancelled" {
				t.Fatal("cancelled lease did not terminate")
			}
		} else {
			_, renewErr := client.Renew(ctx, task.TaskID, RenewRequest(request))
			var status *HTTPError
			if !errors.As(renewErr, &status) || status.StatusCode != 409 {
				t.Fatal("expired lease renewed")
			}
			completed = true // Expiry revoked the old completion authority.
			claim, claimErr := client.Claim(ctx, ClaimRequest{SchemaVersion: ContractVersion, WorkerID: reg.WorkerID, BootID: boot, Capabilities: policy.Capabilities()})
			if leaseFault == "cancel_expire" {
				if claimErr != nil || claim.Task != nil {
					t.Fatal("expired cancelled task retained or regained dispatch authority")
				}
				t.Log("cancelled lease expired; maintenance returned no cancelled task; inspect cleared native lease fields")
				return
			}
			if claimErr != nil || claim.Task == nil || claim.Task.TaskID != task.TaskID || claim.Task.AttemptID == task.AttemptID {
				t.Fatal("expired task did not recover with a new attempt")
			}
			fresh := *claim.Task
			completion.AttemptID = fresh.AttemptID
			completion.LeaseToken = fresh.LeaseToken
			if _, cleanupErr := client.Complete(ctx, fresh.TaskID, completion); cleanupErr != nil {
				t.Fatal("recovered attempt cleanup failed")
			}
		}
		completed = true
		t.Logf("live %s: broker authority revoked; attempt terminated or recovered with a new identity", leaseFault)
		return
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

// This opt-in test consumes operator-dispatched /32 tasks. Faults are confined
// to memory after the real broker resolves credentials; no target settings or
// stored credentials are changed. Production result and completion APIs are used.
func TestRealWindowsManagedCollectionFailures(t *testing.T) {
	instance := os.Getenv("TOPO_REAL_MANAGED_INSTANCE")
	if instance == "" {
		t.Skip("real managed instance not explicitly configured")
	}
	originHash := sha256.Sum256([]byte(instance))
	if hex.EncodeToString(originHash[:]) != os.Getenv("TOPO_REAL_MANAGED_ORIGIN_SHA256") {
		t.Fatal("acceptance origin is not approved")
	}
	mode := os.Getenv("TOPO_REAL_MANAGED_FAULT")
	if mode != "wrong_password" && mode != "untrusted_certificate" && mode != "deadline" && mode != "wrong_identity" && mode != "permission_denied" && mode != "network_timeout" && mode != "response_bound" {
		t.Fatal("reviewed live fault is required")
	}
	token, err := credentialref.Resolve(os.Getenv("TOPO_REAL_MANAGED_TOKEN_REF"))
	if err != nil {
		t.Fatal("token reference unavailable")
	}
	startup, err := LoadWinRMStartupConfig(os.Getenv("TOPO_REAL_MANAGED_ALLOWLIST"), os.Getenv("TOPO_REAL_MANAGED_CA_CERTS"))
	if err != nil {
		t.Fatal("local authority unavailable")
	}
	policy := Policy{WorkerPool: os.Getenv("TOPO_REAL_MANAGED_POOL"), SiteID: os.Getenv("TOPO_REAL_MANAGED_SITE"), AllowWinRMWindows: true, WinRMAllowlist: startup.Allowlist, WinRMTrustDigest: startup.TrustDigest, MaxConcurrency: 1}
	digest, err := policy.Digest()
	if err != nil {
		t.Fatal("policy invalid")
	}
	client, err := NewClient(instance, string(token), nil)
	if err != nil {
		t.Fatal("client invalid")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	boot, err := randomID()
	if err != nil {
		t.Fatal("boot unavailable")
	}
	reg, err := client.Register(ctx, RegisterRequest{SchemaVersion: ContractVersion, BootID: boot, WorkerPool: policy.WorkerPool, SiteID: policy.SiteID, Version: "windows-live-fault-acceptance", Capabilities: policy.Capabilities(), PolicyDigest: digest, MaxConcurrency: 1, StartedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal("registration failed")
	}
	for range 2 {
		var task *Task
		for task == nil {
			claim, err := client.Claim(ctx, ClaimRequest{SchemaVersion: ContractVersion, WorkerID: reg.WorkerID, BootID: boot, Capabilities: policy.Capabilities()})
			if err != nil {
				t.Fatal("claim failed")
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
		complete := CompleteRequest{SchemaVersion: ContractVersion, WorkerID: reg.WorkerID, BootID: boot, AttemptID: task.AttemptID, LeaseToken: task.LeaseToken, Failure: &Failure{Code: "acceptance_cleanup", Message: "live negative probe cleanup"}}
		if task.Operation != OperationWinRMWindowsV1 || task.TargetPartition == nil || len(task.TargetPartition.CIDRs) != 1 {
			t.Fatal("unexpected task")
		}
		prefix, err := netip.ParsePrefix(task.TargetPartition.CIDRs[0])
		if err != nil || prefix.Bits() != 32 || !targetAllowed(prefix.Addr(), policy.WinRMAllowlist) {
			t.Fatal("task outside local authority")
		}
		credential, err := client.Credential(ctx, task.TaskID, CredentialRequest{SchemaVersion: ContractVersion, WorkerID: reg.WorkerID, BootID: boot, AttemptID: task.AttemptID, LeaseToken: task.LeaseToken})
		if err != nil {
			_, _ = client.Complete(ctx, task.TaskID, complete)
			t.Fatal("broker failed")
		}
		if mode == "wrong_identity" {
			connection, dialErr := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(prefix.Addr().String(), "5986"))
			if dialErr != nil {
				t.Fatal("real target unavailable for identity verification")
			}
			tlsConnection := tls.Client(connection, &tls.Config{RootCAs: startup.RootCAs, MinVersion: tls.VersionTLS12, ServerName: "acceptance-identity.invalid"})
			handshakeCtx, stop := context.WithTimeout(ctx, 10*time.Second)
			handshakeErr := tlsConnection.HandshakeContext(handshakeCtx)
			stop()
			tlsConnection.Close()
			var hostnameError x509.HostnameError
			if !errors.As(handshakeErr, &hostnameError) {
				t.Fatal("wrong server identity did not fail certificate verification")
			}
			complete.Failure = &Failure{Code: "operation_failed", Message: "server certificate identity mismatch"}
			result, completeErr := client.Complete(ctx, task.TaskID, complete)
			if completeErr != nil || result.TaskState != "failed" {
				t.Fatal("identity failure did not terminate managed task")
			}
			t.Logf("task %s: wrong server identity rejected by real TLS handshake before NTLM; no result or IRE", task.TaskID)
			continue
		}
		if mode == "permission_denied" && credential.Username != "topo-wmi-denied" {
			t.Fatal("dedicated denial credential was not returned by broker")
		}
		realPassword := credential.Password
		if mode == "wrong_password" {
			credential.Password = "deliberately-invalid-acceptance-password"
		}
		roots := startup.RootCAs
		if mode == "untrusted_certificate" {
			roots = x509.NewCertPool()
		}
		source := &testCredentialSource{credential: credential}
		collectCtx, stop := context.WithCancel(ctx)
		if mode == "deadline" {
			stop()
			collectCtx, stop = context.WithTimeout(ctx, time.Nanosecond)
		}
		started := time.Now()
		var observation model.ObservationEnvelope
		var collectErr error
		if mode == "response_bound" {
			// The real verified target response is read by the same production
			// plugin with a deliberately smaller local test budget. No task may
			// select this budget or supply an operation.
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
			transport.DialContext = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
			transport.TLSHandshakeTimeout = 10 * time.Second
			plugin := winrm.Plugin{Config: winrm.Config{Username: credential.Username, Password: credential.Password, AuthMode: winrm.AuthModeNTLM, HostInterfacesOnly: true, Concurrency: 1, OperationTimeout: 10 * time.Second, MaxResponseBytes: 64, HTTPClient: &http.Client{Transport: transport}}}
			observation, collectErr = plugin.Discover(collectCtx, discovery.Request{JobID: task.TaskID, SiteID: policy.SiteID, CollectorID: "worker-pool-" + policy.WorkerPool, Targets: []string{"https://" + net.JoinHostPort(prefix.Addr().String(), "5986") + "/wsman"}})
			transport.CloseIdleConnections()
		} else {
			observation, collectErr = (Executor{Policy: policy, WinRMRootCAs: roots}).ExecuteWithCredentials(collectCtx, *task, source)
		}
		stop()
		if mode == "deadline" {
			if !errors.Is(collectErr, context.DeadlineExceeded) || time.Since(started) > time.Second {
				t.Fatal("local deadline did not bound execution")
			}
			complete.Failure = &Failure{Code: "operation_failed", Message: "context deadline exceeded"}
			result, completeErr := client.Complete(ctx, task.TaskID, complete)
			if completeErr != nil || result.TaskState != "failed" {
				t.Fatal("deadline failure did not terminate managed task")
			}
			t.Logf("task %s: deadline; bounded cancellation, managed failure, no observation or IRE", task.TaskID)
			continue
		}
		if collectErr != nil {
			_, _ = client.Complete(ctx, task.TaskID, complete)
			t.Fatal("expected a bounded no-data observation")
		}
		if len(observation.Assets) != 0 || len(observation.Relationships) != 0 || len(observation.Errors) == 0 || time.Since(started) > 15*time.Second {
			_, _ = client.Complete(ctx, task.TaskID, complete)
			t.Fatal("fault did not fail closed within bound")
		}
		if mode == "permission_denied" {
			denied := false
			for _, failure := range observation.Errors {
				message := strings.ToLower(failure.Message)
				if strings.Contains(message, "http 401") {
					t.Fatal("permission test failed authentication")
				}
				denied = denied || strings.Contains(message, "access is denied") || strings.Contains(message, "access denied") || strings.Contains(message, "accessdenied") || strings.Contains(message, "0x80070005")
			}
			if !denied {
				t.Fatal("real WMI permission denial not established")
			}
		}
		payload, err := json.Marshal(observation)
		if err != nil {
			t.Fatal("observation encoding failed")
		}
		for _, secret := range []string{string(token), task.LeaseToken, realPassword, credential.Password} {
			if strings.Contains(string(payload), secret) {
				t.Fatal("secret leaked into observation")
			}
		}
		if mode == "network_timeout" || mode == "response_bound" {
			expected := "timeout"
			if mode == "response_bound" {
				expected = "exceeded 64 bytes"
			}
			matched := false
			for _, failure := range observation.Errors {
				matched = matched || strings.Contains(strings.ToLower(failure.Message), expected) || (mode == "network_timeout" && strings.Contains(strings.ToLower(failure.Message), "deadline exceeded"))
				t.Logf("sanitized bound error after %s: %s", time.Since(started).Round(time.Millisecond), failure.Message)
			}
			if !matched || (mode == "network_timeout" && time.Since(started) < 9*time.Second) {
				_, _ = client.Complete(ctx, task.TaskID, complete)
				t.Fatal("live bound failure not established")
			}
		}
		sum := sha256.Sum256(payload)
		ack, err := client.SubmitResult(ctx, task.TaskID, ResultChunkRequest{SchemaVersion: ContractVersion, WorkerID: reg.WorkerID, BootID: boot, AttemptID: task.AttemptID, LeaseToken: task.LeaseToken, ChunkNumber: 0, ChunkCount: 1, Checksum: hex.EncodeToString(sum[:]), ObservationJSON: string(payload)})
		if err != nil || !ack.Accepted {
			_, _ = client.Complete(ctx, task.TaskID, complete)
			t.Fatal("negative observation rejected")
		}
		complete.Success = true
		complete.ChunkCount = 1
		complete.Failure = nil
		result, err := client.Complete(ctx, task.TaskID, complete)
		if err != nil || result.TaskState != "complete" {
			t.Fatal("negative no-data completion failed")
		}
		t.Logf("task %s: %s; zero assets/relationships, %d sanitized errors, bounded execution, managed no-data completion", task.TaskID, mode, len(observation.Errors))
	}
}
