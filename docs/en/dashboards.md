---
title: SQL query examples
navTitle: Dashboards
section: Reference
order: 220
description: Example SQL queries for Pulse ingestor resources and metric series.
tags: [dashboards, query, examples]
updated: 2026-10-29
---

# SQL query examples

Pulse uses SQL. The Dashboard DSL has been removed; SQL dashboards are planned in Pulse.

Ingestors use stable resource keys for hosts, containers, VMs, filesystems, and endpoints. Dimensions identify bounded categories such as interface, environment, or Compose service. Query these identities instead of runtime container IDs or changing addresses.

Pulse's `samples` read view has `series_id`, `ts`, and `value`. Join `series` on `series_id` for `metric`, `unit`, `resource_key`, and `dimensions`. The `states` and `events` read views expose current facts and historical occurrences.

These examples have not been tested against a Pulse base. Adjust the time range and resource keys, then verify the queries against your target Pulse version. Optional collectors may have no data.

## CPU observations for one host

```sql
SELECT s.ts, s.value, r.unit, r.resource_key, r.dimensions
FROM samples AS s
JOIN series AS r ON r.series_id = s.series_id
WHERE r.metric = 'system.cpu.usage'
  AND r.resource_key = 'host:server-01'
  AND s.ts >= '2026-10-23 00:00:00'
  AND s.ts < '2026-10-24 00:00:00'
ORDER BY s.ts
LIMIT 1000
```

## Maximum filesystem usage by resource

```sql
SELECT r.resource_key, r.unit, max(s.value) AS max_usage
FROM samples AS s
JOIN series AS r ON r.series_id = s.series_id
WHERE r.metric = 'system.filesystem.usage'
  AND s.ts >= '2026-10-23 00:00:00'
  AND s.ts < '2026-10-24 00:00:00'
GROUP BY r.resource_key, r.unit
ORDER BY max_usage DESC
```

## Network counter rates

Pulse provides `rate(metric=..., from=..., to=...)` and `increase(...)` table functions for counters. For example:

```sql
SELECT *
FROM rate(
  metric = 'system.network.rx',
  from = '2026-10-23 00:00:00',
  to = '2026-10-24 00:00:00'
)
LIMIT 1000
```

Calculate rates or increases for each series before grouping by resource or Compose service. Summing cumulative container counters first produces misleading results when containers restart or membership changes.


## Host overview

Latest CPU and memory usage with the last host heartbeat. Untested against a live base; a NULL metric means no recent sample, not zero use. Hosts with no samples in this window are absent, so compare with your enrollment list.

```sql
SELECT r.resource_key,
       argMaxIf(toNullable(s.value), s.ts, r.metric = 'system.cpu.usage') AS cpu_percent,
       argMaxIf(toNullable(s.value), s.ts, r.metric = 'system.memory.usage') AS memory_percent,
       maxIf(toNullable(s.ts), r.metric = 'ingestor.heartbeat') AS last_heartbeat
FROM samples s JOIN series r ON r.series_id = s.series_id
WHERE r.resource_type = 'host'
  AND r.metric IN ('system.cpu.usage', 'system.memory.usage', 'ingestor.heartbeat')
  AND s.ts >= now() - INTERVAL 10 MINUTE AND s.ts < now()
GROUP BY r.resource_key
ORDER BY r.resource_key
```

This overview combines host ingestors. Use the [rollout heartbeat query](/en/rollout#roll-out-in-stages-and-verify) to distinguish individual binaries on one host.

## Filesystem capacity trend

Hourly average used bytes and minimum available bytes over the last day. Untested; each resource and mount remains separate, including shared APFS volumes whose totals must not be summed. For time-to-full estimation, see [alert SQL](/en/alerts#disk-full-in-less-than-seven-days).

```sql
SELECT r.resource_key, r.dimensions, toStartOfHour(s.ts) AS hour,
       avgIf(toNullable(s.value), r.metric = 'system.filesystem.used') AS used_bytes,
       minIf(toNullable(s.value), r.metric = 'system.filesystem.available') AS available_bytes
FROM samples s JOIN series r ON r.series_id = s.series_id
WHERE r.metric IN ('system.filesystem.used', 'system.filesystem.available')
  AND s.ts >= now() - INTERVAL 24 HOUR AND s.ts < now()
GROUP BY r.resource_key, r.dimensions, hour
ORDER BY r.resource_key, hour
```

## Backup freshness table

Latest reported backup age in hours, including PVE copies per storage and PBS groups per datastore/namespace. Untested. Age is measured at `observed_at`; a missing row or stale observation is not a successful backup. Marker availability, PVE guest coverage, and PBS group completeness must be checked separately.

```sql
SELECT r.resource_key, r.metric, r.dimensions,
       argMax(s.value, s.ts) / 3600 AS age_hours, max(s.ts) AS observed_at
FROM samples s JOIN series r ON r.series_id = s.series_id
WHERE r.metric IN ('backup.last_success.age', 'proxmox.backup.last_success.age',
                   'pbs.group.last_snapshot.age')
  AND s.ts >= now() - INTERVAL 30 MINUTE AND s.ts < now()
GROUP BY r.resource_key, r.metric, r.dimensions
ORDER BY age_hours DESC
```

## Series budget by metric

Compare observed cardinality with the default 10,000 series per metric and 50,000 per base. Untested. This counts registered series, including series without recent samples; changing resource identities or dimensions can increase it.

```sql
SELECT metric, count() AS series_count
FROM series
GROUP BY metric
ORDER BY series_count DESC
```

Use `SELECT count() AS total_series FROM series` for the base total. See [fleet sizing](/en/rollout#size-the-base) for values/minute and enrollment bursts.

## ICMP packet loss and latency

Use `uptime.icmp.loss` (percent) and `uptime.icmp.rtt.median` (milliseconds) in separate panels. ICMP no longer emits `uptime.check.duration`. The [SmokePing replacement guide](/en/ingestor-uptime#replacing-smokeping) provides per-target time series and a five-minute percentile query. Those percentiles describe round medians; missing RTTs during total loss are not zero latency.
