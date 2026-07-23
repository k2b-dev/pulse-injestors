---
title: Proxmox module
navTitle: Proxmox
section: Modules
order: 440
description: Local Proxmox VE collection through pvesh.
tags: [proxmox, ceph, modules, reference]
updated: 2026-07-23
---

# Proxmox module

The Proxmox module collects local Proxmox VE data through `pvesh`. It supports clustered and standalone nodes.

## Resources

| Resource type | Resource key | Label | Scope |
|---|---|---|---|
| `proxmox-node` | `proxmox-node:<cluster-or-host>:<node>` | Proxmox node name | One Proxmox node and local collector aggregates. |
| `proxmox-cluster` | `proxmox-cluster:<cluster>` | Cluster name | Real cluster quorum and node totals. Not emitted on standalone hosts. |
| `proxmox-vm` | `proxmox-vm:<cluster-or-host>:<vmid>` | VM name, then VMID | One QEMU virtual machine. |
| `proxmox-container` | `proxmox-container:<cluster-or-host>:<vmid>` | container name, then VMID | One LXC container. |
| `proxmox-backup-job` | `proxmox-backup-job:<cluster-or-host>:<job>` | backup job ID | One Proxmox backup job. |
| `ceph-cluster` | `ceph-cluster:<fsid-or-cluster>` | `Ceph <fsid>` | Ceph cluster returned by the Proxmox API. |
| `ceph-pool` | `ceph-pool:<fsid-or-cluster>:<pool>` | pool name | One Ceph pool. |

The cluster name namespaces nodes, guests, and backup jobs in a cluster. The configured host ID is the namespace on a standalone installation. The Ceph FSID namespaces Ceph resources when available.

## Metrics

### Nodes and cluster

Node usage is emitted on each `proxmox-node`. Node totals are emitted on `proxmox-cluster` for a real cluster and on the local node for a standalone installation.

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `proxmox.nodes.total` | gauge | | `host` | `3` | Proxmox nodes visible locally. |
| `proxmox.nodes.online` | gauge | | `host` | `3` | Visible Proxmox nodes currently online. |
| `proxmox.node.cpu.usage` | gauge | percent | `host`, `node` | `14.2` | Current CPU usage for one node. |
| `proxmox.node.cpu.cores` | gauge | | `host`, `node` | `16` | CPU core count for one node. |
| `proxmox.node.memory.used` | gauge | bytes | `host`, `node` | `8589934592` | Used memory for one node. |
| `proxmox.node.memory.total` | gauge | bytes | `host`, `node` | `34359738368` | Total memory for one node. |
| `proxmox.node.memory.usage` | gauge | percent | `host`, `node` | `25` | Used memory percentage for one node. |
| `proxmox.node.disk.used` | gauge | bytes | `host`, `node` | `214748364800` | Used root disk bytes for one node. |
| `proxmox.node.disk.total` | gauge | bytes | `host`, `node` | `536870912000` | Total root disk bytes for one node. |
| `proxmox.node.disk.usage` | gauge | percent | `host`, `node` | `40` | Root disk used percentage for one node. |
| `proxmox.node.uptime` | gauge | seconds | `host`, `node` | `86400` | Uptime for one node. |

### Guests and cluster resources

Guest rows are emitted on `proxmox-vm` or `proxmox-container`. Other Proxmox resource types remain on the local node aggregate.

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `proxmox.resource.cpu.usage` | gauge | percent | `host`, `type`, `node`, optional `vmid` | `42.3` | Current CPU usage for one Proxmox resource. |
| `proxmox.resource.cpu.cores` | gauge | | `host`, `type`, `node`, optional `vmid` | `4` | CPU core count for one Proxmox resource. |
| `proxmox.resource.memory.used` | gauge | bytes | `host`, `type`, `node`, optional `vmid` | `2147483648` | Used memory for one Proxmox resource. |
| `proxmox.resource.memory.total` | gauge | bytes | `host`, `type`, `node`, optional `vmid` | `4294967296` | Total memory for one Proxmox resource. |
| `proxmox.resource.memory.usage` | gauge | percent | `host`, `type`, `node`, optional `vmid` | `50` | Used memory percentage for one Proxmox resource. |
| `proxmox.resource.disk.used` | gauge | bytes | `host`, `type`, `node`, optional `vmid` | `32212254720` | Used disk bytes for one Proxmox resource. |
| `proxmox.resource.disk.total` | gauge | bytes | `host`, `type`, `node`, optional `vmid` | `68719476736` | Total disk bytes for one Proxmox resource. |
| `proxmox.resource.disk.usage` | gauge | percent | `host`, `type`, `node`, optional `vmid` | `46.9` | Used disk percentage for one Proxmox resource. |
| `proxmox.resource.uptime` | gauge | seconds | `host`, `type`, `node`, optional `vmid` | `7200` | Uptime for one Proxmox resource. |
| `proxmox.resources.by_type` | gauge | | `host`, `type` | `12` | Resources of one Proxmox type. |
| `proxmox.resources.by_status` | gauge | | `host`, `type`, `status` | `10` | Resources of one type and status. |

