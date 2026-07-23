---
title: Dashboard examples
navTitle: Dashboards
section: Reference
order: 220
description: Query and Dashboard DSL patterns for ingestor telemetry.
tags: [dashboards, query, examples]
updated: 2026-07-23
---

# Dashboard examples

The ingestors model data so dashboards can group stable resources without depending on runtime IDs.

## Query patterns

| Question | Query |
|---|---|
| CPU trend per host | `metric system.cpu.usage avg every 5m group by resource since 24h` |
| Maximum filesystem usage per mount | `metric system.filesystem.usage max every 5m group by resource since 24h` |
| Receive throughput per interface | `metric system.network.rx rate every 5m reduce sum group by resource since 24h` |
| Total package updates across hosts | `metric system.packages.updates.total latest every 5m reduce sum since 24h` |
| CPU per Compose service | `metric docker.compose.service.cpu.usage avg every 5m group by resource since 24h` |
| Total receive throughput per container | `metric docker.container.network.rx rate every 5m reduce sum group by resource since 24h` |
| Memory per Proxmox VM | `metric proxmox.resource.memory.used avg every 5m group by resource since 24h entity_type proxmox-vm` |
| Failed Proxmox tasks | `events proxmox.task.failed count every 1h since 7d` |
| Availability ratio per endpoint | `metric uptime.check.availability avg every 5m group by resource since 24h` |
| Check duration per endpoint | `metric uptime.check.duration avg every 5m group by resource since 24h` |
| TLS certificate lifetime | `metric uptime.tls.certificate.expires_in min every 1h group by resource since 30d` |

`group by resource` returns one series per browsable resource. `reduce` combines multiple metric variants only after Pulse computes each variant correctly. This is useful for summing container interfaces or producing fleet totals.

## Reference dashboards

The repository includes complete Dashboard DSL examples:

- `examples/dashboards/linux-hosts.pulse`
- `examples/dashboards/docker-overview.pulse`
- `examples/dashboards/proxmox-overview.pulse`
- `examples/dashboards/uptime-overview.pulse`

Compile an example before saving it:

```sh
cld pulse dashboards compile --file examples/dashboards/docker-overview.pulse --json
```

Run each widget query against the target Pulse base before using the dashboard. Optional collectors can be absent, so a syntactically valid dashboard may have empty widgets on systems that do not expose those signals.
