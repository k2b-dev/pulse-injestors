---
title: Disk health module
navTitle: Disk health
section: Modules
order: 400
description: SMART and NVMe disk health collection.
tags: [disk, health, modules, reference]
updated: 2026-10-23
---

# Disk health module

The disk health module reports SMART and NVMe health data when host tools are available.

In `pulse-linux`, this collector runs every 3600 seconds by default. Override `diskhealth` in `[runner.cadence]`. States are sent on change and refreshed after six hours when checked again; table cadences describe collection. Other binaries retain their own schedules.

## Resource types

| Resource type | Resource key | Used for |
|---|---|---|
| `host` | `host:<host-id>` | SMART/NVMe tool availability and device counts. |
| `disk` | `disk:<host-id>:<serial-or-device>` | SMART and NVMe states, metrics, and device-level failures for one disk. |

Disk labels prefer the reported NVMe model plus a short serial, for example `Samsung SSD 990 PRO (S7ABC123)`. If model or serial data is not available, the module falls back to the device path.

## Metrics

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.disk.smart.devices` | gauge | — | — | `2` | Devices found by `smartctl --scan-open`. | 3600 s |
| `system.disk.smart.attribute.raw` | gauge | — | `device`, `attribute` | `0` | Selected wear attributes without a dedicated metric: `wear_leveling_count`, `media_wearout_indicator`, `percent_lifetime_remain`. | 3600 s |
| `system.disk.smart.reallocated_sectors` | gauge | — | `device` | `0` | SMART `reallocated_sector_ct`. | 3600 s |
| `system.disk.smart.pending_sectors` | gauge | — | `device` | `0` | SMART `current_pending_sector`. | 3600 s |
| `system.disk.smart.uncorrectable_sectors` | gauge | — | `device` | `0` | SMART `offline_uncorrectable`. | 3600 s |
| `system.disk.smart.udma_crc_errors` | counter | — | `device` | `0` | SMART `udma_crc_error_count`. | 3600 s |
| `system.disk.smart.power_on_time` | counter | seconds | `device` | `65520000` | Cumulative powered-on time, converted from SMART hours. | 3600 s |
| `system.disk.smart.power_cycles` | counter | — | `device` | `120` | SMART `power_cycle_count`. | 3600 s |
| `system.disk.smart.temperature` | gauge | celsius | `device` | `37` | SMART `temperature_celsius` or `airflow_temperature_cel`. | 3600 s |
| `system.disk.nvme.devices` | gauge | — | — | `1` | NVMe devices found by `nvme list -o json`. | 3600 s |
| `system.disk.nvme.critical_warning` | gauge | — | `device` | `0` | NVMe critical warning bitfield as number. | 3600 s |
| `system.disk.nvme.temperature.celsius` | gauge | celsius | `device` | `26.85` | NVMe temperature converted to Celsius. | 3600 s |
| `system.disk.nvme.percentage_used` | gauge | percent | `device` | `3` | NVMe estimated endurance used. | 3600 s |
| `system.disk.nvme.available_spare` | gauge | percent | `device` | `100` | NVMe available spare. | 3600 s |
| `system.disk.nvme.media_errors` | counter | — | `device` | `0` | NVMe media/data integrity errors. | 3600 s |
| `system.disk.nvme.error_log_entries` | counter | — | `device` | `0` | NVMe error information log entries. | 3600 s |

## States

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.disk.smart.available` | state boolean | — | — | `true` | Whether `smartctl` is available. | 3600 s; change / 6 h |
| `system.disk.smart.health_available` | state boolean | — | `device` | `true` | Whether SMART health was collected for the device. | 3600 s; change / 6 h |
| `system.disk.smart.status` | state string | — | `device` | `PASSED` | Raw SMART health status. | 3600 s; change / 6 h |
| `system.disk.smart.healthy` | state boolean | — | `device` | `true` | Whether SMART status is `PASSED` or `OK`. | 3600 s; change / 6 h |
| `system.disk.smart.attributes_available` | state boolean | — | `device` | `true` | Whether SMART attributes were collected. | 3600 s; change / 6 h |
| `system.disk.nvme.available` | state boolean | — | — | `true` | Whether `nvme` is available. | 3600 s; change / 6 h |
| `system.disk.nvme.present` | state boolean | — | `device` | `true` | NVMe device returned by `nvme list`. | 3600 s; change / 6 h |
| `system.disk.model` | state string | — | `device` | `Samsung SSD 990 PRO` | Device model reported by NVMe. | 3600 s; change / 6 h |
| `system.disk.serial` | state string | — | `device` | `S7ABC123456` | Device serial reported by NVMe. | 3600 s; change / 6 h |
| `system.disk.nvme.smart_available` | state boolean | — | `device` | `true` | Whether `nvme smart-log` succeeded. | 3600 s; change / 6 h |
| `system.disk.nvme.healthy` | state boolean | — | `device` | `true` | Whether `critical_warning` is `0`. | 3600 s; change / 6 h |

## Events

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.disk.smart.scan.failed` | event | — | `operation=smart_scan` | `error` | `smartctl --scan-open` failed. | on trigger at 3600 s |
| `system.disk.smart.health.failed` | event | — | `device`, `operation=smart_health` | `error` | SMART health check failed for a device. | on trigger at 3600 s |
| `system.disk.smart.attributes.failed` | event | — | `device`, `operation=smart_attributes` | `error` | SMART attribute collection failed for a device. | on trigger at 3600 s |
| `system.disk.nvme.list.failed` | event | — | `operation=nvme_list` | `error` | `nvme list -o json` failed. | on trigger at 3600 s |
| `system.disk.nvme.smart.failed` | event | — | `device`, `operation=nvme_smart` | `error` | `nvme smart-log` failed or returned invalid JSON. | on trigger at 3600 s |

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `device` | `/dev/nvme0n1` | device signals | Device path. |
| `attribute` | `wear_leveling_count` | SMART raw attribute metric | Normalized SMART attribute name. |
| `operation` | `nvme_smart` | failure events | Bounded collector operation that failed. |

## Requirements

- `smartctl` and/or `nvme`.
- Host device access and permissions.

## Failure behavior

Missing tools emit availability states. Device-level command failures emit device-scoped states and events without stopping other devices.

Duplicate raw SMART attributes are omitted when a dedicated metric exists. Temperature is sent once per device even when both SMART temperature attributes exist. NVMe uses only `system.disk.nvme.temperature.celsius`; the Kelvin metric is removed. `system.disk.smart.power_on_hours` is replaced by the seconds counter `system.disk.smart.power_on_time`.
