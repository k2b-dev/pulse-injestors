---
title: Docker module
navTitle: Docker
section: Modules
order: 310
description: Resources, dimensions, and collected signals for Docker Engine monitoring.
tags: [docker, modules, reference]
updated: 2026-07-23
---

# Docker module

The Docker module collects daemon, container, Compose, image, network attachment, and mount data through the Docker Engine API.

## Resources

| Resource type | Resource key | Label | Scope |
|---|---|---|---|
| `docker-daemon` | `docker-daemon:<host>` | `Docker on server-01` | One Docker daemon. |
| `docker-container` | `docker-container:<host>:compose:<project>:<service>:<number>` | Docker name, for example `app-core` | One logical container across image updates and recreates. |
| `docker-compose-project` | `docker-compose-project:<host>:<project>` | Compose project name | One Compose project. |
| `docker-compose-service` | `docker-compose-service:<host>:<project>:<service>` | `<project>/<service>` | One logical Compose service. |
| `docker-image` | `docker-image:<host>:<repository>:<tag>` | `repository:tag` | One image reference used by a collected container. |

Compose container identity uses project, service, and container number. Non-Compose identity uses the Docker container name. The runtime container ID is a state value and does not change the resource identity.

Image identity prefers `repository:tag`, then a shortened repository digest, then a shortened image ID. Full image IDs and digests are state values.

Mounts and network attachments are not separate resources. They are parts of a container and use bounded `mount_*` or `network` dimensions on the `docker-container` resource.

## Metrics

### Docker daemon

Emitted once per Docker daemon.

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `docker.containers.total` | gauge | | `host` | `12` | Total containers known to the daemon. |
| `docker.containers.running` | gauge | | `host` | `9` | Containers currently running. |
| `docker.containers.healthy` | gauge | | `host` | `7` | Containers with a configured healthy Docker healthcheck. |
| `docker.containers.unhealthy` | gauge | | `host` | `1` | Containers with a configured non-healthy Docker healthcheck. |
| `docker.images.updates_available` | gauge | | `host` | `2` | Image resources with a detected registry update. Registry checks must be enabled. |

### Docker container

Emitted once per logical container unless a row names an additional attachment dimension.

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `docker.container.cpu.usage` | gauge | percent | container | `21.5` | Current CPU usage for one container. |
| `docker.container.memory.used` | gauge | bytes | container | `134217728` | Memory used by one container, excluding reclaimable cache where available. |
| `docker.container.memory.limit` | gauge | bytes | container | `1073741824` | Configured memory limit for one container. |
| `docker.container.memory.usage` | gauge | percent | container | `12.5` | Used memory as a percentage of the container limit. |
| `docker.container.pids.current` | gauge | | container | `18` | Current process count in one container. |
| `docker.container.network.rx` | counter | bytes | container, `interface` | `12345678` | Bytes received by one container interface. |
| `docker.container.network.tx` | counter | bytes | container, `interface` | `87654321` | Bytes transmitted by one container interface. |
| `docker.container.network.rx_errors` | counter | count | container, `interface` | `0` | Receive errors on one container interface. |
| `docker.container.network.tx_errors` | counter | count | container, `interface` | `0` | Transmit errors on one container interface. |
| `docker.container.blockio.read` | counter | bytes | container | `104857600` | Block I/O bytes read by one container. |
| `docker.container.blockio.write` | counter | bytes | container | `52428800` | Block I/O bytes written by one container. |
| `docker.container.restart_count` | gauge | | container | `1` | Number of container restarts reported by Docker. |
| `docker.container.exit_code` | gauge | | container | `0` | Last container process exit code. |
| `docker.container.restart_policy.maximum_retry_count` | gauge | | container | `3` | Maximum retries configured by the restart policy. |
| `docker.container.uptime` | gauge | seconds | container | `3600` | Time since the current container instance started. |
| `docker.container.health.failing_streak` | gauge | | container | `0` | Consecutive failed Docker healthchecks. |
| `docker.container.mounts.total` | gauge | | container | `3` | Mounts attached to one container. |
| `docker.container.mounts.by_type` | gauge | | container, `mount_type` | `2` | Mounts of one Docker mount type. |
| `docker.container.network.ip_prefix_len` | gauge | bits | container, `network` | `16` | IP prefix length for one network attachment. |
| `docker.container.network.aliases.count` | gauge | | container, `network` | `2` | Aliases configured on one network attachment. |
| `docker.container.mount.filesystem.total` | gauge | bytes | container, mount | `107374182400` | Total host filesystem bytes behind one mount source. |
| `docker.container.mount.filesystem.available` | gauge | bytes | container, mount | `64424509440` | Available host filesystem bytes behind one mount source. |
| `docker.container.mount.filesystem.used` | gauge | bytes | container, mount | `42949672960` | Used host filesystem bytes behind one mount source. |
| `docker.container.mount.filesystem.usage` | gauge | percent | container, mount | `40` | Used percentage of the host filesystem behind one mount source. |

