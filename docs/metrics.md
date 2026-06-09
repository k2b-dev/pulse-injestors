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

## Linux network module

| Name | Type | Unit | Dimensions | Source | Cost |
|---|---|---:|---|---|---|
| `system.network.available` | state bool | | Linux `/proc/net/dev` | cheap |
| `system.network.rx` | counter | `bytes` | `interface` | Linux `/proc/net/dev` | cheap |
| `system.network.rx_packets` | counter | `count` | `interface` | Linux `/proc/net/dev` | cheap |
| `system.network.rx_errors` | counter | `count` | `interface` | Linux `/proc/net/dev` | cheap |
| `system.network.rx_dropped` | counter | `count` | `interface` | Linux `/proc/net/dev` | cheap |
| `system.network.tx` | counter | `bytes` | `interface` | Linux `/proc/net/dev` | cheap |
| `system.network.tx_packets` | counter | `count` | `interface` | Linux `/proc/net/dev` | cheap |
| `system.network.tx_errors` | counter | `count` | `interface` | Linux `/proc/net/dev` | cheap |
| `system.network.tx_dropped` | counter | `count` | `interface` | Linux `/proc/net/dev` | cheap |

## Linux package module

| Name | Type | Unit | Dimensions | Source | Cost |
|---|---|---:|---|---|---|
| `system.packages.available` | state bool | | package manager detection | optional |
| `system.packages.manager.available` | state bool | `manager` | `apt`, `dnf`, `pacman` lookup | optional |
| `system.packages.manager.updates_available` | state bool | `manager` | package manager command | optional |
| `system.packages.manager.updates` | gauge | `count` | `manager` | package manager cache | optional |
| `system.packages.updates.total` | gauge | `count` | | derived | optional |
| `system.packages.manager.failed` | event | `manager` | package manager command | optional |

## Linux systemd module

| Name | Type | Unit | Dimensions | Source | Cost |
|---|---|---:|---|---|---|
| `system.systemd.units.configured` | state bool | | config | cheap |
| `system.systemd.available` | state bool | | `systemctl` lookup | optional |
| `system.service.available` | state bool | `service` | `systemctl show` | optional |
| `system.service.loaded` | state bool | `service` | `systemctl show` | optional |
| `system.service.active` | state bool | `service` | `systemctl show` | optional |
| `system.service.load_state` | state string | `service` | `systemctl show` | optional |
| `system.service.active_state` | state string | `service` | `systemctl show` | optional |
| `system.service.sub_state` | state string | `service` | `systemctl show` | optional |
| `system.service.unit_file_state` | state string | `service` | `systemctl show` | optional |
| `system.service.description` | state string | `service` | `systemctl show` | optional |
| `system.service.collect.failed` | event | `service` | `systemctl show` | optional |
| `system.service.failed` | event | `service` | `systemctl show` | optional |

## Linux disk health module

| Name | Type | Unit | Dimensions | Source | Cost |
|---|---|---:|---|---|---|
| `system.disk.smart.available` | state bool | | `smartctl` lookup | optional |
| `system.disk.smart.devices` | gauge | `count` | | `smartctl --scan-open` | optional |
| `system.disk.smart.health_available` | state bool | `device` | `smartctl -H` | optional |
| `system.disk.smart.status` | state string | `device` | `smartctl -H` | optional |
| `system.disk.smart.healthy` | state bool | `device` | `smartctl -H` | optional |
| `system.disk.smart.scan.failed` | event | | `smartctl --scan-open` | optional |
| `system.disk.smart.health.failed` | event | `device` | `smartctl -H` | optional |
| `system.disk.nvme.available` | state bool | | `nvme` lookup | optional |
| `system.disk.nvme.devices` | gauge | `count` | | `nvme list -o json` | optional |
| `system.disk.nvme.present` | state bool | `device`, `model`, `serial` | `nvme list -o json` | optional |
| `system.disk.nvme.smart_available` | state bool | `device`, `model`, `serial` | `nvme smart-log` | optional |
| `system.disk.nvme.healthy` | state bool | `device`, `model`, `serial` | `nvme smart-log` | optional |
| `system.disk.nvme.critical_warning` | gauge | `count` | `device`, `model`, `serial` | `nvme smart-log` | optional |
| `system.disk.nvme.temperature` | gauge | `kelvin` | `device`, `model`, `serial` | `nvme smart-log` | optional |
| `system.disk.nvme.percentage_used` | gauge | `percent` | `device`, `model`, `serial` | `nvme smart-log` | optional |
| `system.disk.nvme.available_spare` | gauge | `percent` | `device`, `model`, `serial` | `nvme smart-log` | optional |
| `system.disk.nvme.media_errors` | gauge | `count` | `device`, `model`, `serial` | `nvme smart-log` | optional |
| `system.disk.nvme.error_log_entries` | gauge | `count` | `device`, `model`, `serial` | `nvme smart-log` | optional |
| `system.disk.nvme.list.failed` | event | | `nvme list -o json` | optional |
| `system.disk.nvme.smart.failed` | event | `device`, `model`, `serial` | `nvme smart-log` | optional |

