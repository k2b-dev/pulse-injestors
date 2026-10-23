---
title: ClickHouse module
navTitle: ClickHouse
section: Modules
order: 434
description: Monitor local ClickHouse health and capacity with bounded probes.
tags: [clickhouse, linux, modules, reference]
updated: 2026-10-23
---

# ClickHouse module

Monitor local ClickHouse 26.9 through its HTTP interface. Add `[clickhouse]` to enable collection; without it there are no queries.

```toml
[clickhouse]
url = "https://127.0.0.1:8443"
user = "monitor"
password_file = "/etc/pulse/clickhouse.password"
ca_file = "/etc/pulse/service-ca.pem"
server_name = "clickhouse.example.test"
```

`url` accepts HTTP or HTTPS without URL credentials, query, or fragment. HTTPS verifies the server with system roots or `ca_file`; `server_name` overrides certificate hostname and SNI. `user` defaults to `default`. Read the password from a file with an optional trailing newline. Requests use `X-ClickHouse-User` and `X-ClickHouse-Key` headers; the password never appears in the URL. Use a monitoring account that can read the required system tables, with a readonly profile and `max_execution_time` set to at most 10 seconds. Password authentication over plain HTTP is accepted only for a literal loopback address or `localhost`.

Every request is `POST <url>/` with a SQL body ending in `FORMAT JSON`. The collector sends no URL settings, so it works with profiles that forbid setting changes; the client-side HTTP timeout remains 10 seconds. The health query is `SELECT 1 AS ok FORMAT JSON`; the daily version query is `SELECT version() AS version FORMAT JSON`.

The capacity query combines rows from `system.disks` (free_space and total_space), `system.metrics` (Query), active `system.parts` (maximum count grouped by database, table, partition_id), `system.merges` (count), `system.errors` (sum of value), and `uptime()`. It returns only aggregate values and disk names, with at most 205 rows (100 disks plus five aggregate values). It does not read application data or emit query text.

All signals use the host resource. Disk names are dimensions; database, table, and partition names are not emitted. Error counts are cumulative since server start and can reset. `queries.running` may include the monitoring query itself.

Override `clickhouse` (60 s), `clickhouse-capacity` (300 s), and `clickhouse-version` (86400 s) in `[runner.cadence]`. With D disks, capacity emits 2D + 5 metrics every five minutes: 1.4 metric samples/minute for one disk. Health emits states only. Use `[linux] profile = "clickhouse"` for `clickhouse-server.service`.

## Signals

Dimensions below are additional to configured global dimensions. Plain counts use an empty unit (`""`). States are sent on change and refreshed after six hours when next observed; daily states are observed and refreshed daily. `once` runs every collection group regardless of cadence.

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `clickhouse.disk.free` | gauge | `bytes` | disk | `1073741824` | Available space reported by system.disks for this disk. | 300 s |
| `clickhouse.disk.total` | gauge | `bytes` | disk | `10737418240` | Total space reported by system.disks for this disk. | 300 s |
| `clickhouse.queries.running` | gauge | `""` | — | `3` | Current Query metric, possibly including this probe. | 300 s |
| `clickhouse.parts.max_per_partition` | gauge | `""` | — | `50` | Largest active-part count in any database/table/partition group; zero when empty. | 300 s |
| `clickhouse.merges.running` | gauge | `""` | — | `1` | Current rows in system.merges. | 300 s |
| `clickhouse.errors` | counter | `""` | — | `5` | Sum of cumulative system.errors values. | 300 s |
| `clickhouse.uptime` | gauge | `seconds` | — | `3600` | Server uptime. | 300 s |
| `clickhouse.version` | state string | `""` | — | `26.9` | Server version(). | 86400 s |
| `clickhouse.available` | state boolean | `""` | collection | `true` | Whether this collection completed; collection is one of health, capacity, version. | 60 / 300 / 86400 s |
| `ingestor.collector.ok` | state boolean | `""` | collector | `true` | Runner result for the scheduled collector name. | 60 / 300 / 86400 s |
| `ingestor.collector.failed` | event | `""` | collector | `error=collection failed` | One sanitized failure event per failed collection; the runner retries on the next tick. | on failure |

## Limits and failures

Each collection has a 10-second deadline, also bounded by the runner deadline. Responses or command stdout are limited to 1 MiB. HTTP collectors do not follow redirects or use environment proxies. Password files are limited to 4 KiB and CA files to 1 MiB; only regular files are accepted. Credentials are reread each collection so file rotation takes effect without a restart.

An unreachable endpoint, failed authentication, invalid TLS, missing command or file, timeout, oversized response, or malformed required field sets availability false and produces one `ingestor.collector.failed` event for that collection group. Other groups remain independent. Valid observations collected before a later request fails can still be emitted; check the matching availability state. Historical values remain in Pulse after a failure or when an optional field disappears.

Start troubleshooting with the endpoint or command, account permissions, certificate hostname, and file permissions. Verify locally with `pulse-linux --config /etc/pulse/pulse-linux.toml --local --pretty once`; this performs all configured probes. See [Linux ingestor](/en/ingestor-linux) for installation and operation.
