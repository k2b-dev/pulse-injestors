---
title: System module
navTitle: System
section: Modules
order: 320
description: Host CPU, memory, load, and uptime collection.
tags: [system, modules, reference]
updated: 2026-10-23
---

# System module

The system module collects core host metrics from procfs.

In `pulse-linux`, this collector runs every 60 seconds by default. Override `system` in `[runner.cadence]`. States are sent on change and refreshed after six hours when checked again; table cadences describe collection. Other binaries retain their own schedules.

## Resource types

- `host`: CPU, memory, swap, load, and uptime data for the monitored host. Example resource key: `host:server-01`.

## Metrics

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.cpu.usage` | gauge | percent | — | `17.4` | CPU busy percentage for the host, sampled over the configured window. | 60 s |
| `system.load.1m` | gauge | — | — | `0.82` | 1 minute load average for the host. | 60 s |
| `system.load.5m` | gauge | — | — | `0.74` | 5 minute load average for the host. | 60 s |
| `system.load.15m` | gauge | — | — | `0.69` | 15 minute load average for the host. | 60 s |
| `system.memory.total` | gauge | bytes | — | `17179869184` | Total memory in bytes for the host. | 60 s |
| `system.memory.available` | gauge | bytes | — | `9126805504` | Available memory in bytes for applications on the host. | 60 s |
| `system.memory.used` | gauge | bytes | — | `8053063680` | Used memory in bytes for the host. | 60 s |
| `system.memory.usage` | gauge | percent | — | `46.9` | Used memory percentage for the host. | 60 s |
| `system.swap.used` | gauge | bytes | — | `268435456` | Used swap in bytes for the host. Emitted only when swap is configured. | 60 s |
| `system.swap.usage` | gauge | percent | — | `12.5` | Used swap percentage for the host. | 60 s |
| `system.uptime` | gauge | seconds | — | `86400` | Host uptime in seconds. | 60 s |
| `system.cpu.time` | counter | seconds | `mode` | `12500.25` | Cumulative aggregate CPU time; mode is user, nice, system, idle, iowait, irq, softirq, or steal. USER_HZ is 100; guest time is already included in user/nice. | 60 s |
| `system.cpu.cores` | gauge | — | — | `8` | Number of logical CPUs reported by procfs. | 60 s |

## States

This module does not emit states.

## Events

This module does not emit events directly. If all core procfs reads fail, the runner reports the collector failure.

## Dimensions

`mode` identifies the eight CPU time modes listed above. There is no automatic `host` dimension.


## Requirements

- Linux procfs access through `host.proc_root`.
- For containerized deployment, mount the host procfs into the ingestor.

## Failure behavior

Reads that succeed still produce metrics. If no core metric can be read, the runner reports a collector failure. CPU usage needs two samples; cumulative mode counters remain available when no CPU delta can be measured.
