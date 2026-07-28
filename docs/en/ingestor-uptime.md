---
title: Uptime ingestor
navTitle: Uptime
section: Ingestors
order: 160
description: Monitor internet connectivity and named ICMP, DNS, TCP, and HTTP endpoints.
tags: [uptime, internet, ingestor]
updated: 2026-07-23
---

# Uptime ingestor

`pulse-uptime` checks internet connectivity and named endpoints from one Linux or macOS probe. Each run sends availability, duration, current diagnostics, HTTP status, and TLS certificate lifetime to Pulse.

## Requirements

- Linux or macOS on `amd64` or `arm64`;
- `curl`, `tar`, Cosign, and permission to install `/usr/local/bin/pulse-uptime`;
- outbound access to the configured targets;
- a Pulse ingest URL and token.

The system `ping` command is required only for ICMP checks. DNS, TCP, and HTTP checks continue when `ping` is unavailable.

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
2. writes a protected config;
3. runs all checks locally;
4. sends the first Pulse batch;
5. enables systemd, cron, or a per-user LaunchAgent.

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
sudo pulse-uptime --config /etc/pulse/ingestor.toml --local --pretty once
systemctl status pulse-uptime.timer
journalctl -u pulse-uptime.service -n 100
```

macOS:

```sh
pulse-uptime \
  --config "$HOME/Library/Application Support/Pulse/ingestor.toml" \
  --local --pretty once

launchctl print "gui/$(id -u)/dev.pulse.pulse-uptime"
tail -n 100 "$HOME/Library/Logs/Pulse/pulse-uptime.err.log"
```

Update by running the installer again. Remove the binary and managed scheduler while preserving the config:

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --uninstall --ingestor=uptime
```

## Default internet checks

`enable_defaults = true` creates eight endpoint resources:

| Check | Type | Target | Expected result |
|---|---|---|---|
| Cloudflare ICMP | ICMP | `1.1.1.1` | Ping succeeds |
| Google ICMP | ICMP | `8.8.8.8` | Ping succeeds |
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

## Collected data

Each target becomes a separate `uptime-endpoint` resource. This keeps dashboards stable when an endpoint address changes but its configured target ID remains the same.

See the [Uptime module](/en/module-uptime) for every metric, state, dimension, default, and failure behavior.