## Docker module

| Name | Type | Unit | Dimensions | Source | Cost |
|---|---|---:|---|---|---|
| `docker.available` | state bool | | Docker Engine `/version` | optional |
| `docker.version` | state string | | Docker Engine `/version` | optional |
| `docker.os` | state string | | Docker Engine `/version` | optional |
| `docker.arch` | state string | | Docker Engine `/version` | optional |
| `docker.containers.total` | gauge | `count` | | Docker Engine `/containers/json` | moderate |
| `docker.containers.running` | gauge | `count` | | Docker Engine `/containers/json` | moderate |
| `docker.compose.project.containers` | gauge | `count` | `compose_project` | Docker labels | moderate |
| `docker.compose.service.containers` | gauge | `count` | `compose_project`, `compose_service` | Docker labels | moderate |
| `docker.container.running` | state bool | `container`, `container_id`, `image` | Docker Engine | moderate |
| `docker.container.status` | state string | `container`, `container_id`, `image` | Docker Engine | moderate |
| `docker.container.image` | state string | `container`, `container_id`, `image` | Docker Engine | moderate |
| `docker.container.stats.available` | state bool | `container`, `container_id`, `image` | Docker stats | moderate |
| `docker.container.inspect.available` | state bool | `container`, `container_id`, `image` | Docker inspect | moderate |
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
| `docker.container.exit_code` | gauge | `code` | `container` | Docker inspect | moderate |
| `docker.container.uptime` | gauge | `seconds` | `container` | Docker inspect | moderate |
| `docker.container.lifecycle.status` | state string | `container` | Docker inspect | moderate |
| `docker.container.paused` | state bool | `container` | Docker inspect | moderate |
| `docker.container.restarting` | state bool | `container` | Docker inspect | moderate |
| `docker.container.oom_killed` | state bool | `container` | Docker inspect | moderate |
| `docker.container.dead` | state bool | `container` | Docker inspect | moderate |
| `docker.container.autoremove` | state bool | `container` | Docker inspect | moderate |
| `docker.container.privileged` | state bool | `container` | Docker inspect | moderate |
| `docker.container.created_at` | state string | `container` | Docker inspect | moderate |
| `docker.container.started_at` | state string | `container` | Docker inspect | moderate |
| `docker.container.finished_at` | state string | `container` | Docker inspect | moderate |
| `docker.container.hostname` | state string | `container` | Docker inspect | moderate |
| `docker.container.runtime` | state string | `container` | Docker inspect | moderate |
| `docker.container.restart_policy` | state string | `container` | Docker inspect | moderate |
| `docker.container.restart_policy.maximum_retry_count` | gauge | `count` | `container` | Docker inspect | moderate |
| `docker.container.image.id` | state string | `container` | Docker inspect | moderate |
| `docker.container.image.reference` | state string | `container` | Docker inspect | moderate |
| `docker.container.health.available` | state bool | `container` | Docker inspect | moderate |
| `docker.container.health.status` | state string | `container` | Docker inspect | moderate |
| `docker.container.health.healthy` | state bool | `container` | Docker inspect | moderate |
| `docker.container.health.failing_streak` | gauge | `count` | `container` | Docker inspect | moderate |
| `docker.container.compose.available` | state bool | `container`, `compose_project`, `compose_service` | Docker labels | moderate |
| `docker.container.compose.project` | state string | `container`, `compose_project` | Docker labels | moderate |
| `docker.container.compose.service` | state string | `container`, `compose_service` | Docker labels | moderate |
| `docker.container.compose.container_number` | state string | `container` | Docker labels | moderate |
| `docker.container.compose.version` | state string | `container` | Docker labels | moderate |
| `docker.container.compose.config_hash` | state string | `container` | Docker labels | moderate |
| `docker.container.compose.working_dir` | state string | `container` | Docker labels | moderate |
| `docker.container.compose.config_files` | state string | `container` | Docker labels | moderate |
| `docker.container.network.connected` | state bool | `container`, `network` | Docker inspect | moderate |
| `docker.container.network.id` | state string | `container`, `network` | Docker inspect | moderate |
| `docker.container.network.endpoint_id` | state string | `container`, `network` | Docker inspect | moderate |
| `docker.container.network.gateway` | state string | `container`, `network` | Docker inspect | moderate |
| `docker.container.network.ip_address` | state string | `container`, `network` | Docker inspect | moderate |
| `docker.container.network.ip_prefix_len` | gauge | `bits` | `container`, `network` | Docker inspect | moderate |
| `docker.container.network.mac_address` | state string | `container`, `network` | Docker inspect | moderate |
| `docker.container.network.aliases` | state array | `container`, `network` | Docker inspect | moderate |
| `docker.container.mounts.total` | gauge | `count` | `container` | Docker inspect | moderate |
| `docker.container.mounts.by_type` | gauge | `count` | `container`, `mount_type` | Docker inspect | moderate |
| `docker.container.mount.rw` | state bool | `container`, `mount_type`, `mount_destination`, `volume` | Docker inspect | moderate |
| `docker.container.mount.source` | state string | `container`, `mount_destination` | Docker inspect | moderate |
| `docker.container.mount.driver` | state string | `container`, `mount_destination`, `volume` | Docker inspect | moderate |
| `docker.container.mount.mode` | state string | `container`, `mount_destination` | Docker inspect | moderate |
| `docker.container.mount.propagation` | state string | `container`, `mount_destination` | Docker inspect | moderate |
| `docker.container.mount.filesystem.total` | gauge | `bytes` | `container`, `mount_destination`, `mount_source` | host `statfs` | moderate |
| `docker.container.mount.filesystem.available` | gauge | `bytes` | `container`, `mount_destination`, `mount_source` | host `statfs` | moderate |
| `docker.container.mount.filesystem.used` | gauge | `bytes` | `container`, `mount_destination`, `mount_source` | derived | moderate |
| `docker.container.mount.filesystem.usage` | gauge | `percent` | `container`, `mount_destination`, `mount_source` | derived | moderate |
| `docker.image.inspect.available` | state bool | `image_id` | Docker image inspect | moderate |
| `docker.image.id` | state string | `image_id` | Docker image inspect | moderate |
| `docker.image.created_at` | state string | `image_id` | Docker image inspect | moderate |
| `docker.image.age` | gauge | `seconds` | `image_id` | Docker image inspect | moderate |
| `docker.image.repo_tags` | state array | `image_id` | Docker image inspect | moderate |
| `docker.image.repo_digests` | state array | `image_id` | Docker image inspect | moderate |
| `docker.image.size` | gauge | `bytes` | `image_id` | Docker image inspect | moderate |
| `docker.image.virtual_size` | gauge | `bytes` | `image_id` | Docker image inspect | moderate |
| `docker.image.arch` | state string | `image_id` | Docker image inspect | moderate |
| `docker.image.os` | state string | `image_id` | Docker image inspect | moderate |
| `docker.image.registry.checkable` | state bool | `image_id` | config + image reference | optional |
| `docker.image.registry.checked` | state bool | `image_id`, `image_ref`, `registry`, `repository`, `tag` | Docker Registry HTTP API v2 | optional |
| `docker.image.registry.remote_digest` | state string | `image_id`, `image_ref`, `registry`, `repository`, `tag` | registry manifest digest | optional |
| `docker.image.registry.local_digest_available` | state bool | `image_id`, `image_ref`, `registry`, `repository`, `tag` | local RepoDigests | optional |
| `docker.image.registry.local_digest` | state string | `image_id`, `image_ref`, `registry`, `repository`, `tag` | local RepoDigests | optional |
| `docker.image.update_available` | state bool | `image_id`, `image_ref`, `registry`, `repository`, `tag` | derived digest comparison | optional |
| `docker.image.registry.check.failed` | event | `image_id`, `image_ref`, `registry`, `repository`, `tag` | Docker Registry HTTP API v2 | optional |

Registry freshness checks are opt-in with `docker.enable_registry_checks` / `PULSE_DOCKER_ENABLE_REGISTRY_CHECKS`. They use anonymous registry manifest requests and may fail for private images or rate-limited registries without failing the Docker collector.

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
