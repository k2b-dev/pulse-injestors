---
title: Valkey module
navTitle: Valkey
section: Modules
order: 433
description: Monitor local Valkey health and capacity with bounded probes.
tags: [valkey, linux, modules, reference]
updated: 2026-10-23
---

# Valkey module

Monitor local Valkey 8 using AUTH and INFO only. Add `[valkey]` to enable collection; no address is assumed without the section.

```toml
[valkey]
address = "127.0.0.1:6379"
tls = true
server_name = "valkey.example.test"
ca_file = "/etc/pulse/service-ca.pem"
password_file = "/etc/pulse/valkey.password"
# username = "monitor"
```

`address` is a required host:port. TLS defaults to true and verifies certificates against system roots or `ca_file`; `server_name` overrides the certificate hostname and SNI. For a TLS-only listener, use its configured TLS port. `tls = false` is supported for an explicitly configured plain listener; `ca_file` and `server_name` then must be absent.

The password is read from a file, with an optional trailing newline. With no username, the collector sends `AUTH <password>`; with `username`, it sends `AUTH <username> <password>`. `username` requires `password_file`. If no password file is configured, AUTH is omitted. Permit INFO for the monitoring account.

Each collector opens its own connection, authenticates, sends `INFO server clients memory persistence stats replication keyspace`, and closes the connection. The modules section is not requested. Duplicate INFO keys use the last value. No key names or values are requested or emitted. The minimal RESP client accepts a successful AUTH reply and one bounded bulk INFO reply. Key counts are summed across INFO keyspace entries, without database dimensions. `maxmemory = 0` means unlimited and omits `valkey.memory.max`.

All signals use the host resource. Override `valkey` (60 s), `valkey-capacity` (300 s), and `valkey-version` (86400 s) in `[runner.cadence]`. The collector emits three metrics per minute and four every five minutes, averaging 3.8 metric samples/minute (3.6 when maxmemory is unlimited). Use `[linux] profile = "valkey"` for `valkey.service`.

## Signals

Dimensions below are additional to configured global dimensions. Plain counts use an empty unit (`""`). States are sent on change and refreshed after six hours when next observed; daily states are observed and refreshed daily. `once` runs every collection group regardless of cadence.

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `valkey.role` | state string | `""` | — | `master` | INFO role: master or primary, slave or replica. | 60 s |
| `valkey.clients` | gauge | `""` | — | `12` | Connected clients. | 60 s |
| `valkey.rejected_connections` | counter | `""` | — | `2` | Cumulative rejected connections. | 60 s |
| `valkey.persistence.ok` | state boolean | `""` | — | `true` | Last RDB save succeeded and, when AOF is enabled, its last write succeeded. | 60 s |
| `valkey.rdb.changes_since_save` | gauge | `""` | — | `100` | Changes since the last RDB save; resets on save. | 60 s |
| `valkey.memory.used` | gauge | `bytes` | — | `1048576` | INFO used_memory. | 300 s |
| `valkey.memory.max` | gauge | `bytes` | — | `1073741824` | Configured maxmemory, omitted when zero (unlimited). | 300 s |
| `valkey.evicted_keys` | counter | `""` | — | `3` | Cumulative evicted keys. | 300 s |
| `valkey.keys` | gauge | `""` | — | `1000` | Sum of key counts across INFO db entries; no keys or values leave Valkey. | 300 s |
| `valkey.version` | state string | `""` | — | `8.0.1` | valkey_version, falling back to the compatibility redis_version field. | 86400 s |
| `valkey.available` | state boolean | `""` | collection | `true` | Whether this collection completed; collection is one of health, capacity, version. | 60 / 300 / 86400 s |
| `ingestor.collector.ok` | state boolean | `""` | collector | `true` | Runner result for the scheduled collector name. | 60 / 300 / 86400 s |
| `ingestor.collector.failed` | event | `""` | collector | `error=collection failed` | One sanitized failure event per failed collection; the runner retries on the next tick. | on failure |

## Limits and failures

Each collection has a 10-second deadline, also bounded by the runner deadline. Responses or command stdout are limited to 1 MiB. HTTP collectors do not follow redirects or use environment proxies. Password files are limited to 4 KiB and CA files to 1 MiB; only regular files are accepted. Credentials are reread each collection so file rotation takes effect without a restart.

An unreachable endpoint, failed authentication, invalid TLS, missing command or file, timeout, oversized response, or malformed required field sets availability false and produces one `ingestor.collector.failed` event for that collection group. Other groups remain independent. Valid observations collected before a later request fails can still be emitted; check the matching availability state. Historical values remain in Pulse after a failure or when an optional field disappears.

Start troubleshooting with the endpoint or command, account permissions, certificate hostname, and file permissions. Verify locally with `pulse-linux --config /etc/pulse/pulse-linux.toml --local --pretty once`; this performs all configured probes. See [Linux ingestor](/en/ingestor-linux) for installation and operation.
