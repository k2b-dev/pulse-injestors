---
title: Configuration
navTitle: Configuration
section: Operate
order: 40
description: Configure Pulse credentials, resource identity, deployment dimensions, retries, and collector settings.
tags: [config, reference]
updated: 2026-07-23
---

# Configuration

The installer creates a minimal TOML config and protects it with mode `0600`.

| Platform | Default path |
|---|---|
| Linux, Proxmox VE, PBS | `/etc/pulse/ingestor.toml` |
| macOS | `~/Library/Application Support/Pulse/ingestor.toml` |
| Docker Compose | `./pulse-docker.toml`, mounted read-only |

## Minimal config

```toml
[pulse]
ingest_url = "https://pulse.example.com/api/pulse/ingest"
ingest_token = "replace-me"

[entity]
id = "server-01"
label = "Server 01"

[dimensions]
environment = "production"
region = "eu-central"

[runner]
interval_seconds = 60
collector_timeout_seconds = 60

[http]
timeout_seconds = 10
max_retries = 3
initial_backoff_ms = 500
```

`entity.id` must remain stable across reinstalls, reboots, container recreation, and host renaming when historical continuity matters. `entity.label` is the human-readable name shown in Pulse.

Use global dimensions only for bounded deployment labels such as `environment`, `region`, `site`, or `role`. Do not put runtime IDs, paths, IP addresses, image digests, or request IDs in global dimensions.

## Credentials

The config contains a bearer token. Keep it readable only by the account that runs the ingestor:

```sh
sudo chmod 600 /etc/pulse/ingestor.toml
```

For unattended enrollment, prefer `--config-source` or `PULSE_INGEST_TOKEN_FILE`. Do not place the raw token in a command-line argument.

## Configuration precedence

When running a binary directly, values are resolved in this order:

1. built-in defaults;
2. TOML config;
3. environment variables;
4. CLI flags.

All binaries accept `--config`. Without it, Linux binaries try `/etc/pulse/ingestor.toml`. The macOS installer passes its per-user config path through launchd.

## Environment variables

| Variable | Purpose |
|---|---|
| `PULSE_INGEST_URL` | Complete Pulse source ingest URL. |
| `PULSE_INGEST_TOKEN` | Bearer token. Prefer a protected config or installer token file. |
| `PULSE_ENTITY_ID` | Stable system identifier. |
| `PULSE_ENTITY_LABEL` | Human-readable system label. |
| `PULSE_DIMENSIONS` | Comma-separated global `key=value` dimensions. |
| `PULSE_INTERVAL_SECONDS` | Interval used by `run` mode and the Docker scheduler. |
| `PULSE_COLLECTOR_TIMEOUT_SECONDS` | Overall timeout per collector. |
| `PULSE_HTTP_TIMEOUT_SECONDS` | HTTP request timeout. |
| `PULSE_HTTP_MAX_RETRIES` | Retry count for network, 408, 429, and 5xx failures. |
| `PULSE_HTTP_INITIAL_BACKOFF_MS` | Initial retry backoff. |
| `PULSE_UPTIME_DISABLE_DEFAULTS` | Disable built-in internet targets for `pulse-uptime`. |
| `PULSE_UPTIME_CONCURRENCY` | Maximum concurrent uptime checks. |
| `PULSE_UPTIME_TIMEOUT_SECONDS` | Default timeout per uptime target. |

The installer additionally accepts `PULSE_INGEST_TOKEN_FILE` and reads the token before writing the protected config.

## Optional collectors

Supported optional collectors are enabled by default. Expected absence produces an availability state and does not stop unrelated telemetry.

Disable collectors explicitly when the local system should not be queried:

```toml
[host]
enable_thermal = false
enable_btrfs = false
enable_zfs = false
enable_ceph = false

[proxmox]
enable_ceph_api = false
enable_local_ceph = false
```

Each [ingestor guide](/en/ingestors) shows its platform-specific section. Complete example configs are maintained under `configs/` in the repository.

`pulse-uptime` targets are configured as repeated `[[uptime.target]]` sections. See [Uptime ingestor](/en/ingestor-uptime#configure-custom-targets) for all supported check types and address formats.

## Validate changes

Inspect collection without sending:

```sh
sudo pulse-linux --config /etc/pulse/ingestor.toml --local --pretty once
```

Send one batch after the local report looks correct:

```sh
sudo pulse-linux --config /etc/pulse/ingestor.toml once
```
