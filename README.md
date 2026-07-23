# pulse-injestors

Focused Go telemetry ingestors for Pulse.

## Native enrollment

The signed installer supports Linux, macOS, Proxmox VE, Proxmox Backup Server, and uptime monitoring on `amd64` and `arm64`:

```sh
curl -fsSL \
  https://github.com/ValentinKolb/pulse-injestors/releases/latest/download/install.sh \
  | sh
```

It installs the selected binary, writes a protected config, verifies local collection, sends the first Pulse batch, and enables systemd, cron, or launchd. It also supports fully unattended enrollment with `--config-source` or provisioning environment variables.

## Docker

Docker is deployed with the maintained Compose files:

```sh
cd deploy/docker
cp pulse-docker.example.toml pulse-docker.toml
chmod 600 pulse-docker.toml
docker compose up -d
```

The complete admin documentation lives in [`docs/en`](docs/en/index.md). Start with:

- [Getting started](docs/en/getting-started.md)
- [Native installation](docs/en/installation.md)
- [Docker ingestor](docs/en/ingestor-docker.md)
- [Operations](docs/en/operations.md)
- [Troubleshooting](docs/en/troubleshooting.md)

## Development

```sh
go test ./...
go build ./...
bun run docs:build
```

Inspect source builds without sending:

```sh
go run ./cmd/pulse-macos --local --pretty once
go run ./cmd/pulse-linux --local --pretty once
go run ./cmd/pulse-uptime --local --pretty once
```

The canonical telemetry contract is summarized in [docs/metrics.md](docs/metrics.md).
