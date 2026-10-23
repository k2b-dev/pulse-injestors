---
title: Roll out to a fleet
navTitle: Fleet rollout
section: Start
order: 35
description: Size a Pulse base, enroll hosts in stages, and verify updates and removal.
tags: [rollout, quotas, operations]
updated: 2026-10-23
---

# Roll out to a fleet

Start with one host of each type. Measure its normal traffic and first inventory before enrolling a group. Use one HTTP ingest source per host, with separate configs and spools for each ingestor on that host.

## Size the base

A base defaults to **2,000 values/minute**, **50,000 metric series** (at most **10,000 per metric**), **2,000 event kinds**, and **2,000 state keys**. Samples, states, and events all consume the values budget. Cloud administrators can raise the values/minute quota per base. Group hosts by operational ownership and access needs, then size each base from its fleet.

These are measured averages after cadence tuning, not peak or guaranteed rates:

| Host or collector | Values/min | Add separately |
|---|---:|---|
| Linux server VM, `pulse-linux` | ~50 | Optional service probes |
| Docker, 70 containers, `pulse-docker` | ~173 | ~54 for its Linux host |
| Proxmox elected node, cluster view with ~58 guests | ~140 | Linux host monitoring on every node |
| Other Proxmox nodes | ~5 each | Linux host monitoring |
| PBS, `pulse-proxmox-backup-server` | ~10 | Linux host monitoring |
| Base services (database or broker) | ~2–10 per VM | Linux host monitoring; depends on configured probes |
| Mac workstation | ~5 | Uptime targets if configured |
| Linux desktop profile | ~12 | Uptime targets if configured |

### Worked fleet example

Assume 25 server VMs, one Docker host with 70 containers, a five-node PVE cluster with 58 guests, two PBS hosts, and eight workstations split equally between Macs and Linux desktops. The 25 VMs are monitored inside their OS as well as through PVE; both views are intentional.

| Group | Calculation | Values/min |
|---|---|---:|
| Server VMs | 25 × 50 | 1,250 |
| Docker host | 173 + 54 | 227 |
| Five PVE nodes | 5 × 50 host + 140 elected + 4 × 5 other | 410 |
| Two PBS hosts | 2 × (50 host + 10 PBS) | 120 |
| Eight workstations | 4 × 5 + 4 × 12 | 68 |
| **Total: 41 hosts/sources** | | **2,075** |

Twice the average is **4,150 values/minute**. Request **4,500 values/minute** as a starting quota; the 2,000 default is too small. Add service probes and the two uptime probe hosts/target sets before doubling the final total. For example, ten VMs with 10 extra service values/min add 100 to the baseline. Probe traffic depends on the configured targets and must be measured.

Series cannot be derived from values/minute: a daily metric is still a series. A planning estimate of 200 series per Linux server-class host (33 × 200), 25 per container (70 × 25), 15 per guest (58 × 15), 200 extra per PBS (2 × 200), and 100 per workstation (8 × 100) gives **10,420 series**. These are sizing allowances, not measured cardinalities; container allowances include daemon, image, and Compose overhead. Budget roughly **21,000 series** with 2× headroom and verify after the pilot. Also check the 10,000-per-metric cap. Package inventory uses repeated state keys with dimensions; 850 package rows are not 850 different state keys or metric series.

The first inventory sends roughly **850 states per Linux host**; unchanged software states refresh weekly. `once` runs all groups, and enrollment uses a temporary spool, so the first daemon cycle sends its own initial inventory again. Stage starts several minutes apart and wait for queues to drain. Restarting with the persistent spool retains the state cache; deleting it causes another inventory burst. Headroom for averages does not guarantee room for simultaneous starts or weekly refreshes.

## Create one source per host

Select the intended CLI profile and base explicitly. Replace example names and IDs with the values returned by your Cloud. Create a new token file; the command refuses to overwrite a file and writes mode `0600` without printing the token.

```sh
cld --profile fleet-admin pulse sources create --base Base01 \
  --name server-01 --kind http_ingest
cld --profile fleet-admin pulse source-tokens create Source01 --base Base01 \
  --name enrollment --output-file /secure/new-server-01.token
```

Transfer that private file through your provisioning secret mechanism to `/run/secrets/pulse-token` on its host. Use the source's ingest URL from Pulse. Never put the token in shell arguments or copy it from terminal output. Several ingestors on one host can share the source; each needs its own token-file copy or a deliberately shared private file and its own spool.

Set **Expected data** after the first delivery:

| Host type | Setting | Reason |
|---|---|---|
| Always-on server, PVE, PBS, Docker, probe host | `expectEverySeconds = 180` | Allows normal delay for a 60-second heartbeat |
| Mac or Linux workstation | `expectDailyBy = "18:00"` | Daily contact by a chosen time in the base timezone; permits sleep |

```sh
cld --profile fleet-admin pulse sources update --base Base01 Source01 --expect-every 180
# For a workstation source instead:
cld --profile fleet-admin pulse sources update --base Base01 Laptop01 --expect-daily-by 18:00
```

Choose a deadline that matches actual workstation use. Pulse checks freshness every minute and notifies base admins; it uses the newest data timestamp. Daily contact does not prove complete daily telemetry. A second ingestor can keep a shared source fresh while another stops, so also inspect heartbeat by ingestor below.

## Install the right ingestors

Download `install.sh` from the chosen release and follow [native prerequisites](/en/installation). Pin the same tested version for a rollout group. For a Linux server:

```sh
sudo env PULSE_INGEST_URL=https://pulse.example.test/api/pulse/ingest \
  PULSE_INGEST_TOKEN_FILE=/run/secrets/pulse-token \
  PULSE_RESOURCE_ID=server-01 \
  sh ./install.sh --unattended --ingestor=linux --linux-profile=server \
    --scheduler=systemd --version=vX.Y.Z
```

