---
title: Telemetry model
navTitle: Telemetry model
section: Reference
order: 30
description: Pulse V1 resources, dimensions, event context, identities, and units.
tags: [telemetry, resources, dimensions, reference]
updated: 2026-07-23
---

# Telemetry model

Every ingestor uses the Pulse V1 telemetry model.

## Signal types

| Type | Purpose | Example |
|---|---|---|
| Metric | Numeric time series. | CPU percentage, memory bytes, task count. |
| State | Latest known value of a fact. | Service status, version, image ID. |
| Event | Historical occurrence. | Collection failure, failed backup task. |
| Resource | Stable object users can browse. | Host, container, filesystem, VM, Ceph pool. |

## Resources

Every resource has a stable `type`, `id`, and human-readable `label`. The canonical resource key is `<type>:<id>`, for example:

- `host:server-01`
- `docker-container:server-01:compose:pulse:api:1`
- `filesystem:server-01:var_lib_docker`
- `proxmox-vm:lab:100`
- `uptime-endpoint:office-berlin:pulse-api`

Labels are display values such as `server-01`, `app-core`, `/var/lib/docker`, or `postgres`. Runtime IDs, hashes, and paths do not replace a useful label.

Create a resource for an object users need to open and understand. Attachment details and bounded categories remain dimensions when a separate resource would add no useful page.

## Dimensions

Dimensions are bounded labels used by exact filters and grouping:

```json
{
  "host": "server-01",
  "environment": "production",
  "compose_project": "pulse",
  "compose_service": "api"
}
```

Suitable dimensions include environment, region, host, service, interface, mount, pool, probe, endpoint, and normalized status. Runtime container IDs, request IDs, full paths, IP addresses, URLs, user IDs, digests, and raw error messages are not dimensions.

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
| `count` | Counter values where the counted item is part of the meaning. |
| empty | Plain cardinalities such as CPU cores, container totals, or process totals. |

Each [module reference](/en/modules) lists the exact resource, dimensions, type, unit, and example for every emitted signal.
