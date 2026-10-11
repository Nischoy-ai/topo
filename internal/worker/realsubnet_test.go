package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/pkg/credentialref"
	"github.com/Nischoy-ai/topo/pkg/model"
)

// This opt-in observer runs the production loop and executor against one
// operator-dispatched three-target lab run. It never changes target settings,
// credentials, task authority or discovery operations. Normal CI skips it.
func TestRealManagedSubnet(t *testing.T) {
	instance := os.Getenv("TOPO_REAL_SUBNET_INSTANCE")
	if instance == "" {
		t.Skip("real subnet acceptance not configured")
	}
	origin := sha256.Sum256([]byte(instance))
	if hex.EncodeToString(origin[:]) != os.Getenv("TOPO_REAL_SUBNET_ORIGIN_SHA256") {
		t.Fatal("origin not approved")
	}
	mode := os.Getenv("TOPO_REAL_SUBNET_MODE")
	if mode != "collect" && mode != "local_deny" && mode != "cancel" {
		t.Fatal("reviewed acceptance mode required")
	}
	token, err := credentialref.Resolve(os.Getenv("TOPO_REAL_SUBNET_TOKEN_REF"))
	if err != nil {
		t.Fatal("worker token reference unavailable")
	}
	startup, err := LoadSSHStartupConfig(os.Getenv("TOPO_REAL_SUBNET_ALLOWLIST"), os.Getenv("TOPO_REAL_SUBNET_KNOWN_HOSTS"))
	if err != nil {
		t.Fatal("local SSH authority unavailable")
	}
	policy := Policy{WorkerPool: "subnet-managed-pilot", SiteID: "azure-subnet-lab", AllowSSHLinux: true, SSHAllowlist: startup.Allowlist, SSHHostKeyDigest: startup.KnownHostsDigest, MaxConcurrency: 2, MaxTaskDuration: 20 * time.Second, RemoteStartInterval: 3 * time.Second}
	if mode == "cancel" {
		policy.RemoteStartInterval = 30 * time.Second
	}
	if err := policy.Validate(); err != nil {
		t.Fatal("policy invalid")
	}
	client, err := NewClient(instance, string(token), nil)
	if err != nil {
		t.Fatal("client invalid")
	}
	ctx, stop := context.WithTimeout(t.Context(), 90*time.Second)
	defer stop()
	evidence := &subnetLiveEvidence{credentials: map[string]int{}, targets: map[string]int{}}
	control := &subnetLiveControl{ControlPlane: client, evidence: evidence, stop: stop}
	executor := subnetLiveExecutor{Executor: Executor{Policy: policy, SSHHostKeyCallback: startup.HostKeyCallback}, evidence: evidence}
	executor.SSHDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		evidence.mu.Lock()
		evidence.dials = append(evidence.dials, address)
		evidence.mu.Unlock()
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	err = Run(ctx, RunConfig{Policy: policy, Version: "subnet-live-acceptance", PollInterval: time.Second, Control: control, Executor: executor})
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal("managed run failed or timed out; inspect terminal tasks")
	}
	evidence.mu.Lock()
	defer evidence.mu.Unlock()
	if mode == "cancel" {
		if evidence.cancelled == 0 || evidence.cancelResponses == 0 || evidence.active != 0 || evidence.maximum > 2 {
			t.Fatal("native cancellation did not stop the production worker")
		}
		if len(evidence.dials) > 1 || evidence.credentials["10.42.1.21/32"] != 0 || evidence.credentials["10.42.1.22/32"] != 0 {
			t.Fatal("cancelled paced tasks reached credentials or network")
		}
		t.Logf("native cancellation: renew_cancelled=%d terminal_cancelled=%d starts=%d dials=%d active=%d", evidence.cancelResponses, evidence.cancelled, len(evidence.starts), len(evidence.dials), evidence.active)
		return
	}
	if evidence.completed != 3 || len(evidence.targets) != 3 {
		t.Fatal("expected exactly three terminal lab partitions")
	}
	for _, target := range []string{"10.42.1.20/32", "10.42.1.21/32", "10.42.1.22/32"} {
		if evidence.targets[target] != 1 {
			t.Fatal("overlap/exclusion target set mismatch")
		}
	}
	if evidence.maximum > 2 || evidence.active != 0 {
		t.Fatal("local concurrency bound violated")
	}
	sort.Slice(evidence.starts, func(i, j int) bool { return evidence.starts[i].Before(evidence.starts[j]) })
	for i := 1; i < len(evidence.starts); i++ {
		if evidence.starts[i].Sub(evidence.starts[i-1]) < 3*time.Second-10*time.Millisecond {
			t.Fatal("remote start pacing violated")
		}
	}
	if mode == "local_deny" {
		if evidence.credentials["10.42.1.21/32"] != 0 || evidence.credentials["10.42.1.22/32"] != 0 {
			t.Fatal("local denial resolved credentials")
		}
		for _, address := range evidence.dials {
			if address != "10.42.1.20:22" {
				t.Fatal("local denial dialed unauthorized target")
			}
		}
	} else if len(evidence.dials) != 3 {
		t.Fatal("expected one connection per selected target")
	}
	safe, _ := json.Marshal(struct {
		Completed, Maximum   int
		Targets, Credentials map[string]int
		Starts               []time.Time
		Dials                []string
	}{evidence.completed, evidence.maximum, evidence.targets, evidence.credentials, evidence.starts, evidence.dials})
	t.Log(string(safe))
}

