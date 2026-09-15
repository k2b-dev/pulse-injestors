# Current Pulse readiness

Verified on 2026-09-15 against Cloud checkout
`0764cd08692f41d8e903481da48938bfa02cfd38` using Bun 1.4.2.

The ingestors use the current resource-only Pulse contract. Old identity fields,
configuration sections, flags and ingest paths have no compatibility aliases.
Use these binaries with the corresponding Cloud release.

## Verification

- `go test -race ./...`, `go vet ./...`, and `go build ./...` pass.
- Linux amd64 and macOS amd64 cross-builds pass; macOS arm64 builds locally and
  Linux arm64 builds in the Docker image.
- `bun run check:pulse` passes against the checkout above. Six controlled
  collector fixtures traverse the production HTTP client and are validated by
  Pulse's actual request schema. All four dashboard examples compile.
- Delivery tests cover restart, exact body/key replay, partial acceptance,
  rejection of one chunk without losing later chunks, disabled-source recovery,
  capacity refusal, locking, expiry, redirects, invalid acknowledgements and
  cancellation during Retry-After.
- Installer integration tests, ShellCheck and GoReleaser configuration checks
  pass. Documentation builds and all 41 checked routes pass. Prose lint findings
  were reviewed; domain vocabulary and existing sentence-case headings remain.
- The Docker image builds. A read-only, network-isolated container starts and
  produces schema-valid failure telemetry with no host mounts: four events and
  twelve states. This checks packaging and unavailable-collector behavior, not
  live Docker-host collection.
- Independent review findings were fixed and re-reviewed with no remaining
  actionable transport/outbox findings.

## Measured fixture size

These counts describe selected fixtures, not a full production profile or
throughput benchmark. Optional modules and additional objects increase volume.

| Fixture | Metrics | Events | States |
| --- | ---: | ---: | ---: |
| Docker container and Compose renderers | 24 | 0 | 24 |
| Proxmox node and guests | 52 | 2 | 35 |
| PBS | 28 | 2 | 27 |
| Linux system | 11 | 0 | 0 |
| Linux runtime | 8 | 0 | 6 |
| Uptime | 10 | 0 | 27 |

The earlier Pulse scenario with 20 metric series per server does not establish
capacity for these profiles. For 60-second sampling and 30-day raw retention,
each continuously sampled series contributes 43,200 samples. Measure complete
selected profiles and multiply by actual host, guest, container and device
counts before sizing the production database.

## Operator settings

Production examples enable a 64 MiB persistent spool. The budget limits queued
JSON; allow extra filesystem space for atomic rewrites and metadata. It does not
guarantee a particular outage duration. Retained deliveries expire with a visible
error after 23 hours, inside Pulse's 24-hour idempotency window. Keep the directory
private and persistent, and use a separate directory per endpoint and source
credential. Drain the queue before token rotation where possible.

Current state in Pulse survives history retention. For short-lived resources,
plan explicit inventory cleanup; retention alone does not bound current states.
Website pageviews and conversions should continue to come from website backends
with their own source tokens and stable website resources.

No release, push, deployment, production ingestion, or full 20–50-server load test
was performed. Live platform permissions and complete hardware-specific profiles
must be checked during the controlled rollout of the matching Cloud release.
