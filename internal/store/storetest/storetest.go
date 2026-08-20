// Package storetest is a shared black-box conformance suite for
// store.Repository implementations. Both Memory and the SQLite-backed
// store run the exact same assertions through the Repository interface
// alone, so the two cannot silently diverge in observable behavior — a
// bug introduced in one backend's resolution logic that the other doesn't
// share is caught here rather than by users noticing inconsistent
// GET /v1/assets results depending on which backend they configured.
package storetest

import (
	"context"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/audit"
	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/model"
)

// Run executes the full conformance suite against a fresh repository
// produced by newRepo for each subtest.
func Run(t *testing.T, newRepo func(t *testing.T) store.Repository) {
	t.Helper()
	t.Run("EmptyRepositoryReturnsEmptyNotError", func(t *testing.T) { testEmpty(t, newRepo(t)) })
	t.Run("SaveObservationRoundTrips", func(t *testing.T) { testRoundTrip(t, newRepo(t)) })
	t.Run("ListAssetsResolvesRepeatedObservations", func(t *testing.T) { testAssetResolution(t, newRepo(t)) })
	t.Run("ListRelationshipsResolvesRepeatedObservations", func(t *testing.T) { testRelationshipResolution(t, newRepo(t)) })
	t.Run("DistinctAssetsAndRelationshipsStaySeparate", func(t *testing.T) { testDistinctEntities(t, newRepo(t)) })
	t.Run("ConcurrentSaveObservationIsSafe", func(t *testing.T) { testConcurrentSave(t, newRepo(t)) })
	t.Run("ResubmittingTheSameObservationIDIsIdempotent", func(t *testing.T) { testIdempotentResubmission(t, newRepo(t)) })
	t.Run("AppendAuditEventBuildsAVerifiableHashChain", func(t *testing.T) { testAuditChain(t, newRepo(t)) })
	t.Run("ConcurrentAppendAuditEventIsSafe", func(t *testing.T) { testConcurrentAuditAppend(t, newRepo(t)) })
}

func testEmpty(t *testing.T, repo store.Repository) {
	t.Helper()
	ctx := context.Background()
	observations, err := repo.ListObservations(ctx)
	if err != nil || len(observations) != 0 {
		t.Fatalf("ListObservations on empty repo: %v, %v", observations, err)
	}
	assets, err := repo.ListAssets(ctx)
	if err != nil || len(assets) != 0 {
		t.Fatalf("ListAssets on empty repo: %v, %v", assets, err)
	}
	relationships, err := repo.ListRelationships(ctx)
	if err != nil || len(relationships) != 0 {
		t.Fatalf("ListRelationships on empty repo: %v, %v", relationships, err)
	}
	entries, err := repo.ListAuditEntries(ctx)
	if err != nil || len(entries) != 0 {
		t.Fatalf("ListAuditEntries on empty repo: %v, %v", entries, err)
	}
}

func testRoundTrip(t *testing.T, repo store.Repository) {
	t.Helper()
	ctx := context.Background()
	envelope := sampleEnvelope("obs-1", "host-1", "web-1")
	if err := repo.SaveObservation(ctx, envelope); err != nil {
		t.Fatal(err)
	}
	observations, err := repo.ListObservations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 {
		t.Fatalf("got %d observations, want 1", len(observations))
	}
	got := observations[0]
	if got.ObservationID != envelope.ObservationID || got.SiteID != envelope.SiteID || got.CollectorID != envelope.CollectorID || got.Plugin != envelope.Plugin {
		t.Fatalf("observation round-trip mismatch: got %#v, want %#v", got, envelope)
	}
	if !got.ObservedAt.Equal(envelope.ObservedAt) {
		t.Fatalf("observed_at mismatch: got %v, want %v", got.ObservedAt, envelope.ObservedAt)
	}
	if len(got.Assets) != len(envelope.Assets) || len(got.Relationships) != len(envelope.Relationships) {
		t.Fatalf("asset/relationship count mismatch: got %d/%d, want %d/%d", len(got.Assets), len(got.Relationships), len(envelope.Assets), len(envelope.Relationships))
	}
	if !reflect.DeepEqual(got.Assets[0].Identifiers, envelope.Assets[0].Identifiers) {
		t.Fatalf("asset identifiers did not round-trip: got %#v, want %#v", got.Assets[0].Identifiers, envelope.Assets[0].Identifiers)
	}
}

