package snmp_test

import (
	"context"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/discovery"
	"github.com/Nischoy-ai/topo/pkg/discovery/snmp"
	"github.com/Nischoy-ai/topo/pkg/lab"
	"github.com/Nischoy-ai/topo/pkg/model"
)

func TestDiscoverManyHostsThroughRealSNMPv3Sessions(t *testing.T) {
	estate := makeEstate(t, lab.DefaultScenario(200, 40, 21))
	server := makeSNMPServer(t, estate)
	plugin := labPlugin()
	request := discovery.Request{SiteID: estate.Scenario.SiteID, CollectorID: "snmp-test", Targets: snmpTargets(t, server, estate)}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	first, err := plugin.Discover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertNoSNMPErrors(t, first)
	firstHosts := hostNativeIDs(first)
	if len(firstHosts) != len(estate.Hosts) {
		t.Fatalf("got %d distinct host assets, want %d", len(firstHosts), len(estate.Hosts))
	}
	if len(first.Assets) != len(estate.Hosts)*2 || len(first.Relationships) != len(estate.Hosts) {
		t.Fatalf("got %d assets and %d relationships, want %d and %d", len(first.Assets), len(first.Relationships), len(estate.Hosts)*2, len(estate.Hosts))
	}

	repository := store.NewMemory()
	if err := repository.SaveObservation(ctx, first); err != nil {
		t.Fatal(err)
	}

	second, err := plugin.Discover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertNoSNMPErrors(t, second)
	secondHosts := hostNativeIDs(second)
	if len(secondHosts) != len(firstHosts) {
		t.Fatalf("got %d distinct host assets on second scan, want %d", len(secondHosts), len(firstHosts))
	}
	for id := range firstHosts {
		if !secondHosts[id] {
			t.Fatalf("host asset %q from first scan is missing from second scan; SNMPv3 engine ID identity is not stable", id)
		}
	}
	if err := repository.SaveObservation(ctx, second); err != nil {
		t.Fatal(err)
	}

	assets, err := repository.ListAssets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := len(estate.Hosts) * 2
	if len(assets) != want {
		t.Fatalf("repeated SNMP scan created duplicates: got %d assets, want %d", len(assets), want)
	}
}

func TestSNMPPermissionFailureReturnsPartialInventory(t *testing.T) {
	scenario := lab.DefaultScenario(1, 0, 7)
	scenario.Faults.PermissionDeniedPercent = 100
	estate := makeEstate(t, scenario)
	observation := discoverOneSNMP(t, estate)
	if len(observation.Assets) != 1 || len(observation.Relationships) != 0 || len(observation.Errors) != 1 {
		t.Fatalf("got assets=%d relationships=%d errors=%d, want host-only partial inventory", len(observation.Assets), len(observation.Relationships), len(observation.Errors))
	}
	if observation.Errors[0].Code != "snmp_partial" || !observation.Errors[0].Retryable {
		t.Fatalf("unexpected partial error: %#v", observation.Errors[0])
	}
}

func TestSNMPMalformedAndUnreachableFailuresAreIsolated(t *testing.T) {
	for _, test := range []struct {
		name  string
		fault lab.Faults
		code  string
	}{
		{name: "malformed", fault: lab.Faults{MalformedPercent: 100}, code: "snmp_parse"},
		{name: "auth_failure", fault: lab.Faults{AuthFailurePercent: 100}, code: "snmp_operation"},
		{name: "timeout", fault: lab.Faults{TimeoutPercent: 100}, code: "snmp_operation"},
	} {
		t.Run(test.name, func(t *testing.T) {
			scenario := lab.DefaultScenario(1, 0, 8)
			scenario.Faults = test.fault
			estate := makeEstate(t, scenario)
			observation := discoverOneSNMP(t, estate)
			if len(observation.Assets) != 0 || len(observation.Errors) != 1 || observation.Errors[0].Code != test.code {
				t.Fatalf("unexpected observation: %#v", observation)
			}
		})
	}
}

func TestSNMPProductionConfigurationRejectsLabModeTarget(t *testing.T) {
	p := snmp.Plugin{Config: snmp.Config{
		Username: "prod", SecurityLevel: snmp.SecurityLevelAuthPriv,
		AuthProtocol: snmp.AuthProtocolSHA, AuthPassphrase: "password123",
		PrivProtocol: snmp.PrivProtocolAES, PrivPassphrase: "password123",
	}}
	err := p.ValidateConfiguration(context.Background(), discovery.Request{Targets: []string{"203.0.113.5:161"}})
	if err != nil {
		t.Fatalf("unexpected error for a valid production config: %v", err)
	}
}

func labPlugin() snmp.Plugin {
	return snmp.Plugin{Config: snmp.Config{
		Username:      lab.LabSNMPUsername,
		SecurityLevel: snmp.SecurityLevelNoAuthNoPriv,
		LabMode:       true,
		Concurrency:   64,
		Timeout:       500 * time.Millisecond,
		Retries:       0,
	}}
}

func discoverOneSNMP(t *testing.T, estate lab.Estate) model.ObservationEnvelope {
	t.Helper()
	server := makeSNMPServer(t, estate)
	plugin := labPlugin()
	observation, err := plugin.Discover(context.Background(), discovery.Request{SiteID: "lab", Targets: snmpTargets(t, server, estate)})
	if err != nil {
		t.Fatal(err)
	}
	return observation
}

func snmpTargets(t *testing.T, server *lab.SNMPServer, estate lab.Estate) []string {
	t.Helper()
	targets := make([]string, 0, len(estate.Hosts))
	for _, host := range estate.Hosts {
		addr, ok := server.Address(host.ID)
		if !ok {
			t.Fatalf("no SNMP address for host %s", host.ID)
		}
		targets = append(targets, addr)
	}
	return targets
}

func makeEstate(t *testing.T, scenario lab.Scenario) lab.Estate {
	t.Helper()
	estate, err := lab.Generate(scenario)
	if err != nil {
		t.Fatal(err)
	}
	return estate
}

func makeSNMPServer(t *testing.T, estate lab.Estate) *lab.SNMPServer {
	t.Helper()
	server, err := lab.NewSNMPServer(estate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return server
}

func assertNoSNMPErrors(t *testing.T, observation model.ObservationEnvelope) {
	t.Helper()
	if len(observation.Errors) != 0 {
		t.Fatalf("SNMP discovery returned %d errors; first: %#v", len(observation.Errors), observation.Errors[0])
	}
}

func hostNativeIDs(observation model.ObservationEnvelope) map[string]bool {
	ids := map[string]bool{}
	for _, asset := range observation.Assets {
		if asset.Type == model.AssetHost {
			ids[asset.NativeID] = true
		}
	}
	return ids
}
