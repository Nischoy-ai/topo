package vmware_test

import (
	"context"
	"crypto/tls"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/discovery"
	"github.com/Nischoy-ai/topo/pkg/discovery/vmware"
	"github.com/Nischoy-ai/topo/pkg/model"
	"github.com/vmware/govmomi/simulator"
)

// TestDiscoverVCenterInventoryOverRealVCSIM proves the plugin's SOAP calls,
// engine/UUID-based identity, and asset/relationship mapping are correct
// against a real vSphere API implementation — govmomi's own vcsim
// simulator — not a hand-rolled stand-in. Unlike SNMP, which needed a
// hand-rolled Topo Lab agent because gosnmp is client-only, govmomi ships
// vcsim specifically for this kind of testing, so no Topo Lab VMware
// fixture exists or is needed.
func TestDiscoverVCenterInventoryOverRealVCSIM(t *testing.T) {
	target, wantHosts, wantVMs, cleanup := startVCSIM(t, "reader", "hunter2hunter2", func(m *simulator.Model) {
		m.Datacenter = 1
		m.Cluster = 1
		m.ClusterHost = 2
		m.Host = 1
		m.Machine = 2
	})
	defer cleanup()

	plugin := vmware.Plugin{Config: vmware.Config{
		Username: "reader", Password: "hunter2hunter2", LabMode: true,
		Concurrency: 4, ConnectTimeout: 5 * time.Second, OperationTimeout: 10 * time.Second,
	}}
	request := discovery.Request{SiteID: "lab", CollectorID: "vmware-test", Targets: []string{target}}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	first, err := plugin.Discover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertNoErrors(t, first)
	assertCounts(t, first, wantHosts, wantVMs)

	repository := store.NewMemory()
	if err := repository.SaveObservation(ctx, first); err != nil {
		t.Fatal(err)
	}

	second, err := plugin.Discover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertNoErrors(t, second)
	assertCounts(t, second, wantHosts, wantVMs)

	firstIDs := nativeIDs(first)
	secondIDs := nativeIDs(second)
	if len(firstIDs) != len(secondIDs) {
		t.Fatalf("got %d distinct native IDs on second scan, want %d", len(secondIDs), len(firstIDs))
	}
	for id := range firstIDs {
		if !secondIDs[id] {
			t.Fatalf("asset %q from the first scan is missing from the second scan; VMware identity is not stable", id)
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
		t.Fatalf("repeated VMware scan created duplicates: got %d stored assets, want %d", len(assets), len(firstIDs))
	}
}

func TestVMwareWrongPasswordIsIsolatedAsConnectError(t *testing.T) {
	target, _, _, cleanup := startVCSIM(t, "reader", "hunter2hunter2", func(m *simulator.Model) {
		m.Datacenter = 1
		m.Host = 1
	})
	defer cleanup()

	plugin := vmware.Plugin{Config: vmware.Config{
		Username: "reader", Password: "definitely-wrong", LabMode: true,
		ConnectTimeout: 5 * time.Second,
	}}
	observation, err := plugin.Discover(context.Background(), discovery.Request{Targets: []string{target}})
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Assets) != 0 || len(observation.Errors) != 1 || observation.Errors[0].Code != "vmware_connect" {
		t.Fatalf("unexpected observation: %#v", observation)
	}
}

func TestVMwareUnreachableTargetIsIsolatedAndRetryable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close() // nothing is listening on this port by the time Discover dials it

	plugin := vmware.Plugin{Config: vmware.Config{
		Username: "reader", Password: "hunter2hunter2", LabMode: true,
		ConnectTimeout: 2 * time.Second,
	}}
	observation, err := plugin.Discover(context.Background(), discovery.Request{Targets: []string{"https://" + addr}})
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Assets) != 0 || len(observation.Errors) != 1 || observation.Errors[0].Code != "vmware_connect" || !observation.Errors[0].Retryable {
		t.Fatalf("unexpected observation: %#v", observation)
	}
}

// startVCSIM builds a vcsim model per configure, serves it over HTTPS with
// a self-signed certificate (matching how a real vCenter is reached, not
// vcsim's plaintext-HTTP default), and returns a target URL with no
// embedded credentials — mirroring how the plugin itself is used,
// credentials never travel in the target string. It also returns the
// model's actual host/VM counts (via Model.Count(), rather than a
// hand-computed guess: vcsim's Machine count is per resource pool, and a
// cluster has exactly one shared root pool regardless of its host count,
// so "hosts * machines" is not the right formula).
//
// Setting Service.Listen with explicit credentials is required for vcsim
// to actually enforce them: left unset, vcsim's SessionManager.Authenticate
// accepts any non-empty username/password, which would make the wrong-
// password acceptance test meaningless.
func startVCSIM(t *testing.T, username, password string, configure func(*simulator.Model)) (target string, wantHosts, wantVMs int, cleanup func()) {
	t.Helper()
	model := simulator.VPX()
	configure(model)
	if err := model.Create(); err != nil {
		t.Fatal(err)
	}
	counts := model.Count()

	model.Service.TLS = new(tls.Config)
	model.Service.Listen = &url.URL{User: url.UserPassword(username, password)}
	server := model.Service.NewServer()

	strippedTarget := *server.URL
	strippedTarget.User = nil

	return strippedTarget.String(), counts.Host, counts.Machine, func() {
		server.Close()
		model.Remove()
	}
}

func assertNoErrors(t *testing.T, observation model.ObservationEnvelope) {
	t.Helper()
	if len(observation.Errors) != 0 {
		t.Fatalf("VMware discovery returned %d errors; first: %#v", len(observation.Errors), observation.Errors[0])
	}
}

func assertCounts(t *testing.T, observation model.ObservationEnvelope, wantHosts, wantVMs int) {
	t.Helper()
	gotHosts, gotVMs, gotInterfaces := 0, 0, 0
	for _, asset := range observation.Assets {
		switch asset.Type {
		case model.AssetHost:
			gotHosts++
		case model.AssetVirtualMachine:
			gotVMs++
		case model.AssetNetworkInterface:
			gotInterfaces++
		}
	}
	if gotHosts != wantHosts || gotVMs != wantVMs {
		t.Fatalf("got hosts=%d vms=%d, want hosts=%d vms=%d (total assets=%d, interfaces=%d)", gotHosts, gotVMs, wantHosts, wantVMs, len(observation.Assets), gotInterfaces)
	}
	if gotInterfaces == 0 {
		t.Fatal("expected at least one network interface asset")
	}
}

func nativeIDs(observation model.ObservationEnvelope) map[string]bool {
	ids := map[string]bool{}
	for _, asset := range observation.Assets {
		ids[string(asset.Type)+":"+asset.NativeID] = true
	}
	return ids
}
