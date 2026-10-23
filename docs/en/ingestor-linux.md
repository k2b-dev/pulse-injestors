---
title: Linux ingestor
navTitle: Linux
section: Ingestors
order: 120
description: Enroll and monitor Linux servers, desktops, and Raspberry Pi systems.
tags: [linux, ingestor]
updated: 2026-10-23
---

# Linux ingestor

`pulse-linux` monitors a Linux host directly. Use it for servers, desktops, virtual machines, and Raspberry Pi systems that are not already monitored by the Proxmox or PBS ingestor.

## Requirements

- Linux on `amd64` or `arm64`;
- `curl`, `tar`, Cosign, and administrator access;
- a Pulse ingest URL and token.

Optional tools increase coverage:

- `smartctl` and `nvme` for disk health;
- `systemctl` for configured service state and failed-unit summaries;
- `journalctl` for kernel errors;
- `rpm` or `dpkg-query` for the installed package inventory;
- `btrfs`, `zpool`, `zfs`, or `ceph` for matching storage systems.

Missing optional tools do not stop CPU, memory, filesystem, or other collectors.

## Enroll interactively

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --ingestor=linux
```

The installer writes `/etc/pulse/pulse-linux.toml`, verifies a local collection, sends the first batch, and enables a systemd service running `run`. It falls back to cron when systemd is unavailable.

The generated config references `/etc/pulse/pulse-linux.token`. systemd loads this private file as a credential. cron runs `once` when used as a fallback.

## Enroll unattended

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  -o /tmp/pulse-install.sh

sudo sh /tmp/pulse-install.sh \
  --unattended \
  --ingestor=linux \
  --config-source=/run/secrets/pulse-ingestor.toml \
  --scheduler=systemd
```

