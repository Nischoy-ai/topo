package aws_test

import (
	"context"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/store"
	awsdiscovery "github.com/Nischoy-ai/topo/pkg/discovery/aws"

	"github.com/Nischoy-ai/topo/pkg/discovery"
	"github.com/Nischoy-ai/topo/pkg/lab"
	"github.com/Nischoy-ai/topo/pkg/model"
)

// TestDiscoverOrganizationInventoryOverRealAPIFixture proves the plugin's
// real aws-sdk-go-v2 SigV4-signed request construction and AWS-JSON-1.1
// response decoding are correct against a real wire-protocol fixture that
// itself performs genuine signature verification (see
// docs/project-plan.md's AWS Organizations slice write-up for why a
// hand-rolled fixture, matching the Kubernetes/SNMP precedent, was used
// instead of LocalStack or a real AWS account).
func TestDiscoverOrganizationInventoryOverRealAPIFixture(t *testing.T) {
	target, wantAccounts, cleanup := startFixture(t, 25)
	defer cleanup()

	plugin := validLabPlugin(target)
	request := discovery.Request{SiteID: "lab", CollectorID: "aws-test", Targets: []string{target}}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	first, err := plugin.Discover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertNoErrors(t, first)
	// organization + root + 4 fixed OUs + one account per host
	wantAssets := wantAccounts + 6
	if len(first.Assets) != wantAssets {
		t.Fatalf("got %d assets, want %d", len(first.Assets), wantAssets)
	}
	wantRelationships := wantAccounts + 5 // root->org, 4x ou->parent, account->parent each
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
			t.Fatalf("asset %q from the first scan is missing from the second scan; AWS Organizations identity is not stable", id)
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
		t.Fatalf("repeated AWS Organizations scan created duplicates: got %d stored assets, want %d", len(assets), len(firstIDs))
	}
}

// TestDiscoverOrganizationAtScale mirrors every other protocol's 500-host
// acceptance scale (see examples/lab/clean-500.json), here as 500 member
// accounts spread across the fixture's fixed OU hierarchy — proving the
// recursive OU/account walk and its pagination-free listing bound hold at
// the project's standard acceptance scale, not only in a small unit test.
func TestDiscoverOrganizationAtScale(t *testing.T) {
	target, wantAccounts, cleanup := startFixture(t, 500)
	defer cleanup()

	plugin := validLabPlugin(target)
	plugin.Config.Concurrency = 4
	request := discovery.Request{SiteID: "lab", CollectorID: "aws-test", Targets: []string{target}}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	observation, err := plugin.Discover(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertNoErrors(t, observation)
	if wantAccounts != 500 {
		t.Fatalf("fixture built %d accounts, want 500", wantAccounts)
	}
	if len(observation.Assets) != wantAccounts+6 {
		t.Fatalf("got %d assets, want %d", len(observation.Assets), wantAccounts+6)
	}
	if ids := nativeIDs(observation); len(ids) != wantAccounts+6 {
		t.Fatalf("got %d distinct native IDs, want %d — duplicate identity at scale", len(ids), wantAccounts+6)
	}
}

func TestAWSWrongSecretIsIsolatedAsOperationError(t *testing.T) {
	target, _, cleanup := startFixture(t, 5)
	defer cleanup()

	plugin := validLabPlugin(target)
	plugin.Config.SecretAccessKey = "definitely-wrong-secret-access-key"
	observation, err := plugin.Discover(context.Background(), discovery.Request{Targets: []string{target}})
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Assets) != 0 || len(observation.Errors) != 1 || observation.Errors[0].Code != "aws_organizations_operation" {
		t.Fatalf("unexpected observation: %#v", observation)
	}
}

func TestAWSUnreachableTargetIsIsolatedAndRetryable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close() // nothing is listening on this port by the time Discover dials it

	plugin := validLabPlugin("http://" + addr)
	observation, err := plugin.Discover(context.Background(), discovery.Request{Targets: []string{"http://" + addr}})
	if err != nil {
		t.Fatal(err)
	}
	if len(observation.Assets) != 0 || len(observation.Errors) != 1 || observation.Errors[0].Code != "aws_organizations_operation" || !observation.Errors[0].Retryable {
		t.Fatalf("unexpected observation: %#v", observation)
	}
}

func validLabPlugin(target string) awsdiscovery.Plugin {
	return awsdiscovery.Plugin{Config: awsdiscovery.Config{
		AccessKeyID: lab.LabAWSAccessKeyID, SecretAccessKey: lab.LabAWSSecretAccessKey,
		SessionToken: lab.LabAWSSessionToken, Region: lab.LabAWSRegion,
		LabMode: true, Concurrency: 4, OperationTimeout: 10 * time.Second,
	}}
}

// startFixture builds a small deterministic estate, serves it over the
// Topo Lab AWS Organizations fixture (real HTTP + real AWS-JSON-1.1 wire
// format + real SigV4 verification), and returns a target URL with no
// embedded credentials — mirroring how the plugin itself is used.
func startFixture(t *testing.T, hostCount int) (target string, wantAccounts int, cleanup func()) {
	t.Helper()
	scenario := lab.DefaultScenario(hostCount, 0, 1)
	estate, err := lab.Generate(scenario)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(lab.NewAWSOrganizationsServer(estate).Handler())
	return server.URL, len(estate.Hosts), server.Close
}

func assertNoErrors(t *testing.T, observation model.ObservationEnvelope) {
	t.Helper()
	if len(observation.Errors) != 0 {
		t.Fatalf("AWS Organizations discovery returned %d errors; first: %#v", len(observation.Errors), observation.Errors[0])
	}
}

func nativeIDs(observation model.ObservationEnvelope) map[string]bool {
	ids := map[string]bool{}
	for _, asset := range observation.Assets {
		ids[string(asset.Type)+":"+asset.NativeID] = true
	}
	return ids
}
