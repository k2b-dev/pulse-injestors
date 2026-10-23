---
title: Enroll a host
navTitle: Getting started
section: Start
order: 20
description: Install a native Pulse ingestor, send the first batch, and start its service.
tags: [start, install]
updated: 2026-10-23
---

# Enroll a host

This guide installs the correct native ingestor, creates a protected configuration, verifies collection, sends the first Pulse batch, and starts the platform service.

For Docker, use the [Docker Compose deployment](/en/ingestor-docker#deploy-with-docker-compose) instead.

## Before you start

You need:

- the complete Pulse ingest URL for a source;
- the bearer token for that source;
- administrator access to the monitored system;
- `curl`, `tar`, and [Cosign](https://docs.sigstore.dev/cosign/system_config/installation/) on `PATH`.

On macOS with Homebrew:

```sh
brew install cosign
```

## Run the installer

Run the installer as your normal user. It invokes `sudo` only for system files.

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh
```

The installer detects macOS, generic Linux, Proxmox VE, or Proxmox Backup Server. On PVE/PBS, also run it with `--ingestor=linux` for host signals. It asks for:

1. Pulse ingest URL and token;
2. a stable system ID and readable label;
3. the collection interval;
4. optional global dimensions;
5. confirmation before installation.

The token input is hidden. The generated config and a separate token file are written with mode `0600`. Each binary has its own config path, such as `/etc/pulse/pulse-linux.toml`, with its token next to it as `pulse-linux.token`.

## Recognize success

Before starting the service or cron fallback, the installer performs a local collection and sends one real batch. A successful run ends with:

```text
pulse-install: enrollment complete
```

It also prints the installed binary, config path, service status command, and log location.

Open Pulse and confirm that the host resource (or `uptime-probe` for `pulse-uptime`) appears with the configured label and receives `ingestor.heartbeat` samples. Set the source's **Expected data** to `expectEverySeconds = 180` for always-on servers, or `expectDailyBy` in the base timezone for workstations. See [Fleet rollout](/en/rollout) for source/token creation and sizing.

Native installs run `run` under systemd or launchd; cron is a fallback that invokes `once`. Collection continues while delivery waits for Pulse. Use [Operations](/en/operations) to check service health and logs.

## Next steps

- Use [Native installation](/en/installation) for unattended enrollment or a fixed version.
- Use [Operations](/en/operations) to inspect services, logs, updates, and removal.
- Open the matching [ingestor guide](/en/ingestors) for platform requirements and collected data.
