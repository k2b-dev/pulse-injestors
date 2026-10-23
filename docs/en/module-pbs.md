---
title: Proxmox Backup Server module
navTitle: PBS
section: Modules
order: 450
description: Local Proxmox Backup Server collection through proxmox-backup-debug.
tags: [pbs, modules, reference]
updated: 2026-10-23
---

# Proxmox Backup Server module

`pulse-proxmox-backup-server` uses the local PBS 4 API through `proxmox-backup-debug`. Run **pulse-linux too**, with profile `pbs`, for host CPU, RAM, disks, SMART, packages and systemd.

## Resources and cadence

| Resource | ID components | Meaning |
|---|---|---|
| `proxmox-backup-server` | stable host ID | Version, availability, task and job totals. |
| `pbs-datastore` | host + datastore | Capacity, snapshot totals and GC. |
| `pbs-job` | host + kind + job | Sync, verify, prune or tape job. |
| `pbs-backup-group` | host + datastore + type/id at root; host + datastore + namespace + type/id otherwise | Latest snapshot for vm/101, ct/203 or host/x; label is `group on datastore` at root or `group in namespace on datastore`. |

Datastores run every 300 seconds, jobs and groups every 900 seconds, and versions every 86400 seconds. `once` runs all phases. States are sent on change and refreshed after six hours when checked again. All times use UTC; ages use the collection timestamp. User-configured dimensions apply in addition to those listed. API text in states and error attributes is limited to 500 Unicode characters.

Namespaces are discovered recursively for each datastore, including root even if the API omits it, then read breadth-first (shallower namespaces before deeper ones). Group age and time come from `/groups` (`last-backup`, Unix seconds); snapshot totals and counts by type sum `backup-count`. The collector never requests full snapshot lists or file details. Root group IDs stay unchanged. Non-root groups add the namespace to their ID and expose a `namespace` dimension. Group identities that exceed Pulse limits use `sha256:` plus the hash of the full identity. Labels are truncated to 240 UTF-16 code units and dimension values to 500, without splitting Unicode characters; deep namespaces therefore remain collectable.

At most 2,000 groups are emitted per host per collection, ordered by datastore, namespace depth, namespace name and group name. `pbs.groups.truncated=true` means further group resources were omitted; their `backup-count` still contributes to datastore totals. `pbs.groups.complete=true` means every namespace was read, independently of this output cap. Query failures or an exhausted groups budget set it to `false` and emit `pbs.datastore.groups.failed` (or `pbs.datastore.list.failed` if discovery failed). Collected groups and summaries are retained; totals from an incomplete scan are partial counts, not full inventory. Check completeness before alerting on datastore totals.

Use `proxmox.backup.last_success.age` from **pulse-proxmox** for guest backup freshness alerts. PBS retains groups of deleted guests, which can remain old indefinitely. PBS group ages are useful for client and namespace backups such as `host/…` groups. `pbs.group.stale=true` marks a group whose last backup is older than `[pbs] stale_after_hours` (default 168 hours), so alerts can exclude old groups. It does not establish that a guest was deleted. A missing last-backup timestamp has no age, an empty time and `stale=false`; a missing group emits no group signals.

Verification failures are reported through the verify job's `pbs.job.last_run.result` and `pbs.job.failed` event. Removed signals: `pbs.datastore.verify.failed_snapshots` (no per-snapshot verification scan) and `pbs.datastore.snapshots.failed` (replaced by `pbs.datastore.groups.failed`). Existing snapshot total, type and latest-time/age signals remain, derived entirely from group summaries.

To check primary, second-copy and offsite freshness, compare the same group on each destination host/datastore. An S3-backed datastore is included when PBS exposes it through the same API. This collector does not query S3 directly or infer replication from a successful job alone.

## Metrics