### Docker Compose project

Emitted once per detected Compose project.

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `docker.compose.project.containers` | gauge | | `host`, `compose_project` | `4` | Containers belonging to one Compose project. |

### Docker Compose service

Emitted once per detected Compose service. Usage values are sums across its current container replicas.

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `docker.compose.service.containers` | gauge | | `host`, `compose_project`, `compose_service` | `2` | Containers belonging to one Compose service. |
| `docker.compose.service.containers.running` | gauge | | `host`, `compose_project`, `compose_service` | `2` | Running containers in one Compose service. |
| `docker.compose.service.cpu.usage` | gauge | percent | `host`, `compose_project`, `compose_service` | `35.2` | Sum of current CPU usage across service containers. |
| `docker.compose.service.memory.used` | gauge | bytes | `host`, `compose_project`, `compose_service` | `268435456` | Sum of memory used across service containers. |
| `docker.compose.service.memory.limit` | gauge | bytes | `host`, `compose_project`, `compose_service` | `2147483648` | Sum of memory limits across service containers. |
| `docker.compose.service.pids.current` | gauge | | `host`, `compose_project`, `compose_service` | `34` | Sum of current processes across service containers. |
| `docker.compose.service.network.rx` | counter | bytes | `host`, `compose_project`, `compose_service` | `22345678` | Sum of received bytes across service containers and interfaces. |
| `docker.compose.service.network.tx` | counter | bytes | `host`, `compose_project`, `compose_service` | `97654321` | Sum of transmitted bytes across service containers and interfaces. |
| `docker.compose.service.blockio.read` | counter | bytes | `host`, `compose_project`, `compose_service` | `209715200` | Sum of block I/O bytes read across service containers. |
| `docker.compose.service.blockio.write` | counter | bytes | `host`, `compose_project`, `compose_service` | `104857600` | Sum of block I/O bytes written across service containers. |

### Docker image

Emitted once per logical image reference used by a collected container.

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---:|---:|---|---:|---|
| `docker.image.age` | gauge | seconds | image | `604800` | Time since the image was created. |
| `docker.image.repo_tags.count` | gauge | | image | `2` | Repository tags attached to the local image. |
| `docker.image.repo_digests.count` | gauge | | image | `1` | Repository digests attached to the local image. |
| `docker.image.size` | gauge | bytes | image | `304087040` | Local image size. |
| `docker.image.virtual_size` | gauge | bytes | image | `304087040` | Docker-reported virtual image size. |

## States

### Docker daemon

Emitted once per Docker daemon.

| Key | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|
| `docker.available` | boolean | `host` | `true` | Whether the Docker Engine API is reachable. |
| `docker.version` | string | `host` | `26.1.4` | Docker Engine version. |
| `docker.os` | string | `host` | `Docker Desktop` | Operating system reported by Docker. |
| `docker.arch` | string | `host` | `aarch64` | Architecture reported by Docker. |

### Docker container

Emitted once per logical container unless a row names an attachment dimension.

