package controlsim_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/worker"
	"github.com/Nischoy-ai/topo/internal/worker/controlsim"
	"github.com/Nischoy-ai/topo/pkg/model"
	"golang.org/x/crypto/ssh"
)

const testToken = "simulator-worker-token"

func TestManualAndScheduledLocalRunsReconcileAndRetainSummaries(t *testing.T) {
	t.Parallel()
	sim := controlsim.New(controlsim.Config{Token: testToken, LeaseDuration: 10 * time.Second, SuccessRawTTL: time.Nanosecond})
	server := httptest.NewTLSServer(sim.Handler())
	defer server.Close()
	client, err := worker.NewClient(server.URL, testToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}

	manualID := sim.RunNow("pool-a")
	sim.UpsertSchedule(controlsim.Schedule{ID: "schedule-a", WorkerPool: "pool-a", Interval: time.Hour, NextRunAt: time.Now().Add(-time.Minute), Active: true})
	policy := worker.Policy{WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- worker.Run(ctx, worker.RunConfig{
			Policy:       policy,
			Version:      "test",
			PollInterval: time.Second,
			Control:      client,
			Executor:     worker.Executor{Policy: policy},
			Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
			BootID:       "boot-manual-scheduled",
		})
	}()
	waitFor(t, 5*time.Second, func() bool { return runState(sim.Snapshot(), manualID) == "complete" })
	scheduledIDs := sim.EnqueueDue()
	if len(scheduledIDs) != 1 {
		t.Fatalf("scheduled run IDs = %#v", scheduledIDs)
	}
	waitFor(t, 5*time.Second, func() bool { return runState(sim.Snapshot(), scheduledIDs[0]) == "complete" })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	snapshot := sim.Snapshot()
	if len(snapshot.Runs) != 2 || len(snapshot.Deliveries) != 2 {
		t.Fatalf("runs=%d deliveries=%d", len(snapshot.Runs), len(snapshot.Deliveries))
	}
	for _, run := range snapshot.Runs {
		if run.State != "complete" || run.Assets == 0 || run.Relationships == 0 || run.CompletedAt.IsZero() {
			t.Fatalf("run = %#v", run)
		}
	}
	for _, delivery := range snapshot.Deliveries {
		if !delivery.Preflighted || !delivery.Applied {
			t.Fatalf("delivery = %#v", delivery)
		}
	}
	second := snapshot.Deliveries[1]
	for _, operation := range append(second.ItemOperations, second.RelationOps...) {
		if operation != "NO_CHANGE" {
			t.Fatalf("repeat operation = %q, delivery = %#v", operation, second)
		}
	}
	if deleted := sim.Cleanup(); deleted != 2 {
		t.Fatalf("cleanup deleted %d raw chunks, want 2", deleted)
	}
	afterCleanup := sim.Snapshot()
	for _, result := range afterCleanup.Results {
		if !result.Deleted || result.Payload != nil {
			t.Fatalf("result retained raw payload after terminal cleanup: %#v", result)
		}
	}
	for _, run := range afterCleanup.Runs {
		if run.State != "complete" || run.Assets == 0 {
			t.Fatalf("cleanup removed run summary: %#v", run)
		}
	}
}

