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

// TestSQLiteMigratesExistingDatabaseFromV1ToLatestSchema simulates a
// database created by the binary that shipped only persistent-storage
// slice 1 (schema version 1: observations/assets/relationships only, no
// audit_entries or schedules table) and confirms the current binary
// upgrades it through every intervening version in place, rather than
// requiring the database to be recreated. Dropping every table added by a
// later migration keeps this valid as a "what did a real v1 database look
// like" simulation regardless of how many schema versions exist today.
func TestSQLiteMigratesExistingDatabaseFromV1ToLatestSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "topo.db")

	v1, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v1.DB().Exec(`DROP TABLE audit_entries; DROP TABLE schedules; DROP TABLE certificate_revocations`); err != nil {
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
	if err := upgraded.UpsertSchedule(context.Background(), store.Schedule{CollectorID: "collector-1", JobType: "discover", IntervalSeconds: 60}); err != nil {
		t.Fatalf("schedules table missing after upgrade: %v", err)
	}
}

// TestSQLiteMigratesExistingDatabaseFromV2ToLatestSchema is the more
// realistic near-term upgrade: a database written by the binary that
// shipped audit-log slice 2 (schema version 2, no schedules table yet)
// upgraded by the binary that adds slice 3.
func TestSQLiteMigratesExistingDatabaseFromV2ToLatestSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "topo.db")

	v2, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v2.DB().Exec(`DROP TABLE schedules; DROP TABLE certificate_revocations`); err != nil {
		t.Fatal(err)
	}
	if _, err := v2.DB().Exec(`PRAGMA user_version = 2`); err != nil {
		t.Fatal(err)
	}
	if err := v2.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("opening a version-2 database with the current binary should upgrade it in place: %v", err)
	}
	defer upgraded.Close()

	if err := upgraded.UpsertSchedule(context.Background(), store.Schedule{CollectorID: "collector-1", JobType: "discover", IntervalSeconds: 60}); err != nil {
		t.Fatalf("schedules table missing after upgrade: %v", err)
	}
}

func TestSQLiteMigratesExistingDatabaseFromV3ToLatestSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "topo.db")
	v3, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v3.DB().Exec(`DROP TABLE certificate_revocations`); err != nil {
		t.Fatal(err)
	}
	if _, err := v3.DB().Exec(`PRAGMA user_version = 3`); err != nil {
		t.Fatal(err)
	}
	if err := v3.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("opening a version-3 database with the current binary should upgrade it in place: %v", err)
	}
	defer upgraded.Close()
	created, err := upgraded.RevokeCertificate(context.Background(), store.CertificateRevocation{
		SerialNumber: "ab",
		Reason:       "migration check",
		RevokedAt:    time.Now().UTC(),
	})
	if err != nil || !created {
		t.Fatalf("certificate_revocations table missing after upgrade: created %v, err %v", created, err)
	}
}

func TestSQLiteCertificateRevocationsSurviveReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "topo.db")
	revocation := store.CertificateRevocation{
		SerialNumber: "deadbeef",
		Reason:       "compromise recovery test",
		RevokedAt:    time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
	}
	first, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if created, err := first.RevokeCertificate(context.Background(), revocation); err != nil || !created {
		t.Fatalf("RevokeCertificate = %v, %v", created, err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	revoked, err := second.IsCertificateRevoked(context.Background(), revocation.SerialNumber)
	if err != nil || !revoked {
		t.Fatalf("revocation did not survive reopen: revoked %v, err %v", revoked, err)
	}
	items, err := second.ListCertificateRevocations(context.Background())
	if err != nil || len(items) != 1 || items[0].Reason != revocation.Reason || !items[0].RevokedAt.Equal(revocation.RevokedAt) {
		t.Fatalf("revocation round trip after reopen = %#v, %v", items, err)
	}
}

func TestSQLiteSchedulesSurviveReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "topo.db")

	first, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	sched := store.Schedule{
		CollectorID:     "collector-1",
		JobType:         "discover",
		IntervalSeconds: 3600,
		NextRunAt:       time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
		CreatedAt:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := first.UpsertSchedule(context.Background(), sched); err != nil {
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

	got, err := second.GetSchedule(context.Background(), "collector-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.CollectorID != sched.CollectorID || got.IntervalSeconds != sched.IntervalSeconds || !got.NextRunAt.Equal(sched.NextRunAt) {
		t.Fatalf("schedule did not survive reopen: got %#v, want %#v", got, sched)
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
