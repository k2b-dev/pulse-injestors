---
title: Uptime ingestor
navTitle: Uptime
section: Ingestors
order: 160
description: Monitor endpoints, public ports, DNS records, TLS certificates, and mail blocklists.
tags: [uptime, internet, ingestor]
updated: 2026-10-29
---

# Uptime ingestor

`pulse-uptime` checks internet connectivity and named endpoints from one Linux or macOS probe. Checks report success, confirmed findings, and execution errors separately. Target cadences keep office pings frequent while public port scans run daily.

## Requirements

- Linux or macOS on `amd64` or `arm64`;
- `curl`, `tar`, Cosign, and permission to install `/usr/local/bin/pulse-uptime`;
- outbound access to the configured targets;
- a Pulse ingest URL and token.

Use systemd or launchd when targets have different cadences. Cron invokes `once`, which checks every target on every invocation; it does not retain daily or hourly schedules. Do not use the minute cron fallback with ports, DNSBL, or other infrequent targets. On systems without systemd, supervise `pulse-uptime run` with your service manager.

ICMP checks require iputils `ping` on Linux or the system `ping` on macOS; BusyBox ping is unsupported. macOS ICMP targets must resolve to IPv4 because this collector does not invoke `ping6`. DNS, TCP, and HTTP checks continue when `ping` is unavailable.

## Enroll interactively

On Linux:

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --ingestor=uptime
```

On macOS, run the same command as the user who should own the LaunchAgent. Do not prefix the complete command with `sudo`; the installer requests elevation only for the binary installation.

The installer asks for the Pulse source, token, stable probe ID, label, dimensions, and interval. It then:

1. installs the signed `pulse-uptime` release;
2. writes a protected config and a separate token file;
3. runs all checks locally;
4. sends the first Pulse batch;
5. starts a systemd service or a per-user LaunchAgent, with cron as a Linux fallback.

The default config is `/etc/pulse/pulse-uptime.toml` on Linux and `~/Library/Application Support/Pulse/pulse-uptime.toml` on macOS. The token uses the same path with `.token` instead of `.toml`. systemd runs the service with `DynamicUser=yes` and `ProtectSystem=strict`; launchd uses `KeepAlive`. Both run `run`.

## Enroll unattended

Prepare a complete TOML file and run:

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  -o /tmp/pulse-install.sh

sudo sh /tmp/pulse-install.sh \
  --unattended \
  --ingestor=uptime \
  --config-source=/run/secrets/pulse-uptime.toml \
  --scheduler=systemd
```

