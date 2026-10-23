---
title: macOS module
navTitle: macOS
section: Modules
order: 430
description: Workstation health, security posture, disk and battery health, and installed software on macOS.
tags: [macos, modules, reference]
updated: 2026-10-23
---

# macOS module

Use these signals to track working Macs, storage pressure, updates, protections, and installed software. See [macOS enrollment](/en/ingestor-macos#workstations) for daily contact expectations and the per-user LaunchAgent.

## Resources and schedule

Host signals use `host:<host-id>` and the configured label. Filesystems use `filesystem:<host-id>:<mount-key>` with the mount path as label. Internal disks use `disk:<host-id>:<device-key>` with the device path as label. Resource components escape delimiters. There are no GPU, display, or Homebrew service resources.

| Collector | Cadence | Purpose |
|---|---|---|
| `macos-health` | 300 s | CPU, memory, load. |
| `macos-filesystem` | 300 s | System and data volume capacity. |
| `macos-updates` | 3600 s | macOS and Homebrew update totals. |
| `macos-posture` | 3600 s | Five security controls. |
| `macos-hardware` | 86400 s | OS, model, CPU, total RAM. |
| `macos-battery` | 86400 s | Laptop battery health and wear. |
| `macos-disk` | 86400 s | Internal physical-disk SMART status. |
| `macos-software` | 86400 s | Application and Homebrew inventory. |

Cadences apply in `run` mode. `once` runs all groups. A base interval longer than a group's cadence limits collection to that interval. Unchanged states refresh on the next scheduled probe after six hours; `software.*` states refresh after seven days. Daily groups therefore refresh ordinary states daily. The persistent spool retains the state cache through restarts.

## Signals

The tables are exhaustive for this module. A dash means no unit or additional dimensions. Configured global dimensions still apply. All metrics are gauges. Runner heartbeat and failure signals are documented under [runner signals](/en/modules#runner-signals).

### Health and capacity

| Signal | Type | Unit | Dimensions | Cadence | Example | Meaning |
|---|---|---|---|---|---|---|
| `system.cpu.usage` | gauge | percent | — | 300 s | `25` | Busy CPU percentage from the second one-second `top` sample. |
| `system.load.1m` | gauge | — | — | 300 s | `1.2` | One-minute load average. |
| `system.load.5m` | gauge | — | — | 300 s | `1.1` | Five-minute load average. |
| `system.load.15m` | gauge | — | — | 300 s | `1.0` | Fifteen-minute load average. |
| `system.memory.used` | gauge | bytes | — | 300 s | `8589934592` | Total minus estimated available memory. |
| `system.memory.available` | gauge | bytes | — | 300 s | `8589934592` | Free, inactive, and speculative pages times the reported page size. |
| `system.memory.usage` | gauge | percent | — | 300 s | `50` | Used memory divided by total memory. |
| `system.filesystem.total` | gauge | bytes | `mount` | 300 s | `500000000000` | Total volume capacity. |
| `system.filesystem.used` | gauge | bytes | `mount` | 300 s | `200000000000` | Used volume bytes. |
| `system.filesystem.available` | gauge | bytes | `mount` | 300 s | `300000000000` | Bytes available to applications. |
| `system.filesystem.usage` | gauge | percent | `mount` | 300 s | `40` | Used divided by total, matching Linux semantics. |
| `system.filesystem.source` | state string | — | `mount` | 300 s | `/dev/disk3s5` | OS device backing the filesystem resource. |

`mount` is only `/` or `/System/Volumes/Data`. APFS volumes can share capacity; do not sum their totals. Available bytes support a disk-full trend in Pulse. Extra APFS helper volumes, external mounts, and network shares are omitted.

### OS and hardware

| Signal | Type | Unit | Dimensions | Cadence | Example | Meaning |
|---|---|---|---|---|---|---|
| `system.os.name` | state string | — | — | 86400 s | `macOS` | Product name from `sw_vers`. |
| `system.os.version` | state string | — | — | 86400 s | `15.5` | OS product version. |
| `system.os.build` | state string | — | — | 86400 s | `24F74` | OS build. |
| `system.hardware.model` | state string | — | — | 86400 s | `Mac14,7` | Hardware model identifier from `hw.model`. |
| `system.hardware.cpu_model` | state string | — | — | 86400 s | `Apple M2` | Chip or CPU brand string from sysctl. |
| `system.memory.total` | gauge | bytes | — | 86400 s | `17179869184` | Installed memory from `hw.memsize`. |

### Updates and posture

| Signal | Type | Unit | Dimensions | Cadence | Example | Meaning |
|---|---|---|---|---|---|---|
| `system.packages.updates.total` | gauge | — | — | 3600 s | `2` | Available updates listed by `softwareupdate -l`; not a security-only count. |
| `system.packages.homebrew.outdated.total` | gauge | — | — | 3600 s | `4` | Outdated formulae plus casks, using the local Homebrew catalog. |
| `macos.softwareupdate.enabled` | state boolean | — | — | 3600 s | `true` | Whether OS update checks are configured. |
| `macos.softwareupdate.available` | state boolean | — | — | 3600 s | `true` | Whether the latest update listing succeeded and was recognized. |
| `package.homebrew.enabled` | state boolean | — | — | 3600 s | `true` | Whether Homebrew probes are configured. |
| `package.homebrew.available` | state boolean | — | — | 3600 s | `true` | Whether enabled Homebrew is installed. |
| `package.homebrew.outdated.available` | state boolean | — | — | 3600 s | `true` | Whether the latest outdated query parsed successfully. |
| `macos.filevault.enabled` | state boolean or string | — | — | 3600 s | `true` | `fdesetup status`: On/Off; otherwise `"unknown"`. |
| `macos.firewall.enabled` | state boolean or string | — | — | 3600 s | `true` | Application Firewall global state from `socketfilterfw --getglobalstate`; otherwise `"unknown"`. |
| `macos.sip.enabled` | state boolean or string | — | — | 3600 s | `true` | `csrutil status`: enabled/disabled; custom or unreadable configurations are `"unknown"`. |
| `macos.gatekeeper.enabled` | state boolean or string | — | — | 3600 s | `true` | `spctl --status`: assessments enabled/disabled; otherwise `"unknown"`. |
| `macos.autoupdate.enabled` | state boolean or string | — | — | 3600 s | `true` | Both SoftwareUpdate preferences below are explicitly enabled; otherwise false or `"unknown"`. |
| `macos.posture.available` | state boolean | — | `check` | 3600 s | `false` | Whether the corresponding posture result is known. |

`check` is `filevault`, `firewall`, `sip`, `gatekeeper`, or `autoupdate`. Automatic updates are true only when `defaults read /Library/Preferences/com.apple.SoftwareUpdate` returns `1` for **both** `AutomaticCheckEnabled` and `AutomaticallyInstallMacOSUpdates`; both reads must succeed. A known zero makes the result false. Missing keys, denied reads, or unexpected output produce unknown. This describes local preferences, not effective MDM policy or successful installation.

The FileVault read can require root/admin access on the deployed macOS version. Other posture reads normally work as the user; any denied read degrades to unknown. No probe escalates privileges. There is no macOS reboot-needed signal from these commands.

### Battery and internal disks

| Signal | Type | Unit | Dimensions | Cadence | Example | Meaning |
|---|---|---|---|---|---|---|
| `system.battery.available` | state boolean | — | — | 86400 s | `false` | An installed AppleSmartBattery is present and readable. False on desktops. |
| `system.battery.health` | state string | — | — | 86400 s | `Normal` | Reported BatteryHealth, or Normal/Service Recommended from permanent failure status; unknown if unavailable. Laptops only. |
| `system.battery.cycle_count` | gauge | — | — | 86400 s | `250` | Battery charge cycles. Laptops only. |
| `system.battery.max_capacity` | gauge | percent | — | 86400 s | `80` | Raw maximum capacity, falling back to nominal charge capacity, divided by design capacity. Laptops only. |
| `system.disk.smart.available` | state boolean | — | — | 86400 s | `true` | `diskutil` listed at least one internal physical disk. Host resource. |
| `system.disk.smart.health_available` | state boolean | — | `device` | 86400 s | `true` | `diskutil info` exposed Verified or Failing SMART status. Disk resource. |
| `system.disk.smart.status` | state string | — | `device` | 86400 s | `Verified` | Native SMART status; omitted when unsupported. Disk resource. |
| `system.disk.smart.healthy` | state boolean | — | `device` | 86400 s | `true` | Verified is true; Failing is false. Disk resource. |

`device` is the internal physical disk path, for example `/dev/disk0`. Unsupported SMART does not mean healthy. The collector probes at most eight internal disks.

### Software inventory and availability

| Signal | Type | Unit | Dimensions | Cadence | Example | Meaning |
|---|---|---|---|---|---|---|
| `software.package.version` | state string | — | `package`, `arch` | 86400 s | `2.4.1` | Installed version; unchanged values refresh weekly. |
| `software.packages.available` | state boolean | — | `arch` | 86400 s | `true` | Inventory read successfully; false on partial or unavailable inventory. |
| `software.packages.truncated` | state boolean | — | `arch` | 86400 s | `false` | Application or Homebrew cap was exceeded. |
| `macos.submodule.ok` | state boolean | — | `submodule` | group cadence | `true` | Group completed without unexpected errors; returns to true on recovery. |

For applications, `arch=app` and `package` is the bundle identifier. Collection walks `/Applications` and its subdirectories (including linked `.app` bundles), stops descending inside each `.app`, and reads Info.plist with `plutil -extract`. The version uses `CFBundleShortVersionString`, falling back to `CFBundleVersion`. At most 1,000 bundles are inspected; duplicate bundle identifiers use the last path in lexical traversal order. Embedded helper apps and system-only `/System/Applications` are omitted.

For Homebrew, `arch=brew`; packages use `formula:<name>` or `cask:<token>` to avoid collisions. Multiple installed versions are sorted and joined. At most 2,000 entries are emitted from `brew info --json=v2 --installed`. `arch` identifies the inventory source here, not CPU architecture. Homebrew automatic updates and analytics are disabled for all probes.

`submodule` is one of the eight group suffixes in the schedule table. Missing optional tools emit availability states; permission errors, malformed responses, output overflow, and timeouts produce `ingestor.collector.failed` with `collector=macos-<group>`. Error attributes never include command output. A failed ordinary probe retries on the next base tick; timeouts retain the group's cadence.

Each command has a deadline: ordinary probes 5 s, Homebrew 20 s by default, and softwareupdate 60 s by default. Output is limited to 8 MiB per command and stderr to 4 KiB. The software inventory has a 120-second total budget, so slow bundles can yield a partial inventory with a visible failure. Daily/hours describe the collection cadence, not a guarantee that every field is supported by every Mac.

## Removed and changed signals

`macos.product_name`, `macos.version`, and `macos.build` become `system.os.name`, `system.os.version`, and `system.os.build`. `system.packages.macos.updates` becomes `system.packages.updates.total`. Failures now use the shared runner event rather than `macos.submodule.failed`.

`system.battery.health` changes from a percentage metric to a state. `system.battery.max_capacity` changes from milliampere-hours to percent of design. Existing Pulse sources that registered the old metric unit need their metric definition migrated before accepting the new unit; this ingestor does not change source schemas. New workstation sources use the new definition.

The following previous keys are no longer emitted, including those replaced above:

- `macos.build`
- `macos.product_name`
- `macos.submodule.failed`
- `macos.system_profiler.enabled`
- `macos.thermal.cpu.available`
- `macos.thermal.cpu.unavailable_reason`
- `macos.thermal.gpu.available`
- `macos.thermal.gpu.unavailable_reason`
- `macos.version`
- `package.homebrew.prefix`
- `package.homebrew.services.available`
- `package.homebrew.services.failed`
- `package.homebrew.version`
- `system.battery.amperage`
- `system.battery.built_in`
- `system.battery.charge`
- `system.battery.charging`
- `system.battery.critical_level`
- `system.battery.current_capacity`
- `system.battery.cycle_usage`
- `system.battery.design_capacity`
- `system.battery.design_cycle_count`
- `system.battery.device`
- `system.battery.fully_charged`
- `system.battery.installed`
- `system.battery.instant_amperage`
- `system.battery.nominal_charge_capacity`
- `system.battery.power`
- `system.battery.raw_voltage`
- `system.battery.serial`
- `system.battery.temperature`
- `system.battery.time_remaining`
- `system.battery.time_to_empty`
- `system.battery.time_to_full`
- `system.battery.virtual_temperature`
- `system.battery.voltage`
- `system.cpu.cores.logical`
- `system.cpu.cores.physical`
- `system.display.available`
- `system.display.connection`
- `system.display.count`
- `system.display.height`
- `system.display.main`
- `system.display.mirror`
- `system.display.online`
- `system.display.refresh_rate`
- `system.display.resolution`
- `system.display.type`
- `system.display.unavailable_reason`
- `system.display.width`
- `system.gpu.bus`
- `system.gpu.cores`
- `system.gpu.metal`
- `system.gpu.model`
- `system.gpu.vendor`
- `system.packages.homebrew.outdated.casks`
- `system.packages.homebrew.outdated.formulae`
- `system.packages.macos.updates`
- `system.power.adapter.voltage`
- `system.power.adapter.watts`
- `system.power.external_charge_capable`
- `system.power.external_connected`
- `system.power.input`
- `system.service.homebrew.by_status`
- `system.service.homebrew.file`
- `system.service.homebrew.present`
- `system.service.homebrew.running`
- `system.service.homebrew.services`
- `system.service.homebrew.status`
- `system.service.homebrew.user`
- `system.uptime`
