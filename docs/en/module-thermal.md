---
title: Thermal module
navTitle: Thermal
section: Modules
order: 360
description: Host thermal sensor collection through sysfs.
tags: [thermal, modules, reference]
updated: 2026-07-23
---

# Thermal module

The thermal module reads host temperature sensors from sysfs.

## Entity types

| Entity type | Entity id | Used for |
|---|---|---|
| `host` | `host:<host-id>` | Temperature availability and all sensor series. |

Each temperature series is distinguished by bounded sensor dimensions on the host resource. For example, an hwmon series can use `sensor=temp1`, `chip=coretemp`, and `label=Package id 0`.

## Metrics

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `system.temperature` | gauge | celsius | `host`, `sensor`, `type` | `51.2` | Temperature in Celsius for one thermal-zone sensor. Emitted on `host`. |
| `system.temperature` | gauge | celsius | `host`, `sensor`, `chip`, `label` | `43.0` | Temperature in Celsius for one hwmon sensor. Emitted on `host`. |

## States

| Key | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|
| `system.temperature.available` | boolean | `host` | `true` | Whether at least one temperature sensor was collected. |

## Events

This module does not emit events directly. If no sensor can be read and sysfs returns errors, the runner reports the collector failure.

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `host` | `server-01` | all signals | Stable host id from configuration. |
| `sensor` | `thermal_zone0` | temperature metrics | Sensor id from sysfs. |
| `type` | `x86_pkg_temp` | thermal-zone metrics | Thermal zone type. |
| `chip` | `coretemp` | hwmon metrics | Hardware monitoring chip name. |
| `label` | `Package id 0` | hwmon metrics | Optional sensor label. |

## Requirements

- Linux sysfs access through `host.sys_root`.
- Hardware and virtualization support for exposed sensors.

## Failure behavior

No sensors produces `system.temperature.available=false`. Individual unreadable sensors are skipped.
