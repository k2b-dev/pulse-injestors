---
title: Proxmox module
navTitle: Proxmox
section: Modules
order: 440
description: Local Proxmox VE collection through pvesh.
tags: [proxmox, ceph, modules, reference]
updated: 2026-10-23
---

# Proxmox VE module

`pulse-proxmox` reads local PVE 9 API data with `pvesh`. Run **pulse-linux too** on every node for host CPU, RAM, disks, SMART, packages and systemd; use its `proxmox-node` profile.

## Election and resources

Each collection phase reads `/cluster/status`. In a real cluster, a node can report cluster-wide data only when its local view has quorum (`quorate=1`). Among online nodes in that view, the node with the lowest positive `nodeid` reports cluster-wide data. If any online candidate lacks a positive ID, all candidates are compared by name instead. Equal IDs also use name order. The API's `local=1` identifies this node; the configured stable host ID is the fallback. An offline local node cannot win against an online node. A standalone node is elected without creating a synthetic cluster resource. A failed status query stops that phase rather than assuming standalone operation.

Election is checked again on every due phase. The next health cycle after a leader disappears transfers cluster health and guest states to the next online candidate; capacity and backup data follow their normal cadences. During a 3/2 partition of a five-node cluster, only the quorate side elects a reporter. Nodes without quorum continue reporting local node health and local storage; they do not report cluster-wide data. Standalone nodes do not require cluster quorum.

| Resource | ID components | Scope |
|---|---|---|
| `proxmox-node` | cluster name + node; host ID + node when standalone | Each node reports only its own availability, quorum, status and version. |
| `proxmox-cluster` | cluster name | Elected node reports quorum, counts and tasks. |
| `proxmox-guest` | cluster name + VMID; host ID + VMID when standalone | All non-template QEMU/LXC guests, including stopped guests. Label is the guest name. |
| `proxmox-storage` | cluster + storage for shared; cluster + node + storage for local | Only the elected node reports shared storage. Each node reports its own local storage. |
| `proxmox-backup-job` | cluster + job ID | Elected node reports backup job configuration. |

Availability failures use the last successfully discovered node and cluster identity. Before the first successful query, the node resource uses the stable host ID for both ID components.

IDs escape delimiters using the shared resource contract. Guest node placement is a state, never a metric dimension, so live migration preserves the guest's series. Templates are skipped entirely; no template state is emitted. Guest metrics include zero values. CPU is the API's fraction of allocated vCPU capacity multiplied by 100. Counter resets can accompany a restart or migration; calculate rates per guest with reset handling.

States are sent on change and refreshed after six hours when checked again. The cadences below describe collection; `once` runs all phases. The runner interval remains the minimum scheduling resolution. User-configured dimensions apply in addition to those listed. API text in states and error attributes is limited to 500 Unicode characters.

## Metrics

| Signal | Type | Unit | Dimensions | Cadence | Example | Meaning |
|---|---|---|---|---|---|---|
| `proxmox.nodes.total` | gauge | — | — | 60 s | 5 | Nodes returned by the local cluster view. |
| `proxmox.nodes.online` | gauge | — | — | 60 s | 5 | Nodes currently online. |
| `proxmox.guest.cpu.usage` | gauge | percent | — | 300 s | 0 | Guest CPU use as a percentage of allocated vCPUs. |
| `proxmox.guest.memory.used` | gauge | bytes | — | 300 s | 0 | Guest memory in use. |
| `proxmox.guest.memory.max` | gauge | bytes | — | 300 s | 0 | Configured guest memory limit. |
| `proxmox.guest.uptime` | gauge | seconds | — | 300 s | 0 | Guest uptime; zero for a stopped guest. |
| `proxmox.guest.disk.read` | counter | bytes | — | 300 s | 0 | Cumulative guest disk bytes read. |
| `proxmox.guest.disk.write` | counter | bytes | — | 300 s | 0 | Cumulative guest disk bytes written. |
| `proxmox.guest.net.in` | counter | bytes | — | 300 s | 0 | Cumulative guest network bytes received. |
| `proxmox.guest.net.out` | counter | bytes | — | 300 s | 0 | Cumulative guest network bytes sent. |
| `proxmox.storage.bytes.used` | gauge | bytes | — | 300 s | 0 | Used Proxmox storage bytes. Only enabled, active storages emit capacity. |
| `proxmox.storage.bytes.total` | gauge | bytes | — | 300 s | 0 | Total Proxmox storage bytes. Only enabled, active storages emit capacity. |
| `proxmox.storage.bytes.available` | gauge | bytes | — | 300 s | 0 | Available Proxmox storage bytes. Only enabled, active storages emit capacity. |
| `proxmox.tasks.recent` | gauge | — | — | 60 s | 0 | Tasks in the API recent-task window. |
| `proxmox.tasks.by_status` | gauge | — | `status` | 60 s | 0 | Recent tasks by normalized status: running, ok, error or unknown. |
| `proxmox.tasks.by_type_status` | gauge | — | `type`, `status` | 60 s | 0 | Recent tasks by task type and normalized status. |
| `proxmox.backup.tasks.recent` | gauge | — | — | 60 s | 0 | Recent vzdump tasks. |
| `proxmox.backup.tasks.success` | gauge | — | — | 60 s | 0 | Recent successful vzdump tasks. |
| `proxmox.backup.tasks.failed` | gauge | — | — | 60 s | 0 | Recent failed vzdump tasks. |
| `proxmox.backup.last_success.age` | gauge | seconds | `storage` | 900 s | 3600 | Age of the newest backup ctime for this guest on this PBS storage. Absent if no backup exists. |

