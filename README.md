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

## Release channels

Tags matching `v*` publish:

- signed native archives and `install.sh` through GitHub Releases;
- `pulse-docker` as `ghcr.io/valentinkolb/pulse-docker`;
- this documentation site as `ghcr.io/valentinkolb/pulse-injestors-docs`.

Stable image releases receive the version, major-minor, commit SHA, and `latest` tags. Pull requests build every native target and both multi-architecture images without publishing them.

Run the documentation image behind a TLS-terminating reverse proxy:

```sh
cd deploy/docs
cp .env.example .env
# Set FIBEL_SITE_URL in .env to the public HTTPS URL.
docker compose up -d
```

The Compose service binds to `127.0.0.1:3000` by default. Change `DOCS_BIND_ADDRESS` only when the container must be reachable outside the host.

The documentation exposes a public, read-only MCP endpoint at `/_fibel/mcp`. The footer contains its setup instructions. To enable the optional documentation assistant, set `FIBEL_AI_MODEL`, `FIBEL_AI_PROVIDER`, and the provider API key in `deploy/docs/.env`. Without a model, the assistant is not rendered.

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
FIBEL_SITE_URL=https://docs.example.test \
FIBEL_AI_PROVIDER=ollama \
FIBEL_AI_MODEL=release-smoke \
bun run docs:build

FIBEL_SITE_URL=https://docs.example.test \
FIBEL_AI_PROVIDER=ollama \
FIBEL_AI_MODEL=release-smoke \
bun run docs:check
```

Inspect source builds without sending:

```sh
go run ./cmd/pulse-macos --local --pretty once
go run ./cmd/pulse-linux --local --pretty once
go run ./cmd/pulse-uptime --local --pretty once
```

The canonical telemetry contract is summarized in [docs/metrics.md](docs/metrics.md).
