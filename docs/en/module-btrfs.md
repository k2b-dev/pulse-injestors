---
title: Btrfs module
navTitle: Btrfs
section: Modules
order: 370
description: Btrfs mount and usage collection.
tags: [btrfs, storage, modules, reference]
updated: 2026-10-23
---

# Btrfs module

The Btrfs module detects Btrfs mounts and collects filesystem usage with `btrfs filesystem usage`.

In `pulse-linux`, this collector runs every 300 seconds by default. Override `btrfs` in `[runner.cadence]`. States are sent on change and refreshed after six hours when checked again; table cadences describe collection. Other binaries retain their own schedules.

## Resource types

| Resource type | Resource key | Used for |
|---|---|---|
| `host` | `host:<host-id>` | Btrfs availability and aggregate filesystem counts. |
| `filesystem` | `filesystem:<host-id>:<mount-key>` | Presence, usage availability, and usage metrics for one Btrfs mount. |

## Metrics

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.btrfs.filesystems` | gauge | — | — | `2` | Number of Btrfs mounts detected on the host. | 300 s |
| `system.btrfs.filesystems.collected` | gauge | — | — | `2` | Number of Btrfs mounts whose usage command succeeded. | 300 s |
| `system.btrfs.device.size` | gauge | bytes | `mount` | `107374182400` | Device size in bytes for one Btrfs filesystem. | 300 s |
| `system.btrfs.device.allocated` | gauge | bytes | `mount` | `21474836480` | Allocated device bytes for one Btrfs filesystem. | 300 s |
| `system.btrfs.device.unallocated` | gauge | bytes | `mount` | `85899345920` | Unallocated device bytes for one Btrfs filesystem. | 300 s |
| `system.btrfs.used` | gauge | bytes | `mount` | `10737418240` | Used bytes for one Btrfs filesystem. | 300 s |
| `system.btrfs.free` | gauge | bytes | `mount` | `96636764160` | Free bytes for one Btrfs filesystem. | 300 s |

## States

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.btrfs.available` | state boolean | — | — | `true` | Whether Btrfs tooling and at least one Btrfs mount are available. | 300 s; change / 6 h |
| `system.btrfs.mount.present` | state boolean | — | `mount` | `true` | A Btrfs mount was detected. | 300 s; change / 6 h |
| `system.btrfs.usage.available` | state boolean | — | `mount` | `true` | `btrfs filesystem usage` succeeded for the mount. | 300 s; change / 6 h |

## Events

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.btrfs.mounts.failed` | event | — | `operation=read_mounts` | `error` | Mountinfo could not be read. | on trigger at 300 s |
| `system.btrfs.usage.failed` | event | — | `mount`, `operation=filesystem_usage` | `error` | Usage collection failed for one Btrfs filesystem. | on trigger at 300 s |

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `mount` | `/data` | mount signals | Btrfs mount point. |
| `operation` | `filesystem_usage` | failure events | Bounded collector operation that failed. |

## Requirements

- `btrfs` command.
- Linux mountinfo through `host.proc_root`.
- Host root path through `host.root`.

## Failure behavior

Missing `btrfs` emits `system.btrfs.available=false`. No Btrfs mounts also emits unavailable without a collector failure. Usage failures are reported per mount.
