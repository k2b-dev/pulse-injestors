# Current Pulse readiness

Status as of 2026-10-23, targeting the current Cloud main / Pulse 0.49 resource and SQL contract. Development Pulse tests and read-only checks on real hosts support a staged pilot; they do not establish a completed production rollout. The observations below record prior verification supplied for this slice, not new remote tests performed during this documentation update.

## Verified live

| Environment | Verified behavior |
|---|---|
| Development Pulse, fictional base/source | Linux daemon collection accepted; token-file delivery and persistent spool; queued parts survive restart. |
| Development systemd service | `Type=notify` readiness, watchdog, hardened unit, and config/token credentials. |
| Development Pulse delivery failures | A conflicting metric was isolated, quarantined, reported, and blocked; disabled-source pause retained data and delivered after recovery without gaps or duplicates in the observed test; 429 retry behavior. The fictional base was removed afterwards. |
| Real PVE cluster node, read-only | Elected reporter, approximately 58 guests, per-guest backup age for each visible PBS storage, Ceph pools, and failed-task events. |
| Real PBS host, read-only | Jobs, garbage collection, and datastore capacity. Group collection initially timed out; after the timeout fix, groups were collected successfully. |

The PVE/PBS checks exercised local platform reads, not a production Pulse rollout. Hostnames, addresses, credentials, and private output are intentionally excluded.

## Volume after cadence tuning

Measured averages: Linux server VM ~50 values/minute; Docker with 70 containers ~173 plus ~54 for its host; elected PVE cluster view ~140 with ~58 guests, other nodes ~5 each; PBS ~10 plus host telemetry; service probes ~2–10 per VM; Mac ~5; Linux desktop ~12. These replace the earlier pre-cadence volume measurements.

Initial Linux inventory contributes roughly 850 states per host, with unchanged software refreshed weekly. Values/minute averages exclude the risk of synchronized enrollment bursts. Use the [fleet sizing example](docs/en/rollout.md) and measure actual series and burst traffic before increasing enrollment. The default base quota is 2,000 values/minute and can be raised by a Cloud administrator.

## Not verified live

- Native macOS collection, launchd lifecycle, and permission-dependent posture checks.
- A standalone PVE node outside the tested cluster, or all cluster failover/partition scenarios on real hosts.
- A second PBS server and end-to-end offsite-copy coverage.
- Native PostgreSQL/Patroni, NATS, Valkey, and ClickHouse service probes against live services.
- Docker delivery into live Pulse; local volume/collector observations do not prove delivery.
- The new alert and dashboard SQL templates against an actual base, including threshold transitions and recovery.
- Installer update/purge behavior on a real fleet; shell fixtures verify the paths and mocked service transitions.

## Local verification and release gate

The existing contract check sends mocked collector fixtures through the actual HTTP client and validates them against the target Cloud schema. It is distinct from a live ingest test. Before release, run it with the intended Cloud checkout and dependencies:

```sh
PULSE_CLOUD_CHECKOUT=/absolute/path/to/cloud bun run check:pulse
```

This slice's acceptance covers formatting, Go vet, race tests, installer fixtures, ShellCheck, and Fibel build/route checks. Consult the implementation check report for actual results; this file does not turn a listed command into a claimed pass. The contract check and remote live checks are not rerun as part of this offline slice.

## Known limits

- Each platform ingestor has a separate config/spool. PVE and PBS require `pulse-linux` for host telemetry. Docker uses Compose, not the native installer. Service profiles select units; explicit service TOML sections enable probes.
- Persistent spools have a bounded disk budget (64 MiB in examples). Parts expire after 23 hours; a full budget drops the oldest first. Quarantine and metadata need additional space. A spool is bound to one source and must not be shared between processes.
- 408, 429, 5xx, and transport failures retry. Redirects, authentication/disabled-source responses, and non-idempotency 409 responses pause delivery. Invalid items are split and quarantined; type/unit conflicts block the affected metric for one hour. Delivery is not a guarantee of lossless retention through arbitrarily long outages.
- Queue/delivery failure telemetry uses the same delivery path. A total outage requires Expected data and local logs. Shared-source freshness cannot detect every stopped ingestor; check heartbeat by ingestor too.
- State times mean last change/refresh, not continuous health or incident start. Removed resources can leave historical series and states. Unknown/missing backup ages or replication lag must not be treated as success or zero.
- PVE backup coverage reflects enabled jobs, while freshness is per visible PBS storage. Storage inaccessible to the elected node is not observed. PBS groups have bounded discovery/time budgets and a 2,000-group cap; inspect completeness/truncation before trusting totals.
- Root collectors use `ProtectSystem=full`; uptime uses DynamicUser and stricter filesystem protection. Permission-dependent optional probes can be unavailable or unknown. Native macOS restrictions remain unverified.
- cron invokes `once`, losing daemon cadences and in-memory suppression between invocations. Use supervised services for the measured volume assumptions.
- Alert defaults allow 20 rules/base, 100 keys/rule, and intervals of at least 300 seconds. SQL examples require validation on the deployed Pulse version; state schema/encoding is version-sensitive.

Next rollout steps are the [fleet guide](docs/en/rollout.md) and [alert templates](docs/en/alerts.md). Expand only after the pilot's service health, delivery, quota, and collector completeness are verified.
