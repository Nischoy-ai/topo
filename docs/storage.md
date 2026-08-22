# Persistent storage and the audit log

Topo's controller stores discovery data — observations, resolved assets, and resolved relationships — a hash-chained audit log of admin/security-relevant actions, recurring discovery schedules, and certificate revocations behind a single `store.Repository` interface. Two implementations exist today: `Memory` (in-memory only, the default) and a SQLite-backed `Store` (`internal/store/sqlite`), opt-in via `topo serve -db-driver sqlite`.

```sh
./bin/topo serve -db-driver sqlite -db-dsn /var/lib/topo/topo.db
```

`-db-driver memory` (the default; equivalent to omitting the flag) keeps today's behavior exactly: nothing survives a controller restart. This remains the right choice for evaluation and Topo Lab use, where a clean slate on every restart is often what you want. `-db-driver sqlite` requires `-db-dsn`, a file path where the database is created if it does not already exist.

## What is persisted, and what is not

Persisted (once `-db-driver sqlite` is configured): every ingested observation (`POST /v1/observations`), the resolved current state of every asset and relationship built from them, the audit log described below, every collector's recurring discovery schedule (see [Server-side recurring discovery scheduling](scheduling.md)), and immutable certificate-serial revocations. `GET /v1/observations`, `GET /v1/assets`, `GET /v1/relationships`, `GET /v1/audit`, `GET /v1/schedules`, and `GET /v1/certificate-revocations` all read from the same store, so a controller restart with `-db-driver sqlite` returns exactly what was there before.

**Still in-memory only, deliberately not addressed by this milestone:** enrollment tokens, collector heartbeats, and job state (the individual job records `POST /v1/jobs` and a due schedule both create). Each was already documented as in-memory-only when built (see `docs/enrollment.md`, `docs/heartbeats.md`, `docs/jobs.md`), and that has not changed — a controller restart still requires re-enrollment tokens to be reissued, and any in-flight job or heartbeat history is lost. Schedules and revocations are exceptions worth persisting themselves: losing a standing cadence is a silent policy change, while losing a revocation can re-enable a compromised but otherwise valid certificate. The memory backend implements the same API semantics for tests and evaluation, but its revocations are not a durable security boundary.

## Why SQLite, not PostgreSQL

`ROADMAP.md`'s release gates have named persistent storage as required since before this milestone existed, and `AGENTS.md` has said not to default to PostgreSQL without evaluating whether it is actually needed yet. Having reached this milestone, the evaluation's conclusion: Topo has no HA or clustered-controller story today — a single controller process handling all traffic is still the only supported deployment shape (see `SECURITY.md`). A client-server database that operators must separately provision, secure, back up, and keep available is not yet justified by anything Topo actually needs; it would add real operational weight for a capability (multiple controllers sharing one database) nothing in this project uses yet. SQLite is a single file, requires no separate service, and is sufficient for a single controller process.

This is a capacity decision, not permanent architecture lock-in. The `Repository` interface — `SaveObservation`, `ListObservations`, `ListAssets`, `ListRelationships` — was written so a `postgres` driver could be added as a third option later without changing it again; revisit this once HA/clustering is actually on the roadmap.

