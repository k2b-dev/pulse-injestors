---
title: Kernel, boot, and OOM module
navTitle: Kernel and boot
section: Modules
order: 351
description: Detect reboots, OOM kills, and recent kernel errors.
tags: [kernel, linux, modules, reference]
updated: 2026-10-23
---

# Kernel, boot, and OOM module

The `kernel` collector checks the host every 60 seconds. Override `kernel` in `[runner.cadence]`. It reads `/proc/vmstat`, `/proc/stat`, `/proc/uptime`, and the local kernel journal. Every signal uses the host resource; no dimensions are added.

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.oom_kills` | counter | — | — | `3` | Raw cumulative `oom_kill` count; use Pulse `increase()` for a time range. | 60 s |
| `system.oom.available` | state boolean | — | — | `true` | Whether vmstat exposes a readable OOM counter. | 60 s; change / 6 h |
| `system.oom_kill` | event | — | — | `count=2` | Counter increased since the previous successful collection in this process; attribute count is the increase. No event on the initial sample or counter reset. | on increase at 60 s |
| `system.boot_time` | state string | — | — | `2026-10-23T08:00:00Z` | Boot time from procfs btime, RFC3339 UTC. | 60 s; change / 6 h |
| `system.boot` | event | — | — | timestamp `2026-10-23T08:00:00Z` | Emitted once per process when the first valid uptime check is below 15 minutes; event timestamp is boot time. | once per process if recent boot |
| `system.kernel.errors` | gauge | — | — | `8` | Number of kernel journal lines at priorities 0–3 since the cursor or time boundary. | 60 s |
| `system.kernel.error` | event | — | — | `count=8`, `sample=…` | One event for a nonempty journal result; count plus first five lines, each limited to 300 characters. | on nonempty result at 60 s |
| `system.kernel.journal.available` | state boolean | — | — | `true` | Whether the local journal query succeeded. | 60 s; change / 6 h |

## Journal access and restart behavior

The collector runs `journalctl -k -p 0..3 --no-pager -q -o short-iso`. With `pulse.spool_dir`, it adds `--cursor-file=<spool_dir>/kernel.cursor`; the first query without a cursor adds `--since=-10min`. The cursor survives restarts. The directory must be writable by the ingestor.

Without a spool directory, the first query covers ten minutes and later queries use the previous successful check time with `--since`. This time boundary is kept only in memory, so restarting can repeat recent errors. `once` also collects the journal and can advance a configured cursor.

The journal probe has a ten-second timeout and an 8 MiB output limit, bounded further by the runner deadline. Missing journalctl emits availability false without a failure event. Unexpected command, permission, or parsing errors are reported by the runner. Journal permissions must allow reading kernel messages; an empty result alone cannot prove that all messages were accessible. Boot and OOM collection remain independent of journal availability.
