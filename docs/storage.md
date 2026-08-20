# Persistent storage

Topo's controller stores discovery data — observations, resolved assets, and resolved relationships — behind a single `store.Repository` interface. Two implementations exist today: `Memory` (in-memory only, the default) and a SQLite-backed `Store` (`internal/store/sqlite`), opt-in via `topo serve -db-driver sqlite`.

```sh
./bin/topo serve -db-driver sqlite -db-dsn /var/lib/topo/topo.db
```

`-db-driver memory` (the default; equivalent to omitting the flag) keeps today's behavior exactly: nothing survives a controller restart. This remains the right choice for evaluation and Topo Lab use, where a clean slate on every restart is often what you want. `-db-driver sqlite` requires `-db-dsn`, a file path where the database is created if it does not already exist.

## What is persisted, and what is not

Persisted (once `-db-driver sqlite` is configured): every ingested observation (`POST /v1/observations`), and the resolved current state of every asset and relationship built from them. `GET /v1/observations`, `GET /v1/assets`, and `GET /v1/relationships` all read from the same store, so a controller restart with `-db-driver sqlite` returns exactly what was there before.

**Still in-memory only, deliberately not addressed by this slice:** enrollment tokens, collector heartbeats, and jobs. Each was already documented as in-memory-only when built (see `docs/enrollment.md`, `docs/heartbeats.md`, `docs/jobs.md`), and that has not changed here — persisting them is a question for a later slice once discovery-data persistence itself is proven in practice, not assumed now. A controller restart still requires re-enrollment tokens to be reissued, and any in-flight job or heartbeat history is lost, exactly as before.

## Why SQLite, not PostgreSQL

`ROADMAP.md`'s release gates have named persistent storage as required since before this milestone existed, and `AGENTS.md` has said not to default to PostgreSQL without evaluating whether it is actually needed yet. Having reached this milestone, the evaluation's conclusion: Topo has no HA or clustered-controller story today — a single controller process handling all traffic is still the only supported deployment shape (see `SECURITY.md`). A client-server database that operators must separately provision, secure, back up, and keep available is not yet justified by anything Topo actually needs; it would add real operational weight for a capability (multiple controllers sharing one database) nothing in this project uses yet. SQLite is a single file, requires no separate service, and is sufficient for a single controller process.

This is a capacity decision, not permanent architecture lock-in. The `Repository` interface — `SaveObservation`, `ListObservations`, `ListAssets`, `ListRelationships` — was written so a `postgres` driver could be added as a third option later without changing it again; revisit this once HA/clustering is actually on the roadmap.

`internal/store/sqlite` uses [`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite), a pure-Go transpilation of SQLite's C source, pinned to `v1.39.0` — the last release declaring `go 1.23.0` compatibility with this project's pinned toolchain. This is not just the usual narrow-dependency reasoning every other pinned dependency in this project shares (implementing a durable, concurrent-safe, ACID storage engine from scratch is exactly the kind of well-trodden work not worth reinventing) — it is also the only realistic option given this project's CI cross-compiles for Windows (`GOOS=windows GOARCH=amd64 go build`) with no Windows C cross-compiler available, which a cgo-based SQLite driver would require.

## Schema and identity

Three tables: `observations` (one row per observation, storing the full envelope as JSON alongside a few indexed metadata columns), `assets`, and `relationships` (one row each per *resolved* entity, keyed by a stable, content-derived ID rather than an autoincrement counter). An asset's ID is `model.StableAssetID` — a hash of its type, native ID, and identifiers, deliberately excluding attributes and evidence so a value change (an OS patch level, a new discovery timestamp) does not change identity. A relationship's ID is `model.StableRelationshipID`, the same idea applied to (type, from, to) — this is new in this slice: relationships were not previously queryable through `store.Repository` at all, even though `SaveObservation` always received them. Saving an observation is an upsert keyed by these IDs: resubmitting the same asset or relationship (the normal case — every repeated discovery scan reports the same entities again) updates the existing row's resolved state and `last_observation_id`, it does not insert a duplicate. `SaveObservation` is also idempotent by `ObservationID` itself, in both `Memory` and the SQLite backend: a collector retrying a delivery whose response was lost replaces that observation in place rather than creating a second copy.

Schema versioning uses SQLite's own `PRAGMA user_version` plus a small in-code migration table (`internal/store/sqlite`'s `migrate` function) — a dedicated migration framework is unwarranted complexity until there is more than one schema revision to actually manage. Opening a database written by a newer Topo version than the running binary understands fails loudly rather than silently misinterpreting the schema.

## Operational notes

- WAL journal mode is enabled for file-backed databases (not for the `:memory:` test-only mode), and `busy_timeout` is set so a momentarily-locked database retries rather than failing immediately.
- The connection pool is capped at one connection. SQLite serializes writers regardless of connection count, so this avoids `SQLITE_BUSY` contention outright rather than relying on `busy_timeout` to paper over it, and keeps a `:memory:` database (private per connection in SQLite's own semantics) coherent across calls.
- `internal/store/sqlite.Store.DB()` exposes the underlying `*sql.DB` for operational needs `store.Repository` deliberately does not cover — taking a consistent backup with SQLite's own `.backup`/`VACUUM INTO` tooling, or direct inspection during an incident. Prefer the `Repository` methods for anything already covered.
- `internal/store/storetest` is a shared black-box conformance test suite: both `Memory` and the SQLite backend run the exact same assertions through the `Repository` interface alone (round-tripping, dedup-by-stable-ID across repeated observations, concurrent-write safety, idempotent resubmission), so the two implementations cannot silently diverge in observable behavior.

## Current limitations and next slices

Enrollment tokens, heartbeats, and job state remain in-memory only (see above). There is no backup/restore tooling beyond SQLite's own file-level tools, no encryption at rest, and no immutable audit log yet — that is the next slice of this milestone. Server-side recurring discovery scheduling (today the controller only queues one-off jobs; recurring schedules are entirely client-side, via `topo agent run -interval`) is the slice after that. See `docs/project-plan.md` for the full milestone spec.