func TestPassword2SSHRunUsesAttemptBoundCredentialAndNoDataSkipsIRE(t *testing.T) {
	t.Parallel()
	sim := controlsim.New(controlsim.Config{
		Token:         testToken,
		SSHCredential: worker.SSHCredential{Username: "topo", Password: "simulator-password"},
	})
	server := httptest.NewTLSServer(sim.Handler())
	defer server.Close()
	client, err := worker.NewClient(server.URL, testToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	localRunID := sim.RunNow("pool-a")
	runID := sim.RunNowSSH("pool-a", "192.0.2.7/32", "binding-1")
	policy := worker.Policy{
		WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true, AllowSSHLinux: true, MaxConcurrency: 2,
		SSHAllowlist:     []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")},
		SSHHostKeyDigest: strings.Repeat("a", 64),
	}
	executor := worker.Executor{
		Policy:             policy,
		SSHHostKeyCallback: func(string, net.Addr, ssh.PublicKey) error { return nil },
		SSHDialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("simulated target offline")
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- worker.Run(ctx, worker.RunConfig{
			Policy: policy, Version: "test", PollInterval: time.Second,
			Control: client, Executor: executor,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), BootID: "boot-ssh",
		})
	}()
	waitFor(t, 5*time.Second, func() bool { return runState(sim.Snapshot(), runID) == "complete" })
	waitFor(t, 5*time.Second, func() bool { return runState(sim.Snapshot(), localRunID) == "complete" })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	snapshot := sim.Snapshot()
	var noData, applied int
	for _, delivery := range snapshot.Deliveries {
		if delivery.NoData {
			noData++
		}
		if delivery.Applied {
			applied++
		}
	}
	if len(snapshot.Deliveries) != 2 || noData != 1 || applied != 1 {
		t.Fatalf("delivery = %#v", snapshot.Deliveries)
	}
	if len(snapshot.CredentialAccesses) != 1 || snapshot.CredentialAccesses[0].Outcome != "allowed" || snapshot.CredentialAccesses[0].Reason != "attempt_bound" {
		t.Fatalf("credential access = %#v", snapshot.CredentialAccesses)
	}
	var collectionErrors int
	for _, run := range snapshot.Runs {
		collectionErrors += run.CollectionErrors
	}
	if len(snapshot.Runs) != 2 || collectionErrors != 1 {
		t.Fatalf("run = %#v", snapshot.Runs)
	}
}

func TestManualAndScheduledSSHProfilesRepeatStableReconciliation(t *testing.T) {
	t.Parallel()
	sim := controlsim.New(controlsim.Config{Token: testToken, SSHCredential: worker.SSHCredential{Username: "topo", Password: "simulator-password"}})
	server := httptest.NewTLSServer(sim.Handler())
	defer server.Close()
	client, err := worker.NewClient(server.URL, testToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	manualID := sim.RunNowSSH("pool-a", "192.0.2.7/32", "binding-1")
	sim.UpsertSchedule(controlsim.Schedule{
		ID: "ssh-schedule", WorkerPool: "pool-a", Operation: worker.OperationSSHLinuxV1,
		Target: "192.0.2.7/32", CredentialBindingID: "binding-1",
		Interval: time.Hour, NextRunAt: time.Now().Add(-time.Minute), Active: true,
	})
	policy := worker.Policy{
		WorkerPool: "pool-a", SiteID: "site-a", AllowSSHLinux: true,
		SSHAllowlist: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, SSHHostKeyDigest: strings.Repeat("a", 64),
	}
	executor := &stableSSHExecutor{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- worker.Run(ctx, worker.RunConfig{
			Policy: policy, Version: "test", PollInterval: time.Second, Control: client,
			Executor: executor, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), BootID: "boot-ssh-repeat",
		})
	}()
	waitFor(t, 5*time.Second, func() bool { return runState(sim.Snapshot(), manualID) == "complete" })
	scheduled := sim.EnqueueDue()
	if len(scheduled) != 1 {
		t.Fatalf("scheduled = %#v", scheduled)
	}
	waitFor(t, 5*time.Second, func() bool { return runState(sim.Snapshot(), scheduled[0]) == "complete" })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	snapshot := sim.Snapshot()
	if len(snapshot.Deliveries) != 2 || len(snapshot.CredentialAccesses) != 2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	for _, operation := range append(snapshot.Deliveries[1].ItemOperations, snapshot.Deliveries[1].RelationOps...) {
		if operation != "NO_CHANGE" {
			t.Fatalf("repeat operation = %q", operation)
		}
	}
}

type stableSSHExecutor struct{}

