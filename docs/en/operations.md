---
title: Operations
navTitle: Operations
section: Operate
order: 50
description: Run, inspect, update, reschedule, and remove installed Pulse ingestors.
tags: [operations]
updated: 2026-07-23
---

# Operations

Native schedulers execute the ingestor with the `once` command. Every execution collects one batch, sends it, and exits.

## Run a manual collection

Linux:

```sh
sudo pulse-linux --config /etc/pulse/ingestor.toml --local --pretty once
sudo pulse-linux --config /etc/pulse/ingestor.toml once
```

macOS:

```sh
pulse-macos \
  --config "$HOME/Library/Application Support/Pulse/ingestor.toml" \
  --local --pretty once
```

Replace the binary with `pulse-proxmox`, `pulse-proxmox-backup-server`, or `pulse-uptime` as appropriate.

## Inspect scheduling and logs

### systemd

```sh
systemctl status pulse-linux.timer
systemctl list-timers pulse-linux.timer
journalctl -u pulse-linux.service -n 100
```

Replace `pulse-linux` with the installed binary name.

### cron

The installer writes `/etc/cron.d/<binary-name>`. Inspect system cron logs for execution output.

```sh
sudo cat /etc/cron.d/pulse-linux
```

### launchd

```sh
launchctl print "gui/$(id -u)/dev.pulse.pulse-macos"
tail -n 100 "$HOME/Library/Logs/Pulse/pulse-macos.err.log"
```

## Update

Re-run the installer with the same ingestor. Existing configuration is preserved.

```sh
curl -fsSL \
  https://github.com/ValentinKolb/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --ingestor=linux
```

Use `--version=vX.Y.Z` to install a specific release. The installer refuses an invalid or unverifiable release.

## Change configuration

Edit the installed TOML file and run one local collection before sending:

```sh
sudo pulse-linux --config /etc/pulse/ingestor.toml --local --pretty once
sudo pulse-linux --config /etc/pulse/ingestor.toml once
```

Use `--reconfigure` only when the installer should replace the existing config from prompts, variables, or `--config-source`.

## Remove an ingestor

```sh
curl -fsSL \
  https://github.com/ValentinKolb/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --uninstall --ingestor=linux
```

This removes the managed binary and scheduler. The config and Pulse data remain.

## Operate Docker

From the Compose directory:

```sh
docker compose ps
docker compose logs -f pulse-docker
docker compose pull
docker compose up -d
```

See [Docker ingestor](/en/ingestor-docker) for the complete deployment.
