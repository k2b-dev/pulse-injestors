---
title: Packages module
navTitle: Packages
section: Modules
order: 410
description: Linux package update count collection.
tags: [packages, linux, modules, reference]
updated: 2026-07-23
---

# Packages module

The packages module reports available package updates from supported Linux package managers.

## Entity types

- `host`: package update data for the monitored host. Example entity id: `host:server-01`.

## Metrics

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `system.packages.updates.total` | gauge |  | `host` | `14` | Total updates across all available supported managers. |
| `system.packages.manager.updates` | gauge |  | `host`, `manager` | `12` | Update count reported by one package manager. |

## States

| Key | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|
| `system.packages.available` | boolean | `host` | `true` | Whether at least one supported package manager was found and collected. |
| `system.packages.manager.available` | boolean | `host`, `manager` | `true` | Whether a supported manager binary exists. |
| `system.packages.manager.updates_available` | boolean | `host`, `manager` | `true` | Whether the manager update query completed successfully. |

## Events

| Kind | Dimensions | Attributes | Trigger |
|---|---|---|---|
| `system.packages.manager.failed` | `host`, `manager`, `operation=list_updates` | `error` | A supported manager exists, but the update query failed. |

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `host` | `server-01` | all signals | Stable host id from configuration. |
| `manager` | `apt` | manager signals | Supported package manager: `apt`, `dnf`, or `pacman`. |
| `operation` | `list_updates` | failure events | Bounded package-manager operation that failed. |

## Requirements

- Supported package manager commands such as `apt`, `dnf`, or `pacman`.
- Permissions and network/cache state required by the package manager.

## Failure behavior

Missing managers emit availability states. Unexpected package manager failures emit manager-scoped events.
