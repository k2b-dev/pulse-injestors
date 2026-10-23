---
title: Docker ingestor
navTitle: Docker
section: Ingestors
order: 110
description: Deploy and operate Docker host, container, Compose, and image monitoring.
tags: [docker, ingestor]
updated: 2026-10-23
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
- `[resource].id` to the stable physical host or Docker VM ID;
- `[resource].label` to the name users should see in Pulse.

Store the source token as exactly one line in `pulse-docker.token`. Create the private file first, then edit it without putting the token in shell arguments:

```sh
sudo install -m 0600 /dev/null pulse-docker.token
sudoedit pulse-docker.token
```

Compose mounts it at `/run/secrets/pulse-ingest-token` and sets `PULSE_INGEST_TOKEN_FILE` to that path. Keep the file for updates; do not recreate it with the command above once it holds a token.

Validate collection without sending:

```sh
sudo docker compose run --rm \
  pulse-docker --local --pretty once
```

Send the first batch:

```sh
sudo docker compose run --rm pulse-docker once
```

Start the service:

```sh
sudo docker compose up -d
sudo docker compose ps
```

`run` collects on the configured interval and delivers queued parts independently. The `pulse-spool` volume preserves queued data across container recreation. For later manual authenticated checks, stop the service first to release its spool. Local checks do not open the spool.

## Operate and update

```sh
cd /opt/pulse-docker
sudo docker compose logs -f pulse-docker
sudo docker compose pull
sudo docker compose up -d
```

Stop and remove the ingestor while keeping its config, token, and named spool volume:

```sh
sudo docker compose down
```

## Compose file

The published `deploy/docker/compose.yml` is:

```yaml
services:
  pulse-docker:
    image: ghcr.io/k2b-dev/pulse-docker:latest
    container_name: pulse-docker
    restart: unless-stopped
    command: ["run"]
    environment:
      PULSE_INTERVAL_SECONDS: ${PULSE_INTERVAL_SECONDS:-60}
      PULSE_INGEST_TOKEN_FILE: /run/secrets/pulse-ingest-token
    secrets:
      - pulse-ingest-token
    read_only: true
    security_opt:
      - no-new-privileges:true
    volumes:
      - pulse-spool:/var/lib/pulse/pulse-docker
      - ./pulse-docker.toml:/etc/pulse/pulse-docker.toml:ro
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - /proc:/host/proc:ro
      - /sys:/host/sys:ro
      - /:/host/root:ro

volumes:
  pulse-spool:

secrets:
  # One line with the source token; keep the file private (chmod 600).
  pulse-ingest-token:
    file: ./pulse-docker.token
```

Create an optional `.env` file to change the collection interval:

```dotenv
PULSE_INTERVAL_SECONDS=60
```

The mode-`0600` token file is a Compose secret. The container environment contains its path, not the token value.

## Default config

```toml
[pulse]
ingest_url = "https://pulse.example.com/api/pulse/ingest"
ingest_token_file = "/run/secrets/pulse-ingest-token"
spool_dir = "/var/lib/pulse/pulse-docker"
spool_max_bytes = 67108864

[resource]
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
concurrency = 4
container_timeout_seconds = 10
stats_timeout_seconds = 120
enable_registry_checks = false
registry_timeout_seconds = 10
```

Container status, health, image references, restart counters, and Compose counts are checked every 60 seconds. Running-container CPU, memory, network, and block I/O are sampled every 300 seconds. Daemon version and images referenced by existing containers are inspected daily. `once` includes every phase.

States are sent on change and refreshed after six hours when next collected. Daily inventory states refresh daily. The stats phase uses its own 120-second budget (`docker.stats_timeout_seconds`) and at most four concurrent container requests. Each container request has a 10-second timeout. Raise the stats budget when large hosts cannot complete their stats phase; a slow phase can delay the next cycle.

Registry checks are disabled by default. If enabled, they run daily for checkable image references in use. Private registries may require credentials that the ingestor does not have, and public registries may rate-limit manifest requests.

## Collected data

| Area | Pulse resources | Data |
|---|---|---|
| Host | `host`, `filesystem` | CPU, memory, swap, load, uptime, mount capacity, inodes, and optional storage data |
| Docker daemon | `docker-daemon` | Availability, daily version, and running/total container counts |
| Containers | `docker-container` | Lifecycle, health, image reference, restarts, CPU, memory used/limit, and aggregate network/block I/O counters |
| Compose | `docker-compose-project`, `docker-compose-service` | One `running/total` string state per project and service |
| Images | `docker-image` | Repository/tag identity, age, size, architecture, OS, tags, digests, and optional registry freshness |

Named-container identity remains stable across normal updates and recreation. Runtime container and image IDs, mount details, and network attachment details are not emitted as signals. Container network counters sum all interfaces. For Compose throughput, calculate rates on container counters before grouping by project or service.

For 70 running containers, regular Docker metrics average 170 values/minute before changed states, refreshes, daily inventory, and ingestor self-monitoring. The 70-container fake-socket regression checks a Docker-only average below 400 values/minute; live-host measurement is separate.

The [Docker module reference](/en/module-docker) lists every metric, state, event, dimension, type, unit, and example.

## Docker Desktop

On Docker Desktop, Linux host metrics describe the Docker virtual machine. Deploy [`pulse-macos`](/en/ingestor-macos) separately when you also need the physical Mac, battery, Homebrew, display, and macOS update data.
