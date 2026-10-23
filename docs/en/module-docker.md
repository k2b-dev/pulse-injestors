---
title: Docker module
navTitle: Docker
section: Modules
order: 310
description: Resources, cadences, dimensions, and signals for Docker Engine monitoring.
tags: [docker, modules, reference]
updated: 2026-10-23
---

# Docker module

The Docker module monitors container lifecycle, health, restarts, and resource use through the local Docker Engine socket. Compose resources show running and total container counts. Image inventory covers images referenced by existing containers, including stopped containers.

## Cadences and volume

| Data | Cadence | Scope |
|---|---|---|
| Daemon availability and running/total counts | 60 seconds | One daemon |
| Container status, health, image reference, and restarts | 60 seconds | Every container, including stopped containers |
| Compose running/total counts | 60 seconds | Every discovered project and service |
| CPU, memory, network, and block I/O | 300 seconds | Running containers |
| Daemon version and image inventory | 24 hours | Daemon and images in use |
| Optional registry checks | 24 hours | Checkable image references in use |

`run` uses these cadences with the runner interval as its scheduling resolution. Keep `runner.interval_seconds = 60` for minute-level status. A longer runner interval delays checks. `once` runs every phase.

States are sent only when their values change and refreshed after six hours when next collected. Daily inventory states refresh on their next daily collection. The status state uses Docker's lifecycle value, such as `running` or `exited`; it excludes elapsed text such as `Up 12 minutes`.

For 70 running containers, the regular metrics contribute 70 restart values plus two daemon counts each minute, and 490 resource-use values every five minutes: **170 metric values/minute** on average. Changed states, six-hour refreshes, daily inventory, failures, and ingestor self-monitoring add values. The fake-socket regression includes 70 Compose services and cold-start inventory and checks a Docker-only average below 400 values/minute. This is a fixture result, not a live-host measurement.

Stats use bounded concurrency (`docker.concurrency`, default `4`). Each container request has a timeout (`docker.container_timeout_seconds`, default `10`). The whole stats phase has an independent budget (`docker.stats_timeout_seconds`, default `120`), so it can exceed a shorter `runner.collector_timeout_seconds`. The runner collects status before slower phases; a slow stats phase can still delay the next cycle. A timed-out phase waits until its next cadence; other phase failures retry on the next runner tick.

## Resources

| Resource type | Resource key | Label | Scope |
|---|---|---|---|
| `docker-daemon` | `docker-daemon:<host>` | `Docker on server-01` | One Docker daemon |
| `docker-container` | `docker-container:<host>:compose:<project>:<service>:<number>` | Docker container name | One logical Compose replica |
| `docker-compose-project` | `docker-compose-project:<host>:<project>` | Project name | One Compose project |
| `docker-compose-service` | `docker-compose-service:<host>:<project>:<service>` | `<project>/<service>` | One Compose service |
| `docker-image` | `docker-image:<host>:<repository>:<tag>` | Image reference | One image reference in use |

Compose container identity uses project, service, and container number. When the number is absent, identity uses the name within the project/service. Non-Compose containers use `docker-container:<host>:name:<name>`. Only unnamed containers fall back to a shortened runtime ID. Normal named-container recreation preserves identity.

Image identity prefers the reference used by a container, then a shortened repository digest, then a shortened local image ID when no reference is available. Extra tags returned by image inspection do not create extra resources. When different container instances use the same logical image reference with different local image IDs, inventory selects one representative: a running container first, then the first image ID in lexical order. Runtime IDs are no longer emitted as signals.

Mounts and network attachments are no longer collected. For filesystem capacity, use the [filesystem module](/en/module-filesystem) on the host.

## Dimensions

All signals also inherit configured global dimensions. The tables use these dimension sets:

