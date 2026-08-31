package controlsim_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/worker"
	"github.com/Nischoy-ai/topo/internal/worker/controlsim"
	"github.com/Nischoy-ai/topo/pkg/model"
)

func TestWorkerPoolBackpressureHonorsLocalAndPoolCapacity(t *testing.T) {
	sim := controlsim.New(controlsim.Config{Token: testToken, LeaseDuration: 2 * time.Second, PoolMaxLeases: 5})
	server := httptest.NewTLSServer(sim.Handler())
	defer server.Close()
	runID := sim.RunNowPartitions("pool-a", 10)
	release := make(chan struct{})
	executor := &syntheticExecutor{AssetsPerPartition: 1, Release: release}

	workers := make([]runningWorker, 0, 2)
	for index := 0; index < 2; index++ {
		workers = append(workers, startWorkerWith(t, server, worker.Policy{WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true, MaxConcurrency: 4}, executor, fmt.Sprintf("capacity-%d", index)))
	}
	t.Cleanup(func() { stopWorkers(t, workers) })
	if !waitForState(5*time.Second, func() bool {
		snapshot := sim.Snapshot()
		return activeTaskCount(snapshot) == 5 && executor.Active.Load() == 5
	}) {
		t.Fatalf("capacity was not reached: active_tasks=%d executor_active=%d peak=%d snapshot=%#v exited_workers=%d", activeTaskCount(sim.Snapshot()), executor.Active.Load(), executor.Maximum.Load(), sim.Snapshot().Tasks, exitedWorkers(workers))
	}
	snapshot := sim.Snapshot()
	perWorker := map[string]int{}
	for _, task := range snapshot.Tasks {
		if task.State == "leased" || task.State == "running" || task.State == "results_received" {
			perWorker[task.WorkerID]++
		}
	}
	for workerID, count := range perWorker {
		if count > 4 {
			t.Fatalf("worker %s holds %d leases, local ceiling is 4", workerID, count)
		}
	}
	if executor.Maximum.Load() != 5 {
		t.Fatalf("peak concurrent execution = %d, want pool ceiling 5", executor.Maximum.Load())
	}
	close(release)
	waitFor(t, 10*time.Second, func() bool { return runState(sim.Snapshot(), runID) == "complete" })
	stopWorkers(t, workers)
	workers = nil
	final := sim.Snapshot()
	if final.Runs[0].Assets != 10 || final.Runs[0].Attempts != 10 {
		t.Fatalf("run summary = %#v", final.Runs[0])
	}
}

func TestWorkerChurnDrainsSamePartitionedRun(t *testing.T) {
	sim := controlsim.New(controlsim.Config{Token: testToken, LeaseDuration: 80 * time.Millisecond, PoolMaxLeases: 4})
	server := httptest.NewTLSServer(sim.Handler())
	defer server.Close()
	runID := sim.RunNowPartitions("pool-a", 8)
	blocked := make(chan struct{})
	first := startWorkerWith(t, server, worker.Policy{WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true, MaxConcurrency: 4}, &syntheticExecutor{AssetsPerPartition: 2, Release: blocked}, "churn-first")
	if !waitForState(3*time.Second, func() bool { return activeTaskCount(sim.Snapshot()) == 4 }) {
		t.Fatalf("first worker did not fill four leases: %#v", sim.Snapshot().Tasks)
	}
	stopWorkers(t, []runningWorker{first})

	second := startWorkerWith(t, server, worker.Policy{WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true, MaxConcurrency: 4}, &syntheticExecutor{AssetsPerPartition: 2}, "churn-second")
	waitFor(t, 8*time.Second, func() bool { return runState(sim.Snapshot(), runID) == "complete" })
	stopWorkers(t, []runningWorker{second})
	snapshot := sim.Snapshot()
	retried := 0
	attempts := 0
	for _, task := range snapshot.Tasks {
		attempts += task.Attempt
		if task.Attempt == 2 {
			retried++
		}
		if task.State != "complete" {
			t.Fatalf("churn left task non-terminal: %#v", task)
		}
	}
	if retried != 4 || attempts != 12 || snapshot.Items != 16 || snapshot.Relations != 8 {
		t.Fatalf("churn summary: retried=%d attempts=%d items=%d relations=%d", retried, attempts, snapshot.Items, snapshot.Relations)
	}
}

