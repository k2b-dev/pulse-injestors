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

## Linux runtime module

| Name | Type | Unit | Dimensions | Source | Cost |
|---|---|---:|---|---|---|
| `system.pressure.available` | state bool | | Linux `/proc/pressure` | optional |
| `system.pressure.resource.available` | state bool | `resource` | Linux `/proc/pressure/{cpu,memory,io}` | optional |
| `system.pressure.avg10` | gauge | `percent` | `resource`, `scope` | Linux PSI | cheap |
| `system.pressure.avg60` | gauge | `percent` | `resource`, `scope` | Linux PSI | cheap |
| `system.pressure.avg300` | gauge | `percent` | `resource`, `scope` | Linux PSI | cheap |
| `system.pressure.total` | counter | `microseconds` | `resource`, `scope` | Linux PSI | cheap |
| `system.processes.available` | state bool | | Linux `/proc/<pid>/stat` | optional |
| `system.processes.total` | gauge | `count` | | Linux `/proc` | cheap |
| `system.processes.by_state` | gauge | `count` | `state` | Linux `/proc/<pid>/stat` | cheap |
| `system.network.sockets.available` | state bool | | Linux `/proc/net/{tcp,tcp6,udp,udp6}` | optional |
| `system.network.sockets.file.available` | state bool | `file` | Linux `/proc/net/{tcp,tcp6,udp,udp6}` | optional |
| `system.network.sockets` | gauge | `count` | `protocol`, `family`, `state` | Linux `/proc/net/{tcp,tcp6,udp,udp6}` | cheap |

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
| `system.disk.smart.attributes_available` | state bool | `device` | `smartctl -A` | optional |
| `system.disk.smart.status` | state string | `device` | `smartctl -H` | optional |
| `system.disk.smart.healthy` | state bool | `device` | `smartctl -H` | optional |
| `system.disk.smart.attribute.raw` | gauge | `count` | `device`, `attribute` | `smartctl -A` | optional |
| `system.disk.smart.reallocated_sectors` | gauge | `count` | `device` | `smartctl -A` | optional |
| `system.disk.smart.pending_sectors` | gauge | `count` | `device` | `smartctl -A` | optional |
| `system.disk.smart.uncorrectable_sectors` | gauge | `count` | `device` | `smartctl -A` | optional |
| `system.disk.smart.udma_crc_errors` | gauge | `count` | `device` | `smartctl -A` | optional |
| `system.disk.smart.power_on_hours` | gauge | `hours` | `device` | `smartctl -A` | optional |
| `system.disk.smart.power_cycles` | gauge | `count` | `device` | `smartctl -A` | optional |
| `system.disk.smart.temperature` | gauge | `celsius` | `device` | `smartctl -A` | optional |
| `system.disk.smart.scan.failed` | event | | `smartctl --scan-open` | optional |
| `system.disk.smart.health.failed` | event | `device` | `smartctl -H` | optional |
| `system.disk.smart.attributes.failed` | event | `device` | `smartctl -A` | optional |
| `system.disk.nvme.available` | state bool | | `nvme` lookup | optional |
| `system.disk.nvme.devices` | gauge | `count` | | `nvme list -o json` | optional |
| `system.disk.nvme.present` | state bool | `device`, `model`, `serial` | `nvme list -o json` | optional |
| `system.disk.nvme.smart_available` | state bool | `device`, `model`, `serial` | `nvme smart-log` | optional |
| `system.disk.nvme.healthy` | state bool | `device`, `model`, `serial` | `nvme smart-log` | optional |
| `system.disk.nvme.critical_warning` | gauge | `count` | `device`, `model`, `serial` | `nvme smart-log` | optional |
| `system.disk.nvme.temperature` | gauge | `kelvin` | `device`, `model`, `serial` | `nvme smart-log` | optional |
| `system.disk.nvme.temperature.celsius` | gauge | `celsius` | `device`, `model`, `serial` | derived from `nvme smart-log` | optional |
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
| `system.btrfs.mount.present` | state bool | `mount`, `source` | Linux mountinfo | optional |
| `system.btrfs.usage.available` | state bool | `mount`, `source` | `btrfs filesystem usage -b` | optional |
| `system.btrfs.filesystems` | gauge | `count` | | Linux mountinfo | optional |
| `system.btrfs.filesystems.collected` | gauge | `count` | | `btrfs filesystem usage -b` | optional |
| `system.btrfs.device.size` | gauge | `bytes` | `mount`, `source` | `btrfs filesystem usage -b` | optional |
| `system.btrfs.device.allocated` | gauge | `bytes` | `mount`, `source` | `btrfs filesystem usage -b` | optional |
| `system.btrfs.device.unallocated` | gauge | `bytes` | `mount`, `source` | `btrfs filesystem usage -b` | optional |
| `system.btrfs.used` | gauge | `bytes` | `mount`, `source` | `btrfs filesystem usage -b` | optional |
| `system.btrfs.free` | gauge | `bytes` | `mount`, `source` | `btrfs filesystem usage -b` | optional |
| `system.btrfs.mounts.failed` | event | | mountinfo read | optional |
| `system.btrfs.usage.failed` | event | `mount`, `source` | `btrfs filesystem usage -b` | optional |

