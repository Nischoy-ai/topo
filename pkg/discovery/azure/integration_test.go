package azure_test

import (
	"context"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/store"
	azuredisc "github.com/Nischoy-ai/topo/pkg/discovery/azure"

	"github.com/Nischoy-ai/topo/pkg/discovery"
	"github.com/Nischoy-ai/topo/pkg/lab"
	"github.com/Nischoy-ai/topo/pkg/model"
)

// TestDiscoverTenantInventoryOverRealAPIFixture proves the plugin's real
// azidentity OAuth2 client-credentials token acquisition and
// azure-sdk-for-go ARM request construction/response decoding are correct
// against a real wire-protocol fixture. See docs/project-plan.md's Azure
// slice write-up for why a hand-rolled fixture, matching the
// Kubernetes/AWS precedent, was used instead of a real Azure tenant.
func TestDiscoverTenantInventoryOverRealAPIFixture(t *testing.T) {
	target, wantSubscriptions, cleanup := startFixture(t, 25)
	defer cleanup()

	plugin := validLabPlugin(target)
	request := discovery.Request{SiteID: "lab", CollectorID: "azure-test", Targets: []string{target}}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	first, err := plugin.Discover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertNoErrors(t, first)
	// tenant + 5 fixed management groups (including the root) + one subscription per host
	wantAssets := wantSubscriptions + 6
	if len(first.Assets) != wantAssets {
		t.Fatalf("got %d assets, want %d", len(first.Assets), wantAssets)
	}
	wantRelationships := wantSubscriptions + 5 // root->tenant, 4x mg->parent, subscription->parent each
	if len(first.Relationships) != wantRelationships {
		t.Fatalf("got %d relationships, want %d", len(first.Relationships), wantRelationships)
	}

	repository := store.NewMemory()
	if err := repository.SaveObservation(ctx, first); err != nil {
		t.Fatal(err)
	}

	second, err := plugin.Discover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertNoErrors(t, second)

	firstIDs := nativeIDs(first)
	secondIDs := nativeIDs(second)
	if len(firstIDs) != len(secondIDs) {
		t.Fatalf("got %d distinct native IDs on second scan, want %d", len(secondIDs), len(firstIDs))
	}
	for id := range firstIDs {
		if !secondIDs[id] {
			t.Fatalf("asset %q from the first scan is missing from the second scan; Azure identity is not stable", id)
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
		t.Fatalf("repeated Azure scan created duplicates: got %d stored assets, want %d", len(assets), len(firstIDs))
	}
}

// TestDiscoverTenantAtScale mirrors every other protocol's 500-host
// acceptance scale (see examples/lab/clean-500.json), here as 500
// subscriptions spread across the fixture's fixed management-group
// hierarchy.
func TestDiscoverTenantAtScale(t *testing.T) {
	target, wantSubscriptions, cleanup := startFixture(t, 500)
	defer cleanup()

	plugin := validLabPlugin(target)
	plugin.Config.Concurrency = 4
	request := discovery.Request{SiteID: "lab", CollectorID: "azure-test", Targets: []string{target}}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	observation, err := plugin.Discover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertNoErrors(t, observation)
	if wantSubscriptions != 500 {
		t.Fatalf("fixture built %d subscriptions, want 500", wantSubscriptions)
	}
	if len(observation.Assets) != wantSubscriptions+6 {
		t.Fatalf("got %d assets, want %d", len(observation.Assets), wantSubscriptions+6)
	}
	if ids := nativeIDs(observation); len(ids) != wantSubscriptions+6 {
		t.Fatalf("got %d distinct native IDs, want %d — duplicate identity at scale", len(ids), wantSubscriptions+6)
	}
}

func TestAzureWrongClientSecretIsIsolatedAsOperationError(t *testing.T) {
	target, _, cleanup := startFixture(t, 5)
	defer cleanup()

	plugin := validLabPlugin(target)
	plugin.Config.ClientSecret = "definitely-wrong-client-secret"
	observation, err := plugin.Discover(context.Background(), discovery.Request{Targets: []string{target}})
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Assets) != 0 || len(observation.Errors) != 1 || observation.Errors[0].Code != "azure_operation" {
		t.Fatalf("unexpected observation: %#v", observation)
	}
}

