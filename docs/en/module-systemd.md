---
title: systemd module
navTitle: systemd
section: Modules
order: 420
description: Configured systemd unit state collection.
tags: [systemd, linux, modules, reference]
updated: 2026-07-23
---

# systemd module

The systemd module reports states for configured systemd units.

## Entity types

| Entity type | Entity id | Used for |
|---|---|---|
| `host` | `host:<host-id>` | systemd availability and configured-unit state. |
| `service` | `service:<host-id>:systemd:<unit>` | State and events for one configured systemd unit. |

## Metrics

This module does not emit metrics.

## States

| Key | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|
| `system.systemd.units.configured` | boolean | `host` | `true` | Whether any units are configured for collection. |
| `system.systemd.available` | boolean | `host` | `true` | Whether `systemctl` is available. |
| `system.service.available` | boolean | `host`, `service`, `service_manager` | `true` | Whether the unit lookup succeeded and the unit is not `not-found`. Emitted on `service`. |
| `system.service.loaded` | boolean | `host`, `service`, `service_manager` | `true` | Whether `LoadState=loaded`. Emitted on `service`. |
| `system.service.active` | boolean | `host`, `service`, `service_manager` | `true` | Whether `ActiveState=active`. Emitted on `service`. |
| `system.service.load_state` | string | `host`, `service`, `service_manager` | `loaded` | Raw systemd `LoadState`. Emitted on `service`. |
| `system.service.active_state` | string | `host`, `service`, `service_manager` | `active` | Raw systemd `ActiveState`. Emitted on `service`. |
| `system.service.sub_state` | string | `host`, `service`, `service_manager` | `running` | Raw systemd `SubState`. Emitted on `service`. |
| `system.service.unit_file_state` | string | `host`, `service`, `service_manager` | `enabled` | Raw systemd `UnitFileState`. Emitted on `service`. |
| `system.service.description` | string | `host`, `service`, `service_manager` | `PostgreSQL database server` | Raw systemd `Description`. Emitted on `service`. |

## Events

| Kind | Dimensions | Attributes | Trigger |
|---|---|---|---|
| `system.service.collect.failed` | `host`, `service`, `service_manager`, `operation=systemctl_show` | `error` | `systemctl show` failed for a configured unit. Emitted on `service`. |
| `system.service.failed` | `host`, `service`, `service_manager` | none | The unit reports `ActiveState=failed`. Emitted on `service`. |

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `host` | `server-01` | all signals | Stable host id from configuration. |
| `service` | `postgresql.service` | unit signals | Configured systemd unit name. |
| `service_manager` | `systemd` | unit signals | Service manager used for the resource. |
| `operation` | `systemctl_show` | failure events | Bounded collector operation that failed. |

## Requirements

- `systemctl`.
- A configured unit list from the Linux profile or `linux.systemd_units`.

## Failure behavior

No configured units emits `system.systemd.units.configured=false`. Unit lookup failures are reported per unit and do not stop other units.
