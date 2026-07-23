---
title: System module
navTitle: System
section: Modules
order: 320
description: Host CPU, memory, load, and uptime collection.
tags: [system, modules, reference]
updated: 2026-06-11
---

# System module

The system module collects core host metrics from procfs.

## Entity types

- `host`: CPU, memory, swap, load, and uptime data for the monitored host. Example entity id: `host:server-01`.

## Metrics

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `system.cpu.usage` | gauge | percent | `host` | `17.4` | CPU busy percentage for the host, sampled over the configured window. |
| `system.load.1m` | gauge | load | `host` | `0.82` | 1 minute load average for the host. |
| `system.load.5m` | gauge | load | `host` | `0.74` | 5 minute load average for the host. |
| `system.load.15m` | gauge | load | `host` | `0.69` | 15 minute load average for the host. |
| `system.memory.total` | gauge | bytes | `host` | `17179869184` | Total memory in bytes for the host. |
| `system.memory.available` | gauge | bytes | `host` | `9126805504` | Available memory in bytes for applications on the host. |
| `system.memory.used` | gauge | bytes | `host` | `8053063680` | Used memory in bytes for the host. |
| `system.memory.usage` | gauge | percent | `host` | `46.9` | Used memory percentage for the host. |
| `system.swap.used` | gauge | bytes | `host` | `268435456` | Used swap in bytes for the host. Emitted only when swap is configured. |
| `system.swap.usage` | gauge | percent | `host` | `12.5` | Used swap percentage for the host. |
| `system.uptime` | gauge | seconds | `host` | `86400` | Host uptime in seconds. |

## States

This module does not emit states.

## Events

This module does not emit events directly. If all core procfs reads fail, the runner reports the collector failure.

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `host` | `server-01` | all signals | Stable host id from configuration. |

## Requirements

- Linux procfs access through `host.proc_root`.
- For containerized deployment, mount the host procfs into the ingestor.

## Failure behavior

Missing procfs files produce a collector failure because the module cannot identify the host system. Optional modules continue independently.