func TestLeaseRenewalAndLossRecovery(t *testing.T) {
	t.Run("renewed task completes past initial lease", func(t *testing.T) {
		sim := controlsim.New(controlsim.Config{Token: testToken, LeaseDuration: 80 * time.Millisecond})
		server := httptest.NewTLSServer(sim.Handler())
		defer server.Close()
		runID := sim.RunNow("pool-a")
		executor := &syntheticExecutor{AssetsPerPartition: 1, Delay: 350 * time.Millisecond}
		running := startWorkerWith(t, server, worker.Policy{WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true}, executor, "renew-success")
		waitFor(t, 5*time.Second, func() bool { return runState(sim.Snapshot(), runID) == "complete" })
		stopWorkers(t, []runningWorker{running})
		task := sim.Snapshot().Tasks[0]
		if task.Attempt != 1 || task.State != "complete" {
			t.Fatalf("renewed task = %#v", task)
		}
	})

	t.Run("renewal loss cancels and fresh boot retries", func(t *testing.T) {
		sim := controlsim.New(controlsim.Config{Token: testToken, LeaseDuration: 80 * time.Millisecond})
		sim.SetRenewAvailable(false)
		server := httptest.NewTLSServer(sim.Handler())
		defer server.Close()
		runID := sim.RunNow("pool-a")
		blocked := &untilCancelledExecutor{started: make(chan struct{}), cancelled: make(chan struct{})}
		first := startWorkerWith(t, server, worker.Policy{WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true}, blocked, "renew-lost")
		select {
		case <-blocked.started:
		case <-time.After(2 * time.Second):
			t.Fatal("first attempt did not start")
		}
		select {
		case <-blocked.cancelled:
		case <-time.After(2 * time.Second):
			t.Fatal("worker did not cancel execution when renewal was lost")
		}
		stopWorkers(t, []runningWorker{first})
		sim.SetRenewAvailable(true)
		second := startWorkerWith(t, server, worker.Policy{WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true}, &syntheticExecutor{AssetsPerPartition: 1}, "renew-retry")
		waitFor(t, 5*time.Second, func() bool { return runState(sim.Snapshot(), runID) == "complete" })
		stopWorkers(t, []runningWorker{second})
		task := sim.Snapshot().Tasks[0]
		if task.Attempt != 2 || task.State != "complete" {
			t.Fatalf("recovered task = %#v", task)
		}
	})
}

func TestCooperativeCancellationStopsReadyAndRunningPartitions(t *testing.T) {
	t.Run("ready run", func(t *testing.T) {
		sim := controlsim.New(controlsim.Config{Token: testToken})
		runID := sim.RunNowPartitions("pool-a", 3)
		if !sim.CancelRun(runID) {
			t.Fatal("ready run was not cancelled")
		}
		snapshot := sim.Snapshot()
		if runState(snapshot, runID) != "cancelled" {
			t.Fatalf("run = %#v", snapshot.Runs)
		}
		for _, task := range snapshot.Tasks {
			if task.State != "cancelled" || task.Attempt != 0 {
				t.Fatalf("ready cancellation task = %#v", task)
			}
		}
	})

	t.Run("running run", func(t *testing.T) {
		sim := controlsim.New(controlsim.Config{Token: testToken, LeaseDuration: 100 * time.Millisecond, PoolMaxLeases: 4})
		server := httptest.NewTLSServer(sim.Handler())
		defer server.Close()
		runID := sim.RunNowPartitions("pool-a", 4)
		executor := &untilCancelledExecutor{started: make(chan struct{}), cancelled: make(chan struct{})}
		running := startWorkerWith(t, server, worker.Policy{WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true, MaxConcurrency: 4}, executor, "cancel-running")
		if !waitForState(3*time.Second, func() bool { return activeTaskCount(sim.Snapshot()) == 4 }) {
			t.Fatalf("running cancellation setup did not lease four tasks: snapshot=%#v exited_workers=%d", sim.Snapshot().Tasks, exitedWorkers([]runningWorker{running}))
		}
		if !sim.CancelRun(runID) {
			t.Fatal("running run was not cancelled")
		}
		waitFor(t, 5*time.Second, func() bool { return runState(sim.Snapshot(), runID) == "cancelled" })
		stopWorkers(t, []runningWorker{running})
		snapshot := sim.Snapshot()
		for _, task := range snapshot.Tasks {
			if task.State != "cancelled" {
				t.Fatalf("running cancellation task = %#v", task)
			}
		}
		if len(snapshot.Results) != 0 || len(snapshot.Deliveries) != 0 {
			t.Fatalf("cancelled run accepted result state: results=%d deliveries=%d", len(snapshot.Results), len(snapshot.Deliveries))
		}
	})
}

