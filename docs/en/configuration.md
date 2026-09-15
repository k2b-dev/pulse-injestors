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
spool_dir = "/var/lib/pulse/pulse-linux"
spool_max_bytes = 67108864

[resource]
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

`resource.id` must remain stable across reinstalls, reboots, container recreation, and host renaming when historical continuity matters. `resource.label` is the human-readable name shown in Pulse.

Use global dimensions only for bounded deployment labels such as `environment`, `region`, `site`, or `role`. Do not put runtime IDs, paths, IP addresses, image digests, or request IDs in global dimensions.

## Persistent delivery

Set `pulse.spool_dir` to a private writable directory and `pulse.spool_max_bytes`
to a positive disk budget. The native installer and example configurations use
64 MiB. Allow additional filesystem space for atomic rewrites and metadata. This is a disk limit, not a guaranteed outage duration: measure your
payload size and multiply it by the number of collections to retain.

The directory must survive process and container restarts. Use the named volume
in the maintained Docker Compose file. On macOS, use an absolute path under the
account's `Library/Application Support/Pulse` directory; TOML does not expand `~`.
Leave `spool_dir` empty only when losing data after exhausted retries is acceptable.

Requests retain their original body and idempotency key across retries. Network
errors, 408, 429 and server failures use bounded retries; redirects are rejected.
Delivery resumes on the next invocation or scheduled collection. Queued data
expires after 23 hours, leaving a margin inside Pulse's 24-hour deduplication
window; expiry is reported as data loss. Your base must retain raw data for at
least the delivery window.

When the byte budget is full, existing data remains queued and the new collection
is refused with an error. Permanent payload failures are retained for inspection
and do not block later batches. Stop the sender before inspecting rejected queue
files: they can contain sensitive telemetry. Correct the producer, then remove
only the rejected files you intend to discard. Do not edit and replay a partially
accepted delivery.

A spool belongs to one endpoint and source token. Before rotating a token, drain
the old queue. If that is impossible, preserve it and configure a new directory;
old queued data cannot automatically be reassigned to another source.

`PULSE_SPOOL_DIR` and `PULSE_SPOOL_MAX_BYTES` override these settings. Removing an
ingestor does not delete its delivery data.

## Collection budget

`runner.interval_seconds` also bounds a collection cycle. Each collector is
additionally limited by `collector_timeout_seconds`. When the cycle expires,
remaining collectors are skipped and available observations can be queued for a
later delivery. Collection and replay share the cycle deadline; filesystem writes
still complete to preserve collected data. Size expensive modules and HTTP retry
budgets so the normal cycle finishes comfortably within 60 seconds.

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
| `PULSE_RESOURCE_ID` | Stable system identifier. |
| `PULSE_RESOURCE_LABEL` | Human-readable system label. |
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
