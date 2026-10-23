---
title: SQL query examples
navTitle: Dashboards
section: Reference
order: 220
description: Example SQL queries for Pulse ingestor resources and metric series.
tags: [dashboards, query, examples]
updated: 2026-10-23
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