| Set | Dimensions | Example | Meaning |
|---|---|---|---|
| daemon | No module-specific dimensions | — | One daemon |
| container | `container`; available `compose_project`, `compose_service`, `compose_container_number` | `container=app-api-1` | Name and Compose membership of one logical container |
| project | `compose_project` | `compose_project=app` | One Compose project |
| service | `compose_project`, `compose_service` | `compose_project=app`, `compose_service=api` | One Compose service |
| image | `image`; available `repository`, `tag` | `image=ghcr.io/example/api:1`, `repository=ghcr.io/example/api`, `tag=1` | One image reference |
| registry | image dimensions plus `registry`, `repository`, `tag` | `registry=ghcr.io` | One registry comparison |

Events add the bounded `operation` dimension. Errors belong in `attributes.error`. No interface, network attachment, mount, address, runtime-ID, or path dimensions are emitted.

## Metrics

### Docker daemon — every 60 seconds

Resource: `docker-daemon`.

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---|---|---|---|
| `docker.containers.total` | gauge | — | daemon | `70` | All containers known to the daemon |
| `docker.containers.running` | gauge | — | daemon | `68` | Containers whose lifecycle state is `running` |

### Docker container

Resource: `docker-container`. Restarts are collected every 60 seconds for all containers. The other metrics are collected every 300 seconds for running containers.

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---|---|---|---|
| `docker.container.restarts` | counter | — | container | `2` | Docker restart count for the current container instance; resets on recreation |
| `docker.container.cpu.usage` | gauge | percent | container | `21.5` | CPU use from Docker's current and previous CPU samples; may exceed 100% across multiple CPUs |
| `docker.container.memory.used` | gauge | bytes | container | `134217728` | Memory usage excluding the inactive-file cache, or Docker's cache fallback when available |
| `docker.container.memory.limit` | gauge | bytes | container | `1073741824` | Memory limit reported by Docker; `0` when absent |
| `docker.container.network.rx` | counter | bytes | container | `12345678` | Received bytes summed across all interfaces in this container |
| `docker.container.network.tx` | counter | bytes | container | `87654321` | Transmitted bytes summed across all interfaces in this container |
| `docker.container.blockio.read` | counter | bytes | container | `104857600` | Read bytes summed across block devices |
| `docker.container.blockio.write` | counter | bytes | container | `52428800` | Written bytes summed across block devices |

CPU is omitted when Docker lacks a usable sample or reports a counter reset. Network and block I/O totals are `0` when Docker reports no entries. Counters can reset on container recreation; changing interface membership can also reduce network totals. Calculate rates per container before grouping by Compose service or project. Compose resources do not emit summed usage or cumulative counters.

### Docker image — every 24 hours

Resource: `docker-image`. Each row is conditional on the corresponding image-inspect field being present; size fields must be positive.

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---|---|---|---|
| `docker.image.age` | gauge | seconds | image | `604800` | Time from the parsed creation timestamp to collection, bounded at zero |
| `docker.image.repo_tags.count` | gauge | — | image | `2` | Number of local tags attached to this in-use image |
| `docker.image.repo_digests.count` | gauge | — | image | `1` | Number of local repository digests attached to this in-use image |
| `docker.image.size` | gauge | bytes | image | `304087040` | Local image size |
| `docker.image.virtual_size` | gauge | bytes | image | `304087040` | Virtual size reported by Docker |

## States

State values have no unit. Compose counts are string states in `running/total` form, so unchanged counts do not consume a metric value each minute.

### Docker daemon

Resource: `docker-daemon`.

| Key | Value type | Unit | Dimensions | Example | Meaning |
|---|---|---|---|---|---|
| `docker.available` | boolean | — | daemon | `true` | Container inventory request succeeded; checked every 60 seconds |
| `docker.version` | string | — | daemon | `27.0.0` | Docker Engine version, checked daily |

### Docker container — every 60 seconds

Resource: `docker-container`.

