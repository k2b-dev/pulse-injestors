# Metrics catalog

This catalog lists the stable metric/state/event names emitted by the current ingestors.

Cost classes:

- `cheap`: local files or fast OS calls.
- `moderate`: command/API calls with timeouts.
- `optional`: best-effort integration; missing tools or unsupported platforms report availability states instead of failing collection.

## Common ingestor states

| Key | Type | Dimensions | Source | Cost |
|---|---:|---|---|---|
| `ingestor.collector.ok` | state bool | `collector` | runner | cheap |
| `ingestor.collector.failed` | event | `collector` | runner | cheap |

## System module

| Name | Type | Unit | Dimensions | Source | Cost |
|---|---|---:|---|---|---|
| `system.load.1m` | gauge | `load` | | Linux `/proc/loadavg`, macOS `sysctl` | cheap |
| `system.load.5m` | gauge | `load` | | Linux `/proc/loadavg`, macOS `sysctl` | cheap |
| `system.load.15m` | gauge | `load` | | Linux `/proc/loadavg`, macOS `sysctl` | cheap |
| `system.cpu.usage` | gauge | `percent` | | Linux `/proc/stat` sample | cheap |
| `system.cpu.cores.logical` | gauge | `count` | | macOS `sysctl` | cheap |
| `system.cpu.cores.physical` | gauge | `count` | | macOS `sysctl` | cheap |
| `system.memory.total` | gauge | `bytes` | | Linux `/proc/meminfo`, macOS `sysctl` | cheap |
| `system.memory.available` | gauge | `bytes` | | Linux `/proc/meminfo`, macOS `vm_stat` | cheap |
| `system.memory.used` | gauge | `bytes` | | derived | cheap |
| `system.memory.usage` | gauge | `percent` | | derived | cheap |
| `system.swap.used` | gauge | `bytes` | | Linux `/proc/meminfo` | cheap |
| `system.swap.usage` | gauge | `percent` | | Linux `/proc/meminfo` | cheap |
| `system.uptime` | gauge | `seconds` | | Linux `/proc/uptime`, macOS `sysctl` | cheap |

## Filesystem module

| Name | Type | Unit | Dimensions | Source | Cost |
|---|---|---:|---|---|---|
| `system.filesystem.total` | gauge | `bytes` | `mount`, `source`, `fstype` | Linux `statfs`, macOS `df` | cheap |
| `system.filesystem.available` | gauge | `bytes` | `mount`, `source`, `fstype` | Linux `statfs`, macOS `df` | cheap |
| `system.filesystem.used` | gauge | `bytes` | `mount`, `source`, `fstype` | derived | cheap |
| `system.filesystem.usage` | gauge | `percent` | `mount`, `source`, `fstype` | derived | cheap |
| `system.filesystem.inodes.used` | gauge | `count` | `mount`, `source`, `fstype` | Linux `statfs` | cheap |
| `system.filesystem.inodes.usage` | gauge | `percent` | `mount`, `source`, `fstype` | Linux `statfs` | cheap |
| `system.filesystem.readonly` | state bool | `mount`, `source`, `fstype` | Linux mountinfo | cheap |

## Docker module

| Name | Type | Unit | Dimensions | Source | Cost |
|---|---|---:|---|---|---|
| `docker.available` | state bool | | Docker Engine `/version` | optional |
| `docker.version` | state string | | Docker Engine `/version` | optional |
| `docker.os` | state string | | Docker Engine `/version` | optional |
| `docker.arch` | state string | | Docker Engine `/version` | optional |
| `docker.containers.total` | gauge | `count` | | Docker Engine `/containers/json` | moderate |
| `docker.containers.running` | gauge | `count` | | Docker Engine `/containers/json` | moderate |
| `docker.container.running` | state bool | `container`, `container_id`, `image` | Docker Engine | moderate |
| `docker.container.status` | state string | `container`, `container_id`, `image` | Docker Engine | moderate |
| `docker.container.image` | state string | `container`, `container_id`, `image` | Docker Engine | moderate |
| `docker.container.cpu.usage` | gauge | `percent` | `container`, `container_id`, `image` | Docker stats | moderate |
| `docker.container.memory.used` | gauge | `bytes` | `container`, `container_id`, `image` | Docker stats | moderate |
| `docker.container.memory.limit` | gauge | `bytes` | `container`, `container_id`, `image` | Docker stats | moderate |
| `docker.container.memory.usage` | gauge | `percent` | `container`, `container_id`, `image` | derived | moderate |
| `docker.container.pids.current` | gauge | `count` | `container`, `container_id`, `image` | Docker stats | moderate |
| `docker.container.network.rx` | counter | `bytes` | `container`, `interface` | Docker stats | moderate |
| `docker.container.network.tx` | counter | `bytes` | `container`, `interface` | Docker stats | moderate |
| `docker.container.network.rx_errors` | counter | `count` | `container`, `interface` | Docker stats | moderate |
| `docker.container.network.tx_errors` | counter | `count` | `container`, `interface` | Docker stats | moderate |
| `docker.container.blockio.read` | counter | `bytes` | `container` | Docker stats | moderate |
| `docker.container.blockio.write` | counter | `bytes` | `container` | Docker stats | moderate |
| `docker.container.restart_count` | gauge | `count` | `container` | Docker inspect | moderate |
| `docker.container.mount.rw` | state bool | `container`, `mount_type`, `mount_destination`, `volume` | Docker inspect | moderate |
| `docker.container.mount.filesystem.total` | gauge | `bytes` | `container`, `mount_destination`, `mount_source` | host `statfs` | moderate |
| `docker.container.mount.filesystem.available` | gauge | `bytes` | `container`, `mount_destination`, `mount_source` | host `statfs` | moderate |
| `docker.container.mount.filesystem.used` | gauge | `bytes` | `container`, `mount_destination`, `mount_source` | derived | moderate |
| `docker.container.mount.filesystem.usage` | gauge | `percent` | `container`, `mount_destination`, `mount_source` | derived | moderate |

