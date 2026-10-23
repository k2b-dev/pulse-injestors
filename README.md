# pulse-injestors

Focused Go telemetry ingestors for Pulse: Linux, macOS, Docker, Proxmox VE, Proxmox Backup Server, and uptime probes.

## Install

The signed native installer supports Linux/macOS on `amd64` and `arm64`. It verifies the release, writes a private config and token file, sends the first batch, and starts a supervised systemd or launchd service:

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --ingestor=linux --linux-profile=server
```

Select `macos`, `proxmox`, `pbs`, or `uptime` as needed. Install `pulse-linux` alongside Proxmox/PBS for host telemetry. Docker uses the maintained [Compose deployment](docs/en/ingestor-docker.md). Re-running the native installer preserves config/token and restarts the service; uninstall keeps local data unless `--purge` is added.

Create one Pulse source per host and set **Expected data** after enrollment. Size the base before rolling out: the default is 2,000 values/minute, and initial inventory creates a burst.

- [Getting started](docs/en/getting-started.md) and [unattended installation](docs/en/installation.md)
- [Fleet rollout and sizing](docs/en/rollout.md)
- [Alert SQL templates](docs/en/alerts.md) and [dashboard queries](docs/en/dashboards.md)
- [Operations](docs/en/operations.md), [configuration](docs/en/configuration.md), and [troubleshooting](docs/en/troubleshooting.md)
- [Current readiness and verification gaps](PRODUCTION-READINESS.md)
- [Full documentation](docs/en/index.md) and [telemetry contract](docs/metrics.md)

## Develop and verify

```sh
go vet -p 1 ./...
go test -race -p 1 ./...
sh tests/install.sh
FIBEL_SITE_URL=https://docs.example.test FIBEL_AI_PROVIDER=ollama \
  FIBEL_AI_MODEL=release-smoke bun run docs:build
FIBEL_SITE_URL=https://docs.example.test FIBEL_AI_PROVIDER=ollama \
  FIBEL_AI_MODEL=release-smoke bun run docs:check
```

Before release, verify the wire contract with the target Cloud checkout and its installed dependencies:

```sh
PULSE_CLOUD_CHECKOUT=/absolute/path/to/cloud bun run check:pulse
```

This fixture check does not replace live delivery verification. SQL examples are marked untested until run against the target Pulse base.

Tags matching `v*` publish signed native archives and `install.sh`, the `ghcr.io/k2b-dev/pulse-docker` image, and the `ghcr.io/k2b-dev/pulse-injestors-docs` image. The maintained documentation Compose file is in [`deploy/docs`](deploy/docs/); it serves the public read-only Fibel MCP endpoint and an optional configured assistant.
