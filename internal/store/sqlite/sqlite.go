// Package sqlite implements store.Repository backed by SQLite, for
// single-controller deployments that need discovery data to survive a
// restart. It uses modernc.org/sqlite, a pure-Go SQLite implementation
// with no cgo dependency — required for this project's
// GOOS=windows cross-compile CI check to keep working, since a cgo-based
// driver would need a Windows C cross-compiler this CI does not have.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Nischoy-ai/topo/internal/audit"
	"github.com/Nischoy-ai/topo/internal/store"
	"github.com/Nischoy-ai/topo/pkg/model"
)

// schemaVersion is tracked via SQLite's own PRAGMA user_version. A
// dedicated migration framework is unwarranted complexity for this
// project's single-controller deployment shape until there is more than
// one schema revision to manage in practice; migrations (below) and
// migrate are the seam a future migration would extend.
const schemaVersion = 3

const migrationV1 = `
CREATE TABLE observations (
	observation_id TEXT PRIMARY KEY,
	envelope_json  TEXT NOT NULL,
	observed_at    TEXT NOT NULL,
	received_at    TEXT NOT NULL
);
CREATE TABLE assets (
	id                   TEXT PRIMARY KEY,
	asset_json           TEXT NOT NULL,
	first_observation_id TEXT NOT NULL,
	last_observation_id  TEXT NOT NULL
);
CREATE TABLE relationships (
	id                   TEXT PRIMARY KEY,
	relationship_json    TEXT NOT NULL,
	first_observation_id TEXT NOT NULL,
	last_observation_id  TEXT NOT NULL
);
`

const migrationV2 = `
CREATE TABLE audit_entries (
	sequence    INTEGER PRIMARY KEY,
	recorded_at TEXT NOT NULL,
	action      TEXT NOT NULL,
	actor       TEXT NOT NULL,
	detail_json TEXT NOT NULL,
	prev_hash   TEXT NOT NULL,
	hash        TEXT NOT NULL UNIQUE
);
`

const migrationV3 = `
CREATE TABLE schedules (
	collector_id     TEXT PRIMARY KEY,
	job_type         TEXT NOT NULL,
	interval_seconds INTEGER NOT NULL,
	next_run_at      TEXT NOT NULL,
	created_at       TEXT NOT NULL,
	updated_at       TEXT NOT NULL
);
`

// migrations maps each schema version to the SQL that upgrades a database
// from version-1 to that version. migrate applies every entry between the
// database's current PRAGMA user_version and schemaVersion in order, so a
// database created under an older binary upgrades incrementally rather than
// needing to be recreated from scratch.
var migrations = map[int]string{
	1: migrationV1,
	2: migrationV2,
	3: migrationV3,
}

// Store implements store.Repository backed by a SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if necessary) a SQLite database at dsn and applies
// any pending schema migration. dsn is a file path; the special value
// ":memory:" opens a private in-memory database, useful for tests that
// want to exercise the real SQL code paths without a file — it provides
// no more durability than store.Memory and must not be used to claim
// persistence.
func Open(dsn string) (*Store, error) {
	source := dsn
	pragmas := "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	if dsn != ":memory:" {
		if !strings.HasPrefix(dsn, "file:") {
			source = "file:" + dsn
		}
		pragmas += "&_pragma=journal_mode(WAL)"
	}
	db, err := sql.Open("sqlite", source+"?"+pragmas)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	// SQLite serializes writers regardless of connection count; a single
	// connection avoids SQLITE_BUSY entirely instead of relying on
	// busy_timeout to paper over concurrent-writer contention, and keeps
	// ":memory:" databases (private per connection) coherent across calls.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the underlying database connection.
func (s *Store) Close() error { return s.db.Close() }

// DB returns the underlying *sql.DB, for operational needs store.Repository
// deliberately does not expose — for example, taking a consistent backup
// with SQLite's own `.backup`/VACUUM INTO tooling, or direct inspection
// during an incident. Prefer the Repository methods for anything this
// package already provides.
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("database schema version %d is newer than this binary supports (%d); upgrade Topo before opening this database", version, schemaVersion)
	}
	for v := version + 1; v <= schemaVersion; v++ {
		if err := s.applyMigration(ctx, v); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) applyMigration(ctx context.Context, version int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema migration to version %d: %w", version, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, migrations[version]); err != nil {
		return fmt.Errorf("apply schema migration to version %d: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, version)); err != nil {
		return fmt.Errorf("set schema version to %d: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema migration to version %d: %w", version, err)
	}
	return nil
}

