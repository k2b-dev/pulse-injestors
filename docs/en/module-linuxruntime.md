---
title: Linux runtime module
navTitle: Linux runtime
section: Modules
order: 350
description: Linux pressure, process, and socket summary collection.
tags: [linux, runtime, modules, reference]
updated: 2026-06-11
---

# Linux runtime module

The Linux runtime module reports pressure stall information, process counts, and socket summaries.

## Entity types

- `host`: Linux runtime data for the monitored host. Example entity id: `host:server-01`.

## Metrics

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `system.pressure.avg10` | gauge | percent | `host`, `resource`, `scope` | `0.12` | PSI stall percentage over 10 seconds for one resource/scope. |
| `system.pressure.avg60` | gauge | percent | `host`, `resource`, `scope` | `0.08` | PSI stall percentage over 60 seconds for one resource/scope. |
| `system.pressure.avg300` | gauge | percent | `host`, `resource`, `scope` | `0.03` | PSI stall percentage over 300 seconds for one resource/scope. |
| `system.pressure.total` | counter | `seconds` | `host`, `resource`, `scope` | `8.123456` | Total stalled time in seconds for one PSI resource/scope. |
| `system.processes.total` | gauge |  | `host` | `216` | Total Linux process count visible in host procfs. |
| `system.processes.by_state` | gauge |  | `host`, `state` | `143` | Linux process count for one process state such as `sleeping`. |
| `system.network.sockets` | gauge |  | `host`, `protocol`, `family`, `state` | `24` | Socket count for one protocol/family/state bucket. |

## States

| Key | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|
| `system.pressure.available` | boolean | `host` | `true` | Whether `/proc/pressure` exists. |
| `system.pressure.resource.available` | boolean | `host`, `resource` | `true` | Whether a PSI resource file was readable. |
| `system.processes.available` | boolean | `host` | `true` | Whether procfs process directories were readable. |
| `system.network.sockets.file.available` | boolean | `host`, `file` | `true` | Whether one socket file such as `tcp6` was readable. |
| `system.network.sockets.available` | boolean | `host` | `true` | Whether at least one socket file was collected. |

## Events

This module does not emit events directly. Missing optional procfs files are represented by availability states.

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `host` | `server-01` | all signals | Stable host id from configuration. |
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