On macOS, omit `sudo` and use `--scheduler=launchd`. See [Native installation](/en/installation#unattended-enrollment) for token-file enrollment and all installer options.

## Verify and operate

Linux:

```sh
sudo pulse-uptime --config /etc/pulse/pulse-uptime.toml --local --pretty once
systemctl status pulse-uptime.service
journalctl -u pulse-uptime.service -n 100
```

macOS:

```sh
pulse-uptime \
  --config "$HOME/Library/Application Support/Pulse/pulse-uptime.toml" \
  --local --pretty once

launchctl print "gui/$(id -u)/dev.pulse.pulse-uptime"
tail -n 100 "$HOME/Library/Logs/Pulse/pulse-uptime.err.log"
```

Update by running the installer again. Remove the binary and managed scheduler while preserving the config, token, and spool:

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --uninstall --ingestor=uptime
```

## Default internet checks

`enable_defaults = true` creates eight endpoint resources:

| Check | Type | Target | Expected result |
|---|---|---|---|
| Cloudflare ICMP | ICMP | `1.1.1.1` | At least one reply per round |
| Google ICMP | ICMP | `8.8.8.8` | At least one reply per round |
| Cloudflare DNS | DNS | `cloudflare.com` | At least one address |
| Google DNS | DNS | `google.com` | At least one address |
| Cloudflare HTTPS TCP | TCP | `1.1.1.1:443` | Connection succeeds |
| Google DNS TCP | TCP | `8.8.8.8:53` | Connection succeeds |
| Cloudflare HTTP | HTTP | `https://www.cloudflare.com/cdn-cgi/trace` | Status `200` |
| Google HTTP | HTTP | `https://www.google.com/generate_204` | Status `204` |

Disable the profile when the probe should check only explicitly configured endpoints.

## Configure custom targets

```toml
[uptime]
enable_defaults = false
concurrency = 4
timeout_seconds = 5

[[uptime.target]]
id = "gateway"
label = "Office gateway"
kind = "icmp"
address = "192.0.2.1"

[[uptime.target]]
id = "internal-dns"
label = "Internal DNS"
kind = "dns"
address = "service.example.internal"

[[uptime.target]]
id = "postgres"
label = "PostgreSQL"
kind = "tcp"
address = "database.example.internal:5432"
timeout_seconds = 3

[[uptime.target]]
id = "pulse-api"
label = "Pulse API"
kind = "http"
address = "https://pulse.example.com/health"
expected_status = 200
```

Target IDs may contain letters, digits, dots, underscores, and hyphens. They become part of the stable endpoint resource ID, so do not rename them unless the monitored logical endpoint changes.

HTTP checks follow at most three redirects. A missing `expected_status` accepts status codes from `200` through `399`. Do not put credentials or secret query parameters in target URLs; the target is sent to Pulse as a current state.

## Replacing SmokePing

Map SmokePing **probes per round** (`pings`) to `count` and **step** to `cadence_seconds`. Pulse defaults to 20 requests, 500 ms apart, every 60 seconds. A healthy round takes about ten seconds; its default timeout is 15 seconds. Keep `[runner].interval_seconds = 60` for this cadence. Targets run concurrently up to `[uptime].concurrency` (default 4).

On a Raspberry Pi, use **Raspberry Pi OS 64-bit (arm64)**. The existing installer supports arm64 on Pi, not 32-bit ARM. Install `iputils-ping` if missing (`sudo apt install iputils-ping`), then use the Linux enrollment command above. The ingestor calls system ping and needs no raw-socket capability on its Go binary. Keep the installer's systemd service and persistent spool. Its unprivileged ping sockets require `net.ipv4.ping_group_range` to allow the service user; check this setting if local ping works but the service reports a permission error.

In `/etc/pulse/pulse-uptime.toml`, set `enable_defaults = false` under `[uptime]` if these should be your only targets, then add:

```toml
[[uptime.target]]
id = "lan-router"
label = "LAN router"
kind = "icmp"
address = "router.example.lan"
count = 20
interval = "500ms"
cadence_seconds = 60
loss_threshold = 25 # Optional: failed at 25% loss or above; default 100%.

[[uptime.target]]
id = "internet-icmp"
label = "Cloudflare ICMP"
kind = "icmp"
address = "1.1.1.1"
count = 20
interval = "500ms"
cadence_seconds = 60
```

Replace the LAN hostname, run `pulse-uptime --config /etc/pulse/pulse-uptime.toml --local --pretty once`, then restart `pulse-uptime.service`. Verify loss and RTT metrics in Pulse. An unavailable ping command is an execution error, not 100% loss. See the [module reference](/en/module-uptime) for timeout and threshold limits and [alert templates](/en/alerts) for sustained packet loss.

Use `uptime.icmp.rtt.median` for latency panels: ICMP no longer emits command wall time as `uptime.check.duration`. Each measured round sends loss, min/median/max/population-standard-deviation RTT, and success. Replies later than two seconds count as lost. With total loss, RTT values are absent. This retains a compact latency distribution per round; it does not store all 20 packet RTTs.

These queries use the Pulse read views. Bind `:from` and `:to` to the panel range. For loss and median-RTT time series, keep the metrics in separate panels because their units differ:

```sql
SELECT s.ts, r.resource_key, r.dimensions, r.metric, r.unit, s.value
FROM samples s JOIN series r ON r.series_id = s.series_id
WHERE r.metric IN ('uptime.icmp.loss', 'uptime.icmp.rtt.median')
  AND s.ts >= :from AND s.ts < :to
ORDER BY s.ts, r.resource_key, r.metric
```

For a five-minute percentile panel:

```sql
SELECT toStartOfInterval(s.ts, INTERVAL 5 MINUTE) AS ts,
       r.resource_key, r.dimensions, r.unit,
       quantileExact(0.5)(s.value) AS p50,
       quantileExact(0.95)(s.value) AS p95,
       quantileExact(0.99)(s.value) AS p99
FROM samples s JOIN series r ON r.series_id = s.series_id
WHERE r.metric = 'uptime.icmp.rtt.median'
  AND s.ts >= :from AND s.ts < :to
GROUP BY ts, r.resource_key, r.dimensions, r.unit
ORDER BY ts, r.resource_key
```

These are percentiles of **round medians**, not individual packets. A default five-minute bucket has about five values, so p95 and p99 often equal its largest median. Keep loss visible alongside the panel; total-loss rounds contribute no latency sample. Do not fill missing RTTs with zero.

## Collected data

Each target becomes a separate `uptime-endpoint` resource. This keeps dashboards stable when an endpoint address changes but its configured target ID remains the same.

See the [Uptime module](/en/module-uptime) for every metric, state, dimension, default, and failure behavior.

## Replace script checks

| Previous script check | Uptime target kind | Default cadence |
|---|---|---|
| Public TCP ports against an expected list | `ports` | Daily |
| A, AAAA, MX, NS, SPF/TXT, DMARC verification | `dns_records` | Hourly |
| Certificate verification and expiry | `tls` | Hourly |
| Mail IPv4 against DNSBLs, with reference queries | `dnsbl` | Hourly |
| LAN device ping | `icmp` | Every minute |

Use `cadence_seconds = 86400` for DNS or TLS checks when retaining the old daily schedule. Set Pulse alerts for both `uptime.check.result=failed` and `uptime.check.result=error`. An exhausted check budget, empty DNS answer, or failed DNSBL reference is an error, never a pass.

For `ports`, `rate_per_second` defaults to `200` and must be positive; each scan also limits concurrent connections to 64. Individual connect timeouts mean filtered and count as not open: unexpected open ports and expected ports that are closed or filtered produce `failed`. A scan with no open ports and no refusals produces `error`, because it cannot distinguish a host that is down from one with every port filtered. Completed scans without scan errors report both `uptime.ports.open` and `uptime.ports.filtered`.

The complete example below includes all four public check families plus LAN ping. Replace the example addresses, names, resolver, source URL, and token path before use. Configure DNSBL queries through a resolver accepted by each list provider. Their reference checks expose blocked or unusable resolvers.

```toml
[pulse]
ingest_url = "https://pulse.example.com/api/pulse/ingest"
# One line with the source token, mode 0600. Under systemd the service
# loads it as a credential instead; see the installation guide.
ingest_token_file = "/etc/pulse/pulse-uptime.token"
# Explicit 64 MiB disk budget; size for measured output and tolerated outage.
spool_dir = "/var/lib/pulse/pulse-uptime"
spool_max_bytes = 67108864

[resource]
id = "office-berlin"
label = "Berlin office"

[dimensions]
environment = "production"
site = "berlin"

[runner]
interval_seconds = 60
collector_timeout_seconds = 30

[http]
timeout_seconds = 10
max_retries = 3
initial_backoff_ms = 500

[uptime]
enable_defaults = false
concurrency = 4
timeout_seconds = 5

# Public port inventory: every port in the range is checked.
[[uptime.target]]
id = "public-ports"
label = "Public server ports"
kind = "ports"
rate_per_second = 200
address = "192.0.2.10" # IPv6: "[2001:db8::10]"
expected = [22, 443]
range = "1-10000"
cadence_seconds = 86400
timeout_seconds = 900

[[uptime.target]]
id = "domain-records"
label = "Domain DNS records"
kind = "dns_records"
name = "example.com"
# Omit resolver to use /etc/resolv.conf nameservers.
resolver = "192.0.2.53:53"
cadence_seconds = 3600
timeout_seconds = 30
records = [
  { type = "A", expect = ["192.0.2.10"] },
  { type = "AAAA", expect = ["2001:db8::10"] },
  { type = "MX", expect = ["10 mail.example.com."] },
  { type = "NS", expect = ["ns1.example.com.", "ns2.example.com."] },
  { type = "TXT", contains = "v=spf1" },
]

[[uptime.target]]
id = "dmarc"
label = "DMARC policy"
kind = "dns_records"
name = "_dmarc.example.com"
resolver = "192.0.2.53:53"
records = [{ type = "TXT", contains = "v=DMARC1; p=reject" }]

[[uptime.target]]
id = "public-certificate"
label = "Public HTTPS certificate"
kind = "tls"
address = "example.com:443"
warn_days = 14
cadence_seconds = 3600
# For an IP address or a private CA:
# server_name = "example.com"
# ca_file = "/etc/pulse/private-ca.pem"

[[uptime.target]]
id = "mail-blocklists"
label = "Mail IP blocklists"
kind = "dnsbl"
address = "192.0.2.25"
resolver = "192.0.2.53:53"
lists = ["bl.spamcop.net", "b.barracudacentral.org", "dnsbl-1.uceprotect.net", "psbl.surriel.com"]
cadence_seconds = 3600
timeout_seconds = 30

[[uptime.target]]
id = "office-printer"
label = "Office printer"
kind = "icmp"
address = "192.0.2.50"
cadence_seconds = 60
```

`dns_records` compares full sets for `expect`. DNS names ignore case and trailing dots. TXT `contains` is case-sensitive and matches any TXT value. DMARC uses its own target because it has a different DNS name. TLS on a service that needs STARTTLS is not supported; use a direct TLS port.

## Detect a stopped prober

The ingestor already sends `ingestor.heartbeat`. Configure **Expected data** on the uptime source in Pulse so missing heartbeat data alerts when the prober stops or cannot deliver. This replaces the external dead-man's-switch service; an extra self-ping target is unnecessary. Allow for the collection interval and delivery retries when choosing the expected-data window. Long scans do not delay heartbeats.

Minute targets and heartbeats continue while a separate lane runs slower checks. Each slow collector queues its result when it finishes; a port scan has a default budget of fifteen minutes. `once` runs every cadence for verification. After editing the config, run `--local --pretty once`, inspect every target's result, then restart the supervised ingestor.
