package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nischoy-ai/topo/internal/audit"
	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/internal/store/sqlite"
	"github.com/Nischoy-ai/topo/internal/store/storetest"
	"github.com/Nischoy-ai/topo/pkg/model"
)

func TestSQLiteConformsToRepository(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Repository {
		t.Helper()
		s, err := sqlite.Open(":memory:")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	})
}

func TestSQLiteDataSurvivesReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "topo.db")

	first, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	envelope := model.ObservationEnvelope{
		SchemaVersion: model.SchemaVersion,
		ObservationID: "obs-1",
		SiteID:        "lab",
		CollectorID:   "test",
		Plugin:        "test",
		ObservedAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Assets:        []model.Asset{{Type: model.AssetHost, NativeID: "host-1", Name: "web-1"}},
		Relationships: []model.Relationship{{Type: "host_has_interface", FromNativeID: "host-1", ToNativeID: "host-1:interface:1"}},
	}
	if err := first.SaveObservation(context.Background(), envelope); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	observations, err := second.ListObservations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 || observations[0].ObservationID != "obs-1" {
		t.Fatalf("observation did not survive reopen: %#v", observations)
	}
	assets, err := second.ListAssets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || assets[0].Asset.NativeID != "host-1" {
		t.Fatalf("asset did not survive reopen: %#v", assets)
	}
	relationships, err := second.ListRelationships(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(relationships) != 1 || relationships[0].Relationship.Type != "host_has_interface" {
		t.Fatalf("relationship did not survive reopen: %#v", relationships)
	}
}

func TestSQLiteAuditLogSurvivesReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "topo.db")

	first, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"enrollment_token_issued", "collector_enrolled"} {
		if _, err := first.AppendAuditEvent(context.Background(), audit.Event{Action: action, Actor: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	entries, err := second.ListAuditEntries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d audit entries after reopen, want 2", len(entries))
	}
	if err := audit.VerifyChain(entries); err != nil {
		t.Fatalf("VerifyChain after reopen: %v", err)
	}

	// A third entry appended after reopen must chain onto the entries
	// written before the restart, not restart the chain from scratch.
	third, err := second.AppendAuditEvent(context.Background(), audit.Event{Action: "job_created", Actor: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if third.Sequence != 3 || third.PrevHash != entries[1].Hash {
		t.Fatalf("post-reopen append did not continue the existing chain: %#v", third)
	}
}

// TestSQLiteMigratesExistingDatabaseToLatestSchema simulates a database
// created by an earlier binary that only knew about schema version 1 (no
// audit_entries table) and confirms the current binary upgrades it in
// place rather than requiring the database to be recreated.
func TestSQLiteMigratesExistingDatabaseToLatestSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "topo.db")

	v1, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v1.DB().Exec(`DROP TABLE audit_entries`); err != nil {
		t.Fatal(err)
	}
	if _, err := v1.DB().Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	if err := v1.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("opening a version-1 database with the current binary should upgrade it in place: %v", err)
	}
	defer upgraded.Close()

	entry, err := upgraded.AppendAuditEvent(context.Background(), audit.Event{Action: "job_created", Actor: "test"})
	if err != nil {
		t.Fatalf("audit_entries table missing after upgrade: %v", err)
	}
	if entry.Sequence != 1 {
		t.Fatalf("got sequence %d, want 1", entry.Sequence)
	}
}

func TestSQLiteRejectsNewerSchemaVersion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "topo.db")
	s, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate a database written by a future Topo version with a newer
	// schema than this binary understands.
	bumped, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bumped.DB().Exec(`PRAGMA user_version = 999`); err != nil {
		t.Fatal(err)
	}
	if err := bumped.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := sqlite.Open(dbPath); err == nil {
		t.Fatal("expected an error opening a database with a newer schema version than this binary supports")
	}
}
