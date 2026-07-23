---
title: Enroll a host
navTitle: Getting started
section: Start
order: 20
description: Install a native Pulse ingestor, send the first batch, and enable scheduling.
tags: [start, install]
updated: 2026-07-23
---

# Enroll a host

This guide installs the correct native ingestor, creates a protected configuration, verifies collection, sends the first Pulse batch, and enables the platform scheduler.

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
  https://github.com/ValentinKolb/pulse-injestors/releases/latest/download/install.sh \
  | sh
```

The installer detects macOS, generic Linux, Proxmox VE, or Proxmox Backup Server. It asks for:

1. Pulse ingest URL and token;
2. a stable system ID and readable label;
3. the collection interval;
4. optional global dimensions;
5. confirmation before installation.

The token input is hidden. The resulting configuration is written with mode `0600`.

## Recognize success

Before enabling a scheduler, the installer performs a local collection and sends one real batch. A successful run ends with:

```text
pulse-install: enrollment complete
```

It also prints the installed binary, config path, scheduler status command, and log location.

Open Pulse and confirm that the host resource appears with the configured label.

## Next steps

- Use [Native installation](/en/installation) for unattended enrollment or a fixed version.
- Use [Operations](/en/operations) to inspect schedules, logs, updates, and removal.
- Open the matching [ingestor guide](/en/ingestors) for platform requirements and collected data.