| Signal | Type | Unit | Dimensions | Cadence | Example | Meaning |
|---|---|---|---|---|---|---|
| `pbs.datastores.total` | gauge | — | — | 300 s | `2` | Total datastore count on the local PBS host. |
| `pbs.datastore.bytes.used` | gauge | bytes | `datastore` | 300 s | `1099511627776` | Used bytes for one PBS datastore. Emitted on `pbs-datastore`. |
| `pbs.datastore.bytes.total` | gauge | bytes | `datastore` | 300 s | `4398046511104` | Total bytes for one PBS datastore. Emitted on `pbs-datastore`. |
| `pbs.datastore.bytes.usage` | gauge | percent | `datastore` | 300 s | `25` | Used percentage for one PBS datastore. Emitted on `pbs-datastore`. |
| `pbs.datastore.bytes.available` | gauge | bytes | `datastore` | 300 s | `3298534883328` | Available bytes for one PBS datastore. Emitted on `pbs-datastore`. |
| `pbs.datastore.estimated_full.seconds_until` | gauge | seconds | `datastore` | 300 s | `7776000` | Seconds until estimated full date. Emitted on `pbs-datastore`. |
| `pbs.datastore.snapshots.total` | gauge | — | `datastore` | 900 s | `180` | Sum of group backup-count across read namespaces; partial when groups.complete is false. |
| `pbs.datastore.snapshots.by_type` | gauge | — | `datastore`, `type` | 900 s | `120` | Sum of group backup-count by type across read namespaces; may be partial. |
| `pbs.datastore.snapshot.latest.age` | gauge | seconds | `datastore` | 900 s | `3600` | Age of the newest last-backup among read groups; may be partial. |
| `pbs.jobs.total` | gauge | — | `kind` | 900 s | `3` | Job count for one kind. |
| `pbs.jobs.enabled` | gauge | — | `kind` | 900 s | `2` | Enabled job count for one kind. |
| `pbs.job.last_run.age` | gauge | seconds | `kind`, `job`, `datastore` | 900 s | `86400` | Age of a job's last run. |
| `pbs.job.next_run.seconds_until` | gauge | seconds | `kind`, `job`, `datastore` | 900 s | `43200` | Seconds until a job's next run. |
| `pbs.tasks.recent` | gauge | — | — | 900 s | `50` | Recent task count. |
| `pbs.tasks.by_status` | gauge | — | `status` | 900 s | `46` | Recent task count by status class. |
| `pbs.tasks.by_type_status` | gauge | — | `type`, `status` | 900 s | `12` | Recent task count by worker type and status class. |
| `pbs.datastore.gc.last_run.age` | gauge | seconds | `datastore` | 900 s | 86400 | Age since the last completed GC run, regardless of success. |
| `pbs.group.last_snapshot.age` | gauge | seconds | `datastore`, non-root `namespace` | 900 s | 3600 | Age of the newest snapshot in this backup group. |

## States

| Signal | Type | Unit | Dimensions | Cadence | Example | Meaning |
|---|---|---|---|---|---|---|
| `pbs.available` | state boolean | — | — | 300 s; also 86400 s | `true` | Whether the latest datastore-usage or version query succeeded. A command setup failure also sets false. |
| `pbs.version` | state string | — | — | 86400 s | `4.0` | PBS version. |
| `pbs.release` | state string | — | — | 86400 s | `4.0` | PBS release. |
| `pbs.repoid` | state string | — | — | 86400 s | `abcd1234` | PBS repository id. |
| `pbs.datastore.present` | state boolean | — | `datastore` | 300 s | `true` | Datastore exists. Emitted on `pbs-datastore`. |
| `pbs.datastore.estimated_full.time` | state string | — | `datastore` | 300 s | `2026-09-01T00:00:00Z` | Estimated full timestamp. Emitted on `pbs-datastore`. |
| `pbs.datastore.snapshot.latest.time` | state string | — | `datastore` | 900 s | `2026-06-11T00:00:00Z` | Newest last-backup timestamp among read groups; may be partial. |
| `pbs.job.present` | state boolean | — | `kind`, `job`, `datastore` | 900 s | `true` | Job exists. |
| `pbs.job.enabled` | state boolean | — | `kind`, `job`, `datastore` | 900 s | `true` | Job is enabled. |
| `pbs.job.schedule` | state string | — | `kind`, `job`, `datastore` | 900 s | `daily` | Job schedule. |
| `pbs.job.last_run.state` | state string | — | `kind`, `job`, `datastore` | 900 s | `OK` | Raw last-run state, limited to 500 characters. |
| `pbs.job.last_run.result` | state string | — | `kind`, `job`, `datastore` | 900 s | `warnings` | Normalized result: ok, warnings, error or unknown. |
| `pbs.job.last_run.time` | state string | — | `kind`, `job`, `datastore` | 900 s | `2026-06-11T00:00:00Z` | Last-run end time. |
| `pbs.job.next_run.time` | state string | — | `kind`, `job`, `datastore` | 900 s | `2026-06-12T00:00:00Z` | Next run time. |
| `pbs.groups.complete` | state boolean | — | — | 900 s | true | Every namespace was read within the groups budget; false on discovery/query failure or timeout. |
| `pbs.group.stale` | state boolean | — | `datastore`, non-root `namespace` | 900 s | false | Last backup is older than stale_after_hours (default 168 h); false if no last-backup timestamp exists. |
| `pbs.groups.truncated` | state boolean | — | — | 900 s | false | More than 2,000 groups found across this host; later groups omitted. |
| `pbs.group.last_snapshot.time` | state string | — | `datastore`, non-root `namespace` | 900 s | 2026-10-23T10:00:00Z | UTC timestamp of the newest snapshot in the group. |
| `pbs.datastore.gc.last_run.state` | state string | — | `datastore` | 900 s | OK | Raw last GC result, limited to 500 characters; empty when no result is available. |
| `pbs.datastore.gc.last_run.result` | state string | — | `datastore` | 900 s | ok | Normalized GC result: ok, warnings, error, running, never or unknown. |
| `pbs.datastore.gc.last_run.time` | state string | — | `datastore` | 900 s | 2026-10-23T00:00:00Z | Last GC completion time; empty if not available. |

