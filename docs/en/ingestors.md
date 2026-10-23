---
title: Ingestors
navTitle: Ingestors
section: Ingestors
order: 100
description: Choose, install, and operate the Pulse ingestor for a monitored platform.
tags: [ingestors]
updated: 2026-10-23
---

# Ingestors

Choose the specialized ingestor for each monitored system. Several ingestors may share a host or source token; keep their configs and spool directories separate.

| Ingestor | Use it for | Installation | Service |
|---|---|---|---|
| [`pulse-docker`](/en/ingestor-docker) | Docker daemon, containers, Compose workloads, images, and selected host data | Docker Compose | Container running `run` |
| [`pulse-linux`](/en/ingestor-linux) | Linux servers, desktops, and Raspberry Pi systems | Native installer | systemd service, cron fallback |
| [`pulse-macos`](/en/ingestor-macos) | MacBooks and Mac desktops | Native installer | per-user LaunchAgent |
| [`pulse-proxmox`](/en/ingestor-proxmox) | Local Proxmox VE nodes, guests, tasks, backups, and Ceph | Native installer | systemd service, cron fallback |
| [`pulse-proxmox-backup-server`](/en/ingestor-proxmox-backup-server) | Local PBS server, datastores, jobs, snapshots, and tasks | Native installer | systemd service, cron fallback |
| [`pulse-uptime`](/en/ingestor-uptime) | Internet connectivity and named ICMP, DNS, TCP, or HTTP endpoints | Native installer | systemd or launchd service, cron fallback |

Proxmox and PBS include the Linux host collectors. Docker Desktop users normally deploy both `pulse-docker` for the Docker VM and `pulse-macos` for the physical Mac.

The individual guides contain:

- platform prerequisites and permissions;
- interactive and unattended enrollment commands;
- service and log checks;
- update and removal commands;
- collected areas and links to exhaustive signal references.

Native services use `run` with systemd watchdog supervision or launchd `KeepAlive`. cron runs `once` as a fallback. See [Operations](/en/operations) for the differences.
