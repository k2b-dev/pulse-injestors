---
title: Alert SQL templates
navTitle: Alerts
section: Operate
order: 60
description: Threshold rules for host capacity, backups, services, uptime, and ingestor delivery.
tags: [alerts, sql, operations]
updated: 2026-10-23
---

# Alert SQL templates

Start with source **Expected data** from [Fleet rollout](/en/rollout), then add rules for the failures that need action. These 20 templates are a menu, not a required set. The default is **20 rules per base**. Start with eight: disk usage, memory, failed systemd units, certificates, backup markers, uptime failed, uptime error, and spool age. Add platform/service rules only where those collectors run; reserve room for splitting large fleets.

Each rule must return a string `key` and finite numeric `value`, with **at most 100 distinct keys**. Before saving, run the SQL, inspect every key and the row count, and narrow by resource or a bounded fleet dimension if necessary. Split groups into separate rules when needed; do not add `LIMIT 100` and silently leave resources unmonitored. Keys include dimensions where multiple filesystems, storage copies, disks, or ingestors share a resource. The comparisons are strict `above` or `below`; one rule can carry warning and critical thresholds. All durations and intervals below are seconds, and intervals must be at least 300.

**Verification:** signal names match the module reference. On 2026-10-23 every template below ran without SQL errors against a development Pulse built from Cloud main, with real `pulse-linux` data in a test base. Templates for services that were not present (Proxmox, PBS, Ceph, Patroni, NATS, Valkey, ClickHouse, uptime) were checked for valid columns only, so check their results once real data arrives. The state templates use `states(state_key, variant_key, value, dimensions, resource_key, source_id, updated_at)`; `value` is JSON text (`true`, `false`, or a quoted string), and `dimensions` is a JSON string.

States are current values, not history. Their timestamps record changes or refreshes, so the state queries allow seven hours for the six-hour refresh cadence. Do not infer incident duration from `updated_at`; use `minDurationSeconds`. Missing or stale input is not healthy: inspect source freshness, collector availability, and rule evaluation errors alongside threshold alerts. For state-only failures, a stale last value can remain until the freshness cutoff.

The rolling windows use `now()` so a saved alert continues to move forward. Manual SQL can use bound `:from` and `:to`, with `:base_tz` supplied by Pulse, but saved literal time parameters do not automatically advance. Copy one SQL block into a file and save it with the settings listed below, for example:

```sh
cld --profile fleet-admin pulse alerts save --base Base01 --name 'Filesystem usage' \
  --sql-file filesystem.sql --comparison above --warn 85 --critical 95 \
  --for 600 --every 300
```

`--for` sets `minDurationSeconds`; `--every` sets `intervalSeconds`. The API supports the same rule fields. Verify the clear/recovery result as well as the failure result before relying on a rule.

## Disk usage per filesystem

Above: warn **85**, critical **95** (%). Duration **600**, interval **300**. **States: no.**

```sql
SELECT concat(r.resource_key, '|', r.dimensions) AS key,
       argMax(s.value, s.ts) AS value
FROM samples s JOIN series r ON r.series_id = s.series_id
WHERE r.metric = 'system.filesystem.usage'
  AND s.ts >= now() - INTERVAL 10 MINUTE AND s.ts < now()
GROUP BY r.resource_key, r.dimensions
```

## Disk full in less than seven days

Below: warn **7**, critical **1** (days). Duration **1800**, interval **900**. **States: no.**

```sql
WITH growth AS (
  SELECT r.resource_key, r.dimensions,
         simpleLinearRegression(
           dateDiff('second', now() - INTERVAL 24 HOUR, s.ts), s.value).1 AS bytes_per_second
  FROM samples s JOIN series r ON r.series_id = s.series_id
  WHERE r.metric = 'system.filesystem.used'
    AND s.ts >= now() - INTERVAL 24 HOUR AND s.ts < now()
  GROUP BY r.resource_key, r.dimensions
  HAVING count() >= 12 AND min(s.ts) <= now() - INTERVAL 23 HOUR
     AND max(s.ts) >= now() - INTERVAL 10 MINUTE
), capacity AS (
  SELECT r.resource_key, r.dimensions, argMax(s.value, s.ts) AS available
  FROM samples s JOIN series r ON r.series_id = s.series_id
  WHERE r.metric = 'system.filesystem.available'
    AND s.ts >= now() - INTERVAL 10 MINUTE AND s.ts < now()
  GROUP BY r.resource_key, r.dimensions
)
SELECT concat(g.resource_key, '|', g.dimensions) AS key,
       greatest(0, c.available) / nullIf(g.bytes_per_second, 0) / 86400 AS value
FROM growth g JOIN capacity c
  ON g.resource_key = c.resource_key AND g.dimensions = c.dimensions
WHERE g.bytes_per_second > 0 AND isFinite(value)
```