## Thermal module

| Name | Type | Unit | Dimensions | Source | Cost |
|---|---|---:|---|---|---|
| `system.temperature.available` | state bool | | Linux sysfs | optional |
| `system.temperature` | gauge | `celsius` | `sensor`, `type`, `chip`, `label` | `/sys/class/thermal`, `/sys/class/hwmon` | cheap |

## Btrfs module

| Name | Type | Unit | Dimensions | Source | Cost |
|---|---|---:|---|---|---|
| `system.btrfs.available` | state bool | | mountinfo + `btrfs` | optional |
| `system.btrfs.device.size` | gauge | `bytes` | `mount`, `source` | `btrfs filesystem usage -b` | optional |
| `system.btrfs.device.allocated` | gauge | `bytes` | `mount`, `source` | `btrfs filesystem usage -b` | optional |
| `system.btrfs.device.unallocated` | gauge | `bytes` | `mount`, `source` | `btrfs filesystem usage -b` | optional |
| `system.btrfs.used` | gauge | `bytes` | `mount`, `source` | `btrfs filesystem usage -b` | optional |
| `system.btrfs.free` | gauge | `bytes` | `mount`, `source` | `btrfs filesystem usage -b` | optional |

## macOS module

| Name | Type | Unit | Dimensions | Source | Cost |
|---|---|---:|---|---|---|
| `macos.product_name` | state string | | `sw_vers` | cheap |
| `macos.version` | state string | | `sw_vers` | cheap |
| `macos.build` | state string | | `sw_vers` | cheap |
| `macos.thermal.cpu.available` | state bool | `source`, `reason` | availability marker | cheap |
| `macos.thermal.gpu.available` | state bool | `source`, `reason` | availability marker | cheap |
| `system.battery.available` | state bool | | `ioreg` | cheap |
| `system.battery.charge` | gauge | `percent` | | `ioreg` | cheap |
| `system.battery.current_capacity` | gauge | `mAh` | | `ioreg` | cheap |
| `system.battery.max_capacity` | gauge | `mAh` | | `ioreg` | cheap |
| `system.battery.design_capacity` | gauge | `mAh` | | `ioreg` | cheap |
| `system.battery.cycle_count` | gauge | `count` | | `ioreg` | cheap |
| `system.battery.voltage` | gauge | `millivolt` | | `ioreg` | cheap |
| `system.battery.amperage` | gauge | `milliampere` | | `ioreg` | cheap |
| `system.battery.temperature` | gauge | `celsius` | | `ioreg` | cheap |
| `system.battery.virtual_temperature` | gauge | `celsius` | | `ioreg` | cheap |
| `system.gpu.cores` | gauge | `count` | `gpu` | `system_profiler` | moderate |
| `system.display.count` | gauge | `count` | | `system_profiler` | moderate |
| `system.display.width` | gauge | `pixels` | `display`, `gpu` | `system_profiler` | moderate |
| `system.display.height` | gauge | `pixels` | `display`, `gpu` | `system_profiler` | moderate |
| `system.display.refresh_rate` | gauge | `hertz` | `display`, `gpu` | `system_profiler` | moderate |
| `system.packages.homebrew.outdated.formulae` | gauge | `count` | | `brew outdated --json=v2` | optional |
| `system.packages.homebrew.outdated.casks` | gauge | `count` | | `brew outdated --json=v2` | optional |
| `system.packages.homebrew.outdated.total` | gauge | `count` | | derived | optional |
| `system.packages.macos.updates` | gauge | `count` | | `softwareupdate -l` when enabled | optional |

## Script module

Scripts can emit any valid Pulse metrics, states, and events. The runner injects `ts`, `entityId`, `entityType`, global dimensions, script dimensions, and `script=<name>` when missing.
