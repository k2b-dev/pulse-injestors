---
title: Uptime module
navTitle: Uptime
section: Modules
order: 470
description: ICMP, DNS, TCP, HTTP, and TLS availability signals for named endpoints.
tags: [uptime, internet, modules, reference]
updated: 2026-07-23
---

# Uptime module

The uptime module checks a bounded list of named endpoints from one probe. Target failures remain normal telemetry so one unavailable endpoint does not break the other checks.

## Entity types

| Entity type | Entity id | Label | Used for |
|---|---|---|---|
| `uptime-probe` | `uptime-probe:<probe-id>` | Configured probe label | Shared runner health for one installation. |
| `uptime-endpoint` | `uptime-endpoint:<probe-id>:<target-id>` | Configured target label | Metrics and states for one logical endpoint. |

Changing a target address does not change the endpoint resource. Changing its configured `id` creates a different resource.

## Metrics

Every metric below is emitted on one `uptime-endpoint` resource.

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `uptime.check.availability` | gauge |  | `probe`, `endpoint`, `check_type` | `1` | Check result: `1` for success and `0` for failure. Not emitted when the probe could not perform the check, such as a missing `ping` command. |
| `uptime.check.duration` | gauge | milliseconds | `probe`, `endpoint`, `check_type` | `24.7` | Wall-clock duration for one completed or timed-out check. |
| `uptime.dns.address_count` | gauge |  | `probe`, `endpoint`, `check_type=dns` | `2` | Number of IP addresses returned by one DNS lookup. |
| `uptime.tls.certificate.expires_in` | gauge | seconds | `probe`, `endpoint`, `check_type=http` | `2592000` | Seconds until the leaf TLS certificate expires for the final HTTPS response. |

Average `uptime.check.availability` over time to calculate an uptime ratio. Group by resource to keep one series per configured target.

## States

Every state below is emitted on one `uptime-endpoint` resource.

| Key | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|
| `uptime.check.measured` | boolean | `probe`, `endpoint`, `check_type` | `true` | Whether the probe could perform this check. |
| `uptime.check.success` | boolean | `probe`, `endpoint`, `check_type` | `true` | Current result. Read together with `uptime.check.measured`. |
| `uptime.check.type` | string | `probe`, `endpoint`, `check_type` | `http` | Configured check type. |
| `uptime.check.target` | string | `probe`, `endpoint`, `check_type` | `https://pulse.example.com/health` | Current configured IP, hostname, host and port, or URL. |
| `uptime.check.error` | string | `probe`, `endpoint`, `check_type` | `timeout` | Stable diagnostic message for the current result. Empty after a successful check. |
| `uptime.dns.addresses` | string | `probe`, `endpoint`, `check_type=dns` | `192.0.2.10, 2001:db8::10` | Sorted addresses from the current DNS result. Empty after a failed lookup. |
| `uptime.http.status_code` | integer | `probe`, `endpoint`, `check_type=http` | `204` | Current HTTP status code, or `0` when no response was received. |
| `uptime.http.final_url` | string | `probe`, `endpoint`, `check_type=http` | `https://example.com/health` | Final URL after redirects. Empty when no response was received. |
| `uptime.http.protocol` | string | `probe`, `endpoint`, `check_type=http` | `HTTP/2.0` | HTTP protocol used by the current response. Empty when no response was received. |
| `uptime.tls.server_name` | string | `probe`, `endpoint`, `check_type=http` | `example.com` | Common name from the current leaf TLS certificate. Empty without a TLS response. |
| `uptime.tls.issuer` | string | `probe`, `endpoint`, `check_type=http` | `Example CA` | Common name of the current leaf certificate issuer. Empty without a TLS response. |
| `uptime.tls.expires_at` | string | `probe`, `endpoint`, `check_type=http` | `2026-09-01T00:00:00Z` | UTC expiry timestamp of the current leaf TLS certificate. Empty without a TLS response. |

DNS and HTTP states are overwritten with empty values or status `0` after a failed result so stale response details do not remain current. The shared runner also emits `ingestor.collector.ok` on the `uptime-probe` resource.

## Events

The module does not emit events for target failures. A failed endpoint can occur every interval and is represented by availability metrics and current states instead of repeated historical events.

The shared runner emits `ingestor.collector.failed` only when the complete uptime collector cannot run, such as an empty target list.

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `probe` | `office-berlin` | all uptime signals | Stable configured ID of the machine or location performing checks. |
| `endpoint` | `pulse-api` | all endpoint signals | Stable configured target ID. |
| `check_type` | `http` | all endpoint signals | One of `icmp`, `dns`, `tcp`, or `http`. |
| Global dimensions | `site=berlin` | all signals | Bounded deployment labels configured under `[dimensions]`. |

Addresses, URLs, DNS results, certificate names, and errors are states rather than dimensions. This avoids unstable or high-cardinality query labels.

## Check behavior

| Type | Address format | Success condition | Additional output |
|---|---|---|---|
| `icmp` | IP address, for example `1.1.1.1` | One system `ping` succeeds before the timeout | No protocol-specific signals |
| `dns` | Hostname, for example `example.com` | The system resolver returns at least one address | Address count and current addresses |
| `tcp` | `host:port`, for example `database.example.com:5432` | A TCP connection is established before the timeout | No protocol-specific signals |
| `http` | HTTP or HTTPS URL | Exact `expected_status`, or any `200`–`399` status when unset | Status, final URL, protocol, and TLS certificate data |

HTTP checks use `GET`, read at most 64 KiB of the body, and follow at most three redirects.

## Failure behavior

- A refused connection, lookup error, HTTP error, status mismatch, failed ping, or timeout emits `availability=0`, `measured=true`, and a concise error state.
- A missing or locally unusable `ping` command emits `measured=false` and does not emit an availability metric for that target.
- One failed target does not stop or suppress other targets.
- Invalid target IDs, address formats, kinds, status codes, duplicate IDs, and negative timeouts are rejected before collection.