Result states classify `OK` as `ok`, `WARNINGS: n` as `warnings`, an empty result as `unknown`, and other non-empty text as `error`. Only `error` produces job failure events; warnings remain visible through the result state. The existing `.state` retains the raw text, limited to 500 characters.

GC first reads `/admin/gc`. If its result is missing, it reads the datastore's `/gc` status. An explicit running status/flag, or a list UPID without a completion time or result, reports `running`. A successful status read with no UPID, completion time or result reports `never`. `unknown` means the data cannot establish a result, for example a stored UPID without a result, malformed status or a failed status query. A stored per-datastore UPID alone does not prove GC is running. `pbs.datastore.gc.failed` makes query and parsing failures visible.

Job `kind` is sync, verify, prune or tape. GC is reported on its datastore. Job age and next-run metrics are omitted when the API has no timestamp. A negative seconds-until value means the scheduled time has passed. Estimated-full is emitted only when PBS provides a prediction; it is not a guaranteed exhaustion date.

## Events

All events have no unit. Command failures carry `attributes.error`. Job failures carry the truncated raw `state` and use `last-run-upid` as `correlationId`. They are emitted once per UPID while it remains in the job results. Task failures are emitted once per UPID while present in the recent task list, with at most ten new failures per cycle after de-duplication. Seen sets are pruned after successful list reads. A restart or a result that leaves and re-enters the lists can repeat an event. Task events carry optional `status`, `workerId`, `startTime`, `endTime`, with the user in `actorId` and UPID in `correlationId`.

| Signal | Type | Unit | Dimensions | Cadence | Example | Meaning |
|---|---|---|---|---|---|---|
| `pbs.config.failed` | event | — | `operation=configure` | each due phase | error | Invalid command configuration. |
| `pbs.version.failed` | event | — | `operation=version` | 86400 s | error | Version query failed. |
| `pbs.datastore.usage.failed` | event | — | optional `datastore`, `operation=datastore_usage` | 300 s | error | Capacity query failed, or PBS returned an error for this datastore; no capacity values are emitted for it. |
| `pbs.datastore.list.failed` | event | — | `operation=datastore_list` | 900 s | error | Datastore discovery failed. |
| `pbs.datastore.groups.failed` | event | — | `datastore`, `operation=groups` | 900 s | error | Namespace or group listing failed or the groups budget expired; collected results are retained. |
| `pbs.datastore.gc.failed` | event | — | `datastore`, `operation=gc_status` | 900 s | error | Per-datastore GC status query or GC status parsing failed. |
| `pbs.job.list.failed` | event | — | `kind`, `operation=job_list` | 900 s | error | One job-kind query failed. |
| `pbs.job.failed` | event | — | `kind`, `job`, optional `datastore`, `status=error` | 900 s | error | A completed job has an error result; warnings do not emit failure events. |
| `pbs.tasks.failed` | event | — | `operation=tasks` | 900 s | error | Task listing failed. |
| `pbs.task.failed` | event | — | optional `node`, `type`, `status` | 900 s | error | Recent task has an error status; at most ten events per collection. |

Missing `proxmox-backup-debug` reports `pbs.available=false` quietly; query, permission, timeout and parsing failures produce scoped events without stopping the other phases.

## Read-only API paths

Commands use `proxmox-backup-debug api get <path> --output-format json`; query parameters become flags, including `--ns` for non-root namespaces. Commands parse stdout only, capped at 64 MiB; larger output fails visibly. Command errors include at most 1 KiB of stderr.

`[pbs] timeout_seconds` defaults to 10. Namespace and group calls use `list_timeout_seconds`, default 120. The groups phase has its own `groups_timeout_seconds` budget, default 300, overriding `[runner] collector_timeout_seconds` for that phase. Each call is bounded by the remaining phase budget. Both list and groups timeouts must be positive; raise the groups budget if `pbs.groups.complete=false` indicates a timeout. Other phases use the runner timeout. `stale_after_hours` must also be positive.

- Capacity: `/status/datastore-usage`.
- Groups: `/admin/datastore`, `/admin/datastore/<store>/namespace?max-depth=7`, then `/admin/datastore/<store>/groups` for each namespace (`?ns=<namespace>` for non-root; omitted at root).
- Jobs: `/admin/gc`, `/admin/prune`, `/admin/sync`, `/admin/verify`, `/tape/backup` (an empty list means no tape jobs).
- GC fallback when the list has no result: `/admin/datastore/<store>/gc`.
- Tasks, in the jobs phase: `/nodes/localhost/tasks?limit=50`.
- Version: `/version`.
