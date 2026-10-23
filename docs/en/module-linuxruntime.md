---
title: Linux runtime module
navTitle: Linux runtime
section: Modules
order: 350
description: Linux pressure, process, and socket summary collection.
tags: [linux, runtime, modules, reference]
updated: 2026-10-23
---

# Linux runtime module

The Linux runtime module reports pressure stall information, process counts, and socket summaries.

In `pulse-linux`, this collector runs every 300 seconds by default. Override `linuxruntime` in `[runner.cadence]`. States are sent on change and refreshed after six hours when checked again; table cadences describe collection. Other binaries retain their own schedules.

## Resource types

- `host`: Linux runtime data for the monitored host. Example resource key: `host:server-01`.

## Metrics

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.pressure.avg10` | gauge | percent | `resource`, `scope` | `0.12` | PSI stall percentage over 10 seconds for one resource/scope. | 300 s |
| `system.pressure.avg60` | gauge | percent | `resource`, `scope` | `0.08` | PSI stall percentage over 60 seconds for one resource/scope. | 300 s |
| `system.pressure.avg300` | gauge | percent | `resource`, `scope` | `0.03` | PSI stall percentage over 300 seconds for one resource/scope. | 300 s |
| `system.pressure.total` | counter | `seconds` | `resource`, `scope` | `8.123456` | Total stalled time in seconds for one PSI resource/scope. | 300 s |
| `system.processes.total` | gauge | — | — | `216` | Total Linux process count visible in host procfs. | 300 s |
| `system.processes.by_state` | gauge | — | `state` | `143` | Linux process count for one process state such as `sleeping`. | 300 s |
| `system.network.sockets` | gauge | — | `protocol`, `family`, `state` | `24` | Socket count for one protocol/family/state bucket. | 300 s |

## States

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.pressure.available` | state boolean | — | — | `true` | Whether `/proc/pressure` exists. | 300 s; change / 6 h |
| `system.pressure.resource.available` | state boolean | — | `resource` | `true` | Whether a PSI resource file was readable. | 300 s; change / 6 h |
| `system.processes.available` | state boolean | — | — | `true` | Whether procfs process directories were readable. | 300 s; change / 6 h |
| `system.network.sockets.file.available` | state boolean | — | `file` | `true` | Whether one socket file such as `tcp6` was readable. | 300 s; change / 6 h |
| `system.network.sockets.available` | state boolean | — | — | `true` | Whether at least one socket file was collected. | 300 s; change / 6 h |

## Events

This module does not emit events directly. Missing optional procfs files are represented by availability states.

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `resource` | `cpu` | PSI states and metrics | PSI resource: `cpu`, `memory`, or `io`. |
| `scope` | `some` | PSI metrics | PSI scope from the kernel, usually `some` or `full`. |
| `state` | `sleeping` | process and socket metrics | Process or socket state bucket. |
| `protocol` | `tcp` | socket metrics | Socket protocol. |
| `family` | `ipv4` | socket metrics | Address family. |
| `file` | `tcp6` | socket availability states | Procfs socket file name. |

## Requirements

- Linux procfs access through `host.proc_root`.

## Failure behavior

Missing optional runtime files emit availability states such as `system.pressure.available=false`. The module does not fail the whole run for unsupported runtime files.

OS, kernel release, hardware identity, and installed package versions are collected daily by [Inventory](/en/module-inventory). Runtime summaries remain on the host resource.
