# Current Pulse readiness

Verified on 2026-10-23 against Cloud `origin/main` (Pulse on ClickHouse, the
0.49 line) and a development Pulse instance. Nothing in this file describes a
production rollout.

## Delivery contract

- `POST /api/pulse/ingest` with the source token as Bearer credential and an
  `Idempotency-Key` derived from the part body from the first attempt.
- Parts stay within 500 items per collection, 1,500 items and 5 MiB.
- 2xx is delivered. 408, 429, 5xx and transport errors are retried with backoff
  and `Retry-After`. 3xx, 401, 403, 404 and non-idempotency 409 responses pause
  delivery and keep the data; Pulse answers 404 for a disabled source.
  Idempotency key conflicts and other 4xx reject the part: it is split until
  the invalid item stands alone, that item goes to quarantine and is reported
  as `ingestor.delivery.rejected`. A metric name with a type/unit definition
  conflict is not sent for one hour. Split-budget exhaustion keeps the part
  queued until the hourly budget resets.
- The spool keeps parts for 23 hours, inside Pulse's 24-hour idempotency window,
  and drops the oldest parts first when its byte budget is exhausted.

## Verification

- `go vet ./...` and `go test -race ./...` pass.
- `PULSE_CLOUD_CHECKOUT=<cloud> bun run check:pulse` passes: six collector
  fixtures traverse the HTTP client and are accepted by Pulse's request schema.
- Live test against a development Pulse with a fictional base and source:
  `pulse-linux run` with a token file and spool; a preloaded unit conflict was
  isolated to one metric, quarantined, reported and then blocked; a disabled
  source (404) kept all cycles queued and delivered them after re-enabling, with
  one heartbeat per cycle, no gap and no duplicate; a restart delivered the
  queued parts; 429 throttling was retried. The base was deleted afterwards.
- `tests/install.sh` and ShellCheck pass for the installer, including the
  hardened systemd service and the token file.

## Known limits

- Volume: one development VM sent about 720 values per minute and a Docker host
  with 70 containers about 5,800, against a default base quota of 2,000 values
  per minute. Cadences and trimmed collectors follow in the next slice; the
  base quota must be sized from those measurements.
- Proxmox, PBS, Ceph, macOS and Docker delivery were not tested live.
- The systemd unit runs root ingestors with `ProtectSystem=full`; finer
  privileges follow with the collector work.