| Key | Value type | Unit | Dimensions | Example | Meaning |
|---|---|---|---|---|---|
| `docker.container.status` | string | — | container | `running` | Stable Docker lifecycle: `created`, `running`, `paused`, `restarting`, `removing`, `exited`, or `dead` |
| `docker.container.health` | string | — | container | `healthy` | `none`, `starting`, `healthy`, or `unhealthy`; `none` means no recognized Docker healthcheck status |
| `docker.container.image` | string | — | container | `ghcr.io/example/api:1` | Image reference from container inventory |

Health and restart count require a successful inspect. An inspect failure emits an event and leaves the last health state unchanged; it does not report a healthy result.

### Docker Compose — every 60 seconds

| Key | Resource | Value type | Unit | Dimensions | Example | Meaning |
|---|---|---|---|---|---|---|
| `docker.compose.project.containers` | `docker-compose-project` | string | — | project | `3/4` | Running containers / total containers in this project |
| `docker.compose.service.containers` | `docker-compose-service` | string | — | service | `1/2` | Running containers / total containers in this service |

A project or service is discovered from container labels. When its last container disappears, the resource is no longer observed; no synthetic zero or deletion state is emitted.

### Docker image — every 24 hours

Resource: `docker-image`. Registry rows are emitted only when `docker.enable_registry_checks = true` (default `false`). Other metadata rows require the field to be present.

| Key | Value type | Unit | Dimensions | Example | Meaning |
|---|---|---|---|---|---|
| `docker.image.inspect.available` | boolean | — | image | `true` | Local image inspection succeeded |
| `docker.image.created_at` | string | — | image | `2026-06-01T12:00:00Z` | Image creation timestamp |
| `docker.image.repo_tags` | string | — | image | `app:latest,app:v1` | Comma-separated local tags |
| `docker.image.repo_digests` | string | — | image | `app@sha256:abc` | Comma-separated local repository digests |
| `docker.image.arch` | string | — | image | `arm64` | Image architecture |
| `docker.image.os` | string | — | image | `linux` | Image operating system |
| `docker.image.registry.checkable` | boolean | — | image or registry | `true` | A tag has an explicit registry or a comparable local digest; unqualified single-name Docker Hub references are skipped |
| `docker.image.registry.checked` | boolean | — | registry | `true` | Remote manifest lookup succeeded |
| `docker.image.registry.remote_digest` | string | — | registry | `sha256:remote` | Remote manifest digest |
| `docker.image.registry.local_digest_available` | boolean | — | registry | `true` | A comparable local repository digest exists |
| `docker.image.registry.local_digest` | string | — | registry | `sha256:local` | Local digest used for comparison |
| `docker.image.update_available` | boolean | — | registry | `false` | Comparable local and remote digests differ |

Registry checks do not use the host's Docker login credentials. A private registry that requires authentication may reject a check. Missing local digests prevent an update comparison. Registry access is never required for local stats. Daily inventory uses `runner.collector_timeout_seconds` (default 60 seconds). If registry checks make that phase time out, raise this budget enough to inspect every image; a timed-out daily phase waits for its next daily cadence.

## Events

Events have no numeric value or unit. Every event includes `attributes.error` with the failure detail.

| Kind | Resource | Dimensions | Example | Trigger |
|---|---|---|---|---|
| `docker.unavailable` | `docker-daemon` | daemon, `operation=containers` | `error="…connect: no such file…"` | Minute-level container inventory request failed |
| `docker.collect.failed` | `docker-daemon` | daemon, `operation=containers` or `operation=version` | `error="…HTTP 500…"` | Stats/image inventory or daily daemon version request failed |
| `docker.container.collect.failed` | `docker-container` | container, `operation=stats` or `operation=inspect` | `error="…context deadline exceeded…"` | Stats or health/restart inspection failed for one container |
| `docker.image.collect.failed` | `docker-image` | image, `operation=inspect` | `error="…HTTP 404…"` | Local image inspection failed |
| `docker.image.registry.check.failed` | `docker-image` | registry, `operation=manifest` | `error="…HTTP 401…"` | Optional registry lookup failed |

