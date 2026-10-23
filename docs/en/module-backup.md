---
title: Backup markers module
navTitle: Backup markers
section: Modules
order: 422
description: Check the age of the last successful backup.
tags: [backup, linux, modules, reference]
updated: 2026-10-23
---

# Backup markers module

Define one `[[backup]]` entry per backup job, with a unique name and exactly one `marker_file` or `command`. Collection runs every 900 seconds; override `backup` in `[runner.cadence]`.

```toml
[[backup]]
name = "database"
marker_file = "/var/lib/backup/database.ok"

[[backup]]
name = "offsite"
command = ["/usr/local/bin/backup-last-success", "offsite"]
```

Update the marker file only after a successful backup. Its modification time is the success time; its content is not read. Commands must print exactly one RFC3339 timestamp or integer Unix timestamp to stdout. They run as an argument vector without a shell, with a 30-second timeout and 4 KiB output limit, also subject to the runner deadline.

Each job uses resource type `backup`, ID `entity.ID(host, name)`, and the configured name as its label. No backup is run by this collector.

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `backup.last_success.age` | gauge | seconds | — | `3600` | Time since successful backup; future timestamps produce a negative age. | 900 s |
| `backup.last_success.time` | state string | — | — | `2026-10-23T08:00:00Z` | Last success as RFC3339 UTC. | 900 s; change / 6 h |
| `backup.marker.available` | state boolean | — | — | `true` | A regular marker file or valid command timestamp was available. | 900 s; change / 6 h |
| `backup.collect.failed` | event | — | — | `error=backup command must print RFC3339 or unix seconds` | Unexpected file, command, timeout, output-size, or timestamp failure on this backup resource. | on failure at 900 s |

Missing marker files emit availability false and omit age, without a failure event. Other failures remain visible as events. Command stdout/stderr is not copied into error events. The last successful value in Pulse remains historical; check availability alongside age.
