---
title: systemd module
navTitle: systemd
section: Modules
order: 420
description: Configured systemd unit state collection.
tags: [systemd, linux, modules, reference]
updated: 2026-10-23
---

# systemd module

The systemd module counts all failed units and reports states for configured units.

In `pulse-linux`, this collector runs every 60 seconds by default. Override `systemd` in `[runner.cadence]`. States are sent on change and refreshed after six hours when checked again; table cadences describe collection. Other binaries retain their own schedules.

## Resource types

| Resource type | Resource key | Used for |
|---|---|---|
| `host` | `host:<host-id>` | systemd availability and configured-unit state. |
| `service` | `service:<host-id>:systemd:<unit>` | State and events for one configured systemd unit. |

## Metrics

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.systemd.failed_units` | gauge | — | — | `2` | Failed units from `systemctl list-units --state=failed --plain --no-legend --no-pager`, including units outside the configured list. | 60 s |

## States

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.systemd.units.configured` | state boolean | — | — | `true` | Whether any units are configured for collection. | 60 s; change / 6 h |
| `system.systemd.available` | state boolean | — | — | `true` | Whether `systemctl` is available. | 60 s; change / 6 h |
| `system.service.available` | state boolean | — | `service`, `service_manager` | `true` | Whether the unit lookup succeeded and the unit is not `not-found`. Emitted on `service`. | 60 s; change / 6 h |
| `system.service.loaded` | state boolean | — | `service`, `service_manager` | `true` | Whether `LoadState=loaded`. Emitted on `service`. | 60 s; change / 6 h |
| `system.service.active` | state boolean | — | `service`, `service_manager` | `true` | Whether `ActiveState=active`. Emitted on `service`. | 60 s; change / 6 h |
| `system.service.load_state` | state string | — | `service`, `service_manager` | `loaded` | Raw systemd `LoadState`. Emitted on `service`. | 60 s; change / 6 h |
| `system.service.active_state` | state string | — | `service`, `service_manager` | `active` | Raw systemd `ActiveState`. Emitted on `service`. | 60 s; change / 6 h |
| `system.service.sub_state` | state string | — | `service`, `service_manager` | `running` | Raw systemd `SubState`. Emitted on `service`. | 60 s; change / 6 h |
| `system.service.unit_file_state` | state string | — | `service`, `service_manager` | `enabled` | Raw systemd `UnitFileState`. Emitted on `service`. | 60 s; change / 6 h |
| `system.service.description` | state string | — | `service`, `service_manager` | `PostgreSQL database server` | Raw systemd `Description`. Emitted on `service`. | 60 s; change / 6 h |
| `system.systemd.failed` | state string | — | — | `a.service,z.service` | Sorted comma-separated failed unit names, limited to 500 characters; empty string when none. | 60 s; change / 6 h |

## Events

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.service.collect.failed` | event | — | `service`, `service_manager`, `operation=systemctl_show` | `error` | `systemctl show` failed for a configured unit. Emitted on `service`. | on trigger at 60 s |
| `system.service.failed` | event | — | `service`, `service_manager` | none | The unit reports `ActiveState=failed`. Emitted on `service`. | on trigger at 60 s |

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `service` | `postgresql.service` | unit signals | Configured systemd unit name. |
| `service_manager` | `systemd` | unit signals | Service manager used for the resource. |
| `operation` | `systemctl_show` | failure events | Bounded collector operation that failed. |

## Requirements

- `systemctl`.
- Optional unit list from the Linux profile or `linux.systemd_units` for individual service states.

## Failure behavior

No configured units emits `system.systemd.units.configured=false`; the failed-unit summary still runs. Missing systemctl or a host not booted with systemd emits `system.systemd.available=false` without failure events. Unit lookup failures are reported per unit and do not stop other units.
