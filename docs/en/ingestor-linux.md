---
title: Linux ingestor
navTitle: Linux
section: Ingestors
order: 120
description: Enroll and monitor Linux servers, desktops, and Raspberry Pi systems.
tags: [linux, ingestor]
updated: 2026-07-23
---

# Linux ingestor

`pulse-linux` monitors a Linux host directly. Use it for servers, desktops, virtual machines, and Raspberry Pi systems that are not already monitored by the Proxmox or PBS ingestor.

## Requirements

- Linux on `amd64` or `arm64`;
- `curl`, `tar`, Cosign, and administrator access;
- a Pulse ingest URL and token.

Optional tools increase coverage:

- `smartctl` and `nvme` for disk health;
- `systemctl` for configured service state;
- `btrfs`, `zpool`, `zfs`, or `ceph` for matching storage systems.

Missing optional tools do not stop CPU, memory, filesystem, or other collectors.

## Enroll interactively

```sh
curl -fsSL \
  https://github.com/ValentinKolb/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --ingestor=linux
```

The installer writes `/etc/pulse/ingestor.toml`, verifies a local collection, sends the first batch, and enables a systemd timer. It falls back to cron when systemd is unavailable.

## Enroll unattended

```sh
curl -fsSL \
  https://github.com/ValentinKolb/pulse-injestors/releases/latest/download/install.sh \
  -o /tmp/pulse-install.sh

sudo sh /tmp/pulse-install.sh \
  --unattended \
  --ingestor=linux \
  --config-source=/run/secrets/pulse-ingestor.toml \
  --scheduler=systemd
```

See [Native installation](/en/installation#unattended-enrollment) for provisioning variables and all installer options.

## Verify and operate

```sh
sudo pulse-linux --config /etc/pulse/ingestor.toml --local --pretty once
systemctl status pulse-linux.timer
journalctl -u pulse-linux.service -n 100
```

Update by running the same installer command again. Remove the binary and scheduler while preserving the config:

```sh
curl -fsSL \
  https://github.com/ValentinKolb/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --uninstall --ingestor=linux
```

## Main settings

```toml
[host]
proc_root = "/proc"
sys_root = "/sys"
root = "/"
enable_thermal = true
enable_btrfs = true
enable_zfs = true
enable_ceph = true

[linux]
profile = "server"
systemd_units = ["ssh.service"]
package_timeout_seconds = 20
disk_health_timeout_seconds = 20
```

Use `profile = "desktop"` for Linux workstations or `profile = "docker-host"` for a host whose important services include Docker.

## Collected data

- CPU, memory, swap, load, uptime, and mounted filesystems;
- network interface counters and TCP/UDP socket summaries;
- pressure stall information and process states;
- available package updates;
- configured systemd services;
- SMART and NVMe disk health;
- temperature sensors;
- Btrfs, ZFS, and Ceph when present;
- configured custom scripts.

See [Modules](/en/modules) for exhaustive signal tables and requirements.
