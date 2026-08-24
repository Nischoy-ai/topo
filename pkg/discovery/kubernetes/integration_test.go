package kubernetes_test

import (
	"context"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/discovery"
	"github.com/Nischoy-ai/topo/pkg/discovery/kubernetes"
	"github.com/Nischoy-ai/topo/pkg/lab"
	"github.com/Nischoy-ai/topo/pkg/model"
)

// TestDiscoverClusterInventoryOverRealAPIFixture proves the plugin's real
// client-go REST calls, UID-based identity, and asset/relationship mapping
// are correct against a real Kubernetes REST API JSON shape. Unlike
// VMware, client-go has no equivalent of vcsim (see the slice write-up in
// docs/project-plan.md), so this exercises the hand-rolled Topo Lab
// fixture instead — the same posture SNMP needed for the same reason.
func TestDiscoverClusterInventoryOverRealAPIFixture(t *testing.T) {
	target, wantNodes, cleanup := startFixture(t, 25)
	defer cleanup()

	plugin := kubernetes.Plugin{Config: kubernetes.Config{
		BearerToken: lab.LabKubernetesToken, LabMode: true,
		Concurrency: 4, OperationTimeout: 10 * time.Second,
	}}
	request := discovery.Request{SiteID: "lab", CollectorID: "kubernetes-test", Targets: []string{target}}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	first, err := plugin.Discover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertNoErrors(t, first)
	assertCounts(t, first, wantNodes, wantNodes) // one pod per node in the fixture

	repository := store.NewMemory()
	if err := repository.SaveObservation(ctx, first); err != nil {
		t.Fatal(err)
	}

	second, err := plugin.Discover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertNoErrors(t, second)
	assertCounts(t, second, wantNodes, wantNodes)

	firstIDs := nativeIDs(first)
	secondIDs := nativeIDs(second)
	if len(firstIDs) != len(secondIDs) {
		t.Fatalf("got %d distinct native IDs on second scan, want %d", len(secondIDs), len(firstIDs))
	}
	for id := range firstIDs {
		if !secondIDs[id] {
			t.Fatalf("asset %q from the first scan is missing from the second scan; Kubernetes identity is not stable", id)
		}
	}

	if err := repository.SaveObservation(ctx, second); err != nil {
		t.Fatal(err)
	}
	assets, err := repository.ListAssets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != len(firstIDs) {
		t.Fatalf("repeated Kubernetes scan created duplicates: got %d stored assets, want %d", len(assets), len(firstIDs))
	}

	relationships, err := repository.ListRelationships(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(relationships) != wantNodes {
		t.Fatalf("got %d pod_runs_on_node relationships, want %d", len(relationships), wantNodes)
	}
}

func TestKubernetesWrongTokenIsIsolatedAsConnectError(t *testing.T) {
	target, _, cleanup := startFixture(t, 5)
	defer cleanup()

	plugin := kubernetes.Plugin{Config: kubernetes.Config{
		BearerToken: "definitely-wrong", LabMode: true, OperationTimeout: 5 * time.Second,
	}}
	observation, err := plugin.Discover(context.Background(), discovery.Request{Targets: []string{target}})
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Assets) != 0 || len(observation.Errors) != 1 || observation.Errors[0].Code != "kubernetes_operation" {
		t.Fatalf("unexpected observation: %#v", observation)
	}
}

func TestKubernetesUnreachableTargetIsIsolatedAndRetryable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close() // nothing is listening on this port by the time Discover dials it

	plugin := kubernetes.Plugin{Config: kubernetes.Config{
		BearerToken: lab.LabKubernetesToken, LabMode: true, OperationTimeout: 2 * time.Second,
	}}
	observation, err := plugin.Discover(context.Background(), discovery.Request{Targets: []string{"http://" + addr}})
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Assets) != 0 || len(observation.Errors) != 1 || observation.Errors[0].Code != "kubernetes_operation" || !observation.Errors[0].Retryable {
		t.Fatalf("unexpected observation: %#v", observation)
	}
}

// startFixture builds a small deterministic estate, serves it over the
// Topo Lab Kubernetes fixture (real HTTP + real Kubernetes JSON types),
// and returns a target URL with no embedded credentials — mirroring how
// the plugin itself is used.
func startFixture(t *testing.T, hostCount int) (target string, wantNodes int, cleanup func()) {
	t.Helper()
	scenario := lab.DefaultScenario(hostCount, 0, 1)
	estate, err := lab.Generate(scenario)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(lab.NewKubernetesServer(estate).Handler())
	return server.URL, len(estate.Hosts), server.Close
}

func assertNoErrors(t *testing.T, observation model.ObservationEnvelope) {
	t.Helper()
	if len(observation.Errors) != 0 {
		t.Fatalf("Kubernetes discovery returned %d errors; first: %#v", len(observation.Errors), observation.Errors[0])
	}
}

func assertCounts(t *testing.T, observation model.ObservationEnvelope, wantNodes, wantPods int) {
	t.Helper()
	gotNodes, gotPods := 0, 0
	for _, asset := range observation.Assets {
		if asset.Type != model.AssetKubernetesObject {
			continue
		}
		switch asset.Identifiers["kind"] {
		case "Node":
			gotNodes++
		case "Pod":
			gotPods++
		}
	}
	if gotNodes != wantNodes || gotPods != wantPods {
		t.Fatalf("got nodes=%d pods=%d, want nodes=%d pods=%d (total assets=%d)", gotNodes, gotPods, wantNodes, wantPods, len(observation.Assets))
	}
}

func nativeIDs(observation model.ObservationEnvelope) map[string]bool {
	ids := map[string]bool{}
	for _, asset := range observation.Assets {
		ids[string(asset.Type)+":"+asset.NativeID] = true
	}
	return ids
}
