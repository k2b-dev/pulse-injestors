---
title: Modules
navTitle: Modules
section: Reference
order: 200
description: Reusable collector modules used by the Pulse ingestor binaries.
tags: [modules, reference]
updated: 2026-07-23
---

# Modules

Collector modules live under `internal/modules` and are reused by the specialized binaries. Each module page lists emitted metrics, states, events, dimensions, units, value types, and examples.

## Host modules

- [`system`](/en/module-system): CPU, memory, load, and uptime from host system files.
- [`filesystem`](/en/module-filesystem): mount and filesystem usage data.
- [`network`](/en/module-network): Linux interface counters.
- [`linuxruntime`](/en/module-linuxruntime): Linux pressure, process counts, and socket summaries.
- [`thermal`](/en/module-thermal): hwmon/sysfs thermal zones where available.

## Platform modules

- [`docker`](/en/module-docker): Docker Engine API container, image, network, mount, and registry freshness data.
- [`macos`](/en/module-macos): macOS system, battery, filesystem, Homebrew, software update, display, and GPU data.
- [`proxmox`](/en/module-proxmox): Proxmox VE cluster, resource, Ceph, task, and backup data through local `pvesh`.
- [`pbs`](/en/module-pbs): Proxmox Backup Server datastore, snapshot, job, and task data through local `proxmox-backup-debug`.

## Storage and service modules

- [`btrfs`](/en/module-btrfs): Btrfs mount presence and usage data.
- [`zfs`](/en/module-zfs): ZFS pool, dataset, and snapshot data through ZFS tools.
- [`ceph`](/en/module-ceph): local Ceph CLI health, OSD, PG, and pool data.
- [`diskhealth`](/en/module-diskhealth): SMART and NVMe disk health through host tools.
- [`packages`](/en/module-packages): package update counts for supported Linux package managers.
- [`systemd`](/en/module-systemd): configured systemd unit states.

## Extension module

- [`script`](/en/module-script): runs configured commands that emit Pulse JSON fragments.

## Connectivity module

- [`uptime`](/en/module-uptime): named ICMP, DNS, TCP, HTTP, and TLS endpoint checks.

Optional modules are best-effort and auto-enabled by default when their binary includes them. Expected absence, such as missing ZFS tools or no Ceph cluster, reports availability states without collector failure events. Unexpected command, permission, timeout, or parsing failures remain visible as events.

## Runner signals

The shared runner emits these signals on the ingestor's root resource:

| Name | Signal type | Dimensions | Example | Meaning |
|---|---|---|---|---|
| `ingestor.collector.ok` | state boolean | `host`, `collector` | `true` | Whether one collector completed without returning an error. |
| `ingestor.collector.failed` | event | `host`, `collector` | `attributes.error` | One collector returned an error. |
| `ingestor.collect.ok` | state boolean | `host` | `false` | No collector produced a signal. |
| `ingestor.collect.empty` | event | `host` | none | The complete run produced no signals. |