## ZFS module

| Name | Type | Unit | Dimensions | Source | Cost |
|---|---|---:|---|---|---|
| `system.zfs.available` | state bool | | `zpool`/`zfs` lookup | optional |
| `system.zfs.zpool.available` | state bool | | `zpool` lookup | optional |
| `system.zfs.zfs.available` | state bool | | `zfs` lookup | optional |
| `system.zfs.pools` | gauge | `count` | | `zpool list` | optional |
| `system.zfs.pool.present` | state bool | `pool` | `zpool list` | optional |
| `system.zfs.pool.health` | state string | `pool` | `zpool list` | optional |
| `system.zfs.pool.healthy` | state bool | `pool` | derived from pool health | optional |
| `system.zfs.pool.size` | gauge | `bytes` | `pool` | `zpool list` | optional |
| `system.zfs.pool.allocated` | gauge | `bytes` | `pool` | `zpool list` | optional |
| `system.zfs.pool.free` | gauge | `bytes` | `pool` | `zpool list` | optional |
| `system.zfs.pool.capacity` | gauge | `percent` | `pool` | `zpool list` | optional |
| `system.zfs.pool.fragmentation` | gauge | `percent` | `pool` | `zpool list` | optional |
| `system.zfs.pool.scan.status` | state string | `pool` | `zpool status` | optional |
| `system.zfs.pool.scan.completed_at` | state string | `pool` | `zpool status` | optional |
| `system.zfs.pool.scan.errors` | gauge | `count` | `pool` | `zpool status` | optional |
| `system.zfs.datasets` | gauge | `count` | | `zfs list` | optional |
| `system.zfs.dataset.present` | state bool | `dataset`, `type` | `zfs list` | optional |
| `system.zfs.dataset.mountpoint` | state string | `dataset`, `type` | `zfs list` | optional |
| `system.zfs.dataset.used` | gauge | `bytes` | `dataset`, `type` | `zfs list` | optional |
| `system.zfs.dataset.available` | gauge | `bytes` | `dataset`, `type` | `zfs list` | optional |
| `system.zfs.dataset.referenced` | gauge | `bytes` | `dataset`, `type` | `zfs list` | optional |
| `system.zfs.dataset.compressratio` | gauge | `ratio` | `dataset`, `type` | `zfs list` | optional |
| `system.zfs.dataset.snapshots` | gauge | `count` | `dataset` | `zfs list -t snapshot` | optional |
| `system.zfs.snapshots` | gauge | `count` | | `zfs list -t snapshot` | optional |
| `system.zfs.pools.failed` | event | | `zpool list` | optional |
| `system.zfs.pool.status.failed` | event | | `zpool status` | optional |
| `system.zfs.datasets.failed` | event | | `zfs list` | optional |
| `system.zfs.snapshots.failed` | event | | `zfs list -t snapshot` | optional |

## Ceph module

