---
title: NATS JetStream module
navTitle: NATS JetStream
section: Modules
order: 432
description: Monitor local NATS JetStream health and capacity with bounded probes.
tags: [nats, linux, modules, reference]
updated: 2026-10-23
---

# NATS JetStream module

Monitor the local NATS broker and its JetStream capacity. Add `[nats]` to enable collection; there is no default connection.

```toml
[nats]
monitor_url = "http://127.0.0.1:8222"
```

Enable NATS monitoring on the loopback interface. `monitor_url` accepts HTTP or HTTPS without credentials, query, or fragment. HTTPS uses system certificate roots. This module has no monitoring authentication or custom CA setting; keep the monitoring listener local. It does not connect to the NATS client port or subscribe to messages.

Every 60 seconds it calls `GET <monitor_url>/healthz`, `GET <monitor_url>/varz`, and `GET <monitor_url>/routez`. Plain `/healthz` includes stream and consumer currency checks when JetStream is enabled. An unhealthy or invalid health response stops that health collection before the remaining requests. Every 300 seconds it calls `GET <monitor_url>/jsz` without account, stream, consumer, or message detail flags. The daily version collector calls `/varz` again. JetStream storage maps to the API's `storage` and `reserved_storage` fields; omitted zero-valued JetStream counts are zero. A meta leader is reported only when `meta_cluster` exists.

With route pooling, `nats.routes` counts the individual pooled connections. For example, three brokers with a route pool size of three normally report six routes per broker (two peers × three connections).

All signals use the host resource. The health error is limited to 240 characters and clears to an empty string after a successful check. The API's health error text is included; other response bodies and transport errors are not copied into diagnostics.

Override `nats` (60 s), `nats-jetstream` (300 s), and `nats-version` (86400 s) in `[runner.cadence]`. A healthy broker emits eight metrics per minute plus five every five minutes, averaging nine metric samples/minute. Counts and storage describe this server; replicated storage on three brokers is not unique logical data size.

Use `[linux] profile = "nats"` for `nats-server.service`. On edge VMs, `profile = "nats-edge"` monitors `haproxy.service` and `keepalived.service` without enabling this collector.

## Signals

Dimensions below are additional to configured global dimensions. Plain counts use an empty unit (`""`). States are sent on change and refreshed after six hours when next observed; daily states are observed and refreshed daily. `once` runs every collection group regardless of cadence.

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `nats.healthy` | state boolean | `""` | — | `true` | Health HTTP status is 200 and JSON status is ok; false on a failed health request. | 60 s |
| `nats.health.error` | state string | `""` | — | `JetStream not enabled` | Truncated API health error, or an empty string after recovery. | 60 s |
| `nats.connections` | gauge | `""` | — | `12` | Current client connections. | 60 s |
| `nats.slow_consumers` | counter | `""` | — | `2` | Cumulative slow-consumer detections. | 60 s |
| `nats.messages.in` | counter | `""` | — | `1000` | Messages received by this broker. | 60 s |
| `nats.messages.out` | counter | `""` | — | `1200` | Messages sent by this broker. | 60 s |
| `nats.bytes.in` | counter | `bytes` | — | `1048576` | Bytes received by this broker. | 60 s |
| `nats.bytes.out` | counter | `bytes` | — | `2097152` | Bytes sent by this broker. | 60 s |
| `nats.memory` | gauge | `bytes` | — | `67108864` | Broker process memory from varz.mem. | 60 s |
| `nats.routes` | gauge | `""` | — | `2` | Current pooled cluster route connections, not the number of peer brokers. | 60 s |
| `nats.jetstream.streams` | gauge | `""` | — | `5` | Local JetStream stream count. | 300 s |
| `nats.jetstream.consumers` | gauge | `""` | — | `10` | Local JetStream consumer count. | 300 s |
| `nats.jetstream.storage.used` | gauge | `bytes` | — | `1048576` | Local JetStream file storage in use. | 300 s |
| `nats.jetstream.storage.reserved` | gauge | `bytes` | — | `2097152` | Local file storage reserved by configured stream limits. | 300 s |
| `nats.jetstream.api.errors` | counter | `""` | — | `3` | Cumulative JetStream API errors. | 300 s |
| `nats.jetstream.meta_leader` | state string | `""` | — | `broker-a` | JetStream metadata leader when meta_cluster is present. | 300 s |
| `nats.version` | state string | `""` | — | `2.12.0` | Broker version from varz. | 86400 s |
| `nats.available` | state boolean | `""` | collection | `true` | Whether this collection completed; collection is one of health, jetstream, version. | 60 / 300 / 86400 s |
| `ingestor.collector.ok` | state boolean | `""` | collector | `true` | Runner result for the scheduled collector name. | 60 / 300 / 86400 s |
| `ingestor.collector.failed` | event | `""` | collector | `error=collection failed` | One sanitized failure event per failed collection; the runner retries on the next tick. | on failure |

## Limits and failures

Each collection has a 10-second deadline, also bounded by the runner deadline. Responses or command stdout are limited to 1 MiB. HTTP collectors do not follow redirects or use environment proxies. Password files are limited to 4 KiB and CA files to 1 MiB; only regular files are accepted. Credentials are reread each collection so file rotation takes effect without a restart.

An unreachable endpoint, failed authentication, invalid TLS, missing command or file, timeout, oversized response, or malformed required field sets availability false and produces one `ingestor.collector.failed` event for that collection group. Other groups remain independent. Valid observations collected before a later request fails can still be emitted; check the matching availability state. Historical values remain in Pulse after a failure or when an optional field disappears.

Start troubleshooting with the endpoint or command, account permissions, certificate hostname, and file permissions. Verify locally with `pulse-linux --config /etc/pulse/pulse-linux.toml --local --pretty once`; this performs all configured probes. See [Linux ingestor](/en/ingestor-linux) for installation and operation.
