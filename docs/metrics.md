# Telemetry contract

Pulse ingestors emit the Pulse V1 batch format. The complete signal catalog is maintained in the Fibel module pages under [`docs/en`](en/modules.md).

## Signal model

- Metrics are numeric time series.
- States are the latest known value of a fact.
- Events are historical occurrences.
- Resources are stable, browsable objects.
- Dimensions are bounded exact-match labels for filtering and grouping.
- Event attributes hold irregular or high-cardinality context.
- Sensitive fields hold protected event values.
- `actorId`, `sessionId`, and `correlationId` are first-class event identities.

Every built-in signal carries an explicit resource:

```json
{
  "resource": {
    "type": "docker-container",
    "id": "server-01:compose:pulse:api:1",
    "label": "pulse-api-1"
  },
  "entityType": "docker-container",
  "entityId": "docker-container:server-01:compose:pulse:api:1"
}
```

`entityType` and `entityId` mirror the canonical resource key used by Pulse queries. Resource labels are human-readable; stable technical identity remains in the resource ID.

## Resource conventions

| Resource | Canonical key |
|---|---|
| Host | `host:<host>` |
| Filesystem | `filesystem:<host>:<mount>` |
| Network interface | `network-interface:<host>:<interface>` |
| Disk | `disk:<host>:<serial-or-device>` |
| Service | `service:<host>:<manager>:<service>` |
| Docker daemon | `docker-daemon:<host>` |
| Docker container | `docker-container:<host>:compose:<project>:<service>:<number>` |
| Compose project | `docker-compose-project:<host>:<project>` |
| Compose service | `docker-compose-service:<host>:<project>:<service>` |
| Docker image | `docker-image:<host>:<repository>:<tag>` |
| ZFS pool | `zfs-pool:<host>:<pool>` |
| ZFS dataset | `zfs-dataset:<host>:<dataset>` |
| Ceph cluster | `ceph-cluster:<fsid-or-host>` |
| Ceph pool | `ceph-pool:<fsid-or-host>:<pool>` |
| Proxmox cluster | `proxmox-cluster:<cluster>` |
| Proxmox node | `proxmox-node:<cluster-or-host>:<node>` |
| Proxmox VM | `proxmox-vm:<cluster-or-host>:<vmid>` |
| Proxmox container | `proxmox-container:<cluster-or-host>:<vmid>` |
| Proxmox backup job | `proxmox-backup-job:<cluster-or-host>:<job>` |
| PBS server | `proxmox-backup-server:<host>` |
| PBS datastore | `pbs-datastore:<host>:<datastore>` |
| PBS job | `pbs-job:<host>:<kind>:<job>` |
| Uptime probe | `uptime-probe:<probe>` |
| Uptime endpoint | `uptime-endpoint:<probe>:<target>` |

Standalone Proxmox nodes do not emit a synthetic cluster resource. Docker mounts and network attachments remain dimensions and states on the logical container resource.

## Dimensions

Use dimensions for bounded values such as:

- `host`, `environment`, `region`, `site`
- `container`, `compose_project`, `compose_service`
- `mount`, `interface`, `service`, `service_manager`
- `pool`, `dataset`, `node`, `type`, `status`

Do not use runtime container IDs, image IDs, digests, UPIDs, user IDs, paths, IP addresses, MAC addresses, or raw error messages as dimensions. Built-in collectors expose these as states, attributes, or identity fields.

## Events

Collector failures use a stable event kind and a bounded `operation` dimension:

```json
{
  "kind": "docker.container.collect.failed",
  "resource": {
    "type": "docker-container",
    "id": "server-01:compose:pulse:api:1",
    "label": "pulse-api-1"
  },
  "dimensions": {
    "host": "server-01",
    "container": "pulse-api-1",
    "operation": "inspect"
  },
  "attributes": {
    "error": "request timed out"
  }
}
```

Proxmox and PBS task events use `actorId` for the task user and `correlationId` for the UPID. Worker/task IDs and timestamps are attributes.

## Units

- `percent` for utilization and percentage values.
- `bytes` for storage, memory, and I/O.
- `seconds` for uptime, age, and duration.
- `milliseconds` only for values measured in milliseconds.
- `celsius` or `kelvin` for temperature.
- `count` for counters where the counted item is part of the metric meaning.
- Empty unit for plain cardinalities such as cores, containers, processes, or updates.

## Complete signal catalog

- [System](en/module-system.md)
- [Filesystem](en/module-filesystem.md)
- [Network](en/module-network.md)
- [Linux runtime](en/module-linuxruntime.md)
- [Thermal](en/module-thermal.md)
- [Packages](en/module-packages.md)
- [systemd](en/module-systemd.md)
- [Disk health](en/module-diskhealth.md)
- [Btrfs](en/module-btrfs.md)
- [ZFS](en/module-zfs.md)
- [Ceph](en/module-ceph.md)
- [Docker](en/module-docker.md)
- [macOS](en/module-macos.md)
- [Proxmox VE](en/module-proxmox.md)
- [Proxmox Backup Server](en/module-pbs.md)
- [Script extension](en/module-script.md)
- [Uptime](en/module-uptime.md)
