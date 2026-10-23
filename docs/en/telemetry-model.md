---
title: Telemetry model
navTitle: Telemetry model
section: Reference
order: 30
description: Pulse V1 resources, dimensions, event context, identities, and units.
tags: [telemetry, resources, dimensions, reference]
updated: 2026-10-23
---

# Telemetry model

Every ingestor sends the current Pulse resource contract to `POST /api/pulse/ingest`. Use a separate HTTP source token for independently managed senders, or share a token between ingestors when you want one source. Each ingestor still needs its own spool directory. Removed identity fields and unknown script or TOML fields are rejected.

Metrics support `gauge` and nonnegative `counter` values. A metric name has one immutable type and unit within a Pulse base. Changing a definition requires a new signal name. Counter rates are calculated per series before aggregation.

## Signal types

| Type | Purpose | Example |
|---|---|---|
| Metric | Numeric time series. | CPU percentage, memory bytes, task count. |
| State | Latest known value of a fact. | Service status, version, image ID. |
| Event | Historical occurrence. | Collection failure, failed backup task. |
| Resource | Stable object users can browse. | Host, container, filesystem, VM, Ceph pool. |

## State freshness and liveness

States are queued when their value changes and refreshed after six hours when observed again (`software.*`: seven days). Daily observations prevent software inventory entries from being forgotten. The cache is persisted at `<spool_dir>/.states` when a spool directory is configured. In Pulse, a state timestamp records its last change or refresh, not the last check.

After the spool drops or rejects data, the cache is cleared so all states observed in the next collection cycle are sent again.

Use `ingestor.heartbeat` for liveness. It is a gauge with value `1` per cycle, with `ingestor=<binary>`. It uses the host resource, except for `pulse-uptime`, which uses its `uptime-probe` resource. Set the Pulse source's **Expected data** (`expectEverySeconds`) so Pulse detects a source that goes silent. See [runner signals](/en/modules#runner-signals) for the complete self-monitoring reference.

## Resources

Every resource has a stable `type`, `id`, and human-readable `label`. The canonical resource key is `<type>:<id>`, for example:

- `host:server-01`
- `docker-container:server-01:compose:pulse:api:1`
- `filesystem:server-01:%2Fvar%2Flib%2Fdocker`
- `proxmox-vm:lab:100`
- `uptime-endpoint:office-berlin:pulse-api`

IDs composed of multiple parts percent-encode separators inside each part. For example, `/var/lib` and `/var_lib` remain different filesystem resources. Supply the raw stable host ID in configuration, without a type prefix; it is not parsed as a legacy key.

Labels are display values such as `server-01`, `app-core`, `/var/lib/docker`, or `postgres`. Runtime IDs, hashes, and paths do not replace a useful label.

Create a resource for an object users need to open and understand. Attachment details and bounded categories remain dimensions when a separate resource would add no useful page.

## Dimensions

Dimensions are bounded labels used by exact filters and grouping:

```json
{
  "environment": "production",
  "compose_project": "pulse",
  "compose_service": "api"
}
```

Suitable dimensions include environment, region, service, interface, mount, pool, probe, endpoint, and normalized status. Runtime container IDs, request IDs, full paths, IP addresses, URLs, user IDs, digests, and raw error messages are not dimensions.

## Event context

| Field | Use | Example |
|---|---|---|
| `attributes` | Irregular or high-cardinality context. | Error text, task timestamps, worker ID. |
| `sensitive` | Protected values with shorter retention. | Raw IP address or precise location. |
| `actorId` | Person or system that acted. | Proxmox task user. |
| `sessionId` | Stable session identity. | Script-defined session. |
| `correlationId` | Trace or operation identity. | Proxmox UPID. |
| `payload` | Additional bounded JSON when structured fields are insufficient. | Domain-specific script output. |

Built-in collector errors put the error message in `attributes.error`. A bounded `operation` dimension identifies the failed collector action.

## Units

| Unit | Use |
|---|---|
| `percent` | Utilization, capacity usage, health, and ratios expressed as percentages. |
| `bytes` | Memory, storage, and I/O sizes. |
| `seconds` | Uptime, age, and duration. |
| `milliseconds` | Short measured latency and request duration. |
| `celsius` / `kelvin` | Temperature. |
| empty (`""`) | Counts, load averages, and dimensionless ratios. Cumulative error and event counts use type `counter`; current totals use `gauge`. |

Each [module reference](/en/modules) lists the exact resource, dimensions, type, unit, and example for every emitted signal.

Host ingestors no longer inject a `host` dimension. Use the resource key to identify the host; existing resource IDs are unchanged. The `host` dimension is reserved and rejected in configuration; other global dimensions remain configurable.
