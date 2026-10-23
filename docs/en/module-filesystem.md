---
title: Filesystem module
navTitle: Filesystem
section: Modules
order: 330
description: Host mount and filesystem usage collection.
tags: [filesystem, modules, reference]
updated: 2026-10-23
---

# Filesystem module

The filesystem module reports mounted filesystems and their capacity.

In `pulse-linux`, this collector runs every 300 seconds by default. Override `filesystem` in `[runner.cadence]`. States are sent on change and refreshed after six hours when checked again; table cadences describe collection. Other binaries retain their own schedules.

## Resource types

| Resource type | Resource key | Used for |
|---|---|---|
| `host` | `host:<host-id>` | Filesystem availability and collector-level failures. |
| `filesystem` | `filesystem:<host-id>:<mount-key>` | Capacity, inode, and readonly signals for one mounted filesystem. |

## Metrics

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.filesystem.total` | gauge | bytes | `mount` | `107374182400` | Total capacity in bytes for one mounted filesystem. | 300 s |
| `system.filesystem.available` | gauge | bytes | `mount` | `64424509440` | Bytes available to unprivileged users on one mounted filesystem. | 300 s |
| `system.filesystem.used` | gauge | bytes | `mount` | `42949672960` | Used bytes on one mounted filesystem. | 300 s |
| `system.filesystem.usage` | gauge | percent | `mount` | `40` | Used bytes as a percentage of total bytes for one mount. | 300 s |
| `system.filesystem.inodes.used` | gauge | — | `mount` | `180322` | Used inode count for one mount when reported by the OS. | 300 s |
| `system.filesystem.inodes.usage` | gauge | percent | `mount` | `6.1` | Used inodes as a percentage of total inodes for one mount. | 300 s |

## States

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.filesystem.type` | state string | — | `mount` | `ext4` | Filesystem type reported by the OS. | 300 s; change / 6 h |
| `system.filesystem.source` | state string | — | `mount` | `/dev/nvme0n1p2` | Kernel or operating-system mount source. | 300 s; change / 6 h |
| `system.filesystem.readonly` | state boolean | — | `mount` | `false` | Whether the mount is read-only. | 300 s; change / 6 h |

## Events

This module does not emit events directly. Mountinfo or `statfs` failures are returned to the runner.

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `mount` | `/var/lib/docker` | filesystem signals | Mount point on the host. |

## Requirements

- Linux mountinfo through `host.proc_root`.
- Host root path through `host.root` for statfs calls.

## Failure behavior

Missing mountinfo produces a collector failure. Individual statfs failures are skipped so other mounts can still report.
