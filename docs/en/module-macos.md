---
title: macOS module
navTitle: macOS
section: Modules
order: 430
description: macOS system, battery, filesystem, update, display, and Homebrew collection.
tags: [macos, modules, reference]
updated: 2026-07-23
---

# macOS module

The macOS module collects host and client-device data from built-in macOS tools.

## Resources

| Resource type | Resource key | Label | Used for |
|---|---|---|---|
| `host` | `host:<host-id>` | configured device label | macOS version, system, memory, CPU, battery, power, software-update, GPU, display, and Homebrew aggregate signals. |
| `filesystem` | `filesystem:<host-id>:<mount-key>` | mount path | Capacity metrics for one local macOS filesystem. |
| `service` | `service:<host-id>:homebrew:<service>` | service name | State for one Homebrew service. |

GPU and display data stay on the host resource. Bounded `gpu` and `display` dimensions distinguish the series without creating separate device resources.

## Metrics

### System and filesystems

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `system.load.1m` | gauge | — | — | `1.12` | 1 minute load average. |
| `system.load.5m` | gauge | — | — | `1.05` | 5 minute load average. |
| `system.load.15m` | gauge | — | — | `0.98` | 15 minute load average. |
| `system.memory.total` | gauge | bytes | — | `34359738368` | Total memory in bytes for the Mac. |
| `system.memory.available` | gauge | bytes | — | `12884901888` | Estimated available memory in bytes from `vm_stat`. |
| `system.memory.used` | gauge | bytes | — | `21474836480` | Used memory in bytes for the Mac. |
| `system.memory.usage` | gauge | percent | — | `62.5` | Used memory percentage for the Mac. |
| `system.cpu.cores.logical` | gauge | | — | `12` | Logical CPU count from `sysctl`. |
| `system.cpu.cores.physical` | gauge | | — | `10` | Physical CPU count from `sysctl`. |
| `system.uptime` | gauge | seconds | — | `86400` | Host uptime. |
| `system.filesystem.total` | gauge | bytes | `mount` | `494384795648` | Total bytes for one local macOS filesystem. Emitted on `filesystem`. |
| `system.filesystem.used` | gauge | bytes | `mount` | `214748364800` | Used bytes for one local macOS filesystem. Emitted on `filesystem`. |
| `system.filesystem.available` | gauge | bytes | `mount` | `279636430848` | Available bytes for one local macOS filesystem. Emitted on `filesystem`. |
| `system.filesystem.usage` | gauge | percent | `mount` | `43` | Used percentage for one local macOS filesystem. Emitted on `filesystem`. |

### Homebrew and software updates

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `system.packages.homebrew.outdated.formulae` | gauge | | — | `5` | Outdated Homebrew formula count. |
| `system.packages.homebrew.outdated.casks` | gauge | | — | `2` | Outdated Homebrew cask count. |
| `system.packages.homebrew.outdated.total` | gauge | | — | `7` | Total outdated Homebrew package count. |
| `system.service.homebrew.services` | gauge | | — | `3` | Homebrew service count. |
| `system.service.homebrew.by_status` | gauge | | `status` | `2` | Homebrew service count by status. |
| `system.packages.macos.updates` | gauge | | — | `1` | Available macOS software updates. |