### Tasks and backup

Aggregate task and backup metrics are emitted on the local `proxmox-node`.

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `proxmox.tasks.recent` | gauge | | `host` | `50` | Recent tasks returned by Proxmox. |
| `proxmox.tasks.by_status` | gauge | | `host`, `status` | `45` | Recent tasks in one normalized status class. |
| `proxmox.tasks.by_type_status` | gauge | | `host`, `type`, `status` | `8` | Recent tasks for one task type and status class. |
| `proxmox.backup.tasks.recent` | gauge | | `host` | `6` | Recent `vzdump` tasks. |
| `proxmox.backup.tasks.success` | gauge | | `host` | `5` | Recent successful `vzdump` tasks. |
| `proxmox.backup.tasks.failed` | gauge | | `host` | `1` | Recent failed `vzdump` tasks. |
| `proxmox.backup.last_success.age` | gauge | seconds | `host` | `43200` | Age of the latest successful backup task. |
| `proxmox.backup.jobs.total` | gauge | | `host` | `4` | Configured backup jobs. |
| `proxmox.backup.jobs.enabled` | gauge | | `host` | `3` | Enabled backup jobs. |
| `proxmox.backup.jobs.disabled` | gauge | | `host` | `1` | Disabled backup jobs. |
| `proxmox.backup.guests.not_backed_up` | gauge | | `host` | `2` | Guests not covered by a backup job. |

### Ceph

Ceph cluster metrics are emitted on `ceph-cluster`; pool metrics are emitted on `ceph-pool`. Signal names match the standalone [Ceph module](/en/module-ceph).

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `system.ceph.osds.total` | gauge | | `host` | `6` | Total Ceph OSDs. |
| `system.ceph.osds.up` | gauge | | `host` | `6` | Ceph OSDs currently up. |
| `system.ceph.osds.in` | gauge | | `host` | `6` | Ceph OSDs currently in. |
| `system.ceph.pgs.total` | gauge | | `host` | `64` | Total placement groups. |
| `system.ceph.pgs.by_state` | gauge | | `host`, `state` | `64` | Placement groups in one state. |
| `system.ceph.bytes.used` | gauge | bytes | `host` | `1099511627776` | Used Ceph cluster bytes. |
| `system.ceph.bytes.total` | gauge | bytes | `host` | `4398046511104` | Total Ceph cluster bytes. |
| `system.ceph.bytes.available` | gauge | bytes | `host` | `3298534883328` | Available Ceph cluster bytes. |
| `system.ceph.pools` | gauge | | `host` | `4` | Ceph pools returned by Proxmox. |
| `system.ceph.pool.bytes.used` | gauge | bytes | `host`, `pool` | `214748364800` | Used bytes for one Ceph pool. |
| `system.ceph.pool.bytes.available` | gauge | bytes | `host`, `pool` | `1073741824000` | Available bytes for one Ceph pool. |
| `system.ceph.pool.objects` | gauge | count | `host`, `pool` | `145223` | Object count for one Ceph pool. |

## States

### Proxmox and resources

| Key | Resource | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|---|
| `proxmox.available` | local `proxmox-node` | boolean | `host` | `true` | Whether local Proxmox collection is available. |
| `proxmox.version` | local `proxmox-node` | string | `host` | `8.2.4` | Proxmox VE version. |
| `proxmox.release` | local `proxmox-node` | string | `host` | `8.2` | Proxmox release. |
| `proxmox.repoid` | local `proxmox-node` | string | `host` | `abcd1234` | Proxmox repository ID. |
| `proxmox.cluster.name` | `proxmox-cluster` | string | `host` | `lab` | Real Proxmox cluster name. |
| `proxmox.cluster.quorate` | `proxmox-cluster` | boolean | `host` | `true` | Whether the real cluster is quorate. |
| `proxmox.node.online` | `proxmox-node` | boolean | `host`, `node` | `true` | Whether one node is online. |
| `proxmox.node.ip` | `proxmox-node` | string | `host`, `node` | `10.0.0.11` | Node IP from cluster status. |
| `proxmox.node.status` | `proxmox-node` | string | `host`, `node` | `online` | Raw node status. |
| `proxmox.resource.present` | guest or local node | boolean | `host`, `type`, `node`, optional `vmid` | `true` | Proxmox resource exists. |
| `proxmox.resource.status` | guest or local node | string | `host`, `type`, `node`, optional `vmid` | `running` | Raw resource status. |
| `proxmox.resource.name` | guest or local node | string | `host`, `type`, `node`, optional `vmid` | `postgres` | Resource display name returned by Proxmox. |
| `proxmox.backup.last_success.time` | local `proxmox-node` | string | `host` | `2026-06-11T00:00:00Z` | Latest successful backup timestamp. |
| `proxmox.backup.job.present` | `proxmox-backup-job` | boolean | `host`, `job` | `true` | Backup job exists. |
| `proxmox.backup.job.enabled` | `proxmox-backup-job` | boolean | `host`, `job` | `true` | Backup job is enabled. |
| `proxmox.backup.job.schedule` | `proxmox-backup-job` | string | `host`, `job` | `daily` | Backup job schedule. |
| `proxmox.backup.job.storage` | `proxmox-backup-job` | string | `host`, `job` | `pbs-main` | Backup target storage. |
| `proxmox.backup.job.mode` | `proxmox-backup-job` | string | `host`, `job` | `snapshot` | Backup mode. |
| `proxmox.backup.guest.covered` | guest | boolean | `host`, `vmid`, `guest`, `type`, `node` | `false` | Guest is listed as not covered by backup jobs. |
| `proxmox.ceph.api.enabled` | local `proxmox-node` | boolean | `host` | `true` | Whether Proxmox Ceph API collection is enabled. |

