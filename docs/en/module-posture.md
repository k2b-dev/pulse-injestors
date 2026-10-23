---
title: Workstation security posture
navTitle: Workstation posture
section: Modules
order: 425
description: Daily encryption, firewall, and automatic-update checks for Linux desktops.
tags: [linux, security, modules, reference]
updated: 2026-10-23
---

# Workstation security posture

The `posture` collector runs daily with `pulse-linux --linux-profile desktop`. It reports three protections on the host resource (`host:<host-id>`, configured label) without changing configuration or elevating privileges. Server profiles do not enable it. Use `[runner.cadence] posture = 86400` to override its schedule; `once` always runs it.

## Signals

These are all signals emitted by this module. A known result is boolean. An unreadable or unsupported check sends the string `"unknown"`, replacing any prior boolean. No state has a unit. States are sent on change and on the first daily check after the six-hour refresh window.

| Signal | Type | Unit | Dimensions | Cadence | Example | Meaning |
|---|---|---|---|---|---|---|
| `system.disk_encryption.enabled` | state boolean or string | — | — | 86400 s | `true` | At least one mounted root/home filesystem has a dm-crypt ancestor. |
| `system.firewall.enabled` | state boolean or string | — | — | 86400 s | `true` | Nonempty nftables ruleset, running firewalld, or active ufw. |
| `system.autoupdate.enabled` | state boolean or string | — | — | 86400 s | `true` | Supported distribution has the automatic-update scheduling described below. |
| `system.posture.available` | state boolean | — | `check` | 86400 s | `false` | Whether the corresponding result is known. |

`check` is `disk_encryption`, `firewall`, or `autoupdate`. Failures also use [runner signals](/en/modules#runner-signals), with `collector=posture` and a concise error attribute.

## What each check establishes

**Encryption:** `lsblk --json --output NAME,TYPE,MOUNTPOINTS` follows the block-device tree. A device of type `crypt` protects its descendants, including LVM volumes above LUKS. A protected mount at `/`, `/home`, or below `/home/` makes the state true. An unrelated encrypted backup disk does not. False requires a visible root device and no protected root/home mount; hidden or unsupported storage gives unknown. This is an “any protected root/home” check, not proof that all user data, swap, or external disks are encrypted. Filesystem-native encryption is outside this check.

**Firewall:** `nft --json list ruleset` must contain at least one object besides metadata, or `firewall-cmd --state` must report running, or `ufw status` must report active. Empty tables still constitute a nonempty ruleset. The check does not audit rule effectiveness. A positive result is decisive. Otherwise, a permission, parsing, or command failure makes the result unknown rather than false. Missing optional tools are skipped; with no usable tool the result is unknown.

**Automatic updates on Debian/Ubuntu:** `apt-config dump` must contain a positive `APT::Periodic::Unattended-Upgrade` value, `APT::Periodic::Enable` must not be explicitly zero, and `apt-daily-upgrade.timer` must be enabled. This checks periodic scheduling, not successful updates. Missing settings or a disabled timer produce false.

**Automatic updates on Fedora/RHEL:** either `dnf-automatic.timer` or `dnf-automatic-install.timer` must be enabled. A generic dnf-automatic timer may be configured only to download or notify; inspect `automatic.conf` to confirm installation policy. Other distributions report unknown. Runtime-only systemd enablement counts as enabled but does not persist through reboot.

## Permissions and failure handling

`lsblk` generally works without root but must support `MOUNTPOINTS`. nftables rules and ufw status usually need root or network-administration privileges. A restricted systemd service may therefore report unknown even on a configured workstation. The collector never runs sudo or changes the service’s permissions. Review the deployed service policy before granting access.

Each command has a five-second timeout, a 1 MiB stdout limit, and 4 KiB bounded stderr. Missing optional executables and unsupported distributions emit availability states without failure events. Permission, timeout, malformed output, and unexpected command errors remain visible through `ingestor.collector.failed`. Re-check the local `--local --pretty once` report after fixing access or installing a missing tool.
