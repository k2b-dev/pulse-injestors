---
title: Filesystem module
navTitle: Filesystem
section: Modules
order: 330
description: Host mount and filesystem usage collection.
tags: [filesystem, modules, reference]
updated: 2026-07-23
---

# Filesystem module

The filesystem module reports mounted filesystems and their capacity.

## Entity types

| Entity type | Entity id | Used for |
|---|---|---|
| `host` | `host:<host-id>` | Filesystem availability and collector-level failures. |
| `filesystem` | `filesystem:<host-id>:<mount-key>` | Capacity, inode, and readonly signals for one mounted filesystem. |

## Metrics

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `system.filesystem.total` | gauge | bytes | `host`, `mount` | `107374182400` | Total capacity in bytes for one mounted filesystem. |
| `system.filesystem.available` | gauge | bytes | `host`, `mount` | `64424509440` | Bytes available to unprivileged users on one mounted filesystem. |
| `system.filesystem.used` | gauge | bytes | `host`, `mount` | `42949672960` | Used bytes on one mounted filesystem. |
| `system.filesystem.usage` | gauge | percent | `host`, `mount` | `40` | Used bytes as a percentage of total bytes for one mount. |
| `system.filesystem.inodes.used` | gauge | count | `host`, `mount` | `180322` | Used inode count for one mount when reported by the OS. |
| `system.filesystem.inodes.usage` | gauge | percent | `host`, `mount` | `6.1` | Used inodes as a percentage of total inodes for one mount. |

## States

| Key | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|
| `system.filesystem.type` | string | `host`, `mount` | `ext4` | Filesystem type reported by the OS. |
| `system.filesystem.source` | string | `host`, `mount` | `/dev/nvme0n1p2` | Kernel or operating-system mount source. |
| `system.filesystem.readonly` | boolean | `host`, `mount` | `false` | Whether the mount is read-only. |

## Events

This module does not emit events directly. Mountinfo or `statfs` failures are returned to the runner.

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `host` | `server-01` | all signals | Stable host id from configuration. |
| `mount` | `/var/lib/docker` | filesystem signals | Mount point on the host. |

## Requirements

- Linux mountinfo through `host.proc_root`.
- Host root path through `host.root` for statfs calls.

## Failure behavior

Missing mountinfo produces a collector failure. Individual statfs failures are skipped so other mounts can still report.