| Key | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|
| `docker.container.running` | boolean | container | `true` | Whether the container is running. |
| `docker.container.status` | string | container | `Up 2 hours` | Human-readable Docker status. |
| `docker.container.image` | string | container | `postgres:16` | Image reference from the container list. |
| `docker.container.runtime.id` | string | container | `1234567890abcdef...` | Current Docker runtime container ID. |
| `docker.container.stats.available` | boolean | container | `true` | Whether runtime stats were collected. |
| `docker.container.inspect.available` | boolean | container | `true` | Whether inspect data was collected. |
| `docker.container.lifecycle.status` | string | container | `running` | Raw lifecycle status. |
| `docker.container.paused` | boolean | container | `false` | Whether the container is paused. |
| `docker.container.restarting` | boolean | container | `false` | Whether the container is restarting. |
| `docker.container.oom_killed` | boolean | container | `false` | Whether Docker reports an out-of-memory kill. |
| `docker.container.dead` | boolean | container | `false` | Whether Docker reports the container as dead. |
| `docker.container.autoremove` | boolean | container | `false` | Whether automatic removal is configured. |
| `docker.container.privileged` | boolean | container | `false` | Whether privileged mode is enabled. |
| `docker.container.created_at` | string | container | `2026-06-10T08:00:00Z` | Container creation timestamp. |
| `docker.container.started_at` | string | container | `2026-06-10T08:05:00Z` | Current instance start timestamp. |
| `docker.container.finished_at` | string | container | `2026-06-10T09:00:00Z` | Last finish timestamp. |
| `docker.container.image.id` | string | container | `sha256:abc...` | Local image ID used by the current instance. |
| `docker.container.image.reference` | string | container | `postgres:16` | Configured image reference. |
| `docker.container.hostname` | string | container | `db` | Hostname configured inside the container. |
| `docker.container.network_mode` | string | container | `bridge` | Configured network mode. |
| `docker.container.runtime` | string | container | `runc` | Configured OCI runtime. |
| `docker.container.restart_policy` | string | container | `unless-stopped` | Restart policy name. |
| `docker.container.health.available` | boolean | container | `true` | Whether a Docker healthcheck is configured. |
| `docker.container.health.status` | string | container | `healthy` | Raw Docker health status. |
| `docker.container.health.healthy` | boolean | container | `true` | Whether the current health status is healthy. |
| `docker.container.compose.available` | boolean | container | `true` | Whether Compose identity labels are present. |
| `docker.container.compose.project` | string | container | `pulse` | Compose project label. |
| `docker.container.compose.service` | string | container | `api` | Compose service label. |
| `docker.container.compose.container_number` | string | container | `1` | Compose replica/container number. |
| `docker.container.compose.version` | string | container | `2.27.0` | Compose version label. |
| `docker.container.compose.config_hash` | string | container | `abc123` | Compose configuration hash. |
| `docker.container.compose.working_dir` | string | container | `/srv/pulse` | Compose project working directory. |
| `docker.container.compose.config_files` | string | container | `/srv/pulse/compose.yml` | Compose configuration file list. |
| `docker.container.network.connected` | boolean | container, `network` | `true` | Whether the container is attached to one Docker network. |
| `docker.container.network.id` | string | container, `network` | `aa12...` | Docker network ID. |
| `docker.container.network.endpoint_id` | string | container, `network` | `bb34...` | Docker endpoint ID. |
| `docker.container.network.gateway` | string | container, `network` | `172.18.0.1` | Gateway for one network attachment. |
| `docker.container.network.ip_address` | string | container, `network` | `172.18.0.5` | Container IP address on one Docker network. |
| `docker.container.network.mac_address` | string | container, `network` | `02:42:ac:12:00:05` | Container MAC address on one Docker network. |
| `docker.container.network.aliases` | string | container, `network` | `api,pulse-api` | Comma-separated aliases on one Docker network. |
| `docker.container.mount.volume` | string | container, mount | `pulse_db` | Docker volume name. |
| `docker.container.mount.driver` | string | container, mount | `local` | Docker volume driver. |
| `docker.container.mount.rw` | boolean | container, mount | `true` | Whether one mount is read-write. |
| `docker.container.mount.source` | string | container, mount | `/var/lib/docker/volumes/pulse_db/_data` | Host-side mount source. |
| `docker.container.mount.mode` | string | container, mount | `z` | Docker mount mode. |
| `docker.container.mount.propagation` | string | container, mount | `rprivate` | Docker mount propagation mode. |

### Docker Compose project and service

Emitted once for each discovered Compose resource.

| Key | Resource | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|---|
| `docker.compose.project.present` | `docker-compose-project` | boolean | `host`, `compose_project` | `true` | Compose project appears in the current container inventory. |
| `docker.compose.service.present` | `docker-compose-service` | boolean | `host`, `compose_project`, `compose_service` | `true` | Compose service appears in the current container inventory. |

