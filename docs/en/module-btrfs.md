---
title: Btrfs module
navTitle: Btrfs
section: Modules
order: 370
description: Btrfs mount and usage collection.
tags: [btrfs, storage, modules, reference]
updated: 2026-07-23
---

# Btrfs module

The Btrfs module detects Btrfs mounts and collects filesystem usage with `btrfs filesystem usage`.

## Entity types

| Entity type | Entity id | Used for |
|---|---|---|
| `host` | `host:<host-id>` | Btrfs availability and aggregate filesystem counts. |
| `filesystem` | `filesystem:<host-id>:<mount-key>` | Presence, usage availability, and usage metrics for one Btrfs mount. |

## Metrics

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `system.btrfs.filesystems` | gauge |  | `host` | `2` | Number of Btrfs mounts detected on the host. |
| `system.btrfs.filesystems.collected` | gauge |  | `host` | `2` | Number of Btrfs mounts whose usage command succeeded. |
| `system.btrfs.device.size` | gauge | bytes | `host`, `mount` | `107374182400` | Device size in bytes for one Btrfs filesystem. |
| `system.btrfs.device.allocated` | gauge | bytes | `host`, `mount` | `21474836480` | Allocated device bytes for one Btrfs filesystem. |
| `system.btrfs.device.unallocated` | gauge | bytes | `host`, `mount` | `85899345920` | Unallocated device bytes for one Btrfs filesystem. |
| `system.btrfs.used` | gauge | bytes | `host`, `mount` | `10737418240` | Used bytes for one Btrfs filesystem. |
| `system.btrfs.free` | gauge | bytes | `host`, `mount` | `96636764160` | Free bytes for one Btrfs filesystem. |

## States

| Key | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|
| `system.btrfs.available` | boolean | `host` | `true` | Whether Btrfs tooling and at least one Btrfs mount are available. |
| `system.btrfs.mount.present` | boolean | `host`, `mount` | `true` | A Btrfs mount was detected. |
| `system.filesystem.source` | string | `host`, `mount` | `/dev/sdb1` | Kernel mount source for the filesystem. |
| `system.btrfs.usage.available` | boolean | `host`, `mount` | `true` | `btrfs filesystem usage` succeeded for the mount. |

## Events

| Kind | Dimensions | Attributes | Trigger |
|---|---|---|---|
| `system.btrfs.mounts.failed` | `host`, `operation=read_mounts` | `error` | Mountinfo could not be read. |
| `system.btrfs.usage.failed` | `host`, `mount`, `operation=filesystem_usage` | `error` | Usage collection failed for one Btrfs filesystem. |

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `host` | `server-01` | all signals | Stable host id from configuration. |
| `mount` | `/data` | mount signals | Btrfs mount point. |
| `operation` | `filesystem_usage` | failure events | Bounded collector operation that failed. |

## Requirements

- `btrfs` command.
- Linux mountinfo through `host.proc_root`.
- Host root path through `host.root`.

## Failure behavior

Missing `btrfs` emits `system.btrfs.available=false`. No Btrfs mounts also emits unavailable without a collector failure. Usage failures are reported per mount.
