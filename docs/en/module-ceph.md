---
title: Ceph module
navTitle: Ceph
section: Modules
order: 390
description: Elected Proxmox Ceph API collection and optional local CLI collection.
tags: [ceph, storage, modules, reference]
updated: 2026-10-23
---

# Ceph module

On PVE, Ceph comes from **pulse-proxmox**, through the PVE API on the elected node only. Health, OSDs, placement groups, capacity and pools are collected every 60 seconds. Run pulse-linux too for the node's host telemetry.

The local `ceph` CLI collector in pulse-linux is **disabled by default** (`[host] enable_ceph = false`). It can be enabled explicitly for a non-PVE Ceph installation; it runs every 300 seconds and has no election. Enable it on only one reporter per Ceph cluster to avoid duplicates. `pulse-proxmox` and `pulse-proxmox-backup-server` never run the local CLI collector.

States are sent on change and refreshed after six hours when checked again. Below, “60 / 300 s” means PVE API / optional local CLI; CLI-only rows remain 300 s.

## Resources

| Resource type | Resource key | Used for |
|---|---|---|
| `host` | `host:<host-id>` | Local Ceph CLI availability before a cluster can be identified. |
| `ceph-cluster` | `ceph-cluster:<fsid-or-host>` | Cluster health, OSD, PG, monitor, capacity, and pool-count signals. |
| `ceph-pool` | `ceph-pool:<fsid-or-host>:<pool>` | Pool presence, bytes, and object metrics. |

The Ceph FSID is the preferred cluster namespace. When FSID is absent, the PVE path uses the cluster name (host ID for standalone PVE); the local CLI path uses the host ID.

## Metrics

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.ceph.osds.total` | gauge | — | — | `6` | Total OSD count for the local Ceph cluster view. | 60 / 300 s |
| `system.ceph.osds.up` | gauge | — | — | `6` | OSDs reported up in the local Ceph cluster view. | 60 / 300 s |
| `system.ceph.osds.in` | gauge | — | — | `6` | OSDs reported in in the local Ceph cluster view. | 60 / 300 s |
| `system.ceph.pgs.total` | gauge | — | — | `128` | Total placement group count for the cluster. | 60 / 300 s |
| `system.ceph.pgs.by_state` | gauge | — | `state` | `128` | Placement group count for one state such as `active+clean`. | 60 / 300 s |
| `system.ceph.bytes.used` | gauge | bytes | — | `1099511627776` | Used bytes for the Ceph cluster. | 60 / 300 s |
| `system.ceph.bytes.total` | gauge | bytes | — | `4398046511104` | Total bytes for the Ceph cluster. | 60 / 300 s |
| `system.ceph.bytes.available` | gauge | bytes | — | `3298534883328` | Available bytes for the Ceph cluster. | 60 / 300 s |
| `system.ceph.mons.quorum` | gauge | — | — | `3` | Number of monitors currently in quorum. | 300 s (CLI only) |
| `system.ceph.pools` | gauge | — | — | `4` | Number of pools returned by the API or `ceph df`. | 60 / 300 s |
| `system.ceph.pool.bytes.used` | gauge | bytes | `pool` | `214748364800` | Used bytes for one Ceph pool. | 60 / 300 s |
| `system.ceph.pool.bytes.available` | gauge | bytes | `pool` | `1073741824000` | Available bytes for one Ceph pool. | 60 / 300 s |
| `system.ceph.pool.objects` | gauge | — | `pool` | `145223` | Object count for one Ceph pool. | 60 / 300 s |
| `system.ceph.pgs.not_active_clean` | gauge | — | — | `2` | Total PGs minus those exactly active+clean; PVE API only. | 60 s |

## States

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.ceph.available` | state boolean | — | — | `true` | Whether the API or local CLI Ceph view is available. | 60 / 300 s; change / 6 h |
| `system.ceph.present` | state boolean | — | — | `true` | Whether a Ceph cluster was identified. Emitted on `ceph-cluster`. | 60 / 300 s; change / 6 h |
| `system.ceph.fsid` | state string | — | — | `45f4536f-...` | Ceph cluster FSID. Emitted on `ceph-cluster`. | 60 / 300 s; change / 6 h |
| `system.ceph.health.status` | state string | — | — | `HEALTH_OK` | Raw Ceph health status. | 60 / 300 s; change / 6 h |
| `system.ceph.health.healthy` | state boolean | — | — | `true` | Whether health status is `HEALTH_OK`. | 60 / 300 s; change / 6 h |
| `system.ceph.mon.in_quorum` | state boolean | — | `monitor` | `true` | Monitor is part of quorum. | 300 s (CLI only); change / 6 h |
| `system.ceph.pool.present` | state boolean | — | `pool` | `true` | Pool returned by the API or `ceph df`. | 60 / 300 s; change / 6 h |

## Events

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.ceph.status.failed` | event | — | `operation=status` | `error` | `ceph status --format json` failed or could not be parsed. | on trigger at 300 s (CLI only) |
| `system.ceph.df.failed` | event | — | `operation` | `error` | CLI capacity query or JSON parsing failed. | 300 s (CLI only), on error |

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `state` | `active+clean` | PG metrics | Ceph PG state name. |
| `monitor` | `mon-a` | monitor states | Monitor name in quorum. |
| `pool` | `rbd` | pool signals | Ceph pool name. |
| `operation` | `status` | failure events | Bounded collector operation that failed. |

## Requirements and API paths

PVE API collection requires local `pvesh` permissions. It calls `/cluster/ceph/status` and `/nodes/<elected>/ceph/pools`; it never probes other nodes for pools. API failures use `proxmox.ceph.status.failed` and `proxmox.ceph.pools.failed` (event, no unit, dimension `operation`, 60 s on error; `attributes.error`). See the [Proxmox module](/en/module-proxmox) for the election rule.

The optional CLI path requires the `ceph` command, local Ceph configuration and permissions. It executes `ceph status --format json` and `ceph df --format json`.

Pool `bytes.available` is the API's `max_avail`, the estimate of additional bytes the pool can store under its replication/erasure rules. It is not a separate physical capacity or a quota. Both collectors emit pool objects as a unitless gauge.

## Failure behavior

Missing or unconfigured Ceph emits `system.ceph.available=false` without noisy follow-up events. Unexpected command or JSON parsing failures emit events.
