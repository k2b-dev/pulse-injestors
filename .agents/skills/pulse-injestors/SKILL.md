---
name: pulse-injestors
description: Use this skill when working in the pulse-injestors repository: adding or changing Go ingestor binaries, collector modules, Pulse payload semantics, Fibel docs, local smoke tests, Docker packaging, or distribution. It captures the repository architecture, verification expectations, and current distribution constraints.
---

# Pulse Ingestors Repository Skill

Use this skill for code, docs, and packaging work in `pulse-injestors`.

## Project model

Pulse ingestors are Go one-shot collectors. Each binary collects telemetry and sends one JSON batch to a configured Pulse HTTP ingest endpoint.

Current binaries:

- `cmd/pulse-docker`: Docker host and container monitoring.
- `cmd/pulse-linux`: Linux host monitoring.
- `cmd/pulse-macos`: macOS device monitoring.
- `cmd/pulse-proxmox`: local Proxmox VE node monitoring through `pvesh`.
- `cmd/pulse-proxmox-backup-server`: local Proxmox Backup Server monitoring through `proxmox-backup-debug`.
- `cmd/pulse-uptime`: ICMP, DNS, TCP, HTTP, and TLS endpoint monitoring.
- `cmd/pulse-mock-server`: local mock ingest endpoint for tests and smoke checks.

Shared logic lives in:

- `internal/pulse`: payload types and HTTP sender.
- `internal/monitoring`: runner, builder, local output, pretty report.
- `internal/config`: TOML/env/CLI config resolution.
- `internal/validation`: semantic Pulse batch validation.
- `internal/modules/*`: reusable collectors.

## Design rules

- Keep binaries specialized. Do not turn them into one large multi-target CLI unless explicitly requested.
- Keep collectors graceful-fail. Optional modules should be auto-enabled by default where a binary supports them. Missing tools, unsupported OS features, and expected absent services should emit concise availability states without collector failure events. Permission, timeout, parsing, and unexpected command failures should remain visible as events.
- Keep collectors one-shot friendly. Scheduling belongs to cron, systemd timers, launchd, or the Docker wrapper.
- Do not log secrets. Token values must not appear in logs, events, or docs examples except as placeholders.
- Prefer existing module patterns over new abstractions.
- Use stable dot-notation signal names.
- Give every built-in signal an explicit stable resource with a human-readable label. Create resources only for objects a user should browse directly.
- Keep dimensions bounded and useful for exact filtering or grouping. Put volatile IDs and current metadata in states. Put irregular error/task context in event attributes.
- Use `actorId`, `sessionId`, and `correlationId` for identities. Do not copy those values into dimensions.
- Use canonical units: `percent`, `bytes`, `seconds`, temperature units, or no unit for plain cardinalities.
- For `pulse-docker`, keep browsable resources explicit: `host`, `docker-daemon`, logical `docker-container`, `docker-compose-project`, `docker-compose-service`, and logical `docker-image`. Mounts and network attachments stay on the container resource with bounded dimensions. Runtime container IDs, image IDs, digests, endpoint IDs, paths, and addresses are state values or event attributes, never resource identities or dimensions.
- Keep Proxmox guests cluster-namespaced, but emit `proxmox-cluster` only for a real cluster. Namespace Ceph resources by FSID when available. Map task users to `actorId` and UPIDs to `correlationId`.

## Common commands

```sh
gofmt -w <changed-go-files>
go test ./...
go build ./...
shellcheck scripts/install.sh tests/install.sh
tests/install.sh
goreleaser check
git diff --check
```

Local output:

```sh
go run ./cmd/pulse-linux --local --pretty once
go run ./cmd/pulse-macos --local --pretty once
go run ./cmd/pulse-uptime --local --pretty once
```

Mock ingest target:

```sh
go run ./cmd/pulse-mock-server --dump-file /tmp/pulse-batch.json
```

Fibel docs:

```sh
bun install
FIBEL_SITE_URL=https://docs.example.test FIBEL_AI_PROVIDER=ollama FIBEL_AI_MODEL=release-smoke bun run docs:build
FIBEL_SITE_URL=https://docs.example.test FIBEL_AI_PROVIDER=ollama FIBEL_AI_MODEL=release-smoke bun run docs:check
bun run docs:dev -- --port 5173
```

## Documentation map

- Fibel docs live in `docs/en/*.md`.
- The shared telemetry contract lives in `docs/metrics.md` and `docs/en/telemetry-model.md`.
- Exhaustive signal tables live on the Fibel module pages.
- Reference Dashboard DSL files live in `examples/dashboards/`.
- Example TOML files live in `configs/`.
- Native releases are defined in `.goreleaser.yaml` and `.github/workflows/release.yml`.
- Native enrollment lives in `scripts/install.sh`; Docker deployment lives in `deploy/docker/`.

When changing a signal, resource, dimension, unit, event field, or failure mode, update the affected Fibel module and ingestor pages in the same change. Keep every module table exhaustive and include type, unit, dimensions, example, and concrete meaning. Compile reference dashboards when query semantics or signal names change.

User-facing ingestor pages must lead with requirements, installation, enrollment, verification, scheduling, logs, updates, and removal. Put resource and signal details after that workflow. Unattended installer mode must never prompt and must validate required input before changing system files. Raw tokens must not be accepted as installer CLI arguments; prefer `--config-source` or `PULSE_INGEST_TOKEN_FILE`. Show the complete maintained Compose file on the Docker Fibel page.

## Distribution state

- `pulse-docker` is a multi-architecture GHCR image deployed through the maintained Compose file. The native installer must never manage Docker.
- Tagged native releases publish signed Linux/macOS `amd64`/`arm64` archives and `install.sh`.
- The native installer supports `pulse-linux`, `pulse-macos`, `pulse-proxmox`, `pulse-proxmox-backup-server`, and `pulse-uptime`.
- CI publishes the Fibel runtime as `ghcr.io/valentinkolb/pulse-injestors-docs` after documentation route, link, and container smoke checks pass.
- The documentation container always exposes the public read-only Fibel MCP endpoint. Its assistant is enabled only when `FIBEL_AI_MODEL` and valid provider configuration are present.
- Both published images are multi-architecture, include SBOM/provenance attestations, and are signed by the repository workflow identity.
- Package-manager-specific distribution is not available.
