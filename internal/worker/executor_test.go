package worker

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

type testCredentialSource struct {
	credential SSHCredential
	err        error
	calls      int
}

func (s *testCredentialSource) SSH(context.Context) (SSHCredential, error) {
	s.calls++
	return s.credential, s.err
}

func TestSSHExecutorEnforcesLocalAllowlistBeforeCredentialResolution(t *testing.T) {
	t.Parallel()
	policy := testSSHPolicy(netip.MustParsePrefix("192.0.2.0/24"))
	source := &testCredentialSource{credential: SSHCredential{Username: "topo", Password: "secret"}}
	dialed := 0
	executor := Executor{
		Policy:             policy,
		SSHHostKeyCallback: func(string, net.Addr, ssh.PublicKey) error { return nil },
		SSHDialContext: func(context.Context, string, string) (net.Conn, error) {
			dialed++
			return nil, errors.New("offline")
		},
	}
	task := testSSHTask("198.51.100.7/32")
	if _, err := executor.ExecuteWithCredentials(t.Context(), task, source); err == nil || !strings.Contains(err.Error(), "outside the local allowlist") {
		t.Fatalf("error = %v", err)
	}
	if source.calls != 0 || dialed != 0 {
		t.Fatalf("credential calls=%d dial calls=%d", source.calls, dialed)
	}
}

func TestSSHExecutorResolvesOneCredentialAndReturnsBoundedNoDataObservation(t *testing.T) {
	t.Parallel()
	policy := testSSHPolicy(netip.MustParsePrefix("192.0.2.0/24"))
	source := &testCredentialSource{credential: SSHCredential{Username: "topo", Password: "secret"}}
	dialedAddress := ""
	executor := Executor{
		Policy:             policy,
		SSHHostKeyCallback: func(string, net.Addr, ssh.PublicKey) error { return nil },
		SSHDialContext: func(_ context.Context, _, address string) (net.Conn, error) {
			dialedAddress = address
			return nil, errors.New("offline")
		},
	}
	observation, err := executor.ExecuteWithCredentials(t.Context(), testSSHTask("192.0.2.7/32"), source)
	if err != nil {
		t.Fatal(err)
	}
	if source.calls != 1 || dialedAddress != "192.0.2.7:22" {
		t.Fatalf("credential calls=%d dialed=%q", source.calls, dialedAddress)
	}
	if observation.Plugin != "ssh-linux" || len(observation.Assets) != 0 || len(observation.Errors) != 1 {
		t.Fatalf("observation = %#v", observation)
	}
}

func TestSSHExecutorCredentialFailureIsRedacted(t *testing.T) {
	t.Parallel()
	const secret = "password-from-provider"
	source := &testCredentialSource{err: errors.New(secret)}
	executor := Executor{Policy: testSSHPolicy(netip.MustParsePrefix("192.0.2.0/24")), SSHHostKeyCallback: func(string, net.Addr, ssh.PublicKey) error { return nil }}
	_, err := executor.ExecuteWithCredentials(t.Context(), testSSHTask("192.0.2.7/32"), source)
	if !errors.Is(err, ErrCredentialResolution) || strings.Contains(err.Error(), secret) {
		t.Fatalf("error = %v", err)
	}
}

func testSSHPolicy(prefix netip.Prefix) Policy {
	return Policy{WorkerPool: "pool-a", SiteID: "site-a", AllowSSHLinux: true, SSHAllowlist: []netip.Prefix{prefix}, SSHHostKeyDigest: strings.Repeat("a", 64)}
}

func testSSHTask(cidr string) Task {
	return Task{
		TaskID: "task-1", RunID: "run-1", AttemptID: "attempt-1", LeaseToken: "token-1",
		LeaseExpiresAt: time.Now().Add(time.Minute), Operation: OperationSSHLinuxV1,
		ProfileID: "ssh-profile", ProfileRevision: 1, CredentialBindingID: "binding-1",
		TargetPartition: &TargetPartition{Key: strings.Repeat("a", 64), Ordinal: 0, Count: 1, CIDRs: []string{cidr}},
		Deadline:        time.Now().Add(2 * time.Minute),
	}
}