func (*stableSSHExecutor) Execute(context.Context, worker.Task) (model.ObservationEnvelope, error) {
	return model.ObservationEnvelope{}, errors.New("credentialed execution path was not used")
}

func (*stableSSHExecutor) ExecuteWithCredentials(ctx context.Context, task worker.Task, source worker.CredentialSource) (model.ObservationEnvelope, error) {
	if _, err := source.SSH(ctx); err != nil {
		return model.ObservationEnvelope{}, err
	}
	return model.ObservationEnvelope{
		SchemaVersion: model.SchemaVersion, ObservationID: "ssh-observation-" + task.TaskID,
		SiteID: "site-a", CollectorID: "worker-pool-pool-a", Plugin: "ssh-linux", JobID: task.TaskID,
		ObservedAt: time.Now().UTC(),
		Assets: []model.Asset{
			{Type: model.AssetHost, NativeID: "ssh-host-stable", Name: "ssh-host"},
			{Type: model.AssetNetworkInterface, NativeID: "ssh-interface-stable", Name: "eth0", Attributes: map[string]any{"mac_address": "02:00:00:00:00:07"}},
		},
		Relationships: []model.Relationship{{Type: "host_has_interface", FromNativeID: "ssh-host-stable", ToNativeID: "ssh-interface-stable"}},
	}, nil
}

func TestCrashLeaseExpiryCreatesFreshAttempt(t *testing.T) {
	t.Parallel()
	clock := &testClock{now: time.Now().UTC()}
	sim := controlsim.New(controlsim.Config{Token: testToken, LeaseDuration: time.Minute, Now: clock.Now})
	server := httptest.NewTLSServer(sim.Handler())
	defer server.Close()
	client, _ := worker.NewClient(server.URL, testToken, server.Client())
	one := registerWorker(t, client, "boot-one")
	two := registerWorker(t, client, "boot-two")
	sim.RunNow("pool-a")

	first, err := client.Claim(t.Context(), claimRequest(one, "boot-one"))
	if err != nil || first.Task == nil {
		t.Fatalf("first claim = %#v, %v", first, err)
	}
	blocked, err := client.Claim(t.Context(), claimRequest(two, "boot-two"))
	if err != nil || blocked.Task != nil {
		t.Fatalf("second live claim = %#v, %v", blocked, err)
	}
	clock.Advance(2 * time.Minute)
	recovered, err := client.Claim(t.Context(), claimRequest(two, "boot-two"))
	if err != nil || recovered.Task == nil {
		t.Fatalf("recovered claim = %#v, %v", recovered, err)
	}
	if recovered.Task.TaskID != first.Task.TaskID || recovered.Task.AttemptID == first.Task.AttemptID || recovered.Task.LeaseToken == first.Task.LeaseToken {
		t.Fatalf("first=%#v recovered=%#v", first.Task, recovered.Task)
	}
	snapshot := sim.Snapshot()
	if snapshot.Tasks[0].Attempt != 2 || snapshot.Tasks[0].LeaseDigest == recovered.Task.LeaseToken {
		t.Fatalf("task = %#v", snapshot.Tasks[0])
	}
}

func TestConcurrentWorkersGetExactlyOneLease(t *testing.T) {
	t.Parallel()
	sim := controlsim.New(controlsim.Config{Token: testToken, LeaseDuration: time.Minute})
	server := httptest.NewTLSServer(sim.Handler())
	defer server.Close()
	const workers = 32
	clients := make([]*worker.Client, workers)
	workerIDs := make([]string, workers)
	for index := range workers {
		clients[index], _ = worker.NewClient(server.URL, testToken, server.Client())
		workerIDs[index] = registerWorker(t, clients[index], "boot-race-"+itoa(index))
	}
	sim.RunNow("pool-a")
	var wg sync.WaitGroup
	var mu sync.Mutex
	claims := make([]worker.Task, 0, 1)
	for index := range workers {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			response, err := clients[index].Claim(context.Background(), claimRequest(workerIDs[index], "boot-race-"+itoa(index)))
			if err != nil {
				t.Errorf("claim %d: %v", index, err)
				return
			}
			if response.Task != nil {
				mu.Lock()
				claims = append(claims, *response.Task)
				mu.Unlock()
			}
		}(index)
	}
	wg.Wait()
	if len(claims) != 1 {
		t.Fatalf("live leases = %d, want 1", len(claims))
	}
}