This fits a straight line to 24 hours of used bytes and divides current available bytes by that growth rate. Flat/shrinking disks and insufficient history produce no prediction. Deletions, filesystem resizing, reserved space, shared APFS capacity, and bursty workloads limit the forecast; keep the direct usage rule too.

## Memory available below ten percent

Below: warn **10**, critical **5** (% available). Duration **600**, interval **300**. **States: no.**

```sql
SELECT concat(r.resource_key, '|', r.dimensions) AS key,
       100 - argMax(s.value, s.ts) AS value
FROM samples s JOIN series r ON r.series_id = s.series_id
WHERE r.metric = 'system.memory.usage'
  AND s.ts >= now() - INTERVAL 10 MINUTE AND s.ts < now()
GROUP BY r.resource_key, r.dimensions
```

The shared usage metric is used/total × 100, with used = total − available. This also works on Macs where total memory is collected daily; “available” there is an OS estimate.

## Failed systemd units

Above: critical **0** (units). Duration **300**, interval **300**. **States: no.**

```sql
SELECT concat(r.resource_key, '|', r.dimensions) AS key,
       argMax(s.value, s.ts) AS value
FROM samples s JOIN series r ON r.series_id = s.series_id
WHERE r.metric = 'system.systemd.failed_units'
  AND s.ts >= now() - INTERVAL 10 MINUTE AND s.ts < now()
GROUP BY r.resource_key, r.dimensions
```

## OOM kills in fifteen minutes

Above: critical **0** (kills). Duration **0**, interval **300**. **States: no.**

```sql
SELECT concat(resource_key, '|', dimensions) AS key, sum(increase) AS value
FROM increase(metric = 'system.oom_kills',
              from = now() - INTERVAL 15 MINUTE, to = now())
WHERE seconds > 0
GROUP BY resource_key, dimensions
```

The counter helper handles resets. It needs observations across time; a first sample alone cannot establish an increase.

## Security updates pending for three days

Above: warn **0** (updates). Duration **259200**, interval **3600**. **States: no.**

```sql
SELECT concat(r.resource_key, '|', r.dimensions) AS key,
       argMax(s.value, s.ts) AS value
FROM samples s JOIN series r ON r.series_id = s.series_id
WHERE r.metric = 'system.packages.security_updates'
  AND s.ts >= now() - INTERVAL 2 HOUR AND s.ts < now()
GROUP BY r.resource_key, r.dimensions
```

Use this instead of calculating reboot age from `system.reboot_required.updated_at`: the current state refreshes and does not preserve the time a reboot first became necessary. The duration starts when the alert first observes a breach, not when a package became available. Keep OS package metadata fresh; collector failures and interrupted evaluations cannot prove three days of continuous observation.

## Certificate expires within 21 or 7 days

Below: warn **21**, critical **7** (days). Duration **0**, interval **3600**. **States: no.**

```sql
SELECT concat(r.resource_key, '|', r.dimensions) AS key,
       argMax(s.value, s.ts) / 86400 AS value
FROM samples s JOIN series r ON r.series_id = s.series_id
WHERE r.metric = 'system.certificate.expires_in'
  AND s.ts >= now() - INTERVAL 2 HOUR AND s.ts < now()
GROUP BY r.resource_key, r.dimensions
```

For remote TLS/HTTPS certificates, use the same query with `uptime.tls.certificate.expires_in`; that is an additional rule unless you deliberately combine the metrics with distinct keys. Expired certificates produce negative days. Missing files or failed checks need availability/error monitoring.

## Backup marker older than 26 hours

Above: critical **93600** (seconds). Duration **0**, interval **900**. **States: no.**

```sql
SELECT concat(r.resource_key, '|', r.dimensions) AS key,
       argMax(s.value, s.ts) AS value
FROM samples s JOIN series r ON r.series_id = s.series_id
WHERE r.metric = 'backup.last_success.age'
  AND s.ts >= now() - INTERVAL 30 MINUTE AND s.ts < now()
GROUP BY r.resource_key, r.dimensions
```

Configure markers in `[[backup]]`. Missing markers emit `backup.marker.available=false` and no age; this metric-only rule cannot detect a backup that has never succeeded. Check that state separately during enrollment and in ongoing availability monitoring.

