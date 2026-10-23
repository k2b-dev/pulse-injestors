---
title: ZFS module
navTitle: ZFS
section: Modules
order: 380
description: ZFS pool, dataset, and snapshot collection.
tags: [zfs, storage, modules, reference]
updated: 2026-10-23
---

# ZFS module

The ZFS module collects pool, dataset, and snapshot data through ZFS tools.

In `pulse-linux`, this collector runs every 300 seconds by default. Override `zfs` in `[runner.cadence]`. States are sent on change and refreshed after six hours when checked again; table cadences describe collection. Other binaries retain their own schedules.

## Resource types

| Resource type | Resource key | Used for |
|---|---|---|
| `host` | `host:<host-id>` | ZFS tool availability and aggregate pool, dataset, and snapshot counts. |
| `zfs-pool` | `zfs-pool:<host-id>:<pool>` | Pool health, capacity, allocation, free-space, fragmentation, and scan signals. |
| `zfs-dataset` | `zfs-dataset:<host-id>:<dataset>` | Dataset state, usage, mountpoint, compression, and snapshot-count signals. |

## Metrics

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.zfs.pools` | gauge | — | — | `1` | Number of ZFS pools returned for the host. | 300 s |
| `system.zfs.pool.size` | gauge | bytes | `pool` | `1999844147200` | Total size in bytes for one ZFS pool. | 300 s |
| `system.zfs.pool.allocated` | gauge | bytes | `pool` | `644245094400` | Allocated bytes in one ZFS pool. | 300 s |
| `system.zfs.pool.free` | gauge | bytes | `pool` | `1355599052800` | Free bytes in one ZFS pool. | 300 s |
| `system.zfs.pool.capacity` | gauge | percent | `pool` | `32` | Used capacity percentage for one ZFS pool. | 300 s |
| `system.zfs.pool.fragmentation` | gauge | percent | `pool` | `8` | Fragmentation percentage for one ZFS pool. | 300 s |
| `system.zfs.pool.scan.errors` | gauge | — | `pool` | `0` | Error count from the latest scan for one ZFS pool. | 300 s |
| `system.zfs.datasets` | gauge | — | — | `12` | Number of ZFS filesystems and volumes returned for the host. | 300 s |
| `system.zfs.dataset.used` | gauge | bytes | `dataset` | `10737418240` | Used bytes for one ZFS dataset or volume. | 300 s |
| `system.zfs.dataset.available` | gauge | bytes | `dataset` | `214748364800` | Available bytes for one ZFS dataset or volume. | 300 s |
| `system.zfs.dataset.referenced` | gauge | bytes | `dataset` | `5368709120` | Referenced bytes for one ZFS dataset or volume. | 300 s |
| `system.zfs.dataset.compressratio` | gauge | — | `dataset` | `1.42` | Compression ratio for one ZFS dataset or volume. | 300 s |
| `system.zfs.dataset.snapshots` | gauge | — | `dataset` | `7` | Snapshot count for one ZFS dataset. | 300 s |
| `system.zfs.snapshots` | gauge | — | — | `34` | Total ZFS snapshot count on the host. | 300 s |

## States

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.zfs.available` | state boolean | — | — | `true` | Whether either `zpool` or `zfs` is available. | 300 s; change / 6 h |
| `system.zfs.zpool.available` | state boolean | — | — | `true` | Whether `zpool` is available. | 300 s; change / 6 h |
| `system.zfs.zfs.available` | state boolean | — | — | `true` | Whether `zfs` is available. | 300 s; change / 6 h |
| `system.zfs.pool.present` | state boolean | — | `pool` | `true` | One ZFS pool was returned by `zpool list`. | 300 s; change / 6 h |
| `system.zfs.pool.health` | state string | — | `pool` | `ONLINE` | Raw health value for one ZFS pool. | 300 s; change / 6 h |
| `system.zfs.pool.healthy` | state boolean | — | `pool` | `true` | Whether one ZFS pool reports `ONLINE`. | 300 s; change / 6 h |
| `system.zfs.pool.scan.status` | state string | — | `pool` | `scrub repaired 0B` | Latest scan status text for one ZFS pool. | 300 s; change / 6 h |
| `system.zfs.pool.scan.completed_at` | state string | — | `pool` | `Sun Jun 7 01:23:45 2026` | Latest scan completion text for one ZFS pool. | 300 s; change / 6 h |
| `system.zfs.dataset.present` | state boolean | — | `dataset` | `true` | One ZFS dataset or volume was returned by `zfs list`. | 300 s; change / 6 h |
| `system.zfs.dataset.type` | state string | — | `dataset` | `filesystem` | ZFS dataset type, usually `filesystem` or `volume`. | 300 s; change / 6 h |
| `system.zfs.dataset.mountpoint` | state string | — | `dataset` | `/tank/data` | Mountpoint for one ZFS dataset when set. | 300 s; change / 6 h |

## Events

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.zfs.pools.failed` | event | — | `operation=pool_list` | `error` | `zpool list` failed. | on trigger at 300 s |
| `system.zfs.pool.status.failed` | event | — | `operation=pool_status` | `error` | `zpool status` failed. | on trigger at 300 s |
| `system.zfs.datasets.failed` | event | — | `operation=dataset_list` | `error` | `zfs list` for filesystems/volumes failed. | on trigger at 300 s |
| `system.zfs.snapshots.failed` | event | — | `operation=snapshot_list` | `error` | `zfs list` for snapshots failed. | on trigger at 300 s |

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `pool` | `tank` | pool signals | ZFS pool name. |
| `dataset` | `tank/data` | dataset and snapshot signals | Dataset name. |
| `operation` | `pool_list` | failure events | Bounded collector operation that failed. |

## Requirements

- `zpool` and/or `zfs` commands.
- Permissions to list pools and datasets.

## Failure behavior

Missing tools emit availability states without a collector failure. Command failures for available tools emit module events.
