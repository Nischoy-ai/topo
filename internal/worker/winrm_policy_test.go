package worker

import (
	"context"
	"crypto/x509"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func testWinRMPolicy() Policy {
	return Policy{WorkerPool: "pool-a", SiteID: "site-a", AllowWinRMWindows: true, WinRMAllowlist: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, WinRMTrustDigest: strings.Repeat("a", 64)}
}

func TestWinRMPolicyAndExecutorRejectBeforeCredentialAccess(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Executor, *Task)
	}{
		{"outside", func(e *Executor, t *Task) { t.TargetPartition.CIDRs = []string{"198.51.100.1/32"} }},
		{"wide", func(e *Executor, t *Task) { t.TargetPartition.CIDRs = []string{"192.0.2.0/24"} }},
		{"disabled", func(e *Executor, t *Task) {
			e.Policy = Policy{WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true}
		}},
		{"no-trust", func(e *Executor, t *Task) { e.WinRMRootCAs = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &testCredentialSource{credential: SSHCredential{Username: `SERVER\topo-scan`, Password: "secret"}}
			task := testSSHTask("192.0.2.7/32")
			task.Operation = OperationWinRMWindowsV1
			executor := Executor{Policy: testWinRMPolicy(), WinRMRootCAs: x509.NewCertPool()}
			tc.edit(&executor, &task)
			if _, err := executor.ExecuteWithCredentials(t.Context(), task, source); err == nil {
				t.Fatal("accepted unsafe task")
			}
			if source.calls != 0 {
				t.Fatal("credential resolved before authorization")
			}
		})
	}
}

func TestWinRMCredentialErrorIsRedacted(t *testing.T) {
	source := &testCredentialSource{err: errors.New("secret-password")}
	task := testSSHTask("192.0.2.7/32")
	task.Operation = OperationWinRMWindowsV1
	executor := Executor{Policy: testWinRMPolicy(), WinRMRootCAs: x509.NewCertPool()}
	_, err := executor.ExecuteWithCredentials(t.Context(), task, source)
	if !errors.Is(err, ErrCredentialResolution) || strings.Contains(err.Error(), "secret-password") {
		t.Fatalf("error=%v", err)
	}
}

func TestRemoteStartLimiterSpacesStartsAndCancels(t *testing.T) {
	limiter := &remoteStartLimiter{interval: 40 * time.Millisecond}
	if err := limiter.wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := limiter.wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 35*time.Millisecond {
		t.Fatal("target starts burst")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := limiter.wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestWinRMPolicyDigestBindsTrustTargetsAndRate(t *testing.T) {
	policy := testWinRMPolicy()
	original, err := policy.Digest()
	if err != nil {
		t.Fatal(err)
	}
	for _, edit := range []func(*Policy){
		func(p *Policy) { p.WinRMTrustDigest = strings.Repeat("b", 64) },
		func(p *Policy) { p.WinRMAllowlist = []netip.Prefix{netip.MustParsePrefix("192.0.2.1/32")} },
		func(p *Policy) { p.RemoteStartInterval = 2 * time.Second },
	} {
		copy := policy
		edit(&copy)
		digest, err := copy.Digest()
		if err != nil || digest == original {
			t.Fatalf("digest=%s error=%v", digest, err)
		}
	}
}

func TestRemoteStartLimiterCancelsWhileQueued(t *testing.T) {
	limiter := &remoteStartLimiter{interval: time.Minute}
	if err := limiter.wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	active, cancelActive := context.WithCancel(t.Context())
	defer cancelActive()
	done := make(chan error, 1)
	go func() { done <- limiter.wait(active) }()
	queued, cancelQueued := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancelQueued()
	start := time.Now()
	if err := limiter.wait(queued); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued error=%v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("queued cancellation blocked")
	}
	cancelActive()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("active error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("active cancellation blocked")
	}
}
