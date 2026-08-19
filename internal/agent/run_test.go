package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/controller"
	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/discovery"
	"github.com/Nischoy-ai/topo/pkg/model"
)

// counterPlugin produces a distinct, deterministic observation on each call
// so tests can tell discovery passes apart without depending on real host
// state.
type counterPlugin struct {
	count atomic.Int64
}

func (*counterPlugin) DescribeCapabilities(context.Context) discovery.Capability {
	return discovery.Capability{}
}
func (*counterPlugin) ValidateConfiguration(context.Context, discovery.Request) error { return nil }
func (*counterPlugin) CheckConnectivity(context.Context, discovery.Request) error     { return nil }
func (p *counterPlugin) Discover(_ context.Context, r discovery.Request) (model.ObservationEnvelope, error) {
	n := p.count.Add(1)
	id := fmt.Sprintf("obs-%d", n)
	return model.ObservationEnvelope{
		SchemaVersion: model.SchemaVersion,
		ObservationID: id,
		SiteID:        r.SiteID,
		CollectorID:   r.CollectorID,
		Plugin:        "counter",
		ObservedAt:    time.Now().UTC(),
		Assets:        []model.Asset{{Type: model.AssetHost, NativeID: "host-" + id}},
	}, nil
}

// failingPlugin always fails discovery.
type failingPlugin struct{}

func (failingPlugin) DescribeCapabilities(context.Context) discovery.Capability {
	return discovery.Capability{}
}
func (failingPlugin) ValidateConfiguration(context.Context, discovery.Request) error { return nil }
func (failingPlugin) CheckConnectivity(context.Context, discovery.Request) error     { return nil }
func (failingPlugin) Discover(context.Context, discovery.Request) (model.ObservationEnvelope, error) {
	return model.ObservationEnvelope{}, errors.New("discovery unavailable")
}

func TestRunDeliversWhileControllerReachable(t *testing.T) {
	repo := store.NewMemory()
	server := httptest.NewServer(controller.New(repo, slog.Default(), "").Handler())
	defer server.Close()

	sender, err := NewSender(server.URL, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := NewSpool(t.TempDir(), testKey(t), 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = Run(ctx, Config{
		SiteID: "default", CollectorID: "agent-1", Interval: 50 * time.Millisecond,
		Plugin: &counterPlugin{}, Sender: sender, Spool: spool,
	})
	if err != nil {
		t.Fatal(err)
	}

	observations, err := repo.ListObservations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) < 2 {
		t.Fatalf("observations = %d, want at least 2", len(observations))
	}
	pending, err := spool.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending spool entries = %v, want none while the controller is reachable", pending)
	}
}

func TestRunBuffersWhileControllerUnreachableThenDrains(t *testing.T) {
	repo := store.NewMemory()
	server := httptest.NewServer(controller.New(repo, slog.Default(), "").Handler())
	// Start with the server closed so the first deliveries fail and spool.
	server.Close()

	sender, err := NewSender(server.URL, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := NewSpool(t.TempDir(), testKey(t), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	plugin := &counterPlugin{}

	offlineCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	_ = Run(offlineCtx, Config{
		SiteID: "default", CollectorID: "agent-1", Interval: 20 * time.Millisecond,
		Plugin: plugin, Sender: sender, Spool: spool,
	})
	cancel()

	pendingBefore, err := spool.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pendingBefore) == 0 {
		t.Fatal("expected observations to be spooled while the controller was unreachable")
	}

	repo2 := store.NewMemory()
	recovered := httptest.NewServer(controller.New(repo2, slog.Default(), "").Handler())
	defer recovered.Close()
	recoveredSender, err := NewSender(recovered.URL, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	onlineCtx, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel2()
	_ = Run(onlineCtx, Config{
		SiteID: "default", CollectorID: "agent-1", Interval: 500 * time.Millisecond,
		Plugin: plugin, Sender: recoveredSender, Spool: spool,
	})

	pendingAfter, err := spool.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pendingAfter) != 0 {
		t.Fatalf("pending spool entries after recovery = %v, want none", pendingAfter)
	}
	observations, err := repo2.ListObservations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) < len(pendingBefore) {
		t.Fatalf("delivered %d observations, want at least the %d that were spooled", len(observations), len(pendingBefore))
	}
}

