---
title: Script module
navTitle: Script
section: Modules
order: 460
description: Custom script extension module for Pulse JSON fragments.
tags: [scripts, modules, reference]
updated: 2026-07-23
---

# Script module

The script module runs configured commands that emit Pulse JSON fragments.

## Resources

Scripts may emit an explicit `resource` object with `type`, `id`, and `label`. Legacy `entityType` and `entityId` fields are normalized into the same resource contract. If a signal omits a resource, the runner injects the current ingestor resource.

## Metrics

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| script-defined | script-defined | script-defined | script-defined plus global dimensions | `custom.queue.depth=12` | Any valid Pulse metric returned by the script JSON. |

## States

| Key | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|
| `script.ok` | boolean | `host`, `script` | `true` | Whether one configured script completed and returned valid JSON. |
| script-defined | script-defined | script-defined plus global dimensions | `custom.role=primary` | Any valid Pulse state returned by the script JSON. |

## Events

| Kind | Dimensions | Structured fields | Trigger |
|---|---|---|---|
| `script.failed` | `host`, `script`, `operation=execute` | `attributes.error` | Script command failed or returned invalid JSON. |
| script-defined | script-defined plus global dimensions | `value`, `attributes`, `sensitive`, `actorId`, `sessionId`, `correlationId`, `payload` | Any valid Pulse event returned by the script JSON. |

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `host` | `server-01` | injected signals | Stable host id from configuration when the script omits entity fields. |
| `script` | `custom-labels` | `script.ok`, `script.failed` | Configured script name. |
| configured dimensions | `role=primary` | script output | Static dimensions from `[script.dimensions]`. |
| `operation` | `execute` | failure events | Bounded script operation that failed. |

## Output contract

Scripts write one Pulse batch object to stdout:

```json
{
  "metrics": [],
  "states": [],
  "events": []
}
```

Use dimensions only for bounded filter and grouping values. Put irregular or high-cardinality event context in `attributes`, protected values in `sensitive`, and identities in `actorId`, `sessionId`, or `correlationId`.

## Requirements

- Executable command configured in `[[script]]`.
- JSON output matching the Pulse batch shape.

## Failure behavior

Script failures emit `script.failed` and do not stop other scripts or collectors.