| Name | Type | Unit | Dimensions | Source | Cost |
|---|---|---:|---|---|---|
| `system.ceph.available` | state bool | | `ceph` lookup | optional |
| `system.ceph.health.status` | state string | | `ceph status --format json` | optional |
| `system.ceph.health.healthy` | state bool | | derived from health status | optional |
| `system.ceph.mons.quorum` | gauge | `count` | | `ceph status --format json` | optional |
| `system.ceph.mon.in_quorum` | state bool | `monitor` | `ceph status --format json` | optional |
| `system.ceph.osds.total` | gauge | `count` | | `ceph status --format json` | optional |
| `system.ceph.osds.up` | gauge | `count` | | `ceph status --format json` | optional |
| `system.ceph.osds.in` | gauge | `count` | | `ceph status --format json` | optional |
| `system.ceph.pgs.total` | gauge | `count` | | `ceph status --format json` | optional |
| `system.ceph.pgs.by_state` | gauge | `count` | `state` | `ceph status --format json` | optional |
| `system.ceph.bytes.used` | gauge | `bytes` | | `ceph status --format json` | optional |
| `system.ceph.bytes.total` | gauge | `bytes` | | `ceph status --format json` | optional |
| `system.ceph.bytes.available` | gauge | `bytes` | | `ceph status --format json` | optional |
| `system.ceph.pools` | gauge | `count` | | `ceph df --format json` | optional |
| `system.ceph.pool.present` | state bool | `pool` | `ceph df --format json` | optional |
| `system.ceph.pool.bytes.used` | gauge | `bytes` | `pool` | `ceph df --format json` | optional |
| `system.ceph.pool.bytes.available` | gauge | `bytes` | `pool` | `ceph df --format json` | optional |
| `system.ceph.pool.objects` | gauge | `count` | `pool` | `ceph df --format json` | optional |
| `system.ceph.status.failed` | event | | `ceph status --format json` | optional |
| `system.ceph.df.failed` | event | | `ceph df --format json` | optional |

## Proxmox module