### Battery, power, display, and GPU

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `system.battery.charge` | gauge | percent | — | `83` | Current battery charge percentage. |
| `system.battery.current_capacity` | gauge | milliampere-hour | — | `5120` | Raw current battery capacity. |
| `system.battery.max_capacity` | gauge | milliampere-hour | — | `6210` | Raw max battery capacity. |
| `system.battery.design_capacity` | gauge | milliampere-hour | — | `6669` | Design battery capacity. |
| `system.battery.nominal_charge_capacity` | gauge | milliampere-hour | — | `6210` | Nominal charge capacity. |
| `system.battery.cycle_count` | gauge | | — | `221` | Battery cycle count. |
| `system.battery.voltage` | gauge | millivolt | — | `12120` | Battery voltage. |
| `system.battery.raw_voltage` | gauge | millivolt | — | `12120` | Raw battery voltage. |
| `system.battery.amperage` | gauge | milliampere | — | `1840` | Battery amperage. |
| `system.battery.instant_amperage` | gauge | milliampere | — | `1750` | Instant battery amperage. |
| `system.battery.time_to_empty` | gauge | minutes | — | `360` | Estimated time to empty. |
| `system.battery.time_to_full` | gauge | minutes | — | `45` | Estimated time to full. |
| `system.battery.time_remaining` | gauge | minutes | — | `360` | Reported remaining time. |
| `system.battery.design_cycle_count` | gauge | | — | `1000` | Design cycle count. |
| `system.power.adapter.watts` | gauge | watt | — | `67` | Power adapter wattage. |
| `system.power.adapter.voltage` | gauge | millivolt | — | `20000` | Power adapter voltage. |
| `system.power.input` | gauge | milliwatt | — | `30000` | System power input. |
| `system.battery.power` | gauge | milliwatt | — | `-12000` | Battery power flow. |
| `system.battery.temperature` | gauge | celsius | — | `31.4` | Battery temperature. |
| `system.battery.virtual_temperature` | gauge | celsius | — | `33.0` | Virtual battery temperature. |
| `system.battery.health` | gauge | percent | — | `93.1` | Max capacity as percentage of design capacity. |
| `system.battery.cycle_usage` | gauge | percent | — | `22.1` | Cycle count as percentage of design cycle count. |
| `system.gpu.cores` | gauge | | `gpu` | `16` | GPU core count from `system_profiler`. Emitted on `host`. |
| `system.display.width` | gauge | pixels | `display`, `gpu` | `3024` | Display pixel width. Emitted on `host`. |
| `system.display.height` | gauge | pixels | `display`, `gpu` | `1964` | Display pixel height. Emitted on `host`. |
| `system.display.refresh_rate` | gauge | hertz | `display`, `gpu` | `120` | Display refresh rate. Emitted on `host`. |
| `system.display.count` | gauge | | — | `2` | Display count from `system_profiler`. |

## States

