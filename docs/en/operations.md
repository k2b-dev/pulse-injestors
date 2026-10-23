---
title: Operations
navTitle: Operations
section: Operate
order: 50
description: Run, inspect, update, reschedule, and remove installed Pulse ingestors.
tags: [operations]
updated: 2026-10-23
---

# Operations

Native systemd services and macOS LaunchAgents execute `run`. Collection queues telemetry every interval while a separate delivery loop sends the oldest queued parts. A slow or paused Pulse does not delay collection. cron remains a fallback for `once`.

## Run a manual collection

Linux:

```sh
sudo pulse-linux --config /etc/pulse/pulse-linux.toml --local --pretty once
```

macOS:

```sh
pulse-macos \
  --config "$HOME/Library/Application Support/Pulse/pulse-macos.toml" \
  --local --pretty once
```

Use the binary and per-binary config path for your installation. Local mode does not open the spool or send data.

For an authenticated manual check on a root-run Linux ingestor, stop its service first:

```sh
sudo systemctl stop pulse-linux.service
sudo pulse-linux --config /etc/pulse/pulse-linux.toml once
sudo systemctl start pulse-linux.service
```

Run the final start command even if `once` fails. `once` collects all enabled collectors within the cycle budget, queues data, and attempts delivery with bounded retries. Persistent undelivered parts stay queued. Do not run a second sender against a service-owned spool. For DynamicUser services such as `pulse-uptime`, verify delivery through service logs rather than running as root against its state directory.

## Inspect services and logs

### systemd

```sh
systemctl status pulse-linux.service
journalctl -u pulse-linux.service -n 100
```

Replace `pulse-linux` with the installed binary name. Reinstalling removes the old `.timer` unit.

The service uses `Type=notify` with readiness notifications and a fixed `WatchdogSec=120s`. A separate loop pings the watchdog while the time since the last completed collection stays within 2 × interval + maximum cycle budget + 60 seconds (the cycle budget is at least one interval and includes the sum of configured collector timeout budgets). If collection stalls beyond that limit, pings stop and systemd restarts the service after the watchdog timeout. `Restart=always` also restarts an exited process. The installer also sets:

- `NoNewPrivileges=yes` and `MemoryMax=256M`;
- `StateDirectory=pulse/<binary>` with mode `0700`;
- `ProtectSystem=full` for root ingestors;
- `DynamicUser=yes` and `ProtectSystem=strict` for `pulse-uptime`.

### cron

The installer writes `/etc/cron.d/<binary-name>` and runs `once` at the configured schedule. cron requires an interval it can represent, in whole minutes. Each invocation starts fresh: collector cadences and in-memory metric blocks do not carry over. Inspect system cron logs for execution output.

```sh
sudo cat /etc/cron.d/pulse-linux
```

### launchd

The per-user LaunchAgent runs `run` with `KeepAlive` and restarts an exited process. It no longer uses `StartInterval` to launch `once`.

```sh
launchctl print "gui/$(id -u)/dev.pulse.pulse-macos"
tail -n 100 "$HOME/Library/Logs/Pulse/pulse-macos.err.log"
```

## Update

Re-run the installer with the same ingestor. Existing configuration is preserved.

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --ingestor=linux
```

Use `--version=vX.Y.Z` to install a specific release. The installer refuses an invalid or unverifiable release.

## Change configuration

Edit the per-binary TOML file, inspect a local collection, then restart the service. systemd copies the config into a credential at service start, so edits require `systemctl restart <binary>.service` to take effect, including interval changes:

```sh
sudo pulse-linux --config /etc/pulse/pulse-linux.toml --local --pretty once
sudo systemctl restart pulse-linux.service
journalctl -u pulse-linux.service -n 100
```

On macOS, restart the loaded agent after editing its config:

```sh
launchctl kickstart -k "gui/$(id -u)/dev.pulse.pulse-macos"
```

Use `--reconfigure` only when the installer should replace the existing config from prompts, variables, or `--config-source`.

## Remove an ingestor

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --uninstall --ingestor=linux
```

This removes the managed binary and scheduler. The config, token file, spool, and data already in Pulse remain.

## Operate Docker

From the Compose directory:

```sh
docker compose ps
docker compose logs -f pulse-docker
docker compose pull
docker compose up -d
```

See [Docker ingestor](/en/ingestor-docker) for the complete deployment.

## Detect a silent host

Set the source's **Expected data** (`expectEverySeconds`) in Pulse to match the expected arrival cadence, allowing for normal collection and delivery delay. Pulse can then detect a source that stops sending. For each ingestor, follow `ingestor.heartbeat` with `ingestor=<binary>` on the host resource, or on `uptime-probe` for `pulse-uptime`. If several ingestors share a source, another active ingestor can keep that source active.

State timestamps show the last change or six-hour refresh, not the last check. Use heartbeat metrics for liveness and [runner signals](/en/modules#runner-signals) for queue and delivery health.