Use the same provisioning variables on each invocation, with that host's identity. For Linux workstations, also set `PULSE_INTERVAL_SECONDS=300` to match the measured desktop volume; the profile alone does not change the runner interval. Run only the commands matching the host; the table shows installer arguments after `sh ./install.sh --unattended --version=vX.Y.Z`.

| Host | Installer arguments / additional deployment |
|---|---|
| Every Linux server VM | `--ingestor=linux --linux-profile=server --scheduler=systemd` |
| Linux workstation | `--ingestor=linux --linux-profile=desktop --scheduler=systemd` |
| Every PVE node | `--ingestor=linux --linux-profile=proxmox-node --scheduler=systemd`, then `--ingestor=proxmox --scheduler=systemd` |
| Every PBS host | `--ingestor=linux --linux-profile=pbs --scheduler=systemd`, then `--ingestor=pbs --scheduler=systemd` |
| Docker host | `--ingestor=linux --linux-profile=docker-host --scheduler=systemd`, plus `pulse-docker` through [Compose](/en/ingestor-docker#deploy-with-docker-compose) |
| Mac workstation | `--ingestor=macos --scheduler=launchd`, as the logged-in user, without sudo |
| External probe and separate LAN probe | Linux server command above, then `--ingestor=uptime --scheduler=systemd --config-source=/run/secrets/pulse-uptime.toml` on each |

`pulse-proxmox` and `pulse-proxmox-backup-server` collect platform data only. Install `pulse-linux` on every Linux host, including those platform nodes. Keep `resource.id` identical across a host's configs. The PVE collector elects one online node for the cluster view; install it on all nodes so reporting can move after failure.

For the uptime config, define explicit public targets on the external host and internal targets on the LAN host; use distinct probe IDs such as `probe-external` and `probe-lan`. See [uptime configuration](/en/ingestor-uptime). A supplied config must reference a durable token file; the installer does not copy that referenced file. For Docker, provision the token as the Compose secret and pin the image version. The native installer never manages Docker.

Service profiles `postgres`, `pg-edge`, `nats`, `nats-edge`, `valkey`, and `clickhouse` select systemd units. Database/broker connections require their own TOML sections; use a complete `--config-source` with `[linux] profile` for those hosts. Do not combine `--config-source` with `--linux-profile`.

## Roll out in stages and verify

1. Enroll one server. Check the first delivery, active service, Expected data, and an emptying spool.
2. Pilot one host of each remaining type. Inspect collector failures and availability, not only HTTP acceptance. For PVE, verify the elected reporter and guest coverage; for PBS, verify `pbs.groups.complete=true`.
3. Measure at least a full day to include slow collectors. Compare series counts and per-minute ingestion with the sizing allowances.
4. Enroll one group at a time with staggered starts. Pause if Pulse returns 429 or queues grow; correct quota or cadence before adding hosts.
5. Enable the [priority alert rules](/en/alerts) after checking each query against real resource identities.

This SQL has not been executed against a live base. It shows the latest heartbeat for every observed resource and ingestor in the last ten minutes:

```sql
SELECT r.resource_key,
       simpleJSONExtractString(r.dimensions, 'ingestor') AS ingestor,
       max(s.ts) AS last_seen, count() AS samples
FROM samples s JOIN series r ON r.series_id = s.series_id
WHERE r.metric = 'ingestor.heartbeat'
  AND s.ts >= now() - INTERVAL 10 MINUTE AND s.ts < now()
GROUP BY r.resource_key, ingestor
ORDER BY r.resource_key, ingestor
```

Compare the result with your enrollment list: missing hosts are absent from this query. A heartbeat with an old sample timestamp is not fresh merely because a queue delivered it now. Inspect `ingestor.spool.oldest_age`, delivery failures, and logs alongside the result. See [SQL examples](/en/dashboards) for capacity and backup checks.

## Update, roll back, or remove

Save a private copy of each config and record the installed version. Update the pilot with the same ingestor, config path, and pinned release:

```sh
sudo sh ./install.sh --unattended --ingestor=linux --version=vX.Y.Z
```

Reruns replace the binary, preserve config/token, skip the enrollment push, and restart the service. systemd readiness is checked for up to 60 seconds; a timeout exits nonzero with the log command. Active means the service started, not that Pulse accepted its data. Verify fresh heartbeats and queue health before updating the next group. Repeat separately for each binary on multi-ingestor hosts. Use `--reconfigure` only to replace configuration deliberately.

To roll back, run the installer with the previously recorded release version. Restore the matching saved config if the new version changed it, then restart and verify. Do not assume an older binary understands a newer config or spool format; test that pair on the pilot and retain a private spool copy before attempting an incompatible downgrade. Do not purge a queue to hide delivery failures. For Docker, restore the previous pinned image and config, then `docker compose up -d` with the existing volume.

```sh
sudo sh ./install.sh --unattended --uninstall --ingestor=linux
# Permanently discard queued data/state cache and remove token files:
sudo sh ./install.sh --unattended --uninstall --purge --ingestor=linux
```

Without `--purge`, config, tokens, and spool stay. With it, TOML stays but the configured spool and managed/configured token files are deleted. Purge accepts plain double-quoted absolute `[pulse]` paths; ambiguous or unsafe paths fail before removal. A shared token file must not be purged while another ingestor uses it. Repeat removal for each installed binary; Docker removal is separate. Retire the host's source/expectation and revoke tokens in Pulse when it leaves monitoring. Historical data in Pulse is not removed by the installer.