| Name | Type | Unit | Dimensions | Source | Cost |
|---|---|---:|---|---|---|
| `proxmox.available` | state bool | | Proxmox API `/version` | optional |
| `proxmox.version` | state string | | Proxmox API `/version` | optional |
| `proxmox.release` | state string | | Proxmox API `/version` | optional |
| `proxmox.repoid` | state string | | Proxmox API `/version` | optional |
| `proxmox.cluster.name` | state string | | Proxmox API `/cluster/status` | optional |
| `proxmox.cluster.quorate` | state bool | | Proxmox API `/cluster/status` | optional |
| `proxmox.node.online` | state bool | `node` | Proxmox API `/cluster/status` | optional |
| `proxmox.node.ip` | state string | `node` | Proxmox API `/cluster/status` | optional |
| `proxmox.nodes.total` | gauge | `count` | | derived from `/cluster/status` | optional |
| `proxmox.nodes.online` | gauge | `count` | | derived from `/cluster/status` | optional |
| `proxmox.resources.by_type` | gauge | `count` | `type` | Proxmox API `/cluster/resources` | optional |
| `proxmox.resources.by_status` | gauge | `count` | `type`, `status` | Proxmox API `/cluster/resources` | optional |
| `proxmox.resource.present` | state bool | `type`, `resource`, `node` | Proxmox API `/cluster/resources` | optional |
| `proxmox.resource.status` | state string | `type`, `resource`, `node` | Proxmox API `/cluster/resources` | optional |
| `proxmox.resource.name` | state string | `type`, `resource`, `node` | Proxmox API `/cluster/resources` | optional |
| `proxmox.resource.cpu.usage` | gauge | `percent` | `type`, `resource`, `node` | Proxmox API `/cluster/resources` | optional |
| `proxmox.resource.cpu.cores` | gauge | `count` | `type`, `resource`, `node` | Proxmox API `/cluster/resources` | optional |
| `proxmox.resource.memory.used` | gauge | `bytes` | `type`, `resource`, `node` | Proxmox API `/cluster/resources` | optional |
| `proxmox.resource.memory.total` | gauge | `bytes` | `type`, `resource`, `node` | Proxmox API `/cluster/resources` | optional |
| `proxmox.resource.memory.usage` | gauge | `percent` | `type`, `resource`, `node` | derived | optional |
| `proxmox.resource.disk.used` | gauge | `bytes` | `type`, `resource`, `node` | Proxmox API `/cluster/resources` | optional |
| `proxmox.resource.disk.total` | gauge | `bytes` | `type`, `resource`, `node` | Proxmox API `/cluster/resources` | optional |
| `proxmox.resource.disk.usage` | gauge | `percent` | `type`, `resource`, `node` | derived | optional |
| `proxmox.resource.uptime` | gauge | `seconds` | `type`, `resource`, `node` | Proxmox API `/cluster/resources` | optional |
| `proxmox.tasks.recent` | gauge | `count` | | Proxmox API `/cluster/tasks` | optional |
| `proxmox.tasks.by_status` | gauge | `count` | `status` | Proxmox API `/cluster/tasks` | optional |
| `proxmox.tasks.by_type_status` | gauge | `count` | `type`, `status` | Proxmox API `/cluster/tasks` | optional |
| `proxmox.backup.tasks.recent` | gauge | `count` | | derived from `/cluster/tasks` type `vzdump` | optional |
| `proxmox.backup.tasks.success` | gauge | `count` | | derived from `/cluster/tasks` type `vzdump` | optional |
| `proxmox.backup.tasks.failed` | gauge | `count` | | derived from `/cluster/tasks` type `vzdump` | optional |
| `proxmox.backup.last_success.time` | state string | | derived from successful `vzdump` task end time | optional |
| `proxmox.backup.last_success.age` | gauge | `seconds` | | derived from successful `vzdump` task end time | optional |
| `proxmox.backup.jobs.total` | gauge | `count` | | Proxmox API `/cluster/backup` | optional |
| `proxmox.backup.jobs.enabled` | gauge | `count` | | Proxmox API `/cluster/backup` | optional |
| `proxmox.backup.jobs.disabled` | gauge | `count` | | Proxmox API `/cluster/backup` | optional |
| `proxmox.backup.job.present` | state bool | `job` | Proxmox API `/cluster/backup` | optional |
| `proxmox.backup.job.enabled` | state bool | `job` | Proxmox API `/cluster/backup` | optional |
| `proxmox.backup.job.schedule` | state string | `job` | Proxmox API `/cluster/backup` | optional |
| `proxmox.backup.job.storage` | state string | `job` | Proxmox API `/cluster/backup` | optional |
| `proxmox.backup.job.mode` | state string | `job` | Proxmox API `/cluster/backup` | optional |
| `proxmox.backup.guests.not_backed_up` | gauge | `count` | | Proxmox API `/cluster/backup-info/not-backed-up` | optional |
| `proxmox.backup.guest.covered` | state bool | `vmid`, `guest`, `type`, `node` | Proxmox API `/cluster/backup-info/not-backed-up` | optional |
| `proxmox.ceph.api.enabled` | state bool | | config | optional |
| `proxmox.ceph.available` | state bool | | Proxmox API `/cluster/ceph/status` | optional |
| `proxmox.ceph.health.status` | state string | | Proxmox API `/cluster/ceph/status` | optional |
| `proxmox.ceph.health.healthy` | state bool | | derived from Ceph health | optional |
| `proxmox.ceph.osds.total` | gauge | `count` | | Proxmox API `/cluster/ceph/status` | optional |
| `proxmox.ceph.osds.up` | gauge | `count` | | Proxmox API `/cluster/ceph/status` | optional |
| `proxmox.ceph.osds.in` | gauge | `count` | | Proxmox API `/cluster/ceph/status` | optional |
| `proxmox.ceph.pgs.total` | gauge | `count` | | Proxmox API `/cluster/ceph/status` | optional |
| `proxmox.ceph.pgs.by_state` | gauge | `count` | `state` | Proxmox API `/cluster/ceph/status` | optional |
| `proxmox.ceph.bytes.used` | gauge | `bytes` | | Proxmox API `/cluster/ceph/status` | optional |
| `proxmox.ceph.bytes.total` | gauge | `bytes` | | Proxmox API `/cluster/ceph/status` | optional |
| `proxmox.ceph.bytes.available` | gauge | `bytes` | | Proxmox API `/cluster/ceph/status` | optional |
| `proxmox.ceph.pools` | gauge | `count` | | Proxmox API `/nodes/{node}/ceph/pools` | optional |
| `proxmox.ceph.pool.present` | state bool | `pool`, `node` | Proxmox API `/nodes/{node}/ceph/pools` | optional |
| `proxmox.ceph.pool.bytes.used` | gauge | `bytes` | `pool`, `node` | Proxmox API `/nodes/{node}/ceph/pools` | optional |
| `proxmox.ceph.pool.bytes.available` | gauge | `bytes` | `pool`, `node` | Proxmox API `/nodes/{node}/ceph/pools` | optional |
| `proxmox.ceph.pool.objects` | gauge | `count` | `pool`, `node` | Proxmox API `/nodes/{node}/ceph/pools` | optional |
| `proxmox.config.failed` | event | | config validation | optional |
| `proxmox.version.failed` | event | | Proxmox API `/version` | optional |
| `proxmox.cluster.status.failed` | event | | Proxmox API `/cluster/status` | optional |
| `proxmox.cluster.resources.failed` | event | | Proxmox API `/cluster/resources` | optional |
| `proxmox.cluster.tasks.failed` | event | | Proxmox API `/cluster/tasks` | optional |
| `proxmox.task.failed` | event | `node`, `type`, `id` | derived from `/cluster/tasks` status | optional |
| `proxmox.backup.failed` | event | `node`, `type`, `id` | derived from failed `vzdump` task | optional |
| `proxmox.backup.jobs.failed` | event | | Proxmox API `/cluster/backup` | optional |
| `proxmox.backup.coverage.failed` | event | | Proxmox API `/cluster/backup-info/not-backed-up` | optional |
| `proxmox.ceph.status.failed` | event | | Proxmox API `/cluster/ceph/status` | optional |
| `proxmox.ceph.pools.failed` | event | | Proxmox API `/nodes/{node}/ceph/pools` | optional |

