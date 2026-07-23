---
title: Local output and scripts
navTitle: Local and scripts
section: Reference
order: 210
description: Inspect an installed ingestor locally and extend collection with bounded script output.
tags: [local, scripts]
updated: 2026-07-23
---

# Local output and scripts

## Inspect collected data

`--local` collects data without sending it. JSON is intended for payload inspection:

```sh
sudo pulse-linux --config /etc/pulse/ingestor.toml --local once
```

Add `--pretty` for a readable system report:

```sh
sudo pulse-linux --config /etc/pulse/ingestor.toml --local --pretty once
```

Add `--verbose` to include collector selection and skip reasons:

```sh
sudo pulse-linux \
  --config /etc/pulse/ingestor.toml \
  --local --pretty --verbose once
```

Use the corresponding installed binary and config path on macOS, Proxmox, or PBS.

## Add a script collector

Configured scripts can add site-specific metrics, states, and events without forking the ingestor:

```toml
[[script]]
name = "site-labels"
command = ["/etc/pulse/scripts/site-labels.sh"]
timeout_seconds = 5
max_output_bytes = 1048576

[script.dimensions]
source = "local-script"
```

Scripts return Pulse JSON fragments on stdout. The runner adds missing timestamps, the configured resource, global dimensions, script dimensions, and `script=<name>`.

See the [script module reference](/en/module-script) for the complete output contract.

## Script failures

A timeout, non-zero exit, oversized output, or invalid JSON is reported without stopping other collectors. Keep script output bounded and write diagnostic logs to stderr, never into the JSON stream.
