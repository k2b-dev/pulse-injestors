---
title: Proxmox Backup Server ingestor
navTitle: Proxmox Backup Server
section: Ingestors
order: 150
description: Enroll a local PBS host and monitor its system, datastores, snapshots, jobs, and tasks.
tags: [pbs, ingestor]
updated: 2026-07-23
---

# Proxmox Backup Server ingestor

`pulse-proxmox-backup-server` runs locally on a Proxmox Backup Server. It combines Linux host telemetry with local `proxmox-backup-debug` data for datastores, snapshots, jobs, and tasks.

## Requirements

- Proxmox Backup Server on `amd64` or `arm64`;
- root access;
- local `proxmox-backup-debug`;
- `curl`, `tar`, and Cosign;
- a Pulse ingest URL and token.

The default service runs as root so it can inspect PBS and the underlying host storage.

## Enroll interactively

```sh
curl -fsSL \
  https://github.com/ValentinKolb/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --ingestor=pbs
```

The installer writes `/etc/pulse/ingestor.toml`, verifies local collection, sends the first batch, and enables `pulse-proxmox-backup-server.timer`.

## Enroll unattended

```sh
curl -fsSL \
  https://github.com/ValentinKolb/pulse-injestors/releases/latest/download/install.sh \
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
  --config /etc/pulse/ingestor.toml \
  --local --pretty once

systemctl status pulse-proxmox-backup-server.timer
journalctl -u pulse-proxmox-backup-server.service -n 100
```

Update by running the installer again. Remove the binary and timer while preserving the config:

```sh
curl -fsSL \
  https://github.com/ValentinKolb/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --uninstall --ingestor=pbs
```

## Main settings

```toml
[linux]
profile = "pbs"
systemd_units = []
package_timeout_seconds = 20
disk_health_timeout_seconds = 20

[pbs]
command_path = "proxmox-backup-debug"
timeout_seconds = 10
```

## Collected data

- Linux CPU, memory, load, filesystems, network, packages, services, disks, temperature, Btrfs, ZFS, and Ceph;
- PBS version and availability;
- datastore used, total, available, and usage percentage;
- estimated full time when PBS exposes it;
- snapshot totals, types, and latest snapshot age;
- garbage collection, prune, sync, and verify jobs;
- recent tasks, task statuses, and failure events.

Datastores and jobs are separate resources so administrators can browse their current state directly. Task users and UPIDs use actor and correlation fields instead of high-cardinality dimensions.

See the [PBS module](/en/module-pbs) and [host modules](/en/modules) for exhaustive signal tables.
