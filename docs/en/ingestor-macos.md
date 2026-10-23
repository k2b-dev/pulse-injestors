---
title: macOS ingestor
navTitle: macOS
section: Ingestors
order: 130
description: Enroll and monitor MacBooks and Mac desktops with a per-user LaunchAgent.
tags: [macos, ingestor]
updated: 2026-10-23
---

# macOS ingestor

`pulse-macos` monitors a MacBook or Mac desktop. The installer runs it as the current user so the user’s Homebrew installation and applications remain accessible.

## Requirements

- macOS on Apple Silicon or Intel;
- administrator access for installing `/usr/local/bin/pulse-macos`;
- a Pulse ingest URL and token;
- Cosign.

Install Cosign with Homebrew:

```sh
brew install cosign
```

Run the installer as the user whose Homebrew installation and applications should be monitored. Do not prefix the complete installer command with `sudo`; it requests elevation only when installing the binary.

## Enroll interactively

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --ingestor=macos
```

The installer writes:

- binary: `/usr/local/bin/pulse-macos`;
- config: `~/Library/Application Support/Pulse/pulse-macos.toml`;
- token: `~/Library/Application Support/Pulse/pulse-macos.token` (mode `0600`);
- LaunchAgent: `~/Library/LaunchAgents/dev.pulse.pulse-macos.plist`;
- logs: `~/Library/Logs/Pulse/`.

It performs a local collection and first Pulse push before loading the LaunchAgent. The agent runs `run` with `KeepAlive`; collection follows the configured interval.

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
  --config "$HOME/Library/Application Support/Pulse/pulse-macos.toml" \
  --local --pretty once

launchctl print "gui/$(id -u)/dev.pulse.pulse-macos"
tail -n 100 "$HOME/Library/Logs/Pulse/pulse-macos.err.log"
```

Update by running the interactive installer again. Remove the binary and LaunchAgent while preserving the config, token, and spool:

```sh
curl -fsSL \
  https://github.com/k2b-dev/pulse-injestors/releases/latest/download/install.sh \
  | sh -s -- --uninstall --ingestor=macos
```

## Workstations

Enroll one Pulse source per machine. In the source’s **Expected data** setting, choose daily (`expectDailyBy`) and a time when that person normally has the computer on. Use the last contact and `ingestor.heartbeat` to check liveness. Do not use a minute-based interval expectation for computers switched off overnight. A daily expectation still applies on non-working days; account for those in your notification policy.

macOS always uses the workstation schedule: CPU, memory, and the system/data volumes every 300 seconds; update counts and security posture hourly; hardware, battery health, internal-disk SMART, and software inventory daily. There is no macOS profile flag. Keep the LaunchAgent in `run` mode to preserve these cadences; `once` runs every group on every invocation.

The LaunchAgent runs only in the logged-in user session and never elevates privileges. FileVault’s `fdesetup status` can require root/admin access on the installed macOS release; when denied, `macos.filevault.enabled` is `"unknown"`. Firewall, SIP, Gatekeeper, and SoftwareUpdate preference reads normally work without root. Any denied or unrecognized posture result also becomes `"unknown"`, with availability false and a collector failure event. The agent does not run `sudo` or change protections. After permission changes, verify with the local command above.

## Main settings

```toml
[runner]
interval_seconds = 300

[macos]
enable_homebrew = true
homebrew_timeout_seconds = 20
enable_software_update = true
software_update_timeout_seconds = 60
```

New macOS configurations enable update checks by default. Existing explicit `enable_software_update = false` settings remain respected. Set it to `true` for OS update counts. Each hourly update cycle has enough time for both configured command deadlines. Homebrew automatic refresh and analytics are disabled during collection; outdated counts reflect the locally cached catalog.

Automatic OS updates count as enabled only when both `AutomaticCheckEnabled` and `AutomaticallyInstallMacOSUpdates` are explicitly `1`. Missing preferences mean unknown, not disabled. This check does not certify an MDM policy or that an update has installed successfully.

The old `enable_system_profiler` and `system_profiler_timeout_seconds` settings remain accepted but have no effect. Display, GPU, power-adapter, charging, and electrical battery details are no longer collected.

## Collected data

- OS name, version, build, hardware model, chip/CPU, and total RAM;
- CPU usage, memory use, load, and capacity for the system/data volumes;
- pending macOS updates and outdated Homebrew package totals;
- FileVault, firewall, SIP, Gatekeeper, and automatic-update posture;
- laptop battery health, cycles, and maximum capacity as a percentage of design;
- internal-disk SMART status where `diskutil` exposes it;
- installed applications and Homebrew formula/cask versions.

macOS does not provide a separate security-update count or a reliable reboot-needed signal through these probes. Do not interpret missing signals as zero or false. Linux’s package collector provides those checks where its package manager supports them.

With two volumes, 100 apps, 50 Homebrew packages, a 300-second base interval, and a persistent state cache, budget about **4.6 values/minute** over a nine-hour working day, including runner signals and ordinary state refreshes. A cold or weekly software inventory adds 150 states that day (0.28/minute averaged over nine hours); the 3,000-package cap adds at most 5.56/minute on such a day. Failures and changes add values. These are estimates, not quotas.

The [macOS module reference](/en/module-macos) lists every signal, its cadence, and the complete removal list.
