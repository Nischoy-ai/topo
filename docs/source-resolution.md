# Source precedence and asset freshness

Topo preserves every immutable observation and also builds a current resolved
asset view. When more than one discovery source reports the same stable asset,
the controller retains each source's latest claim instead of silently losing
the claim that did not win.

Configure discovery-plugin precedence when starting the controller, from
highest to lowest priority:

```sh
topo serve \
  -db-driver sqlite \
  -db-dsn /var/lib/topo/topo.db \
  -source-precedence vmware,ssh-linux,snmp
```

Plugin names are case-sensitive. A name may appear only once; the list accepts
at most 64 names of at most 128 bytes each. The setting is deployment
configuration, not data stored in SQLite, so service definitions must retain
the same flag across restarts. Source claims and their timestamps are durable.
The Helm chart exposes the same ordered list as `sourcePrecedence`.

## Resolution rules

An independent source is the tuple `(site_id, collector_id, plugin)`. For every
stable asset ID, resolution applies these rules in order:

1. A plugin named earlier in `-source-precedence` wins over one named later.
2. Every unlisted plugin ranks after all explicitly listed plugins.
3. Sources at the same rank resolve by their latest `observed_at` timestamp.
4. An exact timestamp tie resolves by a stable hash of the source tuple, making
   the result independent of request arrival and map iteration order.

An older observation delivered late cannot roll one source's current claim
backward. It can still extend that source's `first_observed_at` history. The
default empty precedence list gives every plugin the same rank, so the freshest
claim wins deterministically.

Precedence applies only when claims already have the same
`model.StableAssetID`. This slice does not infer that unlike native identities
refer to the same real object; cross-source correlation is a separate graph and
identity-policy problem.

## Asset API visibility

`GET /v1/assets` keeps its existing top-level fields and adds:

- `winning_source`: the site, collector, plugin, precedence rank, and
  first/latest observation metadata for the selected claim;
- `sources`: every contributing source, ordered by the same rules used to pick
  the winner;
- `first_observed_at` and `last_observed_at`: the full asset's earliest/latest
  timestamps across contributing sources;
- `conflicts`: disagreements in `name`, identifier values, or top-level
  attribute values, with each source's value and whether the field was present.

Evidence is not considered a conflict: evidence paths, collection timestamps,
and confidence values naturally vary between sources. Topo also does not label
an asset `stale` itself because there is no universal valid threshold across a
one-minute local scan, an hourly vCenter inventory, and a daily cloud-account
scan. Consumers can apply an appropriate policy to the exposed timestamps.

## SQLite migration

SQLite schema version 5 adds `asset_claims`, keyed by stable asset ID plus the
source tuple's stable hash. Upgrading an older database reconstructs claims from
the retained observation envelopes inside the same all-pending-migrations
transaction. A decode or insertion failure therefore rolls the database and
`PRAGMA user_version` back together. Backup/restore tests cover every supported
schema through the version-5 forward migration.

Relationships retain their existing latest-observation resolution in this
slice. Relationship-source precedence, omission-based retirement, alerts, and
the M3 1K/10K/100K scale gate remain explicit follow-ups.