func testAssetResolution(t *testing.T, repo store.Repository) {
	t.Helper()
	ctx := context.Background()
	first := sampleEnvelope("obs-1", "host-1", "web-1")
	second := sampleEnvelope("obs-2", "host-1", "web-1")
	second.Assets[0].Attributes = map[string]any{"changed": true}
	if err := repo.SaveObservation(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveObservation(ctx, second); err != nil {
		t.Fatal(err)
	}
	assets, err := repo.ListAssets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	hostAssets := filterAssets(assets, model.AssetHost)
	if len(hostAssets) != 1 {
		t.Fatalf("got %d host assets across two observations of the same host, want 1 (deduplicated)", len(hostAssets))
	}
	resolved := hostAssets[0]
	if resolved.FirstObservationID != "obs-1" || resolved.LastObservationID != "obs-2" {
		t.Fatalf("got first=%q last=%q, want first=obs-1 last=obs-2", resolved.FirstObservationID, resolved.LastObservationID)
	}
	if !reflect.DeepEqual(resolved.Asset.Attributes, map[string]any{"changed": true}) {
		t.Fatalf("resolved asset did not pick up the latest observation's attributes: %#v", resolved.Asset.Attributes)
	}
}

func testRelationshipResolution(t *testing.T, repo store.Repository) {
	t.Helper()
	ctx := context.Background()
	first := sampleEnvelope("obs-1", "host-1", "web-1")
	second := sampleEnvelope("obs-2", "host-1", "web-1")
	if err := repo.SaveObservation(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveObservation(ctx, second); err != nil {
		t.Fatal(err)
	}
	relationships, err := repo.ListRelationships(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(relationships) != 1 {
		t.Fatalf("got %d relationships across two observations of the same relationship, want 1 (deduplicated)", len(relationships))
	}
	resolved := relationships[0]
	if resolved.FirstObservationID != "obs-1" || resolved.LastObservationID != "obs-2" {
		t.Fatalf("got first=%q last=%q, want first=obs-1 last=obs-2", resolved.FirstObservationID, resolved.LastObservationID)
	}
	if resolved.Relationship.Type != "host_has_interface" || resolved.Relationship.FromNativeID != "host-1" {
		t.Fatalf("unexpected relationship: %#v", resolved.Relationship)
	}
}

func testDistinctEntities(t *testing.T, repo store.Repository) {
	t.Helper()
	ctx := context.Background()
	if err := repo.SaveObservation(ctx, sampleEnvelope("obs-1", "host-1", "web-1")); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveObservation(ctx, sampleEnvelope("obs-2", "host-2", "web-2")); err != nil {
		t.Fatal(err)
	}
	assets, err := repo.ListAssets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(filterAssets(assets, model.AssetHost)) != 2 {
		t.Fatalf("got %d host assets for two distinct hosts, want 2", len(filterAssets(assets, model.AssetHost)))
	}
	relationships, err := repo.ListRelationships(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(relationships) != 2 {
		t.Fatalf("got %d relationships for two distinct hosts, want 2", len(relationships))
	}
}

func testConcurrentSave(t *testing.T, repo store.Repository) {
	t.Helper()
	ctx := context.Background()
	const workers = 16
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "obs-concurrent-" + string(rune('a'+i))
			env := sampleEnvelope(id, "host-1", "web-1")
			if err := repo.SaveObservation(ctx, env); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	observations, err := repo.ListObservations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != workers {
		t.Fatalf("got %d observations after %d concurrent saves, want %d", len(observations), workers, workers)
	}
	assets, err := repo.ListAssets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(filterAssets(assets, model.AssetHost)) != 1 {
		t.Fatalf("got %d host assets after %d concurrent saves of the same host, want 1 (deduplicated)", len(filterAssets(assets, model.AssetHost)), workers)
	}
}

func testIdempotentResubmission(t *testing.T, repo store.Repository) {
	t.Helper()
	ctx := context.Background()
	envelope := sampleEnvelope("obs-1", "host-1", "web-1")
	if err := repo.SaveObservation(ctx, envelope); err != nil {
		t.Fatal(err)
	}
	// A retried delivery of the exact same observation_id must replace, not
	// duplicate — a collector cannot tell whether an earlier delivery
	// attempt actually landed before it retried.
	if err := repo.SaveObservation(ctx, envelope); err != nil {
		t.Fatal(err)
	}
	observations, err := repo.ListObservations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 {
		t.Fatalf("got %d observations after resubmitting the same observation_id twice, want 1", len(observations))
	}
}

func testAuditChain(t *testing.T, repo store.Repository) {
	t.Helper()
	ctx := context.Background()
	actions := []string{"enrollment_token_issued", "collector_enrolled", "certificate_rotated", "job_created"}
	for i, action := range actions {
		entry, err := repo.AppendAuditEvent(ctx, audit.Event{Action: action, Actor: "tester", Detail: map[string]string{"i": string(rune('a' + i))}})
		if err != nil {
			t.Fatal(err)
		}
		if entry.Sequence != int64(i+1) {
			t.Fatalf("append %d: got sequence %d, want %d", i, entry.Sequence, i+1)
		}
	}
	entries, err := repo.ListAuditEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(actions) {
		t.Fatalf("got %d audit entries, want %d", len(entries), len(actions))
	}
	for i, e := range entries {
		if e.Action != actions[i] {
			t.Fatalf("entry %d: got action %q, want %q", i, e.Action, actions[i])
		}
	}
	if err := audit.VerifyChain(entries); err != nil {
		t.Fatalf("VerifyChain on entries returned by ListAuditEntries: %v", err)
	}
}

func testConcurrentAuditAppend(t *testing.T, repo store.Repository) {
	t.Helper()
	ctx := context.Background()
	const workers = 16
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := repo.AppendAuditEvent(ctx, audit.Event{Action: "job_created", Actor: "worker"}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	entries, err := repo.ListAuditEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != workers {
		t.Fatalf("got %d audit entries after %d concurrent appends, want %d", len(entries), workers, workers)
	}
	if err := audit.VerifyChain(entries); err != nil {
		t.Fatalf("VerifyChain after concurrent appends: %v", err)
	}
}

func filterAssets(assets []store.ResolvedAsset, assetType model.AssetType) []store.ResolvedAsset {
	out := make([]store.ResolvedAsset, 0, len(assets))
	for _, a := range assets {
		if a.Asset.Type == assetType {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func sampleEnvelope(observationID, hostNativeID, hostName string) model.ObservationEnvelope {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ifaceNativeID := hostNativeID + ":interface:1"
	evidence := []model.Evidence{{Source: "storetest", Collected: now, Confidence: 1}}
	return model.ObservationEnvelope{
		SchemaVersion: model.SchemaVersion,
		ObservationID: observationID,
		SiteID:        "lab",
		CollectorID:   "storetest-relay",
		Plugin:        "storetest",
		ObservedAt:    now,
		Assets: []model.Asset{
			{Type: model.AssetHost, NativeID: hostNativeID, Name: hostName, Identifiers: map[string]string{"hostname": hostName}, Attributes: map[string]any{"os": "linux"}, Evidence: evidence},
			{Type: model.AssetNetworkInterface, NativeID: ifaceNativeID, Name: "eth0", Identifiers: map[string]string{"host_native_id": hostNativeID}, Evidence: evidence},
		},
		Relationships: []model.Relationship{
			{Type: "host_has_interface", FromNativeID: hostNativeID, ToNativeID: ifaceNativeID, Evidence: evidence},
		},
	}
}
