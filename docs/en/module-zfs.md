---
title: ZFS module
navTitle: ZFS
section: Modules
order: 380
description: ZFS pool, dataset, and snapshot collection.
tags: [zfs, storage, modules, reference]
updated: 2026-07-23
---

# ZFS module

The ZFS module collects pool, dataset, and snapshot data through ZFS tools.

## Entity types

| Entity type | Entity id | Used for |
|---|---|---|
| `host` | `host:<host-id>` | ZFS tool availability and aggregate pool, dataset, and snapshot counts. |
| `zfs-pool` | `zfs-pool:<host-id>:<pool>` | Pool health, capacity, allocation, free-space, fragmentation, and scan signals. |
| `zfs-dataset` | `zfs-dataset:<host-id>:<dataset>` | Dataset state, usage, mountpoint, compression, and snapshot-count signals. |

## Metrics

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `system.zfs.pools` | gauge |  | `host` | `1` | Number of ZFS pools returned for the host. |
| `system.zfs.pool.size` | gauge | bytes | `host`, `pool` | `1999844147200` | Total size in bytes for one ZFS pool. |
| `system.zfs.pool.allocated` | gauge | bytes | `host`, `pool` | `644245094400` | Allocated bytes in one ZFS pool. |
| `system.zfs.pool.free` | gauge | bytes | `host`, `pool` | `1355599052800` | Free bytes in one ZFS pool. |
| `system.zfs.pool.capacity` | gauge | percent | `host`, `pool` | `32` | Used capacity percentage for one ZFS pool. |
| `system.zfs.pool.fragmentation` | gauge | percent | `host`, `pool` | `8` | Fragmentation percentage for one ZFS pool. |
| `system.zfs.pool.scan.errors` | gauge | count | `host`, `pool` | `0` | Error count from the latest scan for one ZFS pool. |
| `system.zfs.datasets` | gauge |  | `host` | `12` | Number of ZFS filesystems and volumes returned for the host. |
| `system.zfs.dataset.used` | gauge | bytes | `host`, `dataset` | `10737418240` | Used bytes for one ZFS dataset or volume. |
| `system.zfs.dataset.available` | gauge | bytes | `host`, `dataset` | `214748364800` | Available bytes for one ZFS dataset or volume. |
| `system.zfs.dataset.referenced` | gauge | bytes | `host`, `dataset` | `5368709120` | Referenced bytes for one ZFS dataset or volume. |
| `system.zfs.dataset.compressratio` | gauge | ratio | `host`, `dataset` | `1.42` | Compression ratio for one ZFS dataset or volume. |
| `system.zfs.dataset.snapshots` | gauge |  | `host`, `dataset` | `7` | Snapshot count for one ZFS dataset. |
| `system.zfs.snapshots` | gauge |  | `host` | `34` | Total ZFS snapshot count on the host. |

## States

| Key | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|
| `system.zfs.available` | boolean | `host` | `true` | Whether either `zpool` or `zfs` is available. |
| `system.zfs.zpool.available` | boolean | `host` | `true` | Whether `zpool` is available. |
| `system.zfs.zfs.available` | boolean | `host` | `true` | Whether `zfs` is available. |
| `system.zfs.pool.present` | boolean | `host`, `pool` | `true` | One ZFS pool was returned by `zpool list`. |
| `system.zfs.pool.health` | string | `host`, `pool` | `ONLINE` | Raw health value for one ZFS pool. |
| `system.zfs.pool.healthy` | boolean | `host`, `pool` | `true` | Whether one ZFS pool reports `ONLINE`. |
| `system.zfs.pool.scan.status` | string | `host`, `pool` | `scrub repaired 0B` | Latest scan status text for one ZFS pool. |
| `system.zfs.pool.scan.completed_at` | string | `host`, `pool` | `Sun Jun 7 01:23:45 2026` | Latest scan completion text for one ZFS pool. |
| `system.zfs.dataset.present` | boolean | `host`, `dataset` | `true` | One ZFS dataset or volume was returned by `zfs list`. |
| `system.zfs.dataset.type` | string | `host`, `dataset` | `filesystem` | ZFS dataset type, usually `filesystem` or `volume`. |
| `system.zfs.dataset.mountpoint` | string | `host`, `dataset` | `/tank/data` | Mountpoint for one ZFS dataset when set. |

## Events

| Kind | Dimensions | Attributes | Trigger |
|---|---|---|---|
| `system.zfs.pools.failed` | `host`, `operation=pool_list` | `error` | `zpool list` failed. |
| `system.zfs.pool.status.failed` | `host`, `operation=pool_status` | `error` | `zpool status` failed. |
| `system.zfs.datasets.failed` | `host`, `operation=dataset_list` | `error` | `zfs list` for filesystems/volumes failed. |
| `system.zfs.snapshots.failed` | `host`, `operation=snapshot_list` | `error` | `zfs list` for snapshots failed. |

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `host` | `server-01` | all signals | Stable host id from configuration. |
| `pool` | `tank` | pool signals | ZFS pool name. |
| `dataset` | `tank/data` | dataset and snapshot signals | Dataset name. |
| `operation` | `pool_list` | failure events | Bounded collector operation that failed. |

## Requirements

- `zpool` and/or `zfs` commands.
- Permissions to list pools and datasets.

## Failure behavior

Missing tools emit availability states without a collector failure. Command failures for available tools emit module events.
