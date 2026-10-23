---
title: Modules
navTitle: Modules
section: Reference
order: 200
description: Reusable collector modules used by the Pulse ingestor binaries.
tags: [modules, reference]
updated: 2026-10-23
---

# Modules

Collector modules live under `internal/modules` and are reused by the specialized binaries. Each module page lists emitted metrics, states, events, dimensions, units, value types, and examples.

## Host modules

- [`system`](/en/module-system): CPU, memory, load, and uptime from host system files.
- [`filesystem`](/en/module-filesystem): mount and filesystem usage data.
- [`network`](/en/module-network): Linux interface counters.
- [`linuxruntime`](/en/module-linuxruntime): Linux pressure, process counts, and socket summaries.
- [`kernel`](/en/module-kernel): OOM kills, boot time, recent boots, and kernel errors.
- [`inventory`](/en/module-inventory): daily OS, kernel, hardware, and installed package versions.
- [`thermal`](/en/module-thermal): hwmon/sysfs thermal zones where available.

## Platform modules

- [`docker`](/en/module-docker): Docker Engine API container, image, network, mount, and registry freshness data.
- [`macos`](/en/module-macos): macOS system, battery, filesystem, Homebrew, software update, display, and GPU data.
- [`proxmox`](/en/module-proxmox): Proxmox VE cluster, resource, Ceph, task, and backup data through local `pvesh`.
- [`pbs`](/en/module-pbs): Proxmox Backup Server datastore, snapshot, job, and task data through local `proxmox-backup-debug`.

## Storage and service modules

- [`btrfs`](/en/module-btrfs): Btrfs mount presence and usage data.
- [`zfs`](/en/module-zfs): ZFS pool, dataset, and snapshot data through ZFS tools.
- [`ceph`](/en/module-ceph): local Ceph CLI health, OSD, PG, and pool data.
- [`diskio`](/en/module-diskio): whole-device I/O, operation, and busy-time counters.
- [`certificates`](/en/module-certificates): local PEM certificate expiration.
- [`backup`](/en/module-backup): last successful backup from marker files or commands.
- [`diskhealth`](/en/module-diskhealth): SMART and NVMe disk health through host tools.
- [`packages`](/en/module-packages): package update counts for supported Linux package managers.
- [`systemd`](/en/module-systemd): configured systemd unit states and all failed units.

## Extension module

- [`script`](/en/module-script): runs configured commands that emit Pulse JSON fragments.

## Connectivity module

- [`uptime`](/en/module-uptime): named ICMP, DNS, TCP, HTTP, and TLS endpoint checks.

Optional modules are best-effort and auto-enabled by default when their binary includes them. Expected absence, such as missing ZFS tools or no Ceph cluster, reports availability states without collector failure events. Unexpected command, permission, timeout, or parsing failures remain visible as events.

## Runner signals

These signals use the ingestor's root resource: `host` for host ingestors and `uptime-probe` for `pulse-uptime`. Host ingestors add no automatic `host` dimension. `pulse-uptime` retains `probe`; configured global dimensions remain supported. The table lists additional dimensions: self-monitoring adds `ingestor=<binary>`, while collector results add `collector=<name>`.

| Name | Signal type | Unit | Added dimensions | Example | Meaning |
|---|---|---|---|---|---|
| `ingestor.heartbeat` | gauge | none | `ingestor` | `1` | One sample per collection cycle; use it for liveness. |
| `ingestor.cycle.duration` | gauge | `seconds` | `ingestor` | `0.8` | Duration of the previous collection and queue write; absent in the first cycle. |
| `ingestor.spool.bytes` | gauge | `bytes` | `ingestor` | `16384` | Queued bytes before the current collection is enqueued. |
| `ingestor.spool.parts` | gauge | none | `ingestor` | `2` | Number of queued parts before enqueueing this collection. |
| `ingestor.spool.oldest_age` | gauge | `seconds` | `ingestor` | `120` | Age of the oldest queued part; zero for an empty queue. |
| `ingestor.delivery.failures` | counter | none | `ingestor` | `3` | Failed delivery attempts since process start, including invalid acknowledgements. |
| `ingestor.clock.skew` | gauge | `seconds` | `ingestor` | `-130` | Pulse server time minus local time, estimated from the HTTP Date header; absent until known. |
| `ingestor.version` | state string | none | `ingestor` | `v0.1.0` | Running binary version. |
| `ingestor.delivery.rejected` | event | none | `ingestor`, `reason` | `attributes.item` | Rejected telemetry; includes status, message, item when isolated, and item counts. `reason` is Pulse's error code. |
| `ingestor.telemetry.dropped` | event | none | `ingestor`, `reason` | `reason=budget` | Parts lost to the byte budget or expiry; attributes include parts and bytes. |
| `ingestor.collector.ok` | state boolean | none | `collector` | `true` | Whether one collector completed without returning an error. |
| `ingestor.collector.failed` | event | none | `collector` | `attributes.error` | One collector returned an error. |

States are queued on value changes and refreshed after six hours when observed again, except `software.*` states, which refresh after seven days. Daily observations keep software entries alive; cache entries are forgotten only after seven days without observation. With a persistent spool, `<spool_dir>/.states` preserves this cache across restarts. A state timestamp is the last change or refresh, not the last check.

After the spool drops or rejects data, all states observed in the next collection cycle are sent again.

Local output includes collector results but bypasses delivery self-monitoring and state suppression. Delivery events also travel through the queue; during an outage, inspect local logs. The removed `ingestor.collect.ok` and `ingestor.collect.empty` signals are no longer emitted.