// TestAzureUnreachableTargetIsIsolatedAndRetryable points the ARM target
// (not the authority) at a dead port, so token acquisition against the
// real Lab authority succeeds and the failure happens on the subsequent
// plain ARM data call — a real net.Error, not azidentity's
// AuthenticationFailedError. That distinction matters: azidentity
// deliberately marks every authentication-phase failure non-retryable
// (see AuthenticationFailedError.NonRetriable in the SDK), including a
// transient network failure encountered while acquiring a token, since it
// cannot distinguish "the authority is briefly unreachable" from "these
// credentials are wrong" — so an unreachable *authority* is correctly
// non-retryable in this plugin's own classification too. An unreachable
// *ARM target* reached after a token was already obtained carries no such
// ambiguity and is retryable, the same as Kubernetes's and AWS's.
func TestAzureUnreachableTargetIsIsolatedAndRetryable(t *testing.T) {
	authority, _, cleanup := startFixture(t, 5)
	defer cleanup()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadTarget := "https://" + listener.Addr().String()
	listener.Close() // nothing is listening on this port by the time Discover dials it

	plugin := validLabPlugin(authority)
	observation, err := plugin.Discover(context.Background(), discovery.Request{Targets: []string{deadTarget}})
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Assets) != 0 || len(observation.Errors) != 1 || observation.Errors[0].Code != "azure_operation" || !observation.Errors[0].Retryable {
		t.Fatalf("unexpected observation: %#v", observation)
	}
}

// TestAzureUnreachableAuthorityIsIsolatedAsNonRetryable documents the
// counterpart case: when the authority itself cannot be reached, token
// acquisition fails and azidentity classifies that failure non-retryable
// (see the longer explanation on TestAzureUnreachableTargetIsIsolatedAndRetryable).
func TestAzureUnreachableAuthorityIsIsolatedAsNonRetryable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAuthority := "https://" + listener.Addr().String()
	listener.Close()

	plugin := validLabPlugin(deadAuthority)
	observation, err := plugin.Discover(context.Background(), discovery.Request{Targets: []string{deadAuthority}})
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Assets) != 0 || len(observation.Errors) != 1 || observation.Errors[0].Code != "azure_operation" || observation.Errors[0].Retryable {
		t.Fatalf("unexpected observation: %#v", observation)
	}
}

func validLabPlugin(target string) azuredisc.Plugin {
	return azuredisc.Plugin{Config: azuredisc.Config{
		TenantID: lab.LabAzureTenantID, ClientID: lab.LabAzureClientID, ClientSecret: lab.LabAzureClientSecret,
		AuthorityURL: target, LabMode: true, Concurrency: 4, OperationTimeout: 10 * time.Second,
	}}
}

// startFixture builds a small deterministic estate, serves it over the
// Topo Lab Azure fixture (real HTTPS + real ARM wire format + real OAuth2
// client-credentials authentication), and returns a target URL with no
// embedded credentials — mirroring how the plugin itself is used.
func startFixture(t *testing.T, hostCount int) (target string, wantSubscriptions int, cleanup func()) {
	t.Helper()
	scenario := lab.DefaultScenario(hostCount, 0, 1)
	estate, err := lab.Generate(scenario)
	if err != nil {
		t.Fatal(err)
	}
	server := lab.NewAzureServer(estate)
	ts := httptest.NewTLSServer(server.Handler())
	server.BaseURL = ts.URL
	return ts.URL, len(estate.Hosts), ts.Close
}

func assertNoErrors(t *testing.T, observation model.ObservationEnvelope) {
	t.Helper()
	if len(observation.Errors) != 0 {
		t.Fatalf("Azure discovery returned %d errors; first: %#v", len(observation.Errors), observation.Errors[0])
	}
}

func nativeIDs(observation model.ObservationEnvelope) map[string]bool {
	ids := map[string]bool{}
	for _, asset := range observation.Assets {
		ids[string(asset.Type)+":"+asset.NativeID] = true
	}
	return ids
}