func TestCancelledAttemptRejectsLateSuccessAndResult(t *testing.T) {
	sim := controlsim.New(controlsim.Config{Token: testToken, LeaseDuration: time.Minute})
	server := httptest.NewTLSServer(sim.Handler())
	defer server.Close()
	client, err := worker.NewClient(server.URL, testToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	registration, err := client.Register(t.Context(), worker.RegisterRequest{SchemaVersion: worker.ContractVersion, BootID: "cancel-late", WorkerPool: "pool-a", SiteID: "site-a", Version: "test", Capabilities: []string{worker.OperationLocalV1}, PolicyDigest: "digest", MaxConcurrency: 1, StartedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	runID := sim.RunNow("pool-a")
	claim, err := client.Claim(t.Context(), worker.ClaimRequest{SchemaVersion: worker.ContractVersion, WorkerID: registration.WorkerID, BootID: "cancel-late", Capabilities: []string{worker.OperationLocalV1}})
	if err != nil || claim.Task == nil {
		t.Fatalf("claim = %#v, %v", claim, err)
	}
	if !sim.CancelRun(runID) {
		t.Fatal("run was not marked for cancellation")
	}
	renewed, err := client.Renew(t.Context(), claim.Task.TaskID, worker.RenewRequest{SchemaVersion: worker.ContractVersion, WorkerID: registration.WorkerID, BootID: "cancel-late", AttemptID: claim.Task.AttemptID, LeaseToken: claim.Task.LeaseToken})
	if err != nil || !renewed.Cancelled {
		t.Fatalf("renew cancellation = %#v, %v", renewed, err)
	}
	observation, err := (&syntheticExecutor{AssetsPerPartition: 1}).Execute(t.Context(), *claim.Task)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(observation)
	sum := sha256.Sum256(payload)
	if _, err := client.SubmitResult(t.Context(), claim.Task.TaskID, worker.ResultChunkRequest{SchemaVersion: worker.ContractVersion, WorkerID: registration.WorkerID, BootID: "cancel-late", AttemptID: claim.Task.AttemptID, LeaseToken: claim.Task.LeaseToken, ChunkNumber: 0, ChunkCount: 1, Checksum: hex.EncodeToString(sum[:]), ObservationJSON: string(payload)}); err == nil {
		t.Fatal("cancelled attempt accepted a late result")
	}
	if _, err := client.Complete(t.Context(), claim.Task.TaskID, worker.CompleteRequest{SchemaVersion: worker.ContractVersion, WorkerID: registration.WorkerID, BootID: "cancel-late", AttemptID: claim.Task.AttemptID, LeaseToken: claim.Task.LeaseToken, Success: true, ChunkCount: 1}); err == nil {
		t.Fatal("cancelled attempt accepted late success")
	}
	completed, err := client.Complete(t.Context(), claim.Task.TaskID, worker.CompleteRequest{SchemaVersion: worker.ContractVersion, WorkerID: registration.WorkerID, BootID: "cancel-late", AttemptID: claim.Task.AttemptID, LeaseToken: claim.Task.LeaseToken, Success: false, Failure: &worker.Failure{Code: "cancelled", Message: "cancelled", Retryable: false}})
	if err != nil || completed.TaskState != "cancelled" || completed.RunState != "cancelled" {
		t.Fatalf("cancel completion = %#v, %v", completed, err)
	}
}

func TestScaleGatesRepeatWithoutDuplicateIdentity(t *testing.T) {
	for _, assetCount := range []int{1000, 10000, 100000} {
		assetCount := assetCount
		t.Run(fmt.Sprintf("assets-%d", assetCount), func(t *testing.T) {
			started := time.Now()
			partitions := assetCount / 1000
			sim := controlsim.New(controlsim.Config{Token: testToken, LeaseDuration: 5 * time.Second, PoolMaxLeases: 128})
			server := httptest.NewTLSServer(sim.Handler())
			defer server.Close()
			firstRun := sim.RunNowPartitions("pool-a", partitions)
			executor := &syntheticExecutor{AssetsPerPartition: 1000}
			workers := make([]runningWorker, 0, 4)
			for index := 0; index < 4; index++ {
				workers = append(workers, startWorkerWith(t, server, worker.Policy{WorkerPool: "pool-a", SiteID: "site-a", AllowLocal: true, MaxConcurrency: 32}, executor, fmt.Sprintf("scale-%d-%d", assetCount, index)))
			}
			waitFor(t, 60*time.Second, func() bool { return runState(sim.Snapshot(), firstRun) == "complete" })
			first := sim.Snapshot()
			wantRelations := assetCount / 2
			if first.Items != assetCount || first.Relations != wantRelations || len(first.Tasks) != partitions || len(first.Results) != partitions {
				t.Fatalf("first scale snapshot: items=%d relations=%d tasks=%d results=%d", first.Items, first.Relations, len(first.Tasks), len(first.Results))
			}

			secondRun := sim.RunNowPartitions("pool-a", partitions)
			waitFor(t, 60*time.Second, func() bool { return runState(sim.Snapshot(), secondRun) == "complete" })
			stopWorkers(t, workers)
			second := sim.Snapshot()
			if second.Items != assetCount || second.Relations != wantRelations || len(second.Tasks) != partitions*2 || len(second.Results) != partitions*2 {
				t.Fatalf("repeat scale snapshot: items=%d relations=%d tasks=%d results=%d", second.Items, second.Relations, len(second.Tasks), len(second.Results))
			}
			for _, run := range second.Runs {
				if run.Assets != assetCount || run.State != "complete" {
					t.Fatalf("scale run = %#v", run)
				}
			}
			for _, delivery := range second.Deliveries {
				if delivery.RunID != secondRun {
					continue
				}
				for _, operation := range delivery.ItemOperations {
					if operation != "NO_CHANGE" {
						t.Fatalf("repeat operation = %q", operation)
					}
				}
				for _, operation := range delivery.RelationOps {
					if operation != "NO_CHANGE" {
						t.Fatalf("repeat relationship operation = %q", operation)
					}
				}
			}
			t.Logf("simulator processed and repeated %d stable assets across %d partitions in %s", assetCount, partitions, time.Since(started).Round(time.Millisecond))
		})
	}
}

func TestRetentionVolumeDrainsHundredThousandInBoundedBatches(t *testing.T) {
	const total = 100000
	const batch = 257
	sim := controlsim.New(controlsim.Config{Token: testToken, SuccessRawTTL: 24 * time.Hour})
	sim.SeedProcessedResults(total, time.Now().Add(-48*time.Hour))
	deleted := 0
	passes := 0
	for {
		count := sim.CleanupBatch(batch)
		if count > batch {
			t.Fatalf("cleanup pass deleted %d rows, batch limit is %d", count, batch)
		}
		if count == 0 {
			break
		}
		deleted += count
		passes++
	}
	rows, tombstones, payloadBytes := sim.ResultStats()
	if deleted != total || rows != total || tombstones != total || payloadBytes != 0 {
		t.Fatalf("retention totals: deleted=%d rows=%d tombstones=%d payload_bytes=%d", deleted, rows, tombstones, payloadBytes)
	}
	if passes <= 1 {
		t.Fatalf("retention completed in %d pass; bounded backlog progress was not exercised", passes)
	}
}

type syntheticExecutor struct {
	AssetsPerPartition int
	Delay              time.Duration
	Release            <-chan struct{}
	Active             atomic.Int64
	Maximum            atomic.Int64
}

func (e *syntheticExecutor) Execute(ctx context.Context, task worker.Task) (model.ObservationEnvelope, error) {
	active := e.Active.Add(1)
	defer e.Active.Add(-1)
	for {
		maximum := e.Maximum.Load()
		if active <= maximum || e.Maximum.CompareAndSwap(maximum, active) {
			break
		}
	}
	if e.Release != nil {
		select {
		case <-ctx.Done():
			return model.ObservationEnvelope{}, ctx.Err()
		case <-e.Release:
		}
	}
	if e.Delay > 0 {
		timer := time.NewTimer(e.Delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return model.ObservationEnvelope{}, ctx.Err()
		case <-timer.C:
		}
	}
	ordinal := 0
	if task.TargetPartition != nil {
		ordinal = task.TargetPartition.Ordinal
	}
	assets := make([]model.Asset, 0, e.AssetsPerPartition)
	relationships := make([]model.Relationship, 0, e.AssetsPerPartition/2)
	for index := 0; index < e.AssetsPerPartition; index += 2 {
		global := ordinal*e.AssetsPerPartition + index
		hostID := fmt.Sprintf("scale-host-%09d", global)
		assets = append(assets, model.Asset{Type: model.AssetHost, NativeID: hostID, Name: hostID})
		if index+1 >= e.AssetsPerPartition {
			continue
		}
		interfaceID := fmt.Sprintf("scale-interface-%09d", global)
		assets = append(assets, model.Asset{Type: model.AssetNetworkInterface, NativeID: interfaceID, Name: "eth0"})
		relationships = append(relationships, model.Relationship{Type: "host_has_interface", FromNativeID: hostID, ToNativeID: interfaceID})
	}
	return model.ObservationEnvelope{
		SchemaVersion: model.SchemaVersion,
		ObservationID: fmt.Sprintf("observation-%s", task.TaskID),
		SiteID:        "site-a",
		CollectorID:   "worker-pool-pool-a",
		Plugin:        worker.OperationLocalV1,
		JobID:         task.TaskID,
		ObservedAt:    time.Now().UTC(),
		Assets:        assets,
		Relationships: relationships,
	}, nil
}

type untilCancelledExecutor struct {
	started   chan struct{}
	cancelled chan struct{}
	onceStart sync.Once
	onceDone  sync.Once
}

func (e *untilCancelledExecutor) Execute(ctx context.Context, _ worker.Task) (model.ObservationEnvelope, error) {
	e.onceStart.Do(func() { close(e.started) })
	<-ctx.Done()
	e.onceDone.Do(func() { close(e.cancelled) })
	return model.ObservationEnvelope{}, ctx.Err()
}

type runningWorker struct {
	cancel context.CancelFunc
	done   <-chan error
}

func startWorkerWith(t *testing.T, server *httptest.Server, policy worker.Policy, executor worker.TaskExecutor, bootID string) runningWorker {
	t.Helper()
	client, err := worker.NewClient(server.URL, testToken, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- worker.Run(ctx, worker.RunConfig{
			Policy:       policy,
			Version:      "test",
			PollInterval: time.Second,
			Control:      client,
			Executor:     executor,
			Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
			BootID:       bootID,
		})
	}()
	return runningWorker{cancel: cancel, done: done}
}

func stopWorkers(t *testing.T, workers []runningWorker) {
	t.Helper()
	for _, running := range workers {
		running.cancel()
	}
	for _, running := range workers {
		select {
		case err := <-running.done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("worker did not stop")
		}
	}
}

func activeTaskCount(snapshot controlsim.Snapshot) int {
	count := 0
	for _, task := range snapshot.Tasks {
		if task.State == "leased" || task.State == "running" || task.State == "results_received" {
			count++
		}
	}
	return count
}

func waitForState(timeout time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return condition()
}

func exitedWorkers(workers []runningWorker) int {
	count := 0
	for _, running := range workers {
		if len(running.done) > 0 {
			count++
		}
	}
	return count
}
