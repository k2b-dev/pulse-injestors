---
title: Linux inventory module
navTitle: Linux inventory
section: Modules
order: 352
description: Identify the OS, kernel, hardware, and installed package versions.
tags: [inventory, linux, modules, reference]
updated: 2026-10-23
---

# Linux inventory module

The `inventory` collector reports host identity and installed software every 86400 seconds. Override `inventory` in `[runner.cadence]`. All signals use the host resource. Memory capacity remains in [System](/en/module-system) and is not repeated here.

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.os.name` | state string | — | — | `Ubuntu` | os-release NAME. | 86400 s; change / 6 h on observation |
| `system.os.version` | state string | — | — | `24.04` | os-release VERSION_ID. | 86400 s; change / 6 h on observation |
| `system.os.pretty` | state string | — | — | `Ubuntu 24.04 LTS` | os-release PRETTY_NAME. | 86400 s; change / 6 h on observation |
| `system.kernel.release` | state string | — | — | `6.8.0-60-generic` | `/proc/sys/kernel/osrelease`. | 86400 s; change / 6 h on observation |
| `system.hardware.vendor` | state string | — | — | `QEMU` | DMI sys_vendor; omitted when absent. | 86400 s; change / 6 h on observation |
| `system.hardware.model` | state string | — | — | `Standard PC` | DMI product_name; omitted when absent. | 86400 s; change / 6 h on observation |
| `system.hardware.cpu_model` | state string | — | — | `AMD EPYC` | First model name in `/proc/cpuinfo`; omitted when absent. | 86400 s; change / 6 h on observation |
| `software.package.version` | state string | — | `package`, `arch` | `0:3.0.1-2` | Installed package version, including RPM epoch. Multiple parallel versions are sorted and joined with comma-space. | 86400 s; change / 7 d |
| `software.packages.installed` | gauge | — | — | `1250` | Number of distinct installed package/architecture identities before the cap. | 86400 s |
| `software.packages.truncated` | state boolean | — | — | `false` | More than 5000 package/architecture identities were found; version states include only the first 5000 in sorted order. | 86400 s; change / 7 d |
| `software.packages.skipped` | state number | — | — | `2` | Malformed package rows skipped in this collection; emitted when nonzero. | 86400 s; change / 7 d |
| `software.packages.available` | state boolean | — | — | `true` | Whether the local package inventory command succeeded. | 86400 s; change / 7 d |

## Sources and limits

Reads `etc/os-release` under `host.root` (falling back to `usr/lib/os-release`), procfs under `host.proc_root`, and DMI under `host.sys_root`. Missing optional identity files are omitted; other read errors are reported by the runner.

Uses `rpm -qa --qf '%{NAME}\t%{ARCH}\t%{EPOCHNUM}:%{VERSION}-%{RELEASE}\n'`, falling back to `dpkg-query -W -f '${db:Status-Abbrev}\t${Package}\t${Architecture}\t${Version}\n'` when rpm is missing. Commands query the local machine's package database, with a 60-second timeout and 8 MiB output limit, bounded by the runner deadline. Missing both tools emits availability false without a failure event. Only dpkg rows with status `ii` (installed) or `hi` (held and installed) are included. Malformed rows are skipped and counted in `software.packages.skipped`; command failures are reported by the runner.

Every `software.*` state refreshes after seven days instead of six hours. Daily observations keep unchanged entries in the state cache; only identities not seen for seven days are forgotten. The first run can send up to 5000 package-version states. Restarting without a persistent spool resends the inventory. These limits are separate from Pulse's per-minute quota; budget for enrollment and weekly refresh bursts.