// SaveObservation is idempotent by ObservationID, matching store.Memory:
// resubmitting the same ID replaces that observation and its
// assets'/relationships' resolved state in place rather than creating a
// duplicate.
func (s *Store) SaveObservation(ctx context.Context, e model.ObservationEnvelope) error {
	envelopeJSON, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal observation: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin save observation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO observations (observation_id, envelope_json, observed_at, received_at)
		VALUES (?, ?, ?, datetime('now'))
		ON CONFLICT(observation_id) DO UPDATE SET
			envelope_json = excluded.envelope_json,
			observed_at   = excluded.observed_at
	`, e.ObservationID, string(envelopeJSON), e.ObservedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return fmt.Errorf("save observation: %w", err)
	}

	for _, a := range e.Assets {
		id := model.StableAssetID(a)
		assetJSON, err := json.Marshal(a)
		if err != nil {
			return fmt.Errorf("marshal asset: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO assets (id, asset_json, first_observation_id, last_observation_id)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				asset_json          = excluded.asset_json,
				last_observation_id = excluded.last_observation_id
		`, id, string(assetJSON), e.ObservationID, e.ObservationID); err != nil {
			return fmt.Errorf("save asset: %w", err)
		}
	}

	for _, r := range e.Relationships {
		id := model.StableRelationshipID(r)
		relationshipJSON, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("marshal relationship: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO relationships (id, relationship_json, first_observation_id, last_observation_id)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				relationship_json   = excluded.relationship_json,
				last_observation_id = excluded.last_observation_id
		`, id, string(relationshipJSON), e.ObservationID, e.ObservationID); err != nil {
			return fmt.Errorf("save relationship: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit save observation: %w", err)
	}
	return nil
}

func (s *Store) ListObservations(ctx context.Context) ([]model.ObservationEnvelope, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT envelope_json FROM observations ORDER BY rowid`)
	if err != nil {
		return nil, fmt.Errorf("list observations: %w", err)
	}
	defer rows.Close()
	var out []model.ObservationEnvelope
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan observation: %w", err)
		}
		var e model.ObservationEnvelope
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			return nil, fmt.Errorf("decode observation: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list observations: %w", err)
	}
	return out, nil
}

func (s *Store) ListAssets(ctx context.Context) ([]store.ResolvedAsset, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, asset_json, first_observation_id, last_observation_id FROM assets ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list assets: %w", err)
	}
	defer rows.Close()
	var out []store.ResolvedAsset
	for rows.Next() {
		var resolved store.ResolvedAsset
		var assetJSON string
		if err := rows.Scan(&resolved.ID, &assetJSON, &resolved.FirstObservationID, &resolved.LastObservationID); err != nil {
			return nil, fmt.Errorf("scan asset: %w", err)
		}
		if err := json.Unmarshal([]byte(assetJSON), &resolved.Asset); err != nil {
			return nil, fmt.Errorf("decode asset: %w", err)
		}
		out = append(out, resolved)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list assets: %w", err)
	}
	return out, nil
}

func (s *Store) ListRelationships(ctx context.Context) ([]store.ResolvedRelationship, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, relationship_json, first_observation_id, last_observation_id FROM relationships ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list relationships: %w", err)
	}
	defer rows.Close()
	var out []store.ResolvedRelationship
	for rows.Next() {
		var resolved store.ResolvedRelationship
		var relationshipJSON string
		if err := rows.Scan(&resolved.ID, &relationshipJSON, &resolved.FirstObservationID, &resolved.LastObservationID); err != nil {
			return nil, fmt.Errorf("scan relationship: %w", err)
		}
		if err := json.Unmarshal([]byte(relationshipJSON), &resolved.Relationship); err != nil {
			return nil, fmt.Errorf("decode relationship: %w", err)
		}
		out = append(out, resolved)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list relationships: %w", err)
	}
	return out, nil
}