## Proxmox Backup Server module

| Name | Type | Unit | Dimensions | Source | Cost |
|---|---|---:|---|---|---|
| `pbs.available` | state bool | | PBS API `/version` | optional |
| `pbs.version` | state string | | PBS API `/version` | optional |
| `pbs.release` | state string | | PBS API `/version` | optional |
| `pbs.repoid` | state string | | PBS API `/version` | optional |
| `pbs.datastores.total` | gauge | `count` | | PBS API `/status/datastore-usage` | optional |
| `pbs.datastore.present` | state bool | `datastore` | PBS API `/status/datastore-usage` | optional |
| `pbs.datastore.bytes.used` | gauge | `bytes` | `datastore` | PBS API `/status/datastore-usage` | optional |
| `pbs.datastore.bytes.total` | gauge | `bytes` | `datastore` | PBS API `/status/datastore-usage` | optional |
| `pbs.datastore.bytes.usage` | gauge | `percent` | `datastore` | derived | optional |
| `pbs.datastore.bytes.available` | gauge | `bytes` | `datastore` | PBS API `/status/datastore-usage` | optional |
| `pbs.datastore.estimated_full.time` | state string | `datastore` | PBS API `/status/datastore-usage` | optional |
| `pbs.datastore.estimated_full.seconds_until` | gauge | `seconds` | `datastore` | derived | optional |
| `pbs.datastore.snapshots.total` | gauge | `count` | `datastore` | PBS API `/admin/datastore/{store}/snapshots` | optional |
| `pbs.datastore.snapshots.by_type` | gauge | `count` | `datastore`, `type` | PBS API `/admin/datastore/{store}/snapshots` | optional |
| `pbs.datastore.snapshot.latest.time` | state string | `datastore` | PBS API `/admin/datastore/{store}/snapshots` | optional |
| `pbs.datastore.snapshot.latest.age` | gauge | `seconds` | `datastore` | derived | optional |
| `pbs.jobs.total` | gauge | `count` | `kind` | PBS API `/admin/gc`, `/admin/prune`, `/admin/sync`, `/admin/verify` | optional |
| `pbs.jobs.enabled` | gauge | `count` | `kind` | PBS API `/admin/gc`, `/admin/prune`, `/admin/sync`, `/admin/verify` | optional |
| `pbs.job.present` | state bool | `kind`, `job`, `datastore` | PBS job APIs | optional |
| `pbs.job.enabled` | state bool | `kind`, `job`, `datastore` | PBS job APIs | optional |
| `pbs.job.schedule` | state string | `kind`, `job`, `datastore` | PBS job APIs | optional |
| `pbs.job.last_run.state` | state string | `kind`, `job`, `datastore` | PBS job APIs | optional |
| `pbs.job.last_run.time` | state string | `kind`, `job`, `datastore` | PBS job APIs | optional |
| `pbs.job.last_run.age` | gauge | `seconds` | `kind`, `job`, `datastore` | derived | optional |
| `pbs.job.next_run.time` | state string | `kind`, `job`, `datastore` | PBS job APIs | optional |
| `pbs.job.next_run.seconds_until` | gauge | `seconds` | `kind`, `job`, `datastore` | derived | optional |
| `pbs.tasks.recent` | gauge | `count` | | PBS API `/nodes/localhost/tasks` | optional |
| `pbs.tasks.by_status` | gauge | `count` | `status` | PBS API `/nodes/localhost/tasks` | optional |
| `pbs.tasks.by_type_status` | gauge | `count` | `type`, `status` | PBS API `/nodes/localhost/tasks` | optional |
| `pbs.config.failed` | event | | config validation | optional |
| `pbs.version.failed` | event | | PBS API `/version` | optional |
| `pbs.datastore.usage.failed` | event | | PBS API `/status/datastore-usage` | optional |
| `pbs.datastore.snapshots.failed` | event | `datastore` | PBS API `/admin/datastore/{store}/snapshots` | optional |
| `pbs.job.list.failed` | event | `kind` | PBS job APIs | optional |
| `pbs.job.failed` | event | `kind`, `job`, `datastore` | derived from PBS job last-run state | optional |
| `pbs.tasks.failed` | event | | PBS API `/nodes/localhost/tasks` | optional |
| `pbs.task.failed` | event | `node`, `type`, `id` | derived from PBS task status | optional |

