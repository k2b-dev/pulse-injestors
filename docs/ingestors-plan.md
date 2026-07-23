# Ingestor plan

This plan keeps shared collection logic in Go modules and ships focused binaries for each deployment target.

## Priority 1: host and container basics

### `pulse-docker`

Purpose: run as a container on Docker Desktop or Linux Docker hosts.

Next useful work:

- Add Docker daemon event ingestion for restart/start/stop history.
- Add Docker volume inventory through `/volumes`.
- Add compose-stack health summary states.
- Add optional authenticated registry freshness checks for private registries.

### `pulse-linux`

Purpose: run as a one-shot binary from cron/systemd timers on Debian, servers, and Raspberry Pi.

Next useful work:

- Harden package update collectors for more distributions and edge cases.
- Add more service presets for common host profiles.
- Extend disk health details from `smartctl` attributes where permissions allow it.
- Add optional top-N process summaries with strict cost limits.

## Priority 2: platform-specific devices

### `pulse-macos`

Purpose: run as a LaunchAgent/cron-style one-shot on MacBooks and Mac desktops.

Next useful work:

- Add more battery trend context once Pulse has history-aware views.

### `pulse-uptime`

Purpose: run as a one-shot internet and endpoint probe on Linux or macOS.

Current scope:

- Built-in ICMP, DNS, TCP, and HTTP internet checks.
- Custom named targets with stable endpoint resources.
- Availability, duration, HTTP status, DNS results, and TLS certificate lifetime.

## Priority 3: infrastructure targets

### `pulse-postgres`

Purpose: monitor one PostgreSQL instance with SQL queries.

Initial modules:

- Connection state and server version.
- Database size and table/index bloat hints.
- Active connections, locks, replication lag.
- Slow query and vacuum/analyze freshness signals.

### `pulse-proxmox`

Purpose: monitor Proxmox nodes and guests through the Proxmox API.

Initial modules:

- Node CPU, memory, storage, and subscription states.
- VM/LXC running state and resource usage.
- Backup freshness and failed task events.
- Cluster quorum and node online states.
- Ceph cluster health, OSD states, pool usage, and PG health when Ceph is enabled.

## Priority 4: storage modules

Shared optional modules should stay reusable from `pulse-docker` and `pulse-linux`.

- `btrfs`: expand device/error/scrub status.
- `zfs`: expand first-pass collector with deeper scrub history and error counters.
- `ceph`: expand first-pass local CLI collector with Proxmox/API-backed details.

## Extension API

Keep the script collector as the low-friction customization path:

- Scripts emit Pulse JSON fragments.
- Runner injects missing resources, timestamps, and dimensions.
- Script failures become events/states and must not break other collectors.