### Ceph

| Key | Resource | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|---|
| `system.ceph.available` | local node or `ceph-cluster` | boolean | `host` | `true` | Whether Proxmox returned Ceph status. |
| `system.ceph.present` | `ceph-cluster` | boolean | `host` | `true` | Ceph cluster was identified. |
| `system.ceph.fsid` | `ceph-cluster` | string | `host` | `45f4536f-...` | Ceph cluster FSID. |
| `system.ceph.health.status` | `ceph-cluster` | string | `host` | `HEALTH_OK` | Raw Ceph health status. |
| `system.ceph.health.healthy` | `ceph-cluster` | boolean | `host` | `true` | Whether Ceph health is `HEALTH_OK`. |
| `system.ceph.pool.present` | `ceph-pool` | boolean | `host`, `pool` | `true` | Ceph pool exists. |

## Events

Command errors use `attributes.error`. Failed task context uses structured identity and attribute fields.

| Kind | Dimensions | Attributes | Identities | Trigger |
|---|---|---|---|---|
| `proxmox.config.failed` | `host`, `operation=configure` | `error` | | Invalid local `pvesh` configuration. |
| `proxmox.version.failed` | `host`, `operation=version` | `error` | | Version query failed. |
| `proxmox.cluster.status.failed` | `host`, `operation=cluster_status` | `error` | | Cluster status failed; standalone fallback is attempted. |
| `proxmox.nodes.failed` | `host`, `operation=nodes` | `error` | | Standalone node query failed. |
| `proxmox.cluster.resources.failed` | `host`, `operation=cluster_resources` | `error` | | Cluster resource query failed. |
| `proxmox.cluster.tasks.failed` | `host`, `operation=cluster_tasks` | `error` | | Recent task query failed. |
| `proxmox.task.failed` | `host`, optional `node`, `type`, `status` | `status`, `taskId`, `startTime`, `endTime` | `actorId=user`, `correlationId=UPID` | Recent task has an error status. |
| `proxmox.backup.failed` | `host`, optional `node`, `type`, `status` | `status`, `taskId`, `startTime`, `endTime` | `actorId=user`, `correlationId=UPID` | Recent `vzdump` task has an error status. |
| `proxmox.backup.jobs.failed` | `host`, `operation=backup_jobs` | `error` | | Backup job list failed. |
| `proxmox.backup.coverage.failed` | `host`, `operation=backup_coverage` | `error` | | Backup coverage query failed. |
| `proxmox.ceph.status.failed` | `host`, `operation=ceph_status` | `error` | | Unexpected Ceph status query failure. |
| `proxmox.ceph.pools.failed` | `host`, `operation=ceph_pools` | `error` | | Ceph pool query failed on all candidate nodes. |

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `host` | `pve-01` | all signals | Stable configured host ID. |
| `node` | `pve-01` | node, resource, and task signals | Proxmox node name. |
| `type` | `qemu` | resources and tasks | Proxmox resource or task type. |
| `status` | `running` | resource and task aggregates | Raw resource status or normalized task status class. |
| `vmid` | `100` | guest signals | Proxmox VM or container ID. |
| `guest` | `postgres` | backup coverage | Guest display name. |
| `job` | `backup-01` | backup job states | Backup job ID. |
| `pool` | `rbd` | Ceph pool signals | Ceph pool name. |
| `state` | `active+clean` | Ceph PG metrics | Placement group state. |
| `operation` | `cluster_resources` | failure events | Bounded collector operation that failed. |

UPIDs, user IDs, task IDs, timestamps, node IPs, and Ceph FSIDs are identity fields, attributes, or states rather than dimensions.

## Requirements

- Runs on a Proxmox VE node.
- Local `pvesh` access and sufficient permissions.

## Failure behavior

Missing `pvesh` emits `proxmox.available=false`. A standalone host does not emit a synthetic cluster resource. Missing or unconfigured Ceph emits `system.ceph.available=false` without follow-up failure events. Other failures remain scoped and do not stop host modules.
