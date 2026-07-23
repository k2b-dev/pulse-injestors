---
title: Disk health module
navTitle: Disk health
section: Modules
order: 400
description: SMART and NVMe disk health collection.
tags: [disk, health, modules, reference]
updated: 2026-07-23
---

# Disk health module

The disk health module reports SMART and NVMe health data when host tools are available.

## Entity types

| Entity type | Entity id | Used for |
|---|---|---|
| `host` | `host:<host-id>` | SMART/NVMe tool availability and device counts. |
| `disk` | `disk:<host-id>:<serial-or-device>` | SMART and NVMe states, metrics, and device-level failures for one disk. |

Disk labels prefer the reported NVMe model plus a short serial, for example `Samsung SSD 990 PRO (S7ABC123)`. If model or serial data is not available, the module falls back to the device path.

## Metrics

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `system.disk.smart.devices` | gauge |  | `host` | `2` | Devices found by `smartctl --scan-open`. |
| `system.disk.smart.attribute.raw` | gauge | count | `host`, `device`, `attribute` | `0` | Raw selected SMART attribute value. |
| `system.disk.smart.reallocated_sectors` | gauge | count | `host`, `device` | `0` | SMART `reallocated_sector_ct`. |
| `system.disk.smart.pending_sectors` | gauge | count | `host`, `device` | `0` | SMART `current_pending_sector`. |
| `system.disk.smart.uncorrectable_sectors` | gauge | count | `host`, `device` | `0` | SMART `offline_uncorrectable`. |
| `system.disk.smart.udma_crc_errors` | gauge | count | `host`, `device` | `0` | SMART `udma_crc_error_count`. |
| `system.disk.smart.power_on_hours` | gauge | hours | `host`, `device` | `18200` | SMART `power_on_hours`. |
| `system.disk.smart.power_cycles` | gauge | count | `host`, `device` | `120` | SMART `power_cycle_count`. |
| `system.disk.smart.temperature` | gauge | celsius | `host`, `device` | `37` | SMART `temperature_celsius` or `airflow_temperature_cel`. |
| `system.disk.nvme.devices` | gauge |  | `host` | `1` | NVMe devices found by `nvme list -o json`. |
| `system.disk.nvme.critical_warning` | gauge |  | `host`, `device` | `0` | NVMe critical warning bitfield as number. |
| `system.disk.nvme.temperature` | gauge | kelvin | `host`, `device` | `300` | NVMe smart-log temperature. |
| `system.disk.nvme.temperature.celsius` | gauge | celsius | `host`, `device` | `26.85` | NVMe temperature converted to Celsius. |
| `system.disk.nvme.percentage_used` | gauge | percent | `host`, `device` | `3` | NVMe estimated endurance used. |
| `system.disk.nvme.available_spare` | gauge | percent | `host`, `device` | `100` | NVMe available spare. |
| `system.disk.nvme.media_errors` | gauge | count | `host`, `device` | `0` | NVMe media/data integrity errors. |
| `system.disk.nvme.error_log_entries` | gauge | count | `host`, `device` | `0` | NVMe error information log entries. |

## States

| Key | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|
| `system.disk.smart.available` | boolean | `host` | `true` | Whether `smartctl` is available. |
| `system.disk.smart.health_available` | boolean | `host`, `device` | `true` | Whether SMART health was collected for the device. |
| `system.disk.smart.status` | string | `host`, `device` | `PASSED` | Raw SMART health status. |
| `system.disk.smart.healthy` | boolean | `host`, `device` | `true` | Whether SMART status is `PASSED` or `OK`. |
| `system.disk.smart.attributes_available` | boolean | `host`, `device` | `true` | Whether SMART attributes were collected. |
| `system.disk.nvme.available` | boolean | `host` | `true` | Whether `nvme` is available. |
| `system.disk.nvme.present` | boolean | `host`, `device` | `true` | NVMe device returned by `nvme list`. |
| `system.disk.model` | string | `host`, `device` | `Samsung SSD 990 PRO` | Device model reported by NVMe. |
| `system.disk.serial` | string | `host`, `device` | `S7ABC123456` | Device serial reported by NVMe. |
| `system.disk.nvme.smart_available` | boolean | `host`, `device` | `true` | Whether `nvme smart-log` succeeded. |
| `system.disk.nvme.healthy` | boolean | `host`, `device` | `true` | Whether `critical_warning` is `0`. |

## Events

| Kind | Dimensions | Attributes | Trigger |
|---|---|---|---|
| `system.disk.smart.scan.failed` | `host`, `operation=smart_scan` | `error` | `smartctl --scan-open` failed. |
| `system.disk.smart.health.failed` | `host`, `device`, `operation=smart_health` | `error` | SMART health check failed for a device. |
| `system.disk.smart.attributes.failed` | `host`, `device`, `operation=smart_attributes` | `error` | SMART attribute collection failed for a device. |
| `system.disk.nvme.list.failed` | `host`, `operation=nvme_list` | `error` | `nvme list -o json` failed. |
| `system.disk.nvme.smart.failed` | `host`, `device`, `operation=nvme_smart` | `error` | `nvme smart-log` failed or returned invalid JSON. |

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `host` | `server-01` | all signals | Stable host id from configuration. |
| `device` | `/dev/nvme0n1` | device signals | Device path. |
| `attribute` | `power_on_hours` | SMART raw attribute metric | Normalized SMART attribute name. |
| `operation` | `nvme_smart` | failure events | Bounded collector operation that failed. |

## Requirements

- `smartctl` and/or `nvme`.
- Host device access and permissions.

## Failure behavior

Missing tools emit availability states. Device-level command failures emit device-scoped states and events without stopping other devices.