## Proxmox guest backup older than 26 hours

Above: critical **93600** (seconds). Duration **0**, interval **900**. **States: yes; verify schema/encoding.**

```sql
WITH covered AS (
  SELECT resource_key, argMax(value, updated_at) AS covered_value
  FROM states
  WHERE state_key = 'proxmox.backup.guest.covered'
    AND updated_at >= now() - INTERVAL 7 HOUR
  GROUP BY resource_key
)
SELECT concat(r.resource_key, '|', r.dimensions) AS key,
       argMax(s.value, s.ts) AS value
FROM samples s JOIN series r ON r.series_id = s.series_id
JOIN covered c ON c.resource_key = r.resource_key
WHERE r.metric = 'proxmox.backup.last_success.age'
  AND c.covered_value = 'true'
  AND s.ts >= now() - INTERVAL 30 MINUTE AND s.ts < now()
GROUP BY r.resource_key, r.dimensions
```

Each storage copy remains separate. False coverage is excluded; unknown coverage is also excluded and must be investigated. Coverage means an enabled job selects the guest, not that every listed storage is a required destination. Filter the `storage` dimension to your backup policy. No age is emitted when a backup has never existed: inspect `proxmox.backup.last_success.time` (empty for a successfully queried storage with no backup) separately. Failed storage queries are also not fresh backups.

## PBS job result is error

Above: critical **0** (1 = error). Duration **0**, interval **900**. **States: yes; verify schema/encoding.**

```sql
SELECT concat(resource_key, '|', dimensions) AS key,
       toFloat64(argMax(value, updated_at) = '"error"') AS value
FROM states
WHERE state_key = 'pbs.job.last_run.result'
  AND updated_at >= now() - INTERVAL 7 HOUR
GROUP BY resource_key, dimensions
```

Warnings and unknown results are distinct from errors. Filter intentionally disabled/retired jobs out of the rule scope.

## Ceph health is not OK

Above: critical **0** (1 = not OK). Duration **300**, interval **300**. **States: yes; verify schema/encoding.**

```sql
SELECT concat(resource_key, '|', dimensions) AS key,
       toFloat64(argMax(value, updated_at) != '"HEALTH_OK"') AS value
FROM states
WHERE state_key = 'system.ceph.health.status'
  AND updated_at >= now() - INTERVAL 7 HOUR
GROUP BY resource_key, dimensions
```

Check `system.ceph.available` too; a missing current health state is not a healthy cluster.

## Patroni has no fresh leader telemetry

Above: critical **0** (1 = absent). Duration **300**, interval **300**. **States: no.**

```sql
SELECT 'postgres-cluster:database' AS key, toFloat64(count() = 0) AS value
FROM samples s JOIN series r ON r.series_id = s.series_id
WHERE r.metric = 'postgres.cluster.members'
  AND r.resource_key = 'postgres-cluster:database'
  AND s.ts >= now() - INTERVAL 5 MINUTE AND s.ts < now()
```

Replace `postgres-cluster:database` with an expected cluster. Only the leader emits cluster metrics, so their absence detects loss of leader reporting even if replica heartbeats continue. This cannot distinguish no leader from failed collection/delivery. A stale `postgres.cluster.leader` state cannot establish current leadership. Use one rule per cluster or an explicit expected-cluster table; discovering only currently reporting clusters would miss a total outage.

## Patroni replication lag

Above: warn **67108864**, critical **268435456** (64/256 MiB). Duration **600**, interval **300**. **States: no.**

```sql
SELECT concat(r.resource_key, '|', r.dimensions) AS key,
       argMax(s.value, s.ts) AS value
FROM samples s JOIN series r ON r.series_id = s.series_id
WHERE r.metric = 'postgres.replication.lag'
  AND s.ts >= now() - INTERVAL 10 MINUTE AND s.ts < now()
GROUP BY r.resource_key, r.dimensions
```

Tune byte thresholds to WAL volume and recovery objectives. Unknown lag is omitted, not zero; inspect `postgres.replication.lag_known` and `patroni.available` alongside this rule.

## NATS unhealthy

Above: critical **0** (1 = unhealthy). Duration **300**, interval **300**. **States: yes; verify schema/encoding.**

```sql
SELECT concat(resource_key, '|', dimensions) AS key,
       toFloat64(argMax(value, updated_at) = 'false') AS value
FROM states
WHERE state_key = 'nats.healthy'
  AND updated_at >= now() - INTERVAL 7 HOUR
GROUP BY resource_key, dimensions
```

