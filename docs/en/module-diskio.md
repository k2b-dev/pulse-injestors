---
title: Disk I/O module
navTitle: Disk I/O
section: Modules
order: 401
description: Whole-device throughput and saturation counters from procfs.
tags: [disk, linux, modules, reference]
updated: 2026-10-23
---

# Disk I/O module

Use disk I/O counters to distinguish a busy disk from a capacity problem. Collection runs every 300 seconds in `pulse-linux`; override `diskio` in `[runner.cadence]`. Calculate rates and increases in Pulse.

## Resources and selection

Each whole block device uses resource type `disk`, ID `entity.ID(host, "/dev/" + device)`, and the device name as its label. For example, `disk:server-01:%2Fdev%2Fsda` matches diskhealth's device-path identity. NVMe diskhealth prefers a controller serial; disk I/O measures individual namespaces, which may share that serial. It therefore retains the device-path identity rather than merging namespace counters into one health resource.

Included names: `sd*`, `vd*`, `xvd*`, `hd*`, `nvme<number>n<number>`, `mmcblk<number>`, `md<number>`, and `dm-<number>`. Partition names and sysfs entries with a `partition` file are excluded, as are loop, RAM, zram, optical, and floppy devices. Device-mapper UUIDs starting with `part` are excluded; ordinary LVM and crypt devices remain. Physical and mapped devices describe different layers; do not sum all of them as host throughput.

## Signals

Dimensions are empty unless global dimensions are configured. All counters are raw cumulative values from `/proc/diskstats`; sectors always represent 512 bytes.

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.disk.io.read_bytes` | counter | bytes | — | `1048576` | Sectors read multiplied by 512. | 300 s |
| `system.disk.io.write_bytes` | counter | bytes | — | `2097152` | Sectors written multiplied by 512. | 300 s |
| `system.disk.io.reads` | counter | — | — | `1234` | Completed read operations. | 300 s |
| `system.disk.io.writes` | counter | — | — | `5678` | Completed write operations. | 300 s |
| `system.disk.io.busy_time` | counter | seconds | — | `45.1` | Time with I/O in progress, from milliseconds. | 300 s |
| `system.disk.io.read_time` | counter | seconds | — | `32.5` | Cumulative read request time, from milliseconds. | 300 s |
| `system.disk.io.write_time` | counter | seconds | — | `40.7` | Cumulative write request time, from milliseconds. | 300 s |
| `system.disk.io.available` | state boolean | — | — | `true` | Host-level availability of diskstats. | 300 s; change / 6 h |

## Requirements and failures

Reads `host.proc_root` and `host.sys_root`. Missing diskstats reports availability false. Permission errors and malformed eligible rows become runner collector failure events; valid devices still report. No direct events are emitted.
