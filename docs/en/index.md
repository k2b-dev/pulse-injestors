---
title: Pulse Ingestors
navTitle: Overview
section: Start
order: 10
description: Install and operate telemetry collectors for Docker, Linux, macOS, Proxmox VE, and Proxmox Backup Server.
tags: [overview]
updated: 2026-10-23
---

# Pulse Ingestors

Pulse Ingestors collect infrastructure telemetry and send it to a configured Pulse source. They report host, container, storage, service, package, and platform health.

## Choose an ingestor

| Target | Ingestor | Deployment |
|---|---|---|
| Docker Engine or Docker Desktop | [`pulse-docker`](/en/ingestor-docker) | Docker Compose |
| Linux server, desktop, or Raspberry Pi | [`pulse-linux`](/en/ingestor-linux) | Native installer with systemd or cron |
| MacBook or Mac desktop | [`pulse-macos`](/en/ingestor-macos) | Native installer with launchd |
| Proxmox VE node | [`pulse-proxmox`](/en/ingestor-proxmox) | Native installer with systemd or cron |
| Proxmox Backup Server | [`pulse-proxmox-backup-server`](/en/ingestor-proxmox-backup-server) | Native installer with systemd or cron |
| Internet connection or named endpoints | [`pulse-uptime`](/en/ingestor-uptime) | Native installer with systemd, cron, or launchd |

The Proxmox and PBS ingestors already include general Linux host monitoring. They can run alongside `pulse-linux` with separate configs and spools, but overlapping collectors produce duplicate host telemetry.

## Start here

- [Enroll a native host](/en/getting-started) interactively.
- [Automate installation](/en/installation#unattended-enrollment) with a config file or provisioning variables.
- [Deploy the Docker ingestor](/en/ingestor-docker#deploy-with-docker-compose) with the published Compose file.
- [Operate an installation](/en/operations) and inspect its service or logs.

## What arrives in Pulse

Services collect on an interval and queue data for independent delivery. They report:

- numeric metrics such as CPU usage, filesystem bytes, temperature, and task counts;
- current states such as versions, service status, pool health, and update availability;
- historical events for failed tasks or unexpected collector failures;
- stable resources such as hosts, containers, VMs, filesystems, services, and storage pools.

See [Telemetry model](/en/telemetry-model) for the shared data model. The [module reference](/en/modules) lists every collected signal.

States are sent on change and refreshed after six hours when observed again. Use `ingestor.heartbeat` for liveness, and set the source's **Expected data** in Pulse to detect a silent source.