type subnetLiveEvidence struct {
	mu                         sync.Mutex
	active, maximum, completed int
	cancelled, cancelResponses int
	starts                     []time.Time
	dials                      []string
	targets, credentials       map[string]int
}
type subnetLiveControl struct {
	ControlPlane
	evidence *subnetLiveEvidence
	stop     context.CancelFunc
}

func (c *subnetLiveControl) Complete(ctx context.Context, id string, r CompleteRequest) (CompleteResponse, error) {
	result, err := c.ControlPlane.Complete(ctx, id, r)
	if err == nil {
		c.evidence.mu.Lock()
		c.evidence.completed++
		if result.TaskState == "cancelled" {
			c.evidence.cancelled++
		}
		finished := c.evidence.completed == 3 || c.evidence.cancelled > 0
		c.evidence.mu.Unlock()
		if finished {
			c.stop()
		}
	}
	return result, err
}

type subnetLiveExecutor struct {
	Executor
	evidence *subnetLiveEvidence
}

func (e subnetLiveExecutor) ExecuteWithCredentials(ctx context.Context, task Task, source CredentialSource) (model.ObservationEnvelope, error) {
	if task.Operation != OperationSSHLinuxV1 || task.TargetPartition == nil || len(task.TargetPartition.CIDRs) != 1 {
		return model.ObservationEnvelope{}, errors.New("unexpected subnet acceptance task")
	}
	target := task.TargetPartition.CIDRs[0]
	if target != "10.42.1.20/32" && target != "10.42.1.21/32" && target != "10.42.1.22/32" {
		return model.ObservationEnvelope{}, errors.New("target outside reviewed three-address acceptance scope")
	}
	e.evidence.mu.Lock()
	e.evidence.active++
	if e.evidence.active > e.evidence.maximum {
		e.evidence.maximum = e.evidence.active
	}
	e.evidence.starts = append(e.evidence.starts, time.Now().UTC())
	e.evidence.targets[target]++
	e.evidence.mu.Unlock()
	defer func() { e.evidence.mu.Lock(); e.evidence.active--; e.evidence.mu.Unlock() }()
	return e.Executor.ExecuteWithCredentials(ctx, task, subnetLiveCredential{source, e.evidence, target})
}

type subnetLiveCredential struct {
	CredentialSource
	evidence *subnetLiveEvidence
	target   string
}

func (c subnetLiveCredential) Password(ctx context.Context) (SSHCredential, error) {
	c.evidence.mu.Lock()
	c.evidence.credentials[c.target]++
	c.evidence.mu.Unlock()
	return c.CredentialSource.Password(ctx)
}

func (c *subnetLiveControl) Renew(ctx context.Context, id string, r RenewRequest) (RenewResponse, error) {
	response, err := c.ControlPlane.Renew(ctx, id, r)
	if err == nil && response.Cancelled {
		c.evidence.mu.Lock()
		c.evidence.cancelResponses++
		c.evidence.mu.Unlock()
	}
	return response, err
}

