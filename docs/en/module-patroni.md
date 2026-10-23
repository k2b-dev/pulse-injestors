---
title: Patroni module
navTitle: Patroni
section: Modules
order: 430
description: Monitor local Patroni health and capacity with bounded probes.
tags: [patroni, linux, modules, reference]
updated: 2026-10-23
---

# Patroni module

Monitor the local Patroni member and, on the leader, its cluster. Add `[patroni]` to enable collection every 60 seconds; without the section no connection is made.

```toml
[patroni]
url = "https://127.0.0.1:8008"
ca_file = "/etc/pulse/service-ca.pem"
server_name = "patroni.example.test"
# cluster = "database" # Optional override for /patroni patroni.scope.
# username = "monitor"
# password_file = "/etc/pulse/patroni.password"
# insecure_skip_verify = false
```

`url` must use HTTP or HTTPS without URL credentials, query, or fragment. HTTPS verifies the server with system roots or `ca_file`; `server_name` overrides the certificate hostname and SNI. Password authentication over plain HTTP is accepted only for a literal loopback address or `localhost`. `insecure_skip_verify` defaults to false; enabling it disables certificate verification. Configure `username` and `password_file` together for Basic authentication. The password file contains one password with an optional trailing newline.

Requests are `GET <url>/patroni`, then `GET <url>/cluster` on every member. No other member is contacted. Roles `leader`, `master`, `primary`, and `standby_leader` become `leader` in Pulse and emit cluster signals.

On replicas, `postgres.replication.lag` is the byte lag reported by the `/cluster` member whose name matches `/patroni`'s `patroni.name`. A lag of `"unknown"`, null, or an absent lag omits the metric and sets `postgres.replication.lag_known = false`; a numeric lag sets it to true. The member's state, when present, becomes `postgres.replication.state`. A missing member fails the collection and sets lag_known false.

`postgres.replication.replay_backlog` separately measures the local received WAL position minus the replayed position, clamped to zero. It is omitted when either position is unavailable. A disconnected replica can have zero local backlog while its cluster lag is unknown.

Member signals use the host resource. Cluster signals use resource type `postgres-cluster`, with ID and label equal to `cluster` or the response's `patroni.scope`. Use a unique scope across clusters in the same Pulse base. The leader fails cluster collection if neither name is available. `/cluster` accepts at most 1000 members.

Set `[linux] profile = "postgres"` to monitor `patroni.service` and `etcd.service`. This does not enable database probes; add [PostgreSQL](/en/module-postgres) for connections, size, and transaction health. The `pg-edge` profile monitors `pgbouncer.service`, `haproxy.service`, and `keepalived.service` on edge VMs.

Override `patroni` in `[runner.cadence]`. A healthy leader emits four metric samples per minute; a replica with known cluster lag and both WAL positions emits three. Cluster signals move to the new leader after failover.

## Signals

Dimensions below are additional to configured global dimensions. Plain counts use an empty unit (`""`). States are sent on change and refreshed after six hours when next observed; daily states are observed and refreshed daily. `once` runs every collection group regardless of cadence.

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `postgres.role` | state string | `""` | — | `leader` | Local role; leader/master/primary/standby_leader normalize to leader. | 60 s |
| `postgres.state` | state string | `""` | — | `running` | Local PostgreSQL state reported by Patroni. | 60 s |
| `postgres.timeline` | gauge | `""` | — | `7` | Local WAL timeline, when reported. | 60 s |
| `postgres.replication.lag` | gauge | `bytes` | — | `1024` | Replica byte lag from its own /cluster member; omitted when unknown. | 60 s |
| `postgres.replication.lag_known` | state boolean | `""` | — | `false` | Whether the replica member has a numeric cluster lag; unknown is not zero. | 60 s |
| `postgres.replication.state` | state string | `""` | — | `streaming` | Replica member state from /cluster, when present. | 60 s |
| `postgres.replication.replay_backlog` | gauge | `bytes` | — | `1024` | Local receive minus replay WAL positions, clamped to zero; omitted when either is absent. | 60 s |
| `postgres.cluster.members` | gauge | `""` | — | `3` | Members listed in the leader response, on the cluster resource. | 60 s; leader only |
| `postgres.cluster.members_running` | gauge | `""` | — | `3` | Members in running or streaming state, on the cluster resource. | 60 s; leader only |
| `postgres.cluster.sync_standbys` | gauge | `""` | — | `1` | Members with sync_standby or quorum_standby role, on the cluster resource. | 60 s; leader only |
| `postgres.cluster.leader` | state string | `""` | — | `db-a` | Leader member name on the cluster resource; omitted if absent. | 60 s; leader only |
| `patroni.available` | state boolean | `""` | collection | `true` | Whether this collection completed; collection is one of health. | 60 s |
| `ingestor.collector.ok` | state boolean | `""` | collector | `true` | Runner result for the scheduled collector name. | 60 s |
| `ingestor.collector.failed` | event | `""` | collector | `error=collection failed` | One sanitized failure event per failed collection; the runner retries on the next tick. | on failure |

## Limits and failures

Each collection has a 10-second deadline, also bounded by the runner deadline. Responses or command stdout are limited to 1 MiB. HTTP collectors do not follow redirects or use environment proxies. Password files are limited to 4 KiB and CA files to 1 MiB; only regular files are accepted. Credentials are reread each collection so file rotation takes effect without a restart.

An unreachable endpoint, failed authentication, invalid TLS, missing command or file, timeout, oversized response, or malformed required field sets availability false and produces one `ingestor.collector.failed` event for that collection group. Other groups remain independent. Valid observations collected before a later request fails can still be emitted; check the matching availability state. Historical values remain in Pulse after a failure or when an optional field disappears.

Start troubleshooting with the endpoint or command, account permissions, certificate hostname, and file permissions. Verify locally with `pulse-linux --config /etc/pulse/pulse-linux.toml --local --pretty once`; this performs all configured probes. See [Linux ingestor](/en/ingestor-linux) for installation and operation.