See [Native installation](/en/installation#unattended-enrollment) for provisioning variables and all installer options.

## Workstations

Enroll one Pulse source per machine. Set the source’s **Expected data** to daily (`expectDailyBy`), choosing a time when the machine is normally on, instead of a minute-based interval. Check last contact and `ingestor.heartbeat` for liveness. Daily expectations still apply on non-working days; account for those in your notification policy.

Use `--linux-profile desktop`, or provision the following settings through the installer’s `--config-source` option:

```toml
[runner]
interval_seconds = 300

[linux]
profile = "desktop"
```

The desktop profile checks host health every 300 seconds, capacity every 900 seconds, package updates and disk health hourly, and inventory plus [workstation security posture](/en/module-posture) daily. It checks `display-manager.service`, `NetworkManager.service`, and `cups.service`; absent units report unavailable. Server profiles keep their existing schedules. Explicit `[runner.cadence]` values still take precedence. The profile alone does not change the base runner interval: use the 300-second setting above to reduce heartbeat and delivery overhead too.

Use the systemd `run` service. Cron invokes `once`, which runs every collector each time and cannot retain the workstation schedule. Preserve the spool directory so unchanged `software.*` states refresh weekly across overnight shutdowns.

Posture checks inspect mounted root/home encryption, firewall state, and unattended-update scheduling. `lsblk` needs mountpoint support; nftables/ufw usually need root or network-administration permissions to read rules. The collector never elevates privileges. A denied check reports `"unknown"` rather than a false result. See the posture page for the Debian and Fedora rules and their limits.

For one disk, two filesystems, one retained NIC, four temperature sensors, and roughly 40 runtime process/socket/pressure samples per capacity cycle, budget **about 11–12 values/minute** over a nine-hour working day, including runner signals and stable state refreshes. A cold or weekly inventory of 1,500 packages adds 2.78/minute averaged over that day. More mounts, sensors, socket states, failures, or state changes increase volume; measure your actual host before setting a quota.

## Verify and operate

```sh
sudo pulse-linux --config /etc/pulse/pulse-linux.toml --local --pretty once
systemctl status pulse-linux.service
journalctl -u pulse-linux.service -n 100
```

Update by running the same installer command again. Remove the binary and scheduler while preserving the config, token, and spool:

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
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
- available package and security updates, plus reboot requirements;
- configured systemd services and failed-unit count;
- disk I/O, OOM kills, boot time, and kernel errors;
- daily OS, kernel, hardware, and installed package inventory;
- configured certificate expiry checks and backup markers;
- SMART and NVMe disk health;
- temperature sensors;
- Btrfs, ZFS, and Ceph when present;
- configured [Patroni](/en/module-patroni), [PostgreSQL](/en/module-postgres), [NATS](/en/module-nats), [Valkey](/en/module-valkey), and [ClickHouse](/en/module-clickhouse) probes;
- configured custom scripts.

See [Modules](/en/modules) for exhaustive signal tables and requirements.

## Collection schedule and volume

Use service mode (`run`) to honor the schedule. `once` always runs all collectors, so running it every minute from cron defeats the volume reduction and repeats package inventory after process restarts without a persistent state cache. The default base interval is 60 seconds.

| Collector names | Server default cadence | Questions answered |
|---|---|---|
| `system`, `systemd`, `kernel` | 60 s | Is the host up, short of CPU/memory, rebooting, or reporting service/kernel failures? |
| `filesystem`, `network`, `linuxruntime`, `diskio`, `thermal`, `btrfs`, `zfs`, `ceph` | 300 s | Is capacity, I/O, network, or temperature becoming a problem? |
| `backup` | 900 s | How old is the last successful backup? |
| `packages`, `certificates`, `diskhealth` | 3600 s | Are security updates, reboot, certificate renewal, or disk replacement needed? |
| `patroni`, `nats`, `valkey`, `clickhouse` | 60 s | Configured local service health and roles. |
| `postgres`, `nats-jetstream`, `valkey-capacity`, `clickhouse-capacity` | 300 s | Configured database and broker capacity. |
| `postgres-version`, `nats-version`, `valkey-version`, `clickhouse-version` | 86400 s | Configured service versions. |
| `inventory` | 86400 s | Which OS, kernel, hardware, and package versions are installed? |
| `script:<name>` | runner interval | Your configured check; optional per-script `cadence_seconds`. |

Override collector names in `[runner.cadence]`. Unknown names are rejected with the valid collector names listed in the error. Values must be at least `runner.interval_seconds`; the runner checks due collectors on base ticks, with half-interval jitter tolerance. Prefer multiples of the base interval. Built-in defaults shorter than the base interval run at that interval. Script-specific runner overrides take precedence over `cadence_seconds`. Collectors scheduled every base interval run first. A failed or cancelled collector is retried on the next tick.

```toml
[runner.cadence]
network = 600
filesystem = 600
"script:custom" = 300

[linux]
certificate_paths = ["/etc/ssl/local/*.pem"]
# Replaces all defaults; [] includes all interfaces.
network_exclude = ["lo", "veth*", "docker*", "br-*"]

[[backup]]
name = "database"
marker_file = "/var/lib/backup/database.ok"
```

The default network filter removes transient container interfaces while retaining physical NICs, host bridges, bonds, and VPNs. A retained interface sends six counters per five-minute check. Host identity comes from the resource, without a duplicate `host` dimension.

A Pulse base's default quota of 2000 values/minute is shared by all its hosts: 50 hosts leave an average budget of 40 values/minute each. Measure your actual output rather than assuming the target of less than 150 values/minute per average VM fits that shared budget. Count metric samples, state changes/refreshes, events, and [runner self-monitoring](/en/modules#runner-signals). Initial and weekly package inventory can create bursts even when the hourly average is low.

Filesystem available bytes over time support a disk-full forecast in Pulse. The collector sends the observations, not a speculative days-to-full metric. Fit a trend over a representative time range and only extrapolate sustained growth.

Detailed references: [Disk I/O](/en/module-diskio), [Kernel, boot, and OOM](/en/module-kernel), [Inventory](/en/module-inventory), [Certificate files](/en/module-certificates), and [Backup markers](/en/module-backup).

## Database and broker VMs

Add only the service sections needed on this VM. An absent section makes no connections and runs no service command. Every configured probe has a 10-second deadline and 1 MiB response limit. Secrets come from private files. See the module pages for accounts, TLS settings, commands, and exhaustive signal tables:

- [Patroni](/en/module-patroni) and [PostgreSQL](/en/module-postgres) on database members;
- [NATS JetStream](/en/module-nats) on brokers;
- [Valkey](/en/module-valkey) on the cache VM;
- [ClickHouse](/en/module-clickhouse) on the analytics VM.

The following `[linux] profile` values add systemd unit checks. They do not enable service probes. `systemd_units` adds deployment-specific units to the selected profile.

| Profile | Units |
|---|---|
| `postgres` | `patroni.service`, `etcd.service` |
| `pg-edge` | `pgbouncer.service`, `haproxy.service`, `keepalived.service` |
| `nats` | `nats-server.service` |
| `nats-edge` | `haproxy.service`, `keepalived.service` |
| `valkey` | `valkey.service` |
| `clickhouse` | `clickhouse-server.service` |

Healthy steady-state service volume at the default cadences:

| Service VM | Metric samples/minute | Assumptions |
|---|---|---|
| PostgreSQL leader + Patroni | 6.6 | Five non-template databases; includes three cluster metrics. |
| PostgreSQL replica + Patroni | 5.6 | Five databases; known cluster lag and local received/replayed WAL positions available. |
| NATS broker | 9 | Eight minute metrics and five JetStream metrics every five minutes. |
| Valkey | 3.8 | A positive maxmemory limit; 3.6 if unlimited. |
| ClickHouse | 1.4 | One disk; seven capacity metrics every five minutes. |

These are additional to host metrics, systemd, inventory, and runner self-monitoring. Stable service states plus their collector-result states add roughly 0.02 values/minute per VM after startup (daily versions and six-hour refreshes). Role changes and failures add state changes and events. Three PostgreSQL members with five databases each contribute about 17.8 service metric samples/minute; three NATS brokers contribute 27. These estimates are sample counts, not encoded bytes or a quota guarantee.
