---
title: Ceph module
navTitle: Ceph
section: Modules
order: 390
description: Local Ceph CLI health, OSD, PG, and pool collection.
tags: [ceph, storage, modules, reference]
updated: 2026-10-23
---

# Ceph module

The Ceph module collects local Ceph cluster data with the `ceph` CLI.

In `pulse-linux`, this collector runs every 300 seconds by default. Override `ceph` in `[runner.cadence]`. States are sent on change and refreshed after six hours when checked again; table cadences describe collection. Other binaries retain their own schedules.

## Resources

| Resource type | Resource key | Used for |
|---|---|---|
| `host` | `host:<host-id>` | Local Ceph CLI availability before a cluster can be identified. |
| `ceph-cluster` | `ceph-cluster:<fsid-or-host>` | Cluster health, OSD, PG, monitor, capacity, and pool-count signals. |
| `ceph-pool` | `ceph-pool:<fsid-or-host>:<pool>` | Pool presence, bytes, and object metrics. |

The Ceph FSID is the preferred cluster namespace. The stable host ID is used only when Ceph does not return an FSID.

## Metrics

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.ceph.osds.total` | gauge | — | — | `6` | Total OSD count for the local Ceph cluster view. | 300 s |
| `system.ceph.osds.up` | gauge | — | — | `6` | OSDs reported up in the local Ceph cluster view. | 300 s |
| `system.ceph.osds.in` | gauge | — | — | `6` | OSDs reported in in the local Ceph cluster view. | 300 s |
| `system.ceph.pgs.total` | gauge | — | — | `128` | Total placement group count for the cluster. | 300 s |
| `system.ceph.pgs.by_state` | gauge | — | `state` | `128` | Placement group count for one state such as `active+clean`. | 300 s |
| `system.ceph.bytes.used` | gauge | bytes | — | `1099511627776` | Used bytes for the Ceph cluster. | 300 s |
| `system.ceph.bytes.total` | gauge | bytes | — | `4398046511104` | Total bytes for the Ceph cluster. | 300 s |
| `system.ceph.bytes.available` | gauge | bytes | — | `3298534883328` | Available bytes for the Ceph cluster. | 300 s |
| `system.ceph.mons.quorum` | gauge | — | — | `3` | Number of monitors currently in quorum. | 300 s |
| `system.ceph.pools` | gauge | — | — | `4` | Number of pools returned by `ceph df`. | 300 s |
| `system.ceph.pool.bytes.used` | gauge | bytes | `pool` | `214748364800` | Used bytes for one Ceph pool. | 300 s |
| `system.ceph.pool.bytes.available` | gauge | bytes | `pool` | `1073741824000` | Available bytes for one Ceph pool. | 300 s |
| `system.ceph.pool.objects` | gauge | — | `pool` | `145223` | Object count for one Ceph pool. | 300 s |

## States

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.ceph.available` | state boolean | — | — | `true` | Whether the local Ceph CLI view is available. | 300 s; change / 6 h |
| `system.ceph.present` | state boolean | — | — | `true` | Whether a Ceph cluster was identified. Emitted on `ceph-cluster`. | 300 s; change / 6 h |
| `system.ceph.fsid` | state string | — | — | `45f4536f-...` | Ceph cluster FSID. Emitted on `ceph-cluster`. | 300 s; change / 6 h |
| `system.ceph.health.status` | state string | — | — | `HEALTH_OK` | Raw Ceph health status. | 300 s; change / 6 h |
| `system.ceph.health.healthy` | state boolean | — | — | `true` | Whether health status is `HEALTH_OK`. | 300 s; change / 6 h |
| `system.ceph.mon.in_quorum` | state boolean | — | `monitor` | `true` | Monitor is part of quorum. | 300 s; change / 6 h |
| `system.ceph.pool.present` | state boolean | — | `pool` | `true` | Pool returned by `ceph df`. | 300 s; change / 6 h |

## Events

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.ceph.status.failed` | event | — | `operation=status` | `error` | `ceph status --format json` failed or could not be parsed. | on trigger at 300 s |
| `system.ceph.df.failed` | event | — | `operation=df\ | parse_df` | `error` | on trigger at 300 s |

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `state` | `active+clean` | PG metrics | Ceph PG state name. |
| `monitor` | `mon-a` | monitor states | Monitor name in quorum. |
| `pool` | `rbd` | pool signals | Ceph pool name. |
| `operation` | `status` | failure events | Bounded collector operation that failed. |

## Requirements

- `ceph` command.
- Local Ceph configuration and permissions.

## Failure behavior

Missing or unconfigured Ceph emits `system.ceph.available=false` without noisy follow-up events. Unexpected command or JSON parsing failures emit events.
