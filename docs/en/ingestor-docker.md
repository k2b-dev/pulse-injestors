---
title: Docker ingestor
navTitle: Docker
section: Ingestors
order: 110
description: Deploy and operate Docker host, container, Compose, and image monitoring.
tags: [docker, ingestor]
updated: 2026-07-23
---

# Docker ingestor

`pulse-docker` monitors one Docker daemon, its containers, Compose workloads, image references, and selected Linux host data. It is distributed only as a container and does not use the native installer.

## Requirements

- Docker Engine with the Compose plugin;
- permission to mount the Docker socket and host filesystems;
- a Pulse ingest URL and token;
- a stable ID for the Docker host.

> Access to `/var/run/docker.sock` is equivalent to administrative Docker access. Only deploy trusted images and protect the Compose directory and Pulse config.

## Deploy with Docker Compose

Create a deployment directory and download the maintained files:

```sh
sudo install -d -m 0750 /opt/pulse-docker
cd /opt/pulse-docker

sudo curl -fsSLo compose.yml \
  https://raw.githubusercontent.com/k2b-dev/pulse-injestors/main/deploy/docker/compose.yml
sudo curl -fsSLo pulse-docker.toml \
  https://raw.githubusercontent.com/k2b-dev/pulse-injestors/main/deploy/docker/pulse-docker.example.toml
sudo chmod 600 pulse-docker.toml
```

Edit `pulse-docker.toml` and set:

- `[pulse].ingest_url`;
- `[pulse].ingest_token`;
- `[entity].id` to the stable physical host or Docker VM ID;
- `[entity].label` to the name users should see in Pulse.

Validate collection without sending:

```sh
sudo docker compose run --rm \
  pulse-docker pulse-injestor --local --pretty once
```

Send the first batch:

```sh
sudo docker compose run --rm pulse-docker once
```

Start scheduled collection:

```sh
sudo docker compose up -d
sudo docker compose ps
```

## Compose file

The published `deploy/docker/compose.yml` is:

```yaml
services:
  pulse-docker:
    image: ghcr.io/k2b-dev/pulse-docker:latest
    container_name: pulse-docker
    restart: unless-stopped
    command: ["schedule"]
    environment:
      PULSE_INTERVAL_SECONDS: ${PULSE_INTERVAL_SECONDS:-60}
    read_only: true
    security_opt:
      - no-new-privileges:true
    volumes:
      - ./pulse-docker.toml:/etc/pulse/ingestor.toml:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - /proc:/host/proc:ro
      - /sys:/host/sys:ro
      - /:/host/root:ro
```

Create an optional `.env` file to change the scheduler interval:

```dotenv
PULSE_INTERVAL_SECONDS=60
```

The Pulse token remains in the mode-`0600` TOML file and is not exposed as a container environment variable.

## Default config

```toml
[pulse]
ingest_url = "https://pulse.example.com/api/pulse/ingest"
ingest_token = "replace-me"

[entity]
id = "server-01"
label = "Server 01"

[dimensions]
environment = "production"

[runner]
interval_seconds = 60
collector_timeout_seconds = 60

[http]
timeout_seconds = 10
max_retries = 3
initial_backoff_ms = 500

[host]
proc_root = "/host/proc"
sys_root = "/host/sys"
root = "/host/root"
cpu_sample_ms = 250
enable_thermal = true
enable_btrfs = true
enable_zfs = true

[docker]
socket_path = "/var/run/docker.sock"
host_root = "/host/root"
concurrency = 4
container_timeout_seconds = 10
enable_registry_checks = false
registry_timeout_seconds = 10
```

Registry checks are disabled by default because private registries may need credentials and public registries may rate-limit manifest requests.

## Operate and update

```sh
cd /opt/pulse-docker
sudo docker compose logs -f pulse-docker
sudo docker compose pull
sudo docker compose up -d
```

Stop and remove the ingestor without deleting its config:

```sh
sudo docker compose down
```

## Collected data

| Area | Pulse resources | Data |
|---|---|---|
| Host | `host`, `filesystem` | CPU, memory, swap, load, uptime, mount capacity, inodes, and optional storage data |
| Docker daemon | `docker-daemon` | Version, inventory, daemon health, and image update totals |
| Containers | `docker-container` | Lifecycle, health, CPU, memory, PIDs, network, block I/O, mounts, and attachments |
| Compose | `docker-compose-project`, `docker-compose-service` | Project inventory, replicas, and aggregate service usage |
| Images | `docker-image` | Repository/tag identity, age, size, architecture, OS, tags, digests, and optional registry freshness |

Container identity remains stable across normal updates and recreation. Runtime IDs and mutable image details are reported as current facts instead of resource identity.

The [Docker module reference](/en/module-docker) lists every metric, state, event, dimension, type, unit, and example.

## Docker Desktop

On Docker Desktop, Linux host metrics describe the Docker virtual machine. Deploy [`pulse-macos`](/en/ingestor-macos) separately when you also need the physical Mac, battery, Homebrew, display, and macOS update data.
