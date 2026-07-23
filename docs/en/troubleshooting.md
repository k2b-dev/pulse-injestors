---
title: Troubleshooting
navTitle: Troubleshooting
section: Operate
order: 60
description: Diagnose failed enrollment, missing resources, scheduler failures, and unavailable collectors.
tags: [troubleshooting]
updated: 2026-07-23
---

# Troubleshooting

## Enrollment stops before scheduling

The installer enables scheduling only after a successful local collection and first Pulse push.

1. Read the final `pulse-install` error.
2. Run the installed binary in local mode.
3. Run one authenticated push.
4. Re-run the installer after correcting the problem.

Linux example:

```sh
sudo pulse-linux --config /etc/pulse/ingestor.toml --local --pretty once
sudo pulse-linux --config /etc/pulse/ingestor.toml once
```

Common causes are an incorrect ingest URL, an invalid token, unavailable DNS, or a config file that the scheduled user cannot read.

## Cosign verification fails

Confirm that `cosign` is installed and on `PATH`:

```sh
cosign version
```

The installer refuses unsigned manifests, releases signed by a different workflow identity, and archives whose SHA-256 value differs from the manifest.

## The binary works manually but not on schedule

Inspect the scheduler under the same account used by installation:

```sh
systemctl status pulse-linux.timer
journalctl -u pulse-linux.service -n 100
```

On macOS:

```sh
launchctl print "gui/$(id -u)/dev.pulse.pulse-macos"
tail -n 100 "$HOME/Library/Logs/Pulse/pulse-macos.err.log"
```

Scheduled processes use a restricted `PATH`. Configure absolute command paths for custom scripts or platform tools when they are not in the scheduler path.

## A module reports `available=false`

Availability states are normal when the monitored system does not use an optional subsystem. Examples include ZFS on a non-ZFS host, Ceph on a standalone server, or temperature sensors hidden by a VM.

Use `--local --pretty --verbose once` to see which collectors ran and why data was unavailable. Optional absence does not stop other modules.

## macOS temperature is unavailable

Detailed CPU and GPU power data may require privileged `powermetrics` access. The default per-user LaunchAgent prioritizes normal macOS, Homebrew, battery, filesystem, GPU inventory, and display collection without granting persistent root execution.

## Docker host metrics are missing

Confirm that the Compose deployment includes the `/proc`, `/sys`, `/`, and Docker socket mounts shown in the [published Compose file](/en/ingestor-docker#compose-file).

On Docker Desktop, Linux host metrics describe the Docker virtual machine. Install `pulse-macos` separately to monitor the physical Mac.

## ICMP checks are not measured

`pulse-uptime` uses the system `ping` command. When it is missing or cannot run locally, the endpoint reports `uptime.check.measured=false` and a matching error state. No downtime metric is emitted for that target.

Install the platform ping package or disable the default targets and configure DNS, TCP, or HTTP checks instead. Other uptime targets continue without ICMP support.

## An uptime target fails while the internet is available

Check the target-specific states in Pulse or run:

```sh
sudo pulse-uptime --config /etc/pulse/ingestor.toml --local --pretty once
```

`uptime.check.error` distinguishes timeouts, DNS failures, refused TCP connections, HTTP request failures, and unexpected status codes. A single failed provider does not imply that every internet path is unavailable; compare the ICMP, DNS, TCP, and HTTP targets.