## States

| Signal | Type | Unit | Dimensions | Cadence | Example | Meaning |
|---|---|---|---|---|---|---|
| `proxmox.available` | state boolean | — | — | each due phase | true | Local command and cluster-status query are available. |
| `proxmox.node.online` | state boolean | — | — | 60 s | true | Local node online flag. |
| `proxmox.node.quorate` | state boolean | — | — | 60 s | true | Quorum in this node's local cluster view; true for a standalone node. |
| `proxmox.node.status` | state string | — | — | 60 s | online | Local status: online or offline. |
| `proxmox.cluster.elected` | state boolean | — | — | 60 s | true | Whether this node is elected. |
| `proxmox.version` | state string | — | — | 86400 s | 9.0 | Local PVE version. |
| `proxmox.release` | state string | — | — | 86400 s | 9.0 | Local PVE release. |
| `proxmox.repoid` | state string | — | — | 86400 s | abc123 | Local PVE repoid. |
| `proxmox.cluster.name` | state string | — | — | 60 s | lab | Real cluster name. |
| `proxmox.cluster.quorate` | state boolean | — | — | 60 s | true | Whether the cluster has quorum. |
| `proxmox.guest.status` | state string | — | — | 60 s; also 300 s | running | Raw guest status, including running, stopped or paused. |
| `proxmox.guest.node` | state string | — | — | 60 s; also 300 s | node-a | Current guest node. |
| `proxmox.guest.ha.state` | state string | — | — | 60 s | started | HA service state, only for managed non-template guests; the API display sentence is never emitted. |
| `proxmox.storage.active` | state boolean | — | — | 300 s | true | Whether Proxmox storage is active. |
| `proxmox.storage.enabled` | state boolean | — | — | 300 s | true | Whether Proxmox storage is enabled. |
| `proxmox.backup.guest.covered` | state boolean | — | — | 900 s | true | At least one enabled job selects this guest; not proof that a backup succeeded. |
| `proxmox.backup.guest.jobs` | state string | — | — | 900 s | hourly,nightly | Sorted comma-separated selecting job IDs, limited to 500 characters; empty when uncovered. |
| `proxmox.backup.last_success.time` | state string | — | `storage` | 900 s | 2026-10-23T10:00:00Z | Newest backup UTC timestamp on this storage; empty if none. |
| `proxmox.backup.job.enabled` | state boolean | — | — | 900 s | true | Backup job enabled. |
| `proxmox.backup.job.schedule` | state string | — | — | 900 s | hourly | Backup job schedule. |
| `proxmox.backup.job.storage` | state string | — | — | 900 s | pbs-main | Backup job storage. |
| `proxmox.ceph.api.enabled` | state boolean | — | — | 60 s | true | Ceph API collection is enabled on the elected node. |

## Backup coverage and copies

Enabled jobs select guests through `all`, VMID lists or pools. `exclude` always wins, and a job's optional `node` restriction applies to the guest's current placement. Coverage is independent of freshness: intentionally excluded guests are uncovered, not failed. Set alert expectations to match your backup policy and per-guest schedules.

A storage whose `nodes=` restriction excludes the elected node is not reported by that node. Shared storage capacity and backup freshness therefore require visibility on the elected node.