// AppendAuditEvent reads the last entry and inserts the next one inside a
// single transaction. Since the *sql.DB backing Store holds exactly one
// connection (see Open), BeginTx serializes this against every other
// caller for its whole duration, so Sequence/PrevHash can never be assigned
// from a stale read the way they could if two callers read-then-wrote
// independently.
func (s *Store) AppendAuditEvent(ctx context.Context, e audit.Event) (audit.Entry, error) {
	detailJSON, err := json.Marshal(e.Detail)
	if err != nil {
		return audit.Entry{}, fmt.Errorf("marshal audit detail: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return audit.Entry{}, fmt.Errorf("begin append audit event: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var lastSeq int64
	var prevHash string
	switch err := tx.QueryRowContext(ctx, `SELECT sequence, hash FROM audit_entries ORDER BY sequence DESC LIMIT 1`).Scan(&lastSeq, &prevHash); {
	case errors.Is(err, sql.ErrNoRows):
		lastSeq, prevHash = 0, ""
	case err != nil:
		return audit.Entry{}, fmt.Errorf("read last audit entry: %w", err)
	}

	entry := audit.Chain(prevHash, lastSeq+1, time.Now(), e)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO audit_entries (sequence, recorded_at, action, actor, detail_json, prev_hash, hash)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, entry.Sequence, entry.RecordedAt.UTC().Format(time.RFC3339Nano), entry.Action, entry.Actor, string(detailJSON), entry.PrevHash, entry.Hash); err != nil {
		return audit.Entry{}, fmt.Errorf("save audit entry: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return audit.Entry{}, fmt.Errorf("commit audit entry: %w", err)
	}
	return entry, nil
}

func (s *Store) ListAuditEntries(ctx context.Context) ([]audit.Entry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sequence, recorded_at, action, actor, detail_json, prev_hash, hash FROM audit_entries ORDER BY sequence`)
	if err != nil {
		return nil, fmt.Errorf("list audit entries: %w", err)
	}
	defer rows.Close()
	var out []audit.Entry
	for rows.Next() {
		var e audit.Entry
		var recordedAt, detailJSON string
		if err := rows.Scan(&e.Sequence, &recordedAt, &e.Action, &e.Actor, &detailJSON, &e.PrevHash, &e.Hash); err != nil {
			return nil, fmt.Errorf("scan audit entry: %w", err)
		}
		t, err := time.Parse(time.RFC3339Nano, recordedAt)
		if err != nil {
			return nil, fmt.Errorf("decode audit entry recorded_at: %w", err)
		}
		e.RecordedAt = t
		if err := json.Unmarshal([]byte(detailJSON), &e.Detail); err != nil {
			return nil, fmt.Errorf("decode audit entry detail: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list audit entries: %w", err)
	}
	return out, nil
}

func (s *Store) UpsertSchedule(ctx context.Context, sched store.Schedule) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO schedules (collector_id, job_type, interval_seconds, next_run_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(collector_id) DO UPDATE SET
			job_type         = excluded.job_type,
			interval_seconds = excluded.interval_seconds,
			next_run_at      = excluded.next_run_at,
			updated_at       = excluded.updated_at
	`, sched.CollectorID, string(sched.JobType), sched.IntervalSeconds,
		sched.NextRunAt.UTC().Format(time.RFC3339Nano),
		sched.CreatedAt.UTC().Format(time.RFC3339Nano),
		sched.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("upsert schedule: %w", err)
	}
	return nil
}

func scanSchedule(row interface {
	Scan(dest ...any) error
}) (store.Schedule, error) {
	var sched store.Schedule
	var jobType, nextRunAt, createdAt, updatedAt string
	if err := row.Scan(&sched.CollectorID, &jobType, &sched.IntervalSeconds, &nextRunAt, &createdAt, &updatedAt); err != nil {
		return store.Schedule{}, err
	}
	sched.JobType = model.JobType(jobType)
	var err error
	if sched.NextRunAt, err = time.Parse(time.RFC3339Nano, nextRunAt); err != nil {
		return store.Schedule{}, fmt.Errorf("decode next_run_at: %w", err)
	}
	if sched.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return store.Schedule{}, fmt.Errorf("decode created_at: %w", err)
	}
	if sched.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return store.Schedule{}, fmt.Errorf("decode updated_at: %w", err)
	}
	return sched, nil
}

func (s *Store) ListSchedules(ctx context.Context) ([]store.Schedule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT collector_id, job_type, interval_seconds, next_run_at, created_at, updated_at FROM schedules ORDER BY collector_id`)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	defer rows.Close()
	var out []store.Schedule
	for rows.Next() {
		sched, err := scanSchedule(rows)
		if err != nil {
			return nil, fmt.Errorf("scan schedule: %w", err)
		}
		out = append(out, sched)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	return out, nil
}

func (s *Store) GetSchedule(ctx context.Context, collectorID string) (store.Schedule, error) {
	row := s.db.QueryRowContext(ctx, `SELECT collector_id, job_type, interval_seconds, next_run_at, created_at, updated_at FROM schedules WHERE collector_id = ?`, collectorID)
	sched, err := scanSchedule(row)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Schedule{}, store.ErrNotFound
	}
	if err != nil {
		return store.Schedule{}, fmt.Errorf("get schedule: %w", err)
	}
	return sched, nil
}

func (s *Store) DeleteSchedule(ctx context.Context, collectorID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM schedules WHERE collector_id = ?`, collectorID); err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	return nil
}
