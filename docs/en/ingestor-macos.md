---
title: macOS ingestor
navTitle: macOS
section: Ingestors
order: 130
description: Enroll and monitor MacBooks and Mac desktops with a per-user LaunchAgent.
tags: [macos, ingestor]
updated: 2026-07-23
---

# macOS ingestor

`pulse-macos` monitors a MacBook or Mac desktop. The installer schedules it as the current user so Homebrew and user services remain accessible.

## Requirements

- macOS on Apple Silicon or Intel;
- administrator access for installing `/usr/local/bin/pulse-macos`;
- a Pulse ingest URL and token;
- Cosign.

Install Cosign with Homebrew:

```sh
brew install cosign
```

Run the installer as the user whose Homebrew installation and services should be monitored. Do not prefix the complete installer command with `sudo`; it requests elevation only when installing the binary.

## Enroll interactively

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --ingestor=macos
```

The installer writes:

- binary: `/usr/local/bin/pulse-macos`;
- config: `~/Library/Application Support/Pulse/ingestor.toml`;
- LaunchAgent: `~/Library/LaunchAgents/dev.pulse.pulse-macos.plist`;
- logs: `~/Library/Logs/Pulse/`.

It performs a local collection and first Pulse push before loading the LaunchAgent.

## Enroll unattended

Run unattended enrollment in the target user session:

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  -o /tmp/pulse-install.sh

sh /tmp/pulse-install.sh \
  --unattended \
  --ingestor=macos \
  --config-source="$HOME/.config/pulse/enrollment.toml" \
  --scheduler=launchd
```

The source config must be readable by that user. The installer copies it to the protected application-support path.

## Verify and operate

```sh
pulse-macos \
  --config "$HOME/Library/Application Support/Pulse/ingestor.toml" \
  --local --pretty once

launchctl print "gui/$(id -u)/dev.pulse.pulse-macos"
tail -n 100 "$HOME/Library/Logs/Pulse/pulse-macos.err.log"
```

Update by running the interactive installer again. Remove the binary and LaunchAgent while preserving the config:

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --uninstall --ingestor=macos
```

## Main settings

```toml
[macos]
enable_homebrew = true
homebrew_timeout_seconds = 20
enable_software_update = false
software_update_timeout_seconds = 60
enable_system_profiler = true
system_profiler_timeout_seconds = 10
```

Software update checks are disabled by default because they may take longer. Enable them when update visibility is worth the additional command cost.

## Collected data

- macOS version, build, CPU cores, load, uptime, memory, and filesystems;
- battery capacity, health, cycle count, charging, and external-power state;
- Homebrew installation, outdated packages, and services;
- optional software update counts;
- GPU and display inventory from `system_profiler`;
- available temperature and power data;
- configured custom scripts.

Detailed CPU and GPU power data may require privileged `powermetrics`. The default per-user LaunchAgent reports a clear unavailable state when macOS does not grant access.

The [macOS module reference](/en/module-macos) lists every metric, state, event, dimension, type, unit, and example.