func TestRunDropsObservationsWhenDiscoveryFails(t *testing.T) {
	repo := store.NewMemory()
	server := httptest.NewServer(controller.New(repo, slog.Default(), "").Handler())
	defer server.Close()

	sender, err := NewSender(server.URL, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := NewSpool(t.TempDir(), testKey(t), 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := Run(ctx, Config{
		SiteID: "default", CollectorID: "agent-1", Interval: 20 * time.Millisecond,
		Plugin: failingPlugin{}, Sender: sender, Spool: spool,
	}); err != nil {
		t.Fatal(err)
	}

	observations, err := repo.ListObservations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 0 {
		t.Fatalf("observations = %d, want 0 when discovery always fails", len(observations))
	}
}

func TestRunSendsHeartbeatsOnIndependentCadence(t *testing.T) {
	repo := store.NewMemory()
	ctrl := controller.New(repo, slog.Default(), "")
	server := httptest.NewServer(ctrl.Handler())
	defer server.Close()

	sender, err := NewSender(server.URL, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := NewSpool(t.TempDir(), testKey(t), 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	// Interval is deliberately much longer than the test's own deadline, so
	// only the independent HeartbeatInterval ticker can be responsible for
	// any heartbeats the controller records.
	err = Run(ctx, Config{
		SiteID: "default", CollectorID: "agent-1", Interval: time.Hour,
		HeartbeatInterval: 30 * time.Millisecond,
		Plugin:            &counterPlugin{}, Sender: sender, Spool: spool,
	})
	if err != nil {
		t.Fatal(err)
	}

	statuses := ctrl.Heartbeats.List()
	if len(statuses) != 1 {
		t.Fatalf("collector statuses = %d, want 1", len(statuses))
	}
	if statuses[0].CollectorID != "agent-1" || statuses[0].SiteID != "default" {
		t.Fatalf("status = %+v", statuses[0])
	}
	if !statuses[0].Alive {
		t.Fatal("expected the collector to still be reported alive")
	}

	observations, err := repo.ListObservations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 {
		t.Fatalf("observations = %d, want exactly 1: Run's own immediate first tick, not its hour-long Interval ticker", len(observations))
	}
}

func TestRunHeartbeatDisabledByDefault(t *testing.T) {
	repo := store.NewMemory()
	ctrl := controller.New(repo, slog.Default(), "")
	server := httptest.NewServer(ctrl.Handler())
	defer server.Close()

	sender, err := NewSender(server.URL, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := NewSpool(t.TempDir(), testKey(t), 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	// HeartbeatInterval left at its zero value: heartbeats must stay off.
	err = Run(ctx, Config{
		SiteID: "default", CollectorID: "agent-1", Interval: 20 * time.Millisecond,
		Plugin: &counterPlugin{}, Sender: sender, Spool: spool,
	})
	if err != nil {
		t.Fatal(err)
	}

	if statuses := ctrl.Heartbeats.List(); len(statuses) != 0 {
		t.Fatalf("collector statuses = %d, want 0 when HeartbeatInterval is unset", len(statuses))
	}
}

func TestRunPollsAndExecutesDispatchedJobs(t *testing.T) {
	repo := store.NewMemory()
	ctrl := controller.New(repo, slog.Default(), "")
	server := httptest.NewServer(ctrl.Handler())
	defer server.Close()

	job, err := ctrl.Jobs.Enqueue("agent-1", model.JobTypeDiscover)
	if err != nil {
		t.Fatal(err)
	}

	sender, err := NewSender(server.URL, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := NewSpool(t.TempDir(), testKey(t), 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	// Interval is deliberately much longer than the test's own deadline;
	// only the queued job, picked up via the HeartbeatInterval poll,
	// should be able to cause a second discovery pass beyond Run's own
	// initial immediate tick.
	err = Run(ctx, Config{
		SiteID: "default", CollectorID: "agent-1", Interval: time.Hour,
		HeartbeatInterval: 30 * time.Millisecond,
		Plugin:            &counterPlugin{}, Sender: sender, Spool: spool,
	})
	if err != nil {
		t.Fatal(err)
	}

	status, err := ctrl.Jobs.Status(job.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "succeeded" {
		t.Fatalf("job status = %+v, want succeeded", status)
	}

	observations, err := repo.ListObservations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) < 2 {
		t.Fatalf("observations = %d, want at least 2: Run's own immediate first tick, plus the dispatched job's discovery pass", len(observations))
	}
}

func TestRunReportsFailedDiscoveryJobResult(t *testing.T) {
	repo := store.NewMemory()
	ctrl := controller.New(repo, slog.Default(), "")
	server := httptest.NewServer(ctrl.Handler())
	defer server.Close()

	job, err := ctrl.Jobs.Enqueue("agent-1", model.JobTypeDiscover)
	if err != nil {
		t.Fatal(err)
	}

	sender, err := NewSender(server.URL, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := NewSpool(t.TempDir(), testKey(t), 1<<20)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err = Run(ctx, Config{
		SiteID: "default", CollectorID: "agent-1", Interval: time.Hour,
		HeartbeatInterval: 30 * time.Millisecond,
		Plugin:            failingPlugin{}, Sender: sender, Spool: spool,
	})
	if err != nil {
		t.Fatal(err)
	}

	status, err := ctrl.Jobs.Status(job.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != "failed" || status.Error == "" {
		t.Fatalf("job status = %+v, want failed with a non-empty error", status)
	}
}

func TestRunRejectsInvalidConfig(t *testing.T) {
	if err := Run(context.Background(), Config{Interval: 0}); err == nil {
		t.Fatal("expected non-positive interval to be rejected")
	}
	if err := Run(context.Background(), Config{Interval: time.Second}); err == nil {
		t.Fatal("expected missing plugin/sender/spool to be rejected")
	}
}
