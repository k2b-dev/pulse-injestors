# pulse-injestors

Initial Go ingestors for Pulse.

Current binaries:

- `pulse-docker`: Docker host/container monitoring.
- `pulse-linux`: Linux host monitoring without Docker.
- `pulse-macos`: macOS device monitoring.
- `pulse-mock-server`: local ingest target for smoke tests.

Metric names, units, dimensions, sources, and cost classes are listed in [docs/metrics.md](docs/metrics.md).

```sh
docker run -d \
  --name pulse-docker \
  --restart unless-stopped \
  -e PULSE_INGEST_URL="https://pulse.example.com/ingest/source" \
  -e PULSE_INGEST_TOKEN="..." \
  -e PULSE_INTERVAL_SECONDS=60 \
  -e PULSE_ENTITY_ID="$(hostname)" \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -v /proc:/host/proc:ro \
  -v /sys:/host/sys:ro \
  -v /:/host/root:ro \
  ghcr.io/valentinkolb/pulse-docker:latest
```

The container runs continuously by default. Use `once` as the final argument to collect, push, and exit.

Inspect locally without sending:

```sh
go run ./cmd/pulse-macos --local once
go run ./cmd/pulse-macos --local --pretty once
go run ./cmd/pulse-linux --local --pretty once
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -v /proc:/host/proc:ro \
  -v /sys:/host/sys:ro \
  -v /:/host/root:ro \
  pulse-docker:local --local --pretty once
```

Local smoke target:

```sh
go run ./cmd/pulse-mock-server
```

macOS local smoke:

```sh
go run ./cmd/pulse-macos --local once
```
