---
title: Native installation
navTitle: Installation
section: Start
order: 30
description: Install and enroll native Pulse ingestors interactively or unattended.
tags: [install, automation]
updated: 2026-10-23
---

# Native installation

The native installer supports `pulse-linux`, `pulse-macos`, `pulse-proxmox`, `pulse-proxmox-backup-server`, and `pulse-uptime`. Docker uses the separate [Compose deployment](/en/ingestor-docker).

Release archives are available for Linux and macOS on `amd64` and `arm64`. The installer authenticates the release manifest with Cosign, verifies the archive SHA-256 checksum, and then installs the selected binary.

The systemd service requires systemd 248 or newer for credential loading.

## Interactive enrollment

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --ingestor=linux
```

Replace `linux` with `macos`, `proxmox`, `pbs`, or `uptime`. Omit `--ingestor` to let interactive mode detect the local platform. Automatic detection selects the platform ingestor; select `uptime` explicitly.

The installer writes:

| Platform | Binary | Config | Service manager |
|---|---|---|---|
| Linux | `/usr/local/bin/pulse-linux` | `/etc/pulse/pulse-linux.toml` | systemd service, cron fallback |
| macOS | `/usr/local/bin/pulse-macos` | `~/Library/Application Support/Pulse/pulse-macos.toml` | per-user LaunchAgent |
| Proxmox VE | `/usr/local/bin/pulse-proxmox` | `/etc/pulse/pulse-proxmox.toml` | systemd service, cron fallback |
| PBS | `/usr/local/bin/pulse-proxmox-backup-server` | `/etc/pulse/pulse-proxmox-backup-server.toml` | systemd service, cron fallback |
| Uptime on Linux | `/usr/local/bin/pulse-uptime` | `/etc/pulse/pulse-uptime.toml` | systemd service, cron fallback |
| Uptime on macOS | `/usr/local/bin/pulse-uptime` | `~/Library/Application Support/Pulse/pulse-uptime.toml` | per-user LaunchAgent |

The generated config references a separate mode-`0600` token file: replace `.toml` with `.token` in the config path. On systemd, the service loads that file through `LoadCredential=pulse-ingest-token:...` and sets `PULSE_INGEST_TOKEN_FILE=%d/pulse-ingest-token`. The installer does not put a token value in generated TOML.

For every ingestor, the systemd unit also uses `LoadCredential=pulse-config:<config path>` and `ExecStart=... --config %d/pulse-config run`. This lets `pulse-uptime` read the protected config with `DynamicUser=yes`. systemd copies the config at service start, so edits take effect after `systemctl restart <binary>.service`.

Several ingestors can run on one host with separate configs and spool directories, even when they share a source token. Proxmox and PBS already collect Linux host signals; running `pulse-linux` alongside them can duplicate those observations.

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

The installer copies the source file to `/etc/pulse/pulse-linux.toml` with mode `0600`. It does not modify the source file or copy a token referenced by it. Set `ingest_token_file` to a durable private file readable by the service. To use the installer's systemd credential wiring, provision the token at `/etc/pulse/pulse-linux.token` before installation. Avoid inline tokens: a supplied TOML file is copied unchanged.

### Use provisioning variables

Use a token file when possible. Raw tokens are intentionally not accepted as command-line flags.

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  -o /tmp/pulse-install.sh

sudo env \
  PULSE_INGEST_URL="https://pulse.example.com/api/pulse/ingest" \
  PULSE_INGEST_TOKEN_FILE="/run/secrets/pulse-token" \
  PULSE_RESOURCE_ID="server-01" \
  PULSE_RESOURCE_LABEL="Server 01" \
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
| `--scheduler` | `auto`, `systemd`, `cron`, `launchd`, `none` | Select the service manager or cron fallback. |
| `--config-source` | file path | Install an existing complete TOML config. |
| `--config-path` | file path | Override the config path; its `.token` neighbor holds the generated token. |
| `--prefix` | directory | Override `/usr/local/bin`. Use `--scheduler=none` with non-system prefixes. |
| `--version` | release version | Install a fixed release instead of `latest`. |
| `--reconfigure` | flag | Replace an existing config. Without it, upgrades preserve the config. |
| `--uninstall` | flag | Remove the selected binary and managed scheduler. |
| `--unattended` | flag | Disable all prompts. |
| `--yes` | flag | Accept the final confirmation. |

## Enrollment guarantees

- The service or cron schedule is enabled only after local collection and the first authenticated push succeed.
- Re-running the installer upgrades the binary and preserves the existing config by default.
- Tokens are not printed or passed as binary arguments.
- Removing an ingestor preserves its config, token, and spool unless the administrator removes it separately.
- Docker is never installed or modified by this script.

## Service behavior

systemd and launchd supervise the long-running `run` command. Collection queues data every interval; delivery runs independently. Reinstalling with systemd disables and removes the old `<binary>.timer` and starts `<binary>.service`.

cron is a fallback for systems without systemd. It runs `once` per scheduled interval. Each invocation collects all enabled collectors within the cycle budget, then delivers with bounded retries. Collector cadences and the in-memory one-hour metric block do not survive between invocations.

For an older installation using `ingestor.toml`, pass its path with `--config-path` when upgrading to preserve it. The new default paths do not automatically migrate that file.

See [Operations](/en/operations) for service status, logs, and hardening.
