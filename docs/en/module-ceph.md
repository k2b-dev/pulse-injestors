---
title: Ceph module
navTitle: Ceph
section: Modules
order: 390
description: Local Ceph CLI health, OSD, PG, and pool collection.
tags: [ceph, storage, modules, reference]
updated: 2026-07-23
---

# Ceph module

The Ceph module collects local Ceph cluster data with the `ceph` CLI.

## Resources

| Resource type | Resource key | Used for |
|---|---|---|
| `host` | `host:<host-id>` | Local Ceph CLI availability before a cluster can be identified. |
| `ceph-cluster` | `ceph-cluster:<fsid-or-host>` | Cluster health, OSD, PG, monitor, capacity, and pool-count signals. |
| `ceph-pool` | `ceph-pool:<fsid-or-host>:<pool>` | Pool presence, bytes, and object metrics. |

The Ceph FSID is the preferred cluster namespace. The stable host ID is used only when Ceph does not return an FSID.

## Metrics

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `system.ceph.osds.total` | gauge |  | `host` | `6` | Total OSD count for the local Ceph cluster view. |
| `system.ceph.osds.up` | gauge |  | `host` | `6` | OSDs reported up in the local Ceph cluster view. |
| `system.ceph.osds.in` | gauge |  | `host` | `6` | OSDs reported in in the local Ceph cluster view. |
| `system.ceph.pgs.total` | gauge |  | `host` | `128` | Total placement group count for the cluster. |
| `system.ceph.pgs.by_state` | gauge |  | `host`, `state` | `128` | Placement group count for one state such as `active+clean`. |
| `system.ceph.bytes.used` | gauge | bytes | `host` | `1099511627776` | Used bytes for the Ceph cluster. |
| `system.ceph.bytes.total` | gauge | bytes | `host` | `4398046511104` | Total bytes for the Ceph cluster. |
| `system.ceph.bytes.available` | gauge | bytes | `host` | `3298534883328` | Available bytes for the Ceph cluster. |
| `system.ceph.mons.quorum` | gauge |  | `host` | `3` | Number of monitors currently in quorum. |
| `system.ceph.pools` | gauge |  | `host` | `4` | Number of pools returned by `ceph df`. |
| `system.ceph.pool.bytes.used` | gauge | bytes | `host`, `pool` | `214748364800` | Used bytes for one Ceph pool. |
| `system.ceph.pool.bytes.available` | gauge | bytes | `host`, `pool` | `1073741824000` | Available bytes for one Ceph pool. |
| `system.ceph.pool.objects` | gauge | count | `host`, `pool` | `145223` | Object count for one Ceph pool. |

## States

| Key | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|
| `system.ceph.available` | boolean | `host` | `true` | Whether the local Ceph CLI view is available. |
| `system.ceph.present` | boolean | `host` | `true` | Whether a Ceph cluster was identified. Emitted on `ceph-cluster`. |
| `system.ceph.fsid` | string | `host` | `45f4536f-...` | Ceph cluster FSID. Emitted on `ceph-cluster`. |
| `system.ceph.health.status` | string | `host` | `HEALTH_OK` | Raw Ceph health status. |
| `system.ceph.health.healthy` | boolean | `host` | `true` | Whether health status is `HEALTH_OK`. |
| `system.ceph.mon.in_quorum` | boolean | `host`, `monitor` | `true` | Monitor is part of quorum. |
| `system.ceph.pool.present` | boolean | `host`, `pool` | `true` | Pool returned by `ceph df`. |

## Events

| Kind | Dimensions | Attributes | Trigger |
|---|---|---|---|
| `system.ceph.status.failed` | `host`, `operation=status` | `error` | `ceph status --format json` failed or could not be parsed. |
| `system.ceph.df.failed` | `host`, `operation=df\|parse_df` | `error` | `ceph df --format json` failed or could not be parsed. |

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `host` | `server-01` | all signals | Stable host id from configuration. |
| `state` | `active+clean` | PG metrics | Ceph PG state name. |
| `monitor` | `mon-a` | monitor states | Monitor name in quorum. |
| `pool` | `rbd` | pool signals | Ceph pool name. |
| `operation` | `status` | failure events | Bounded collector operation that failed. |

## Requirements

- `ceph` command.
- Local Ceph configuration and permissions.

## Failure behavior

Missing or unconfigured Ceph emits `system.ceph.available=false` without noisy follow-up events. Unexpected command or JSON parsing failures emit events.
