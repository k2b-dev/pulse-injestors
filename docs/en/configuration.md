---
title: Configuration
navTitle: Configuration
section: Operate
order: 40
description: Configure Pulse credentials, resource identity, deployment dimensions, retries, and collector settings.
tags: [config, reference]
updated: 2026-10-23
---

# Configuration

The installer creates a minimal TOML config and protects it with mode `0600`.

| Platform | Default path |
|---|---|
| Native Linux | `/etc/pulse/<binary>.toml`, for example `/etc/pulse/pulse-linux.toml` |
| Native macOS | `~/Library/Application Support/Pulse/<binary>.toml` |
| Docker Compose | `./pulse-docker.toml`, mounted at `/etc/pulse/pulse-docker.toml` |

## Minimal config

```toml
[pulse]
ingest_url = "https://pulse.example.com/api/pulse/ingest"
ingest_token_file = "/etc/pulse/pulse-linux.token"
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

Set `pulse.spool_dir` to a durable writable directory owned by the ingestor user with mode `0700`. The native installer and example configs use a 64 MiB `pulse.spool_max_bytes` budget. Allow extra disk space for temporary writes, metadata, and quarantine files. Size the budget from measured output and the outage you want to cover.

On Linux, the installer uses `/var/lib/pulse/<binary>`. systemd creates it with `StateDirectory=pulse/<binary>`; symlinks to DynamicUser state directories are supported. On macOS, the default is `~/Library/Application Support/Pulse/<binary>-spool`. Use an absolute path in TOML, which does not expand `~`. Docker uses the `pulse-spool` named volume.

Collection queues one file per part (`*.part`). A separate delivery loop sends parts oldest-first, preserving each body's idempotency key across retries. Collection does not wait for Pulse. Parts expire after 23 hours, inside Pulse's 24-hour idempotency window.

If the directory still contains `*.json` files from the earlier outbox format, the ingestor refuses to start and names the directory in its error. Deliver those files with the earlier version, or delete them if you accept losing that data, before starting the new version.

When queued data reaches the byte budget, the oldest parts are dropped to make room. A single collection larger than the whole budget is refused instead. `ingestor.telemetry.dropped` reports `reason=budget` or `reason=expired`; logs also report eviction and expiry. Loss cannot be recovered from the spool.

Without `spool_dir`, data stays in memory and is lost on exit. The default memory budget is 16 MiB; a positive `spool_max_bytes` overrides it. Use persistent storage for production.

Only one process can own a spool directory. Each ingestor needs its own directory, including ingestors that share a token. A spool is bound to its endpoint and source token. Before rotating a token, drain the old queue. If that is impossible, preserve the old directory and configure a new one. Old queued data is not automatically reassigned.

`PULSE_SPOOL_DIR` and `PULSE_SPOOL_MAX_BYTES` override these settings. Uninstalling does not delete the spool.

## Delivery responses

| Response | Behavior |
|---|---|
| 2xx with a valid acknowledgement | Delivered; remove the part from the queue. |
| Transport error, 408, 429, 5xx, or invalid 2xx acknowledgement | Retry with exponential backoff and `Retry-After`. |
| 3xx, 401, 403, 404, or non-idempotency 409 | Pause delivery, keep data queued, and probe slowly. Redirects are never followed. Pulse currently returns 404 for a disabled source. |
| 409 whose message names an idempotency key conflict, or other 4xx | Reject the payload and split it to isolate invalid items for quarantine. |

Backoff is capped at five minutes; a longer `Retry-After` takes precedence. In `run`, delivery continues retrying independently of collection. Repeated blocked-delivery logs are limited to one per outcome/status/error-code combination every ten minutes; paused delivery logs at ERROR level. Pulse's error code and message appear in the logs when provided.

Rejected parts are split in halves until an invalid item stands alone. The split budget is 256 per hour. The isolated item goes to `<spool_dir>/quarantine/`, which retains at most ten JSON files, and `ingestor.delivery.rejected` identifies it. When the split budget is exhausted, the part stays queued and delivery waits until the hourly window resets. A failed split is retried; it does not quarantine healthy items. Without a persistent spool, no quarantine file is kept.

Only a type or unit definition conflict blocks a metric name: new samples with that name are omitted for one hour in the running process. Other single-item rejections are quarantined and reported without blocking the name. Fix the cause shown in the rejection. The block is not persisted across restarts.

After the spool drops or rejects data, the state cache is cleared so all states observed in the next collection cycle are sent again.

## Collection budget

`runner.interval_seconds` bounds each collection cycle. `collector_timeout_seconds` also limits each collector. When the cycle deadline passes, remaining collectors are skipped; already collected observations can still be queued. Filesystem writes finish to preserve data. Keep normal collection comfortably below the interval.

`run` honors collector cadences where a collector defines one. `once` collects all enabled collectors within the cycle budget, queues the result, then delivers with at most `http.max_retries` retries. Its delivery phase has a separate deadline of one runner interval. Undelivered persistent parts stay queued for the next run; memory-only parts are lost when it exits.

## Credentials

Use `[pulse] ingest_token_file`, `--ingest-token-file`, or `PULSE_INGEST_TOKEN_FILE`. The file must be regular and contain exactly one token, with optional surrounding whitespace. Use mode `0600`; the reader also accepts group-readable files such as `0640`, but rejects any access for other users.

```sh
sudo chmod 600 /etc/pulse/pulse-linux.token
```

`PULSE_INGEST_TOKEN` still works when a secret file is unavailable. The raw `--ingest-token` CLI flag has been removed because arguments can appear in process listings. Existing TOML `ingest_token` is still accepted, but use a separate file for new configurations. Do not set both token forms in TOML or both environment token forms.

For generated native configs, the installer writes `<config path without .toml>.token` with mode `0600`. systemd loads it through `LoadCredential=pulse-ingest-token:...` and sets `PULSE_INGEST_TOKEN_FILE=%d/pulse-ingest-token`. If no token or token-file setting is supplied, the binary tries `$CREDENTIALS_DIRECTORY/pulse-ingest-token`.

## Configuration precedence

When running a binary directly, values are resolved in this order:

1. built-in defaults;
2. TOML config;
3. environment variables;
4. CLI flags.

All binaries accept `--config`. Without it, binaries try `/etc/pulse/<binary>.toml`. The macOS installer passes its per-user config path through launchd.

## Environment variables

| Variable | Purpose |
|---|---|
| `PULSE_INGEST_URL` | Complete Pulse source ingest URL. |
| `PULSE_INGEST_TOKEN_FILE` | Private file containing the source token. |
| `PULSE_INGEST_TOKEN` | Bearer token when a private file is unavailable. |
| `PULSE_SPOOL_DIR` | Private persistent spool directory. |
| `PULSE_SPOOL_MAX_BYTES` | Queue byte budget. |
| `PULSE_RESOURCE_ID` | Stable system identifier. |
| `PULSE_RESOURCE_LABEL` | Human-readable system label. |
| `PULSE_DIMENSIONS` | Comma-separated global `key=value` dimensions. |
| `PULSE_INTERVAL_SECONDS` | Collection interval in `run` mode. |
| `PULSE_COLLECTOR_TIMEOUT_SECONDS` | Overall timeout per collector. |
| `PULSE_HTTP_TIMEOUT_SECONDS` | HTTP request timeout. |
| `PULSE_HTTP_MAX_RETRIES` | Bounded retry count for `once`; `run` keeps retrying. |
| `PULSE_HTTP_INITIAL_BACKOFF_MS` | Initial retry backoff. |
| `PULSE_UPTIME_DISABLE_DEFAULTS` | Disable built-in internet targets for `pulse-uptime`. |
| `PULSE_UPTIME_CONCURRENCY` | Maximum concurrent uptime checks. |
| `PULSE_UPTIME_TIMEOUT_SECONDS` | Default timeout per uptime target. |

A token source from the environment or CLI replaces the TOML token source.

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
sudo pulse-linux --config /etc/pulse/pulse-linux.toml --local --pretty once
```

Restart the service to load the change and inspect its logs:

```sh
sudo systemctl restart pulse-linux.service
journalctl -u pulse-linux.service -n 100
```

For an authenticated manual `once` check, stop the service first so it releases the spool, then restart it after the check. See [Operations](/en/operations#run-a-manual-collection).
