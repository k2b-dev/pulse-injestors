# pulse-injestors

Focused Go telemetry ingestors for Pulse.

## Native enrollment

The signed installer supports Linux, macOS, Proxmox VE, Proxmox Backup Server, and uptime monitoring on `amd64` and `arm64`:

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh
```

It installs the selected binary, writes a per-binary config and separate private token file, verifies local collection, sends the first Pulse batch, and starts a supervised `run` service under systemd or launchd. cron is a fallback that runs `once`. It also supports fully unattended enrollment with `--config-source` or provisioning environment variables.

## Docker

Docker is deployed with the maintained Compose files:

```sh
cd deploy/docker
cp pulse-docker.example.toml pulse-docker.toml
chmod 600 pulse-docker.toml
# Set the ingest URL and stable resource identity in pulse-docker.toml.
# Create a private file, then enter the source token as one line in your editor.
install -m 0600 /dev/null pulse-docker.token
${EDITOR:-vi} pulse-docker.token
docker compose up -d
```

Compose mounts the token as a secret and keeps queued telemetry in the `pulse-spool` volume. See [Docker enrollment](docs/en/ingestor-docker.md) for local verification before starting the service.

## Release channels

Tags matching `v*` publish:

- signed native archives and `install.sh` through GitHub Releases;
- `pulse-docker` as `ghcr.io/k2b-dev/pulse-docker`;
- this documentation site as `ghcr.io/k2b-dev/pulse-injestors-docs`.

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

## Verify against the target Cloud release

This release uses the current Pulse resource contract without legacy aliases.
Configuration uses `[resource]` and `PULSE_RESOURCE_ID`; ingest uses
`POST /api/pulse/ingest`. Deploy it with the matching Cloud version.

```sh
go test -race ./...
go vet ./...
PULSE_CLOUD_CHECKOUT=/absolute/path/to/cloud bun run check:pulse
```

The contract check requires the target Cloud checkout with its dependencies
installed and its supported Bun version. It runs mocked collector fixtures
through the HTTP client, validates the wire payloads with Pulse's current
schema. It does not access live servers. Pulse uses SQL; see the untested
[SQL query examples](docs/en/dashboards.md).

Production examples enable a private, bounded spool. Collection and delivery
run independently. Parts expire after 23 hours; a full budget drops the oldest
parts first. See [configuration](docs/en/configuration.md) for response handling,
quarantine, token rotation, and recovery.

States are sent on change and refreshed after six hours when observed again.
Use `ingestor.heartbeat` for liveness and set the source's **Expected data** in
Pulse. Measure the selected collector profile before choosing a source/base
quota and spool budget. See [current readiness](PRODUCTION-READINESS.md) for
verification scope and known limits.