The collector marks a failed health request unhealthy. Collector availability still matters for other NATS collection groups.

## Valkey persistence is not OK

Above: critical **0** (1 = persistence failure). Duration **300**, interval **300**. **States: yes; verify schema/encoding.**

```sql
SELECT concat(resource_key, '|', dimensions) AS key,
       toFloat64(argMax(value, updated_at) = 'false') AS value
FROM states
WHERE state_key = 'valkey.persistence.ok'
  AND updated_at >= now() - INTERVAL 7 HOUR
GROUP BY resource_key, dimensions
```

This covers the last RDB save and, when enabled, the last AOF write. Missing or failed INFO collection needs the `valkey.available` check.

## ClickHouse disk free below fifteen percent

Below: warn **15**, critical **5** (% free). Duration **600**, interval **300**. **States: no.**

```sql
SELECT concat(r.resource_key, '|', r.dimensions) AS key,
       100 * argMaxIf(s.value, s.ts, r.metric = 'clickhouse.disk.free') /
         nullIf(argMaxIf(s.value, s.ts, r.metric = 'clickhouse.disk.total'), 0) AS value
FROM samples s JOIN series r ON r.series_id = s.series_id
WHERE r.metric IN ('clickhouse.disk.free', 'clickhouse.disk.total')
  AND s.ts >= now() - INTERVAL 10 MINUTE AND s.ts < now()
GROUP BY r.resource_key, r.dimensions
HAVING countIf(r.metric = 'clickhouse.disk.free') > 0
   AND countIf(r.metric = 'clickhouse.disk.total') > 0 AND isFinite(value)
```

The `disk` dimension keeps each disk separate. A zero/missing total does not yield a finite percentage and is excluded.

## Uptime probe finding: failed

Above: critical **0** (1 = failed). Duration **300**, interval **300**. **States: yes; verify schema/encoding.**

```sql
SELECT concat(resource_key, '|', dimensions) AS key,
       toFloat64(argMax(value, updated_at) = '"failed"') AS value
FROM states
WHERE state_key = 'uptime.check.result'
  AND updated_at >= now() - INTERVAL 7 HOUR
GROUP BY resource_key, dimensions
```

A failed check is a confirmed finding, such as an unexpected port or an HTTP status mismatch. For daily port scans, replace the seven-hour state window with 26 hours and use an interval of 3600; filter by `check_type` so minute checks keep the shorter window.

## Uptime probe could not complete: error

Above: warn **0** (1 = error). Duration **300**, interval **300**. **States: yes; verify schema/encoding.**

```sql
SELECT concat(resource_key, '|', dimensions) AS key,
       toFloat64(argMax(value, updated_at) = '"error"') AS value
FROM states
WHERE state_key = 'uptime.check.result'
  AND updated_at >= now() - INTERVAL 7 HOUR
GROUP BY resource_key, dimensions
```

Keep this separate from the failed rule. Errors emit no success sample; averaging `uptime.check.success` would otherwise hide them. Apply the same cadence-specific window adjustment as above for daily checks. DNSBL overall and per-list results have different dimensions.

## Ingestor spool oldest age exceeds fifteen minutes

Above: warn **900**, critical **3600** (seconds). Duration **300**, interval **300**. **States: no.**

```sql
SELECT concat(r.resource_key, '|', r.dimensions) AS key,
       argMax(s.value, s.ts) AS value
FROM samples s JOIN series r ON r.series_id = s.series_id
WHERE r.metric = 'ingestor.spool.oldest_age'
  AND s.ts >= now() - INTERVAL 10 MINUTE AND s.ts < now()
GROUP BY r.resource_key, r.dimensions
```

The ingestor dimension separates binaries sharing a host. During a complete delivery outage this metric cannot reach Pulse; Expected data and local logs are the immediate detectors.

## Ingestor delivery failures increasing

Above: warn **0** (attempts in 15 minutes). Duration **0**, interval **300**. **States: no.**

```sql
SELECT concat(resource_key, '|', dimensions) AS key, sum(increase) AS value
FROM increase(metric = 'ingestor.delivery.failures',
              from = now() - INTERVAL 15 MINUTE, to = now())
WHERE seconds > 0
GROUP BY resource_key, dimensions
```

The process counter resets on restart. Failures include rejected attempts, not only outages. Inspect logs and `ingestor.delivery.rejected`; use Expected data if delivery stops entirely.
