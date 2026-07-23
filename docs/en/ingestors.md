---
title: Ingestors
navTitle: Ingestors
section: Ingestors
order: 100
description: Choose, install, and operate the Pulse ingestor for a monitored platform.
tags: [ingestors]
updated: 2026-07-23
---

# Ingestors

Install one specialized ingestor for each monitored system.

| Ingestor | Use it for | Installation | Scheduling |
|---|---|---|---|
| [`pulse-docker`](/en/ingestor-docker) | Docker daemon, containers, Compose workloads, images, and selected host data | Docker Compose | Container scheduler |
| [`pulse-linux`](/en/ingestor-linux) | Linux servers, desktops, and Raspberry Pi systems | Native installer | systemd or cron |
| [`pulse-macos`](/en/ingestor-macos) | MacBooks and Mac desktops | Native installer | per-user launchd |
| [`pulse-proxmox`](/en/ingestor-proxmox) | Local Proxmox VE nodes, guests, tasks, backups, and Ceph | Native installer | systemd or cron |
| [`pulse-proxmox-backup-server`](/en/ingestor-proxmox-backup-server) | Local PBS server, datastores, jobs, snapshots, and tasks | Native installer | systemd or cron |
| [`pulse-uptime`](/en/ingestor-uptime) | Internet connectivity and named ICMP, DNS, TCP, or HTTP endpoints | Native installer | systemd, cron, or launchd |

Proxmox and PBS include the Linux host collectors. Docker Desktop users normally deploy both `pulse-docker` for the Docker VM and `pulse-macos` for the physical Mac.

The individual guides contain:

- platform prerequisites and permissions;
- interactive and unattended enrollment commands;
- scheduler and log checks;
- update and removal commands;
- collected areas and links to exhaustive signal references.