// Two independent worker boots claim concurrently from an operator-dispatched
// three-target run. The third claim must be empty at the configured two-lease
// pool ceiling. This probe never asks for credentials or connects to a target.
func TestRealSubnetPoolCapacity(t *testing.T) {
	instance := os.Getenv("TOPO_REAL_SUBNET_INSTANCE")
	if instance == "" {
		t.Skip("real subnet acceptance not configured")
	}
	origin := sha256.Sum256([]byte(instance))
	if hex.EncodeToString(origin[:]) != os.Getenv("TOPO_REAL_SUBNET_ORIGIN_SHA256") {
		t.Fatal("origin not approved")
	}
	token, err := credentialref.Resolve(os.Getenv("TOPO_REAL_SUBNET_TOKEN_REF"))
	if err != nil {
		t.Fatal("worker token reference unavailable")
	}
	startup, err := LoadSSHStartupConfig(os.Getenv("TOPO_REAL_SUBNET_ALLOWLIST"), os.Getenv("TOPO_REAL_SUBNET_KNOWN_HOSTS"))
	if err != nil {
		t.Fatal("local SSH authority unavailable")
	}
	policy := Policy{WorkerPool: "subnet-managed-pilot", SiteID: "azure-subnet-lab", AllowSSHLinux: true, SSHAllowlist: startup.Allowlist, SSHHostKeyDigest: startup.KnownHostsDigest, MaxConcurrency: 2}
	digest, err := policy.Digest()
	if err != nil {
		t.Fatal("policy invalid")
	}
	client, err := NewClient(instance, string(token), nil)
	if err != nil {
		t.Fatal("client invalid")
	}
	ctx, stop := context.WithTimeout(t.Context(), 90*time.Second)
	defer stop()
	var registrations [2]RegisterResponse
	var boots [2]string
	for i := range 2 {
		boots[i], err = randomID()
		if err != nil {
			t.Fatal("boot unavailable")
		}
		registrations[i], err = client.Register(ctx, RegisterRequest{SchemaVersion: ContractVersion, BootID: boots[i], WorkerPool: policy.WorkerPool, SiteID: policy.SiteID, Version: "subnet-pool-capacity-acceptance", Capabilities: policy.Capabilities(), PolicyDigest: digest, MaxConcurrency: 2, StartedAt: time.Now().UTC()})
		if err != nil {
			t.Fatal("registration failed")
		}
	}
	requests := [2]ClaimRequest{}
	for i := range 2 {
		requests[i] = ClaimRequest{SchemaVersion: ContractVersion, WorkerID: registrations[i].WorkerID, BootID: boots[i], Capabilities: policy.Capabilities()}
	}
	var tasks [2]*Task
	var claimErrors [2]error
	for tasks[0] == nil && tasks[1] == nil {
		var waiting sync.WaitGroup
		for i := range 2 {
			waiting.Go(func() {
				response, err := client.Claim(ctx, requests[i])
				tasks[i] = response.Task
				claimErrors[i] = err
			})
		}
		waiting.Wait()
		if claimErrors[0] != nil || claimErrors[1] != nil {
			t.Fatal("concurrent live claim failed")
		}
		if tasks[0] == nil && tasks[1] == nil {
			select {
			case <-ctx.Done():
				t.Fatal("operator dispatch wait expired")
			case <-time.After(time.Second):
			}
		}
	}
	defer func() {
		for i, task := range tasks {
			if task != nil {
				cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				_, err := client.Complete(cleanup, task.TaskID, CompleteRequest{SchemaVersion: ContractVersion, WorkerID: registrations[i].WorkerID, BootID: boots[i], AttemptID: task.AttemptID, LeaseToken: task.LeaseToken, Failure: &Failure{Code: "acceptance_probe", Message: "pool capacity probe; no credential or connection"}})
				cancel()
				if err != nil {
					t.Error("pool probe cleanup failed; inspect lease expiry")
				}
			}
		}
	}()
	if tasks[0] == nil || tasks[1] == nil || tasks[0].TaskID == tasks[1].TaskID {
		t.Fatal("two concurrent boots did not receive distinct bounded leases")
	}
	for _, task := range tasks {
		if task.Operation != OperationSSHLinuxV1 || task.TargetPartition == nil || len(task.TargetPartition.CIDRs) != 1 {
			t.Fatal("unexpected probe task")
		}
	}
	third, err := client.Claim(ctx, requests[0])
	if err != nil || third.Task != nil {
		t.Fatal("two-lease pool ceiling did not reject the third claim")
	}
	t.Logf("two distinct concurrent worker leases: tasks=%s,%s; third claim empty; no credential or target connection", tasks[0].TaskID, tasks[1].TaskID)
}
