---
title: Packages module
navTitle: Packages
section: Modules
order: 410
description: Linux package update count collection.
tags: [packages, linux, modules, reference]
updated: 2026-10-23
---

# Packages module

The packages module reports available package updates from supported Linux package managers.

In `pulse-linux`, this collector runs every 3600 seconds by default. Override `packages` in `[runner.cadence]`. States are sent on change and refreshed after six hours when checked again; table cadences describe collection. Other binaries retain their own schedules.

## Resource types

- `host`: package update data for the monitored host. Example resource key: `host:server-01`.

## Metrics

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.packages.updates.total` | gauge | — | — | `14` | Total updates across all available supported managers. | 3600 s |
| `system.packages.security_updates` | gauge | — | — | `3` | Available security updates: apt sources containing `-security`, or distinct DNF package names across security advisories, versions, and architectures. Omitted when neither query succeeds. | 3600 s |

## States

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.packages.available` | state boolean | — | — | `true` | Whether at least one supported package manager was found and collected. | 3600 s; change / 6 h |
| `system.packages.manager.available` | state boolean | — | `manager` | `true` | Whether a supported manager binary exists. | 3600 s; change / 6 h |
| `system.packages.manager.updates_available` | state boolean | — | `manager` | `true` | Whether the manager update query completed successfully. | 3600 s; change / 6 h |
| `system.reboot_required` | state boolean | — | — | `true` | `run/reboot-required` exists under `host.root` on Debian/Ubuntu, or RHEL-family `needs-restarting -r` returns 1 (0 means false). Missing needs-restarting omits the state. | 3600 s; change / 6 h |

## Events

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.packages.manager.failed` | event | — | `manager`, `operation=list_updates` | `error` | A supported manager exists, but the update query failed. | on trigger at 3600 s |

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `manager` | `apt` | manager signals | Supported package manager: `apt`, `dnf`, or `pacman`. |
| `operation` | `list_updates` | failure events | Bounded package-manager operation that failed. |

## Requirements

- Supported package manager commands such as `apt`, `dnf`, or `pacman`.
- Read access to local package metadata.
- A separate OS update job must keep package metadata current.

## Failure behavior

Missing managers emit availability states. Unexpected package manager failures emit manager-scoped events.

Queries use local metadata only: `apt list --upgradeable`, `dnf --cacheonly check-update --quiet`, `dnf --cacheonly -q updateinfo list --security --available`, and `pacman -Qu`. The collector does not refresh repositories. DNF exit 100 and pacman exit 1 are accepted for their update queries. Stale caches mean stale update counts; missing DNF caches remain a visible failure.

The duplicate `system.packages.manager.updates` metric is removed. Installed package versions are in [Inventory](/en/module-inventory).
