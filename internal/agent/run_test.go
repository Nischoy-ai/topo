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

	sender, err := NewSender(server.URL, "")
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

	sender, err := NewSender(server.URL, "")
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
	recoveredSender, err := NewSender(recovered.URL, "")
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

	sender, err := NewSender(server.URL, "")
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

func TestRunRejectsInvalidConfig(t *testing.T) {
	if err := Run(context.Background(), Config{Interval: 0}); err == nil {
		t.Fatal("expected non-positive interval to be rejected")
	}
	if err := Run(context.Background(), Config{Interval: time.Second}); err == nil {
		t.Fatal("expected missing plugin/sender/spool to be rejected")
	}
}
