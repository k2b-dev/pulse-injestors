---
title: Proxmox VE ingestor
navTitle: Proxmox VE
section: Ingestors
order: 140
description: Enroll a local Proxmox VE node and monitor its host, guests, tasks, backups, and Ceph.
tags: [proxmox, ceph, ingestor]
updated: 2026-07-23
---

# Proxmox VE ingestor

`pulse-proxmox` runs locally on each monitored Proxmox VE node. It combines Linux host telemetry with local `pvesh` data for nodes, QEMU VMs, LXC containers, tasks, backup jobs, and Ceph.

It supports standalone and clustered Proxmox VE installations. Install it on every node whose local host and guest view should remain available independently.

## Requirements

- Proxmox VE on `amd64` or `arm64`;
- root access;
- local `pvesh`;
- `curl`, `tar`, and Cosign;
- a Pulse ingest URL and token.

The default systemd service runs as root so it can access Proxmox, disks, filesystems, and optional storage tools.

## Enroll interactively

```sh
curl -fsSL \
  https://github.com/ValentinKolb/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --ingestor=proxmox
```

The installer writes `/etc/pulse/ingestor.toml`, validates host and Proxmox collection, sends the first batch, and enables `pulse-proxmox.timer`.

Use a stable entity ID unique to the physical node. In a cluster, each node uses its own host ID while cluster and guest resources are derived consistently from Proxmox data.

## Enroll unattended

```sh
curl -fsSL \
  https://github.com/ValentinKolb/pulse-injestors/releases/latest/download/install.sh \
  -o /tmp/pulse-install.sh

sudo sh /tmp/pulse-install.sh \
  --unattended \
  --ingestor=proxmox \
  --config-source=/run/secrets/pulse-proxmox.toml \
  --scheduler=systemd
```

See [Native installation](/en/installation#unattended-enrollment) for token-file enrollment and all supported options.

## Verify and operate

```sh
sudo pulse-proxmox --config /etc/pulse/ingestor.toml --local --pretty once
systemctl status pulse-proxmox.timer
journalctl -u pulse-proxmox.service -n 100
```

Update by running the installer again. Remove the binary and timer while preserving the config:

```sh
curl -fsSL \
  https://github.com/ValentinKolb/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --uninstall --ingestor=proxmox
```

## Main settings

```toml
[linux]
profile = "proxmox"
systemd_units = []
package_timeout_seconds = 20
disk_health_timeout_seconds = 20

[proxmox]
pvesh_path = "pvesh"
timeout_seconds = 10
enable_ceph_api = true
enable_local_ceph = true
```

Ceph collection is enabled by default. A node without Ceph reports `system.ceph.available=false` without producing follow-up failure events. Set both Ceph options to `false` when the host must not be queried for Ceph.

## Collected data

- Linux CPU, memory, load, filesystems, network, packages, services, disks, temperature, Btrfs, and ZFS;
- Proxmox version, cluster quorum, and node availability;
- QEMU VM and LXC status, CPU, memory, disk, and uptime;
- recent tasks and failure events;
- backup jobs, successful backup age, and uncovered guests;
- Ceph cluster, OSD, placement-group, capacity, and pool data.

Tasks remain events on their owning node or resource. Proxmox users are stored as event actors and UPIDs as correlation IDs instead of dimensions.

See the [Proxmox module](/en/module-proxmox), [Ceph module](/en/module-ceph), and [host modules](/en/modules) for exhaustive signal tables.