`internal/store/sqlite` uses [`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite), a pure-Go transpilation of SQLite's C source, pinned to `v1.39.0` — the last release declaring `go 1.23.0` compatibility with this project's pinned toolchain. This is not just the usual narrow-dependency reasoning every other pinned dependency in this project shares (implementing a durable, concurrent-safe, ACID storage engine from scratch is exactly the kind of well-trodden work not worth reinventing) — it is also the only realistic option given this project's CI cross-compiles for Windows (`GOOS=windows GOARCH=amd64 go build`) with no Windows C cross-compiler available, which a cgo-based SQLite driver would require.

## Schema and identity

Six tables: `observations` (one row per observation, storing the full envelope as JSON alongside a few indexed metadata columns), `assets` and `relationships` (one row each per *resolved* entity, keyed by a stable, content-derived ID rather than an autoincrement counter), `audit_entries` (one row per audit log entry, described below), `schedules` (one row per collector with a recurring schedule), and `certificate_revocations` (one immutable row per canonical hexadecimal certificate serial). An asset's ID is `model.StableAssetID` — a hash of its type, native ID, and identifiers, deliberately excluding attributes and evidence so a value change does not change identity. A relationship's ID is `model.StableRelationshipID`, the same idea applied to (type, from, to). Saving an observation is an upsert keyed by these IDs; `SaveObservation` is also idempotent by `ObservationID`. A schedule is likewise an upsert keyed by `collector_id`. Revocation is different: the first reason and timestamp win, and repeating the same serial never mutates the incident record.

Schema versioning uses SQLite's own `PRAGMA user_version` plus a small in-code migration table. `migrate` applies every pending version in order: version 1 introduced discovery data, version 2 `audit_entries`, version 3 `schedules`, and version 4 `certificate_revocations`. Tests upgrade real-shaped v1, v2, and v3 schemas to the latest version and prove revocations survive reopen. Opening a database written by a newer Topo version than the running binary understands still fails loudly rather than silently misinterpreting the schema.

## Audit log

`GET /v1/audit` (operator bearer key required) returns an append-only, hash-chained log of admin/security-relevant controller actions:

- `enrollment_token_issued` — actor is `api-key`; detail carries `token_fingerprint` (a SHA-256 fingerprint, never the raw token — see below) and `expires_at`.
- `collector_enrolled` — actor is the enrolling collector's ID; detail carries `serial_number` and `expires_at`.
- `certificate_rotated` — actor is the collector's verified peer-certificate identity; detail carries the previous and new serials plus `expires_at`.
- `certificate_revoked` — actor is `api-key`; detail carries the canonical `serial_number` and bounded operator-supplied `reason`. The durable revocation row is authoritative; this audit append follows the same best-effort policy as other actions.
- `job_created` — actor is `api-key`; detail carries the target `collector_id`, `job_id`, and `job_type`.
- `schedule_created` / `schedule_updated` / `schedule_deleted` — actor is `api-key`; detail carries `collector_id`, `job_type`, and `interval_seconds` (omitted for `schedule_deleted`). See [Server-side recurring discovery scheduling](scheduling.md).

Each entry (`internal/audit.Entry`) has a `sequence` number, `recorded_at` timestamp, `action`, `actor`, a `detail` map, and a `hash` that covers the entry's own content plus the previous entry's `hash` (`prev_hash`). "Immutable" here means *tamper-evident*, not physically write-once: editing, reordering, or deleting an entry after the fact breaks the chain from that point forward, which `internal/audit.VerifyChain` (run it over whatever `GET /v1/audit` returns) detects — this is the same class of guarantee as a Merkle/hash chain generally, not cryptographic non-repudiation, and `GET /v1/audit` itself does not re-verify the chain before returning it.

Detail values are always short strings, never secret material — an enrollment token is referenced only by a fingerprint (`SHA-256(token)`, truncated to 8 bytes/16 hex characters), which lets an operator correlate a log entry with a specific token during an investigation without the log itself ever holding something that grants access if read by the wrong party. `internal/controller`'s `TestEnrollmentTokenIssuanceIsAudited` test asserts the raw token never appears in the audit response.

Appending an entry is best-effort with respect to the action it records: if `AppendAuditEvent` itself fails (for example, a full disk under `-db-driver sqlite`), the controller logs the failure but does not undo or fail the action that already completed, since the action's own effects (a minted token, an issued certificate, a queued job) live outside `store.Repository` and cannot be rolled back through it. This is a deliberate choice, not an oversight — a fail-closed audit log (reject the action if it cannot be audited) is a stronger but different guarantee this slice does not claim.

## Operational notes

- WAL journal mode is enabled for file-backed databases (not for the `:memory:` test-only mode), and `busy_timeout` is set so a momentarily-locked database retries rather than failing immediately.
- The connection pool is capped at one connection. SQLite serializes writers regardless of connection count, so this avoids `SQLITE_BUSY` contention outright rather than relying on `busy_timeout` to paper over it, and keeps a `:memory:` database (private per connection in SQLite's own semantics) coherent across calls.
- `internal/store/sqlite.Store.DB()` exposes the underlying `*sql.DB` for operational needs `store.Repository` deliberately does not cover — taking a consistent backup with SQLite's own `.backup`/`VACUUM INTO` tooling, or direct inspection during an incident. Prefer the `Repository` methods for anything already covered.
- `internal/store/storetest` is a shared black-box conformance test suite: both `Memory` and the SQLite backend run the exact same assertions through the `Repository` interface alone, including immutable/idempotent and concurrent revocation behavior, so the two implementations cannot silently diverge in observable behavior.
- Appending an audit entry reads the last entry and inserts the next one inside a single transaction; combined with the single-connection pool above, this serializes concurrent appends the same way `SaveObservation` is already serialized, so `sequence`/`prev_hash` can never be assigned from a stale read.

## Current limitations

Enrollment tokens, heartbeats, and job state remain in-memory only (see above). There is no backup/restore tooling beyond SQLite's own file-level tools, and no encryption at rest. The audit log is tamper-evident, not tamper-proof — see "Audit log" above for exactly what that guarantee does and does not cover, and note `GET /v1/audit` does not itself re-verify the chain. **This completes the "persistent observation/audit storage and scheduling" milestone** — all three slices (persistent storage, the audit log, and server-side recurring discovery scheduling) are implemented. See `docs/project-plan.md` for what comes after, and [Server-side recurring discovery scheduling](scheduling.md) for that slice's own current limitations.
