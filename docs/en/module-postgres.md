---
title: PostgreSQL module
navTitle: PostgreSQL
section: Modules
order: 431
description: Monitor local PostgreSQL health and capacity with bounded probes.
tags: [postgres, linux, modules, reference]
updated: 2026-10-23
---

# PostgreSQL module

Monitor local PostgreSQL 18 through `psql`, including replicas. An empty `[postgres]` section enables collection; without it no command runs.

```toml
[postgres]
command = ["runuser", "-u", "postgres", "--", "psql"]
database = "postgres"
```

Both settings above are defaults. The ingestor needs permission to run the command and read the PostgreSQL statistics. The default uses the local `postgres` operating-system account and local peer authentication. For a custom command, use an argument array without a shell. Keep passwords in a private PostgreSQL password file; never put passwords in `command` or `database`. Database names cannot be libpq connection strings.

The collector appends `-X -At -q -w -v ON_ERROR_STOP=1 --dbname <database> -c <SQL>`. `-w` forbids password prompts. Each SQL batch runs `BEGIN READ ONLY`, sets `statement_timeout = '10s'`, performs one JSON-producing SELECT, and commits. No extensions or writes are required; the reads work during recovery. An authentication or permission failure is visible as unavailable, with no stdout or stderr copied into diagnostics.

The capacity SELECT reads client backends from `pg_stat_activity`, `max_connections`, non-template database names and `pg_database_size(oid)` from `pg_database` (alphabetical order, at most 100), maximum `age(datfrozenxid)`, oldest open non-idle client-backend transaction age excluding the probe itself, and summed deadlocks from `pg_stat_database`. The daily SELECT reads `current_setting('server_version')`. It does not collect SQL text, user names, or rows from application tables.

All signals use the host resource. Database names are bounded dimensions; only the first 100 non-template databases are reported. Deadlocks are a sum of database counters: removing databases or resetting statistics can lower the counter. Calculate rates with reset handling.

Override `postgres` (300 s) and `postgres-version` (86400 s) in `[runner.cadence]`. With D databases, capacity emits 8 + D metric samples every five minutes: 2.6 samples/minute for five databases, at most 21.6 for 100. Use `[linux] profile = "postgres"` for `patroni.service` and `etcd.service`, and [Patroni](/en/module-patroni) for role and cluster health.

## Signals

Dimensions below are additional to configured global dimensions. Plain counts use an empty unit (`""`). States are sent on change and refreshed after six hours when next observed; daily states are observed and refreshed daily. `once` runs every collection group regardless of cadence.

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `postgres.connections` | gauge | `""` | state | `2` | Client connections grouped as active, idle, idle in transaction, or other; all four buckets are emitted. | 300 s |
| `postgres.connections.max` | gauge | `""` | — | `100` | Configured max_connections; some slots may be reserved. | 300 s |
| `postgres.database.size` | gauge | `bytes` | database | `1048576` | Database size, excluding templates; at most 100 databases. | 300 s |
| `postgres.xid.age.max` | gauge | `""` | — | `50000` | Maximum age of datfrozenxid across databases; watch transaction ID exhaustion. | 300 s |
| `postgres.transaction.oldest_age` | gauge | `seconds` | — | `30` | Age of the oldest non-idle open client-backend transaction excluding the probe; zero when none exists. | 300 s |
| `postgres.deadlocks` | counter | `""` | — | `3` | Cumulative deadlocks summed across database statistics. | 300 s |
| `postgres.version` | state string | `""` | — | `18.0` | PostgreSQL server_version. | 86400 s |
| `postgres.available` | state boolean | `""` | collection | `true` | Whether this collection completed; collection is one of capacity, version. | 300 / 86400 s |
| `ingestor.collector.ok` | state boolean | `""` | collector | `true` | Runner result for the scheduled collector name. | 300 / 86400 s |
| `ingestor.collector.failed` | event | `""` | collector | `error=collection failed` | One sanitized failure event per failed collection; the runner retries on the next tick. | on failure |

## Limits and failures

Each collection has a 10-second deadline, also bounded by the runner deadline. Responses or command stdout are limited to 1 MiB. HTTP collectors do not follow redirects or use environment proxies. Password files are limited to 4 KiB and CA files to 1 MiB; only regular files are accepted. Credentials are reread each collection so file rotation takes effect without a restart.

An unreachable endpoint, failed authentication, invalid TLS, missing command or file, timeout, oversized response, or malformed required field sets availability false and produces one `ingestor.collector.failed` event for that collection group. Other groups remain independent. Valid observations collected before a later request fails can still be emitted; check the matching availability state. Historical values remain in Pulse after a failure or when an optional field disappears.

Start troubleshooting with the endpoint or command, account permissions, certificate hostname, and file permissions. Verify locally with `pulse-linux --config /etc/pulse/pulse-linux.toml --local --pretty once`; this performs all configured probes. See [Linux ingestor](/en/ingestor-linux) for installation and operation.
