---
title: Native installation
navTitle: Installation
section: Start
order: 30
description: Install and enroll native Pulse ingestors interactively or unattended.
tags: [install, automation]
updated: 2026-07-23
---

# Native installation

The native installer supports `pulse-linux`, `pulse-macos`, `pulse-proxmox`, `pulse-proxmox-backup-server`, and `pulse-uptime`. Docker uses the separate [Compose deployment](/en/ingestor-docker).

Release archives are available for Linux and macOS on `amd64` and `arm64`. The installer authenticates the release manifest with Cosign, verifies the archive SHA-256 checksum, and then installs the selected binary.

## Interactive enrollment

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --ingestor=linux
```

Replace `linux` with `macos`, `proxmox`, `pbs`, or `uptime`. Omit `--ingestor` to let interactive mode detect the local platform. Automatic detection selects the platform ingestor; select `uptime` explicitly.

The installer writes:

| Platform | Binary | Config | Default scheduler |
|---|---|---|---|
| Linux | `/usr/local/bin/pulse-linux` | `/etc/pulse/ingestor.toml` | systemd, cron fallback |
| macOS | `/usr/local/bin/pulse-macos` | `~/Library/Application Support/Pulse/ingestor.toml` | per-user launchd agent |
| Proxmox VE | `/usr/local/bin/pulse-proxmox` | `/etc/pulse/ingestor.toml` | systemd, cron fallback |
| PBS | `/usr/local/bin/pulse-proxmox-backup-server` | `/etc/pulse/ingestor.toml` | systemd, cron fallback |
| Uptime on Linux | `/usr/local/bin/pulse-uptime` | `/etc/pulse/ingestor.toml` | systemd, cron fallback |
| Uptime on macOS | `/usr/local/bin/pulse-uptime` | `~/Library/Application Support/Pulse/ingestor.toml` | per-user launchd agent |

## Unattended enrollment

`--unattended` never prompts. It validates all required input before changing system files and exits non-zero when enrollment cannot be completed.

### Use a complete config file

This is the recommended path for Ansible, cloud-init, configuration management, and secret mounts:

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  -o /tmp/pulse-install.sh

sudo sh /tmp/pulse-install.sh \
  --unattended \
  --ingestor=linux \
  --config-source=/run/secrets/pulse-ingestor.toml \
  --scheduler=systemd
```

The installer copies the source file to `/etc/pulse/ingestor.toml` with mode `0600`. It does not modify the source file.

### Use provisioning variables

Use a token file when possible. Raw tokens are intentionally not accepted as command-line flags.

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  -o /tmp/pulse-install.sh

sudo env \
  PULSE_INGEST_URL="https://pulse.example.com/api/pulse/ingest" \
  PULSE_INGEST_TOKEN_FILE="/run/secrets/pulse-token" \
  PULSE_ENTITY_ID="server-01" \
  PULSE_ENTITY_LABEL="Server 01" \
  PULSE_INTERVAL_SECONDS=60 \
  PULSE_DIMENSIONS="environment=production,region=eu-central" \
  sh /tmp/pulse-install.sh \
    --unattended \
    --ingestor=linux \
    --scheduler=systemd
```

`PULSE_INGEST_TOKEN` is supported when the provisioning system cannot provide a file. Ensure the environment is not printed by the provisioning system.

## Installer options

| Option | Values | Purpose |
|---|---|---|
| `--ingestor` | `auto`, `linux`, `macos`, `proxmox`, `pbs`, `uptime` | Select the native ingestor. Unattended mode requires an explicit value. |
| `--scheduler` | `auto`, `systemd`, `cron`, `launchd`, `none` | Select scheduling. |
| `--config-source` | file path | Install an existing complete TOML config. |
| `--config-path` | file path | Override the destination config path. |
| `--prefix` | directory | Override `/usr/local/bin`. Use `--scheduler=none` with non-system prefixes. |
| `--version` | release version | Install a fixed release instead of `latest`. |
| `--reconfigure` | flag | Replace an existing config. Without it, upgrades preserve the config. |
| `--uninstall` | flag | Remove the selected binary and managed scheduler. |
| `--unattended` | flag | Disable all prompts. |
| `--yes` | flag | Accept the final confirmation. |

## Enrollment guarantees

- The scheduler is enabled only after local collection and the first authenticated push succeed.
- Re-running the installer upgrades the binary and preserves the existing config by default.
- Tokens are not printed or passed as binary arguments.
- Removing an ingestor preserves its config unless the administrator removes it separately.
- Docker is never installed or modified by this script.