func TestResultChunkIsChecksummedAttemptBoundAndIdempotent(t *testing.T) {
	t.Parallel()
	sim := controlsim.New(controlsim.Config{Token: testToken, LeaseDuration: time.Minute})
	server := httptest.NewTLSServer(sim.Handler())
	defer server.Close()
	client, _ := worker.NewClient(server.URL, testToken, server.Client())
	workerID := registerWorker(t, client, "boot-result")
	sim.RunNow("pool-a")
	claim, err := client.Claim(t.Context(), claimRequest(workerID, "boot-result"))
	if err != nil || claim.Task == nil {
		t.Fatalf("claim = %#v, %v", claim, err)
	}
	policy := worker.Policy{WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true}
	observation, err := (worker.Executor{Policy: policy}).Execute(t.Context(), *claim.Task)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(observation)
	sum := sha256.Sum256(payload)
	request := worker.ResultChunkRequest{SchemaVersion: worker.ContractVersion, WorkerID: workerID, BootID: "boot-result", AttemptID: claim.Task.AttemptID, LeaseToken: claim.Task.LeaseToken, ChunkNumber: 0, ChunkCount: 1, Checksum: hex.EncodeToString(sum[:]), ObservationJSON: string(payload)}
	first, err := client.SubmitResult(t.Context(), claim.Task.TaskID, request)
	if err != nil || !first.Accepted || first.Duplicate {
		t.Fatalf("first result = %#v, %v", first, err)
	}
	duplicate, err := client.SubmitResult(t.Context(), claim.Task.TaskID, request)
	if err != nil || !duplicate.Accepted || !duplicate.Duplicate {
		t.Fatalf("duplicate result = %#v, %v", duplicate, err)
	}
	wrong := request
	wrong.AttemptID = "attempt-other"
	if _, err := client.SubmitResult(t.Context(), claim.Task.TaskID, wrong); err == nil {
		t.Fatal("result from another attempt was accepted")
	}
	if _, err := client.Complete(t.Context(), claim.Task.TaskID, worker.CompleteRequest{SchemaVersion: worker.ContractVersion, WorkerID: workerID, BootID: "boot-result", AttemptID: claim.Task.AttemptID, LeaseToken: claim.Task.LeaseToken, Success: true, ChunkCount: 1}); err != nil {
		t.Fatal(err)
	}
	snapshot := sim.Snapshot()
	if len(snapshot.Results) != 1 || len(snapshot.Deliveries) != 1 || !snapshot.Deliveries[0].Preflighted || !snapshot.Deliveries[0].Applied {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func registerWorker(t *testing.T, client *worker.Client, bootID string) string {
	t.Helper()
	response, err := client.Register(t.Context(), worker.RegisterRequest{SchemaVersion: worker.ContractVersion, BootID: bootID, WorkerPool: "pool-a", SiteID: "site-a", Version: "test", Capabilities: []string{worker.OperationLocalV1}, PolicyDigest: "digest", StartedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	return response.WorkerID
}

func claimRequest(workerID, bootID string) worker.ClaimRequest {
	return worker.ClaimRequest{SchemaVersion: worker.ContractVersion, WorkerID: workerID, BootID: bootID, Capabilities: []string{worker.OperationLocalV1}}
}

func runState(snapshot controlsim.Snapshot, runID string) string {
	for _, run := range snapshot.Runs {
		if run.ID == runID {
			return run.State
		}
	}
	return ""
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