| Key | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|
| `macos.product_name` | string | — | `macOS` | Product name from `sw_vers`. |
| `macos.version` | string | — | `15.5` | macOS product version. |
| `macos.build` | string | — | `24F74` | macOS build version. |
| `macos.submodule.ok` | boolean | `submodule` | `false` | A macOS submodule failed. |
| `system.filesystem.source` | string | `mount` | `/dev/disk3s5` | Operating-system source for one filesystem. Emitted on `filesystem`. |
| `package.homebrew.available` | boolean | — | `true` | Whether `brew` exists. |
| `package.homebrew.prefix` | string | — | `/opt/homebrew` | Homebrew prefix. |
| `package.homebrew.version` | string | — | `Homebrew 4.5.0` | First line of `brew --version`. |
| `package.homebrew.outdated.available` | boolean | — | `true` | Whether `brew outdated --json=v2` succeeded. |
| `package.homebrew.services.available` | boolean | — | `true` | Whether `brew services info --all --json` succeeded. |
| `system.service.homebrew.present` | boolean | `service`, `service_manager` | `true` | Homebrew service exists. Emitted on `service`. |
| `system.service.homebrew.status` | string | `service`, `service_manager` | `started` | Homebrew service status. Emitted on `service`. |
| `system.service.homebrew.user` | string | `service`, `service_manager` | `valentin` | Homebrew service user. Emitted on `service`. |
| `system.service.homebrew.file` | string | `service`, `service_manager` | `~/Library/LaunchAgents/homebrew.mxcl.postgresql.plist` | Homebrew service plist file. Emitted on `service`. |
| `system.service.homebrew.running` | boolean | `service`, `service_manager` | `true` | Whether service status is `started`. Emitted on `service`. |
| `macos.softwareupdate.available` | boolean | — | `true` | Whether `softwareupdate -l` succeeded. |
| `system.battery.available` | boolean | — | `true` | Whether AppleSmartBattery data was available. |
| `system.battery.installed` | boolean | — | `true` | Battery installed flag. |
| `system.battery.charging` | boolean | — | `false` | Battery charging flag. |
| `system.battery.fully_charged` | boolean | — | `false` | Fully charged flag. |
| `system.power.external_connected` | boolean | — | `true` | External power connected. |
| `system.power.external_charge_capable` | boolean | — | `true` | External power can charge. |
| `system.battery.critical_level` | boolean | — | `false` | Battery critical level flag. |
| `system.battery.built_in` | boolean | — | `true` | Built-in battery flag. |
| `system.battery.serial` | string | — | `D867...` | Battery serial. |
| `system.battery.device` | string | — | `bq40z651` | Battery device name. |
| `macos.thermal.cpu.available` | boolean | `source` | `false` | Whether CPU thermal telemetry is available. |
| `macos.thermal.cpu.unavailable_reason` | string | `source` | `privileged_or_tool_required` | Why CPU thermal telemetry is unavailable. |
| `macos.thermal.gpu.available` | boolean | `source` | `false` | Whether GPU thermal telemetry is available. |
| `macos.thermal.gpu.unavailable_reason` | string | `source` | `privileged_or_tool_required` | Why GPU thermal telemetry is unavailable. |
| `macos.system_profiler.enabled` | boolean | — | `true` | Whether the system profiler display submodule is enabled. |
| `system.display.available` | boolean | — | `true` | Whether display data was collected. |
| `system.display.unavailable_reason` | string | — | `disabled` | Why display collection is unavailable. |
| `system.gpu.model` | string | `gpu` | `Apple M3 Pro` | GPU model. Emitted on `host`. |
| `system.gpu.vendor` | string | `gpu` | `Apple` | GPU vendor. Emitted on `host`. |
| `system.gpu.metal` | string | `gpu` | `spdisplays_metal3` | Metal support string. Emitted on `host`. |
| `system.gpu.bus` | string | `gpu` | `Built-In` | GPU bus. Emitted on `host`. |
| `system.display.online` | boolean | `display`, `gpu` | `true` | Display online flag. Emitted on `host`. |
| `system.display.main` | boolean | `display`, `gpu` | `true` | Main display flag. Emitted on `host`. |
| `system.display.mirror` | string | `display`, `gpu` | `Off` | Display mirror value. Emitted on `host`. |
| `system.display.type` | string | `display`, `gpu` | `Built-In Retina LCD` | Display type. Emitted on `host`. |
| `system.display.connection` | string | `display`, `gpu` | `Internal` | Display connection type. Emitted on `host`. |
| `system.display.resolution` | string | `display`, `gpu` | `3024 x 1964 Retina` | Display resolution string. Emitted on `host`. |

## Events

| Kind | Dimensions | Attributes | Trigger |
|---|---|---|---|
| `macos.submodule.failed` | `submodule` | `error` | OS, system, filesystem, battery, display, Homebrew, or software update submodule failed. |
| `package.homebrew.services.failed` | `operation` | `error` | Homebrew service query failed or returned invalid JSON. |

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `mount` | `/System/Volumes/Data` | filesystem metrics | Local filesystem mount point. |
| `service` | `postgresql@16` | Homebrew service signals | Homebrew service name. |
| `service_manager` | `homebrew` | Homebrew service signals | Service manager used for the resource. |
| `status` | `started` | Homebrew service aggregate metrics | Homebrew service status. |
| `submodule` | `battery` | submodule failure signals | macOS submodule name. |
| `source` | `powermetrics` | thermal availability states | Telemetry source. |
| `gpu` | `Apple M3 Pro` | GPU and display signals | GPU identifier. |
| `display` | `Color LCD` | display signals | Display identifier. |
| `operation` | `services_info` | Homebrew failure events | Bounded Homebrew operation that failed. |

## Requirements

- macOS host.
- Built-in commands such as `sw_vers`, `sysctl`, `vm_stat`, `df`, `ioreg`, and optionally `brew`, `softwareupdate`, `system_profiler`.

## Failure behavior

Optional tools and disabled submodules emit availability states. Unexpected command failures emit submodule events.