For each active, enabled PBS storage visible on the elected node, the collector lists backup content and takes the newest `ctime` per VMID. Storage-specific age and time distinguish copies exposed as PVE storages. Empty time means no backup exists on that successfully queried storage; query failures emit an event and do not invent a result. For a second PBS or offsite datastore not exposed to PVE, compare the corresponding `pbs-backup-group` on the destination PBS. A successful sync job alone is not proof that every guest was copied. See the [PBS module](/en/module-pbs).

## Ceph

Only the elected node uses the PVE Ceph API. All Ceph signals, units, dimensions and resources are listed in the [Ceph module](/en/module-ceph). The local Ceph CLI is not run by this binary. After a successful Ceph query, an availability failure updates the same `ceph-cluster` resource using the cached FSID. Before the first success, availability remains on the reporting cluster or standalone node.

## Events

All events have no unit. Command failures carry `attributes.error`; task failures carry `status`, `taskId`, `startTime` and `endTime` where available. Task users are `actorId`, and UPIDs are `correlationId`. Each task event kind is deduplicated by UPID while the task remains in the current task list. At most ten new events of each kind are emitted per health cycle, after removing duplicates; remaining failures can be emitted on later cycles. The seen set is pruned to the current task list. A restart, election handover or task that leaves and re-enters the list can repeat a failure.

| Signal | Type | Unit | Dimensions | Cadence | Example | Meaning |
|---|---|---|---|---|---|---|
| `proxmox.config.failed` | event | — | `operation=configure` | each due phase | error | Invalid command configuration. |
| `proxmox.cluster.status.failed` | event | — | `operation=cluster_status` | each due phase | error | Cannot read election; phase stops. |
| `proxmox.version.failed` | event | — | `operation=version` | 86400 s | error | Version query failed. |
| `proxmox.cluster.resources.failed` | event | — | `operation=cluster_resources` | 60 / 300 / 900 s | error | Guest resource query failed. |
| `proxmox.cluster.tasks.failed` | event | — | `operation=cluster_tasks` | 60 s | error | Task query failed. |
| `proxmox.ha.failed` | event | — | `operation=ha` | 60 s | error | HA status query failed. |
| `proxmox.storage.failed` | event | — | `operation=storage` | 300 / 900 s | error | Storage listing failed. |
| `proxmox.backup.jobs.failed` | event | — | `operation=backup_jobs` | 900 s | error | Coverage and job configuration query failed. |
| `proxmox.backup.content.failed` | event | — | `storage`, `operation=backup_content` | 900 s | error | One PBS storage backup listing failed. |
| `proxmox.ceph.status.failed` | event | — | `operation=ceph_status` | 60 s | error | Unexpected Ceph status failure. |
| `proxmox.ceph.pools.failed` | event | — | `operation=ceph_pools` | 60 s | error | Ceph pool query failed. |
| `proxmox.task.failed` | event | — | optional `node`, `type`, `status` | 60 s, on first observation | error | Recent failed task; at most ten new task failures per cycle. |
| `proxmox.backup.failed` | event | — | optional `node`, `type`, `status` | 60 s, on first observation | error | Recent failed vzdump task; at most ten new failures per cycle. |

## Read-only API paths

Commands use `pvesh get <path> --output-format json`; query parameters become CLI flags. Commands parse stdout only, capped at 64 MiB; larger output fails visibly. Command errors include at most 1 KiB of stderr. `[proxmox] timeout_seconds` defaults to 10; storage content lists use `list_timeout_seconds`, which defaults to 60 and must be positive. Both are bounded by `[runner] collector_timeout_seconds`; raise the collector timeout when a whole phase with several lists needs more time.

- Every phase: `/cluster/status`.
- Version: `/version`.
- Elected health: `/cluster/resources`, `/cluster/ha/status/current`, `/cluster/tasks`, `/cluster/ceph/status`, `/nodes/<local>/ceph/pools` (last two only when Ceph API is enabled).
- Capacity: `/nodes/<local>/storage` on every node; `/cluster/resources` only on the elected node.
- Elected backups: `/cluster/resources`, `/cluster/backup`, `/nodes/<local>/storage`, and `/nodes/<local>/storage/<storage>/content?content=backup` for each active, enabled PBS storage.

Missing `pvesh` reports unavailable quietly. Missing or unconfigured Ceph reports unavailable quietly; other API, permission, timeout and JSON errors remain visible as events. No SSH or direct guest connection is used.
