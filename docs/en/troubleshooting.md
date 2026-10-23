---
title: Troubleshooting
navTitle: Troubleshooting
section: Operate
order: 60
description: Diagnose failed enrollment, missing resources, scheduler failures, and unavailable collectors.
tags: [troubleshooting]
updated: 2026-10-23
---

# Troubleshooting

## Enrollment stops before scheduling

The installer enables scheduling only after a successful local collection and first Pulse push.

1. Read the final `pulse-install` error.
2. Run the installed binary in local mode.
3. Run one authenticated push.
4. Re-run the installer after correcting the problem.

If a service is already running, stop it before an authenticated `once` check and start it again afterwards. Only one sender can own the spool. Local mode does not open the spool.

Linux example:

```sh
sudo pulse-linux --config /etc/pulse/pulse-linux.toml --local --pretty once
sudo pulse-linux --config /etc/pulse/pulse-linux.toml once
```

Common causes are an incorrect ingest URL, an invalid token, unavailable DNS, or a config file that the scheduled user cannot read.

## Cosign verification fails

Confirm that `cosign` is installed and on `PATH`:

```sh
cosign version
```

The installer refuses unsigned manifests, releases signed by a different workflow identity, and archives whose SHA-256 value differs from the manifest.

## The binary works manually but not as a service

Inspect the service under the same account used by installation:

```sh
systemctl status pulse-linux.service
journalctl -u pulse-linux.service -n 100
```

On macOS:

```sh
launchctl print "gui/$(id -u)/dev.pulse.pulse-macos"
tail -n 100 "$HOME/Library/Logs/Pulse/pulse-macos.err.log"
```

Scheduled processes use a restricted `PATH`. Configure absolute command paths for custom scripts or platform tools when they are not in the scheduler path.

## Delivery is paused

`outcome=paused` means the ingest endpoint returned 3xx, 401, 403, 404, or a non-idempotency 409. Collection continues and data stays queued, subject to the spool budget and 23-hour expiry. The service probes slowly. Repeated ERROR logs for the same status and error code appear at most every ten minutes.

- For 3xx, correct the ingest URL. Redirects are never followed.
- For 404, check whether the source is disabled or its base is being cleared. Re-enable the source when appropriate. Also verify the ingest URL.
- For 401, check whether the token was revoked or is incorrect. Replacing a token changes the spool binding; preserve the old queue and use a new spool directory if you cannot drain it first.
- For 403 or 409, read Pulse's error code and message in the log and resolve the source or base condition. A 409 whose message names an idempotency key conflict is rejected for isolation and quarantine instead of pausing delivery.

After recovery, the service resumes delivery oldest-first. A successful local collection alone does not prove that Pulse accepts data.

## Pulse throttles delivery with 429

The delivery loop honors `Retry-After` and exponential backoff while collection continues. Watch `ingestor.spool.bytes`, `ingestor.spool.parts`, and `ingestor.spool.oldest_age` for a growing backlog.

Check the source and base quotas in Pulse. Measure the complete collector profile, then adjust the collection interval, selected modules, or quota. A larger spool delays loss but does not resolve a sustained ingestion limit.

## Pulse rejects an item

Read the `pulse rejected telemetry; moved to quarantine` log or `ingestor.delivery.rejected` event. Both include Pulse's error details; an isolated item is named. A common cause is an existing metric name with a different type or unit.

The ingestor splits rejected parts to isolate invalid items, up to 256 splits per hour. When the budget is exhausted, the part stays queued and delivery waits until the hourly window resets. A failed split is retried; healthy items are not quarantined because of the limit or a split failure. The persistent spool retains at most ten quarantine files. These contain telemetry, so inspect them locally and protect their contents.

For a root-run Linux installation, list files and inspect the selected file:

```sh
sudo ls -lt /var/lib/pulse/pulse-linux/quarantine/
sudo less /var/lib/pulse/pulse-linux/quarantine/SELECTED_FILE.json
```

Replace `SELECTED_FILE.json` with the actual filename before running the second command. Inspect `status`, `code`, `message`, and `batch`. Correct the producer or signal definition; quarantined data is not retried automatically. Only a type or unit definition conflict blocks the metric name for one hour in the running process, then tries it again. Other single-item rejections are quarantined and reported without blocking the name. Without `spool_dir`, there is no quarantine file.

## The clock differs from Pulse

`ingestor.clock.skew` estimates Pulse time minus local time from the HTTP Date header. A negative value means the local clock is ahead. The ingestor warns when the absolute difference exceeds two minutes. Pulse rejects data more than five minutes in the future.

Check the host's time synchronization and correct its clock. For example, inspect `timedatectl status` on a systemd host. Check quarantine for samples already rejected because of their timestamps.

## The spool is full or data expires

`spool budget exhausted; dropped oldest telemetry` means older queued parts were removed to fit new data. `ingestor.telemetry.dropped` reports `reason=budget`; `reason=expired` means parts reached 23 hours. A collection larger than the entire budget is refused instead.

Restore delivery first, then size `spool_max_bytes` from measured output. Check free disk space as well as the configured budget: temporary writes, metadata, and quarantine need additional space. Dropped parts cannot be recovered from the queue.

The spool directory must be writable, owned by the service user, and private (`0700`). Do not change a DynamicUser directory to root ownership. If the log says another process owns the spool, check for a running service before starting a manual sender.

After the spool drops or rejects data, all states observed in the next collection cycle are sent again.

## The spool contains legacy outbox files

If startup reports `contains legacy outbox JSON files`, the named directory still holds `*.json` files from the earlier outbox format. Deliver them with the earlier version, or delete them if you accept losing that data, before starting the new version. JSON files inside `quarantine/` do not trigger this check.

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
sudo pulse-uptime --config /etc/pulse/pulse-uptime.toml --local --pretty once
```

`uptime.check.error` distinguishes timeouts, DNS failures, refused TCP connections, HTTP request failures, and unexpected status codes. A single failed provider does not imply that every internet path is unavailable; compare the ICMP, DNS, TCP, and HTTP targets.
