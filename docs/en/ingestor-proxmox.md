---
title: Proxmox VE ingestor
navTitle: Proxmox VE
section: Ingestors
order: 140
description: Enroll a local Proxmox VE node and monitor guests, tasks, backups, and Ceph.
tags: [proxmox, ceph, ingestor]
updated: 2026-10-23
---

# Proxmox VE ingestor

`pulse-proxmox` runs locally on each monitored Proxmox VE node. It reads local `pvesh` data for nodes, QEMU VMs, LXC containers, tasks, backup jobs, and Ceph.

It supports standalone and clustered Proxmox VE installations. Install it on every PVE node so election can transfer reporting when a node goes offline.

## Run pulse-linux too

Install [pulse-linux](/en/ingestor-linux) separately on the same host with `[linux] profile = "proxmox-node"`. It supplies host CPU, RAM, disks, SMART, packages and systemd signals. The Proxmox installer does not auto-install it. Keep a separate config, token and spool for each ingestor.

## Requirements

- Proxmox VE on `amd64` or `arm64`;
- root access;
- local `pvesh`;
- `curl`, `tar`, and Cosign;
- a Pulse ingest URL and token.

The default systemd service runs as root so it can read the local Proxmox API.

## Enroll interactively

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --ingestor=proxmox
```

The installer writes `/etc/pulse/pulse-proxmox.toml`, validates Proxmox collection, sends the first batch, and enables `pulse-proxmox.service`.

Use a stable resource ID unique to the physical node. In a cluster, each node uses its own host ID while cluster and guest resources are derived consistently from Proxmox data.

The generated config references `/etc/pulse/pulse-proxmox.token`. systemd loads this private file as a credential. The service runs `run`; cron remains a fallback that runs `once`.

## Enroll unattended

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
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
sudo pulse-proxmox --config /etc/pulse/pulse-proxmox.toml --local --pretty once
systemctl status pulse-proxmox.service
journalctl -u pulse-proxmox.service -n 100
```

Update by running the installer again. Remove the binary and service while preserving the config, token, and spool:

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --uninstall --ingestor=proxmox
```

## Main settings

```toml
[proxmox]
pvesh_path = "pvesh"
timeout_seconds = 10
list_timeout_seconds = 120
backup_timeout_seconds = 180
enable_ceph_api = true
```

Ceph collection is enabled by default. A node without Ceph reports `system.ceph.available=false` without producing follow-up failure events. Set `enable_ceph_api = false` when the host must not be queried for Ceph.

## Collected data

- Proxmox version, cluster quorum, and node availability;
- QEMU VM and LXC status, placement, HA state, CPU, memory, disk/network counters, and uptime;
- recent tasks and failure events;
- backup jobs, successful backup age, and uncovered guests;
- Ceph cluster, OSD, placement-group, capacity, and pool data.

Tasks are events on the cluster resource, or the local node when standalone. Proxmox users are stored as event actors and UPIDs as correlation IDs instead of dimensions.

See the [Proxmox module](/en/module-proxmox), [Ceph module](/en/module-ceph), and [host modules](/en/modules) for exhaustive signal tables.

The `proxmox-node` Linux profile watches pve-cluster, corosync, pvedaemon, pveproxy, pvestatd, pve-ha-lrm and pve-ha-crm. The systemd module does not expand unit globs: add concrete `ceph-mon@<name>.service` and `ceph-osd@<id>.service` names to `systemd_units` when needed. On standalone PVE, corosync may be absent.

Cluster-wide reporting requires quorum. The elected node is the online node with the lowest node ID in the quorate view; missing IDs trigger name order. Only it reports cluster-wide data. Guest states and Ceph run every 60 seconds, guest metrics and storage capacity every 300 seconds, backup coverage and freshness every 900 seconds, and versions every 86400 seconds. See the module page for failure and partition behavior.