### Docker image

Emitted once per image resource.

| Key | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|
| `docker.image.inspect.available` | boolean | image | `true` | Whether local image inspection succeeded. |
| `docker.image.id` | string | image | `sha256:abc...` | Full local Docker image ID. |
| `docker.image.created_at` | string | image | `2026-06-01T12:00:00Z` | Image creation timestamp. |
| `docker.image.repo_tags` | string | image | `app:latest,app:v1` | Comma-separated local repository tags. |
| `docker.image.repo_digests` | string | image | `app@sha256:...` | Comma-separated local repository digests. |
| `docker.image.arch` | string | image | `arm64` | Image architecture. |
| `docker.image.os` | string | image | `linux` | Image operating system. |
| `docker.image.registry.checkable` | boolean | image, registry | `true` | Whether the image reference can be checked against a registry. |
| `docker.image.registry.checked` | boolean | image, registry | `true` | Whether the remote registry lookup succeeded. |
| `docker.image.registry.remote_digest` | string | image, registry | `sha256:remote...` | Remote manifest digest. |
| `docker.image.registry.local_digest_available` | boolean | image, registry | `true` | Whether a comparable local digest exists. |
| `docker.image.registry.local_digest` | string | image, registry | `sha256:local...` | Local digest used for comparison. |
| `docker.image.update_available` | boolean | image, registry | `false` | Whether local and remote digests differ. |

## Events

Errors are stored in `attributes.error`. The bounded failed operation remains a dimension.

| Kind | Resource | Dimensions | Attributes | Trigger |
|---|---|---|---|---|
| `docker.unavailable` | `docker-daemon` | `host`, `operation=version` | `error` | Docker version request failed. |
| `docker.collect.failed` | `docker-daemon` | `host`, `operation=containers` | `error` | Container inventory failed. |
| `docker.container.collect.failed` | `docker-container` | container, `operation=stats\|inspect` | `error` | Runtime stats or inspect failed for one container. |
| `docker.image.collect.failed` | `docker-image` | image, `operation=inspect` | `error` | Local image inspection failed. |
| `docker.image.registry.check.failed` | `docker-image` | image, registry, `operation=manifest` | `error` | Registry manifest lookup failed. |

## Dimensions

Dimension sets used above:

- **container**: `host`, `container`, and available `compose_project`, `compose_service`, `compose_container_number`.
- **image**: `host`, `image`, and available `repository`, `tag`.
- **mount**: container dimensions plus `mount_type`, `mount_destination`, and optional `volume`.
- **registry**: image dimensions plus `registry`, `repository`, and `tag`.

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `host` | `server-01` | all Docker signals | Stable configured host ID. |
| `container` | `pulse-api-1` | container signals | Human-readable Docker container name. |
| `compose_project` | `pulse` | Compose resources and containers | Compose project. |
| `compose_service` | `api` | Compose services and containers | Compose service. |
| `compose_container_number` | `1` | Compose containers | Stable Compose replica/container number. |
| `interface` | `eth0` | container network counters | Interface returned by Docker stats. |
| `network` | `pulse_default` | network attachment signals | Docker network name. |
| `mount_type` | `volume` | mount signals | Docker mount type. |
| `mount_destination` | `/var/lib/postgresql/data` | mount signals | Path inside the container. |
| `volume` | `pulse_db` | mount signals | Docker volume name when present. |
| `image` | `ghcr.io/example/api:latest` | image signals | Human-readable image reference. |
| `repository` | `ghcr.io/example/api` | image and registry signals | Image repository. |
| `tag` | `latest` | image and registry signals | Image tag. |
| `registry` | `ghcr.io` | registry checks | Registry host. |
| `operation` | `inspect` | failure events | Bounded collector operation that failed. |

Runtime container IDs, image IDs, network IDs, endpoint IDs, digests, paths, IP addresses, MAC addresses, and aliases are states or event attributes, not dimensions.

## Requirements

- Docker Engine socket access.
- Host root mounted at `host.root` for mount filesystem usage.
- Outbound registry access when registry checks are enabled.

## Failure behavior

An unavailable Docker socket emits `docker.available=false` and `docker.unavailable`. Container and image failures remain scoped to the affected resource and do not stop unrelated collection. Registry checks are optional and do not fail the Docker collector.
