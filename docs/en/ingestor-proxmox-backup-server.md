---
title: Proxmox Backup Server ingestor
navTitle: Proxmox Backup Server
section: Ingestors
order: 150
description: Enroll a local PBS host and monitor datastores, snapshots, jobs, and tasks.
tags: [pbs, ingestor]
updated: 2026-10-23
---

# Proxmox Backup Server ingestor

`pulse-proxmox-backup-server` runs locally on a Proxmox Backup Server. It reads local `proxmox-backup-debug` data for datastores, snapshots, jobs, and tasks.

## Run pulse-linux too

Install [pulse-linux](/en/ingestor-linux) separately on the same host with `[linux] profile = "pbs"`. It supplies host CPU, RAM, disks, SMART, packages and systemd signals. The Proxmox installer does not auto-install it. Keep a separate config, token and spool for each ingestor.

## Requirements

- Proxmox Backup Server on `amd64` or `arm64`;
- root access;
- local `proxmox-backup-debug`;
- `curl`, `tar`, and Cosign;
- a Pulse ingest URL and token.

The default service runs as root so it can read the local PBS API.

## Enroll interactively

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --ingestor=pbs
```

The installer writes `/etc/pulse/pulse-proxmox-backup-server.toml`, verifies local collection, sends the first batch, and enables `pulse-proxmox-backup-server.service`.

The generated config references `/etc/pulse/pulse-proxmox-backup-server.token`. systemd loads this private file as a credential. The service runs `run`; cron remains a fallback that runs `once`.

## Enroll unattended

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  -o /tmp/pulse-install.sh

sudo sh /tmp/pulse-install.sh \
  --unattended \
  --ingestor=pbs \
  --config-source=/run/secrets/pulse-pbs.toml \
  --scheduler=systemd
```

See [Native installation](/en/installation#unattended-enrollment) for token-file enrollment and all supported options.

## Verify and operate

```sh
sudo pulse-proxmox-backup-server \
  --config /etc/pulse/pulse-proxmox-backup-server.toml \
  --local --pretty once

systemctl status pulse-proxmox-backup-server.service
journalctl -u pulse-proxmox-backup-server.service -n 100
```

Update by running the installer again. Remove the binary and service while preserving the config, token, and spool:

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --uninstall --ingestor=pbs
```

## Main settings

```toml
[pbs]
command_path = "proxmox-backup-debug"
timeout_seconds = 10
list_timeout_seconds = 60
```

## Collected data

- PBS version and availability;
- datastore used, total, available, and usage percentage;
- estimated full time when PBS exposes it;
- snapshot totals, types, and latest snapshot age;
- datastore garbage collection, prune, sync, verify, and tape jobs;
- recent tasks, task statuses, and failure events.

Datastores, jobs and backup groups are separate resources so administrators can browse their current state directly. Task users and UPIDs use actor and correlation fields instead of high-cardinality dimensions.

See the [PBS module](/en/module-pbs) and [host modules](/en/modules) for exhaustive signal tables.

The `pbs` Linux profile watches `proxmox-backup.service` and `proxmox-backup-proxy.service`. Datastore capacity runs every 300 seconds, jobs and backup groups every 900 seconds, and versions every 86400 seconds. Each backup group exposes its newest snapshot age in the root or a nested namespace on each datastore, including an S3-backed datastore when exposed by PBS. Compare groups on both PBS hosts to check the second copy.
