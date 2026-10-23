---
title: Proxmox Backup Server module
navTitle: PBS
section: Modules
order: 450
description: Local Proxmox Backup Server collection through proxmox-backup-debug.
tags: [pbs, modules, reference]
updated: 2026-07-23
---

# Proxmox Backup Server module

The PBS module collects Proxmox Backup Server data locally through `proxmox-backup-debug`.

## Resources

| Resource type | Resource key | Used for |
|---|---|---|
| `proxmox-backup-server` | `proxmox-backup-server:<host-id>` | PBS server availability, version, tasks, and aggregate datastore count. |
| `pbs-datastore` | `pbs-datastore:<host-id>:<datastore>` | Per-datastore usage, estimated-full, snapshot count, and latest snapshot signals. |
| `pbs-job` | `pbs-job:<host-id>:<kind>:<job>` | Job state, last-run, next-run, and job-failure signals. |

`host:<host-id>` is used by Linux host modules in the `pulse-proxmox-backup-server` binary. The PBS module itself emits only the resource types above.

## Scope notes

- Datastore usage and snapshot signals are emitted once per datastore on `pbs-datastore`.
- Job signals are emitted once per job on `pbs-job`.
- Task aggregate signals stay on `proxmox-backup-server`; individual recent tasks are reported as events on that server resource.
- `pbs.datastore.snapshots.failed` is scoped to the datastore that failed.

## Metrics

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `pbs.datastores.total` | gauge |  | — | `2` | Total datastore count on the local PBS host. |
| `pbs.datastore.bytes.used` | gauge | bytes | `datastore` | `1099511627776` | Used bytes for one PBS datastore. Emitted on `pbs-datastore`. |
| `pbs.datastore.bytes.total` | gauge | bytes | `datastore` | `4398046511104` | Total bytes for one PBS datastore. Emitted on `pbs-datastore`. |
| `pbs.datastore.bytes.usage` | gauge | percent | `datastore` | `25` | Used percentage for one PBS datastore. Emitted on `pbs-datastore`. |
| `pbs.datastore.bytes.available` | gauge | bytes | `datastore` | `3298534883328` | Available bytes for one PBS datastore. Emitted on `pbs-datastore`. |
| `pbs.datastore.estimated_full.seconds_until` | gauge | seconds | `datastore` | `7776000` | Seconds until estimated full date. Emitted on `pbs-datastore`. |
| `pbs.datastore.snapshots.total` | gauge |  | `datastore` | `180` | Snapshot count in one datastore. Emitted on `pbs-datastore`. |
| `pbs.datastore.snapshots.by_type` | gauge |  | `datastore`, `type` | `120` | Snapshot count by backup type. Emitted on `pbs-datastore`. |
| `pbs.datastore.snapshot.latest.age` | gauge | seconds | `datastore` | `3600` | Age of latest snapshot in one datastore. Emitted on `pbs-datastore`. |
| `pbs.jobs.total` | gauge |  | `kind` | `3` | Job count for one kind. |
| `pbs.jobs.enabled` | gauge |  | `kind` | `2` | Enabled job count for one kind. |
| `pbs.job.last_run.age` | gauge | seconds | `kind`, `job`, `datastore` | `86400` | Age of a job's last run. |
| `pbs.job.next_run.seconds_until` | gauge | seconds | `kind`, `job`, `datastore` | `43200` | Seconds until a job's next run. |
| `pbs.tasks.recent` | gauge |  | — | `50` | Recent task count. |
| `pbs.tasks.by_status` | gauge |  | `status` | `46` | Recent task count by status class. |
| `pbs.tasks.by_type_status` | gauge |  | `type`, `status` | `12` | Recent task count by worker type and status class. |

## States

| Key | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|
| `pbs.available` | boolean | — | `true` | Whether local PBS version collection succeeded. |
| `pbs.version` | string | — | `3.2.2` | PBS version. |
| `pbs.release` | string | — | `3.2` | PBS release. |
| `pbs.repoid` | string | — | `abcd1234` | PBS repository id. |
| `pbs.datastore.present` | boolean | `datastore` | `true` | Datastore exists. Emitted on `pbs-datastore`. |
| `pbs.datastore.estimated_full.time` | string | `datastore` | `2026-09-01T00:00:00Z` | Estimated full timestamp. Emitted on `pbs-datastore`. |
| `pbs.datastore.snapshot.latest.time` | string | `datastore` | `2026-06-11T00:00:00Z` | Latest snapshot timestamp. Emitted on `pbs-datastore`. |
| `pbs.job.present` | boolean | `kind`, `job`, `datastore` | `true` | Job exists. |
| `pbs.job.enabled` | boolean | `kind`, `job`, `datastore` | `true` | Job is enabled. |
| `pbs.job.schedule` | string | `kind`, `job`, `datastore` | `daily` | Job schedule. |
| `pbs.job.last_run.state` | string | `kind`, `job`, `datastore` | `OK` | Raw last-run state. |
| `pbs.job.last_run.time` | string | `kind`, `job`, `datastore` | `2026-06-11T00:00:00Z` | Last-run end time. |
| `pbs.job.next_run.time` | string | `kind`, `job`, `datastore` | `2026-06-12T00:00:00Z` | Next run time. |

## Events

| Kind | Dimensions | Attributes | Identities | Trigger |
|---|---|---|---|---|
| `pbs.config.failed` | `operation=configure` | `error` | | Invalid local command configuration. |
| `pbs.version.failed` | `operation=version` | `error` | | Version query failed. |
| `pbs.datastore.usage.failed` | `operation=datastore_usage` | `error` | | Datastore usage query failed. |
| `pbs.datastore.snapshots.failed` | `datastore`, `operation=snapshots` | `error` | | Snapshot query failed for one datastore. |
| `pbs.job.list.failed` | `kind`, `operation=job_list` | `error` | | Job list failed for one job kind. |
| `pbs.job.failed` | `kind`, `job`, optional `datastore`, `status=error` | `state` | | Job last-run state is not `OK`/`ok`. |
| `pbs.tasks.failed` | `operation=tasks` | `error` | | Recent task query failed. |
| `pbs.task.failed` | optional `node`, `type`, `status` | `status`, `workerId`, `startTime`, `endTime` | `actorId=user`, `correlationId=UPID` | Recent task status class is `error`. |

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `datastore` | `main` | datastore and job signals | PBS datastore name. |
| `kind` | `sync` | job signals | PBS job kind: `gc`, `prune`, `sync`, or `verify`. |
| `job` | `sync-main` | job signals | Job id. |
| `node` | `localhost` | task events | PBS task node. |
| `type` | `backup` | task aggregate metrics and events | PBS worker type. |
| `status` | `ok` | task aggregate metrics | Normalized task status class. |
| `operation` | `datastore_usage` | failure events | Bounded collector operation that failed. |

Worker IDs, UPIDs, users, and timestamps are attributes or first-class identity fields rather than dimensions.

## Requirements

- Runs on a Proxmox Backup Server host.
- `proxmox-backup-debug` and local permissions.

## Failure behavior

Missing `proxmox-backup-debug` emits `pbs.available=false`. Unavailable local API data emits availability states or scoped failure events without stopping host modules.