Container and image failures preserve results from other resources. The runner also emits its shared `ingestor.collector.*` failure signals when a phase exhausts its overall timeout. Check socket permissions and Docker availability for daemon failures; check timeout settings for incomplete large-host collection.

## Signal changes

Update queries that use the previous Docker signal set:

| Previous signal | Replacement or changed behavior |
|---|---|
| `docker.container.restart_count` (gauge) | `docker.container.restarts` (counter), every 60 seconds |
| `docker.container.lifecycle.status` | `docker.container.status` now carries the stable lifecycle value |
| `docker.container.health.status` | `docker.container.health`, including `none` for no recognized healthcheck status |
| `docker.container.image.reference` | `docker.container.image` |
| `docker.compose.project.containers` (gauge) | Same key as a `running/total` string state; query states instead of metrics |
| `docker.compose.service.containers` (gauge) | Same key as a `running/total` string state; query states instead of metrics |
| `docker.compose.service.containers.running` (gauge) | Running count is the first part of `docker.compose.service.containers` |
| `docker.container.network.rx`, `docker.container.network.tx` | Same counter names, now one total per container with no `interface` dimension |

The following signals are removed. This list also includes old names replaced above.

### Container lifecycle and configuration

- `docker.container.autoremove`
- `docker.container.created_at`
- `docker.container.dead`
- `docker.container.exit_code`
- `docker.container.finished_at`
- `docker.container.hostname`
- `docker.container.image.id`
- `docker.container.image.reference`
- `docker.container.inspect.available`
- `docker.container.lifecycle.status`
- `docker.container.memory.usage`
- `docker.container.network_mode`
- `docker.container.oom_killed`
- `docker.container.paused`
- `docker.container.pids.current`
- `docker.container.privileged`
- `docker.container.restart_count`
- `docker.container.restart_policy`
- `docker.container.restart_policy.maximum_retry_count`
- `docker.container.restarting`
- `docker.container.running`
- `docker.container.runtime`
- `docker.container.runtime.id`
- `docker.container.started_at`
- `docker.container.stats.available`
- `docker.container.uptime`

### Container health detail

- `docker.container.health.available`
- `docker.container.health.failing_streak`
- `docker.container.health.healthy`
- `docker.container.health.status`

### Container Compose label states

- `docker.container.compose.available`
- `docker.container.compose.config_files`
- `docker.container.compose.config_hash`
- `docker.container.compose.container_number`
- `docker.container.compose.project`
- `docker.container.compose.service`
- `docker.container.compose.version`
- `docker.container.compose.working_dir`

### Mounts

- `docker.container.mount.driver`
- `docker.container.mount.filesystem.available`
- `docker.container.mount.filesystem.total`
- `docker.container.mount.filesystem.usage`
- `docker.container.mount.filesystem.used`
- `docker.container.mount.mode`
- `docker.container.mount.propagation`
- `docker.container.mount.rw`
- `docker.container.mount.source`
- `docker.container.mount.volume`
- `docker.container.mounts.by_type`
- `docker.container.mounts.total`

### Network attachment and error detail

- `docker.container.network.aliases`
- `docker.container.network.aliases.count`
- `docker.container.network.connected`
- `docker.container.network.endpoint_id`
- `docker.container.network.gateway`
- `docker.container.network.id`
- `docker.container.network.ip_address`
- `docker.container.network.ip_prefix_len`
- `docker.container.network.mac_address`
- `docker.container.network.rx_errors`
- `docker.container.network.tx_errors`

### Compose aggregates

- `docker.compose.service.containers.running`
- `docker.compose.project.present`
- `docker.compose.service.cpu.usage`
- `docker.compose.service.memory.limit`
- `docker.compose.service.memory.used`
- `docker.compose.service.pids.current`
- `docker.compose.service.present`

### Daemon summaries and image IDs

- `docker.arch`
- `docker.os`
- `docker.containers.healthy`
- `docker.containers.unhealthy`
- `docker.image.id`
- `docker.images.updates_available`