## macOS module

| Name | Type | Unit | Dimensions | Source | Cost |
|---|---|---:|---|---|---|
| `macos.product_name` | state string | | `sw_vers` | cheap |
| `macos.version` | state string | | `sw_vers` | cheap |
| `macos.build` | state string | | `sw_vers` | cheap |
| `macos.system_profiler.enabled` | state bool | | config | cheap |
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
| `system.battery.health` | gauge | `percent` | | derived from max/design capacity | cheap |
| `system.battery.cycle_usage` | gauge | `percent` | | derived from cycle/design cycle count | cheap |
| `system.gpu.cores` | gauge | `count` | `gpu` | `system_profiler` | moderate |
| `system.display.available` | state bool | `reason` when disabled | `system_profiler` / config | moderate |
| `system.display.count` | gauge | `count` | | `system_profiler` | moderate |
| `system.display.width` | gauge | `pixels` | `display`, `gpu` | `system_profiler` | moderate |
| `system.display.height` | gauge | `pixels` | `display`, `gpu` | `system_profiler` | moderate |
| `system.display.refresh_rate` | gauge | `hertz` | `display`, `gpu` | `system_profiler` | moderate |
| `system.packages.homebrew.outdated.formulae` | gauge | `count` | | `brew outdated --json=v2` | optional |
| `system.packages.homebrew.outdated.casks` | gauge | `count` | | `brew outdated --json=v2` | optional |
| `system.packages.homebrew.outdated.total` | gauge | `count` | | derived | optional |
| `package.homebrew.services.available` | state bool | | `brew services info --all --json` | optional |
| `system.service.homebrew.services` | gauge | `count` | | `brew services info --all --json` | optional |
| `system.service.homebrew.present` | state bool | `service` | `brew services info --all --json` | optional |
| `system.service.homebrew.status` | state string | `service` | `brew services info --all --json` | optional |
| `system.service.homebrew.running` | state bool | `service` | derived from service status | optional |
| `system.service.homebrew.user` | state string | `service` | `brew services info --all --json` | optional |
| `system.service.homebrew.file` | state string | `service` | `brew services info --all --json` | optional |
| `system.service.homebrew.by_status` | gauge | `count` | `status` | derived | optional |
| `package.homebrew.services.failed` | event | | `brew services info --all --json` | optional |
| `system.packages.macos.updates` | gauge | `count` | | `softwareupdate -l` when enabled | optional |

## Script module

Scripts can emit any valid Pulse metrics, states, and events. The runner injects `ts`, `entityId`, `entityType`, global dimensions, script dimensions, and `script=<name>` when missing.
