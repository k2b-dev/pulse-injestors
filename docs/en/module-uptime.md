---
title: Uptime module
navTitle: Uptime
section: Modules
order: 470
description: Endpoint availability, expected ports and DNS records, TLS expiry, and mail IP blocklists.
tags: [uptime, internet, modules, reference]
updated: 2026-10-29
---

# Uptime module

Each target reports `uptime.check.result`: `ok`, `failed` (a confirmed finding), or `error` (the check could not complete). Alert on both `failed` and `error`. An error never contributes a successful sample to availability.

## Resources and scheduling

| Resource type | Resource key | Label | Meaning |
|---|---|---|---|
| `uptime-probe` | `uptime-probe:<probe-id>` | Probe label | Collector health and heartbeat. |
| `uptime-endpoint` | `uptime-endpoint:<probe-id>:<target-id>` | Target label | One configured check. Its identity survives address changes. |

| Kind | Default cadence | Target | Success condition |
|---|---|---|---|
| `icmp` | 60 seconds | IP or hostname in `address` | Round loss is below `loss_threshold` (default 100%). |
| `dns` | 60 seconds | Hostname in `address` | System resolver returns at least one address. |
| `tcp` | 60 seconds | `host:port` in `address` | Connection succeeds. |
| `http` | 60 seconds | URL in `address` | Exact `expected_status`, or any 200–399 response when unset. |
| `ports` | 86400 seconds | Host or bracketed IPv6 in `address` | Every expected port is open and every other scanned port is closed or filtered. |
| `dns_records` | 3600 seconds | DNS `name` and `records` | Every record set or TXT substring matches. |
| `tls` | 3600 seconds | `host:port` in `address` | Chain and hostname verify; expiry is at least `warn_days` away. |
| `dnsbl` | 3600 seconds | IPv4 in `address`, zones in `lists` | Each reference query works and the IP is absent from every list. |

Set `cadence_seconds` on any target to override its cadence. `run` checks due targets on the runner interval; `once` checks every target. Keep the runner interval at 60 seconds for minute checks. States are sent on change and refreshed after six hours when their checks next run.

## Metrics

All metrics belong to an `uptime-endpoint`. Dimensions `probe`, `endpoint`, and `check_type` are present throughout; DNSBL also emits per-list success samples with a `list` dimension, alongside the overall target sample without `list`.

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---|---|---|---|
| `uptime.check.success` | gauge | — | base, optional `list` | `1` | 1 for `ok`, 0 for `failed`; absent for `error`. |
| `uptime.check.duration` | gauge | milliseconds | base | `24.7` | Elapsed non-ICMP check time, including errors. Omitted for ICMP; use its median RTT. |
| `uptime.icmp.loss` | gauge | percent | base, `check_type=icmp` | `5` | Requests without a reply within two seconds, as a percentage of the round; absent on execution or parsing errors. |
| `uptime.icmp.rtt.min` | gauge | milliseconds | base, `check_type=icmp` | `4.1` | Smallest reply RTT in the round. |
| `uptime.icmp.rtt.median` | gauge | milliseconds | base, `check_type=icmp` | `5.2` | Median reply RTT; average of the middle pair for even counts. |
| `uptime.icmp.rtt.max` | gauge | milliseconds | base, `check_type=icmp` | `9.8` | Largest reply RTT in the round. |
| `uptime.icmp.rtt.stddev` | gauge | milliseconds | base, `check_type=icmp` | `1.4` | Population standard deviation of reply RTTs. |
| `uptime.dns.address_count` | gauge | — | base, `check_type=dns` | `2` | Addresses in a completed lookup. |
| `uptime.ports.open` | gauge | — | base, `check_type=ports` | `2` | Open ports in a completed scan; absent on scan errors. |
| `uptime.ports.filtered` | gauge | — | base, `check_type=ports` | `9980` | Ports whose TCP connections timed out in a completed scan; absent on scan errors. |
| `uptime.tls.certificate.expires_in` | gauge | seconds | base, `check_type=http` or `tls` | `2592000` | Leaf certificate lifetime; negative after expiry. Use a Pulse threshold alert for advance warning. |

Average `uptime.check.success` by resource for the fraction of completed checks that succeeded. Monitor `uptime.check.result=error` separately; missing samples are not proof of availability. For DNSBL, filter on the desired `list` or select the overall sample without that dimension.

## ICMP rounds

`count` is the number of probes per round (default 20, range 1–100). `interval` is a duration string between probes (default `"500ms"`, range `"200ms"`–`"1m"`). The lower bound allows unprivileged system ping on Linux and macOS. `cadence_seconds` is the step between rounds (default 60). Different targets share the configured concurrency limit.

A round allows one final interval for macOS ping, two seconds for replies, and three seconds for startup and DNS. Its default timeout is `count × interval + 5 seconds`, rounded up to whole seconds, or the global uptime timeout if larger: 15 seconds with defaults. An explicit target `timeout_seconds` must be at least that calculated round budget. The timeout must also fit within `cadence_seconds`; invalid combinations fail at startup. On macOS, ping receives its own deadline three seconds before the calculated timeout so it can print a complete summary. Linux ping gets no deadline, because iputils then counts replies instead of probes and keeps sending under loss; its per-reply wait bounds the round. Allow about 15 seconds per group of concurrent default targets, plus time for other checks; increase concurrency or cadence when groups approach the runner interval.

The collector parses per-reply `time=` values from system ping with `LC_ALL=C`. A `time<` value uses the displayed upper bound. Only the first reply for each sequence contributes; duplicates do not reduce loss or skew RTTs. Replies later than two seconds count as lost on both platforms. macOS reports these in its summary without RTT lines, so the collector uses that count to distinguish late replies from malformed output. All four RTT metrics are omitted at 100% loss. A measured round sends five loss/RTT values plus `uptime.check.success`; total loss sends only loss and success. Raw packet samples are not stored.

`loss_threshold` accepts a percentage greater than 0 and at most 100. Success is false when loss is **greater than or equal to** the threshold; the default succeeds if at least one reply arrives. Missing ping, permission failures, incomplete output, and cancellation produce `error` with no ICMP or success metrics. Total packet loss is a measured `failed` round.

## States

| Key | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|
| `uptime.check.result` | string | base, optional `list` | `error` | `ok`, `failed`, or `error`. |
| `uptime.check.message` | string | base, optional `list` | `DNSBL reference check failed` | Reason, at most 500 characters; empty after success. |
| `uptime.check.type` | string | base | `dns_records` | Configured kind. |
| `uptime.check.target` | string | base | `example.com` | Address or DNS name being checked. |
| `uptime.ports.unexpected` | string | base, ports | `25,8080` | Unexpected open ports, sorted comma list, at most 500 characters. |
| `uptime.ports.missing` | string | base, ports | `443` | Expected ports that are closed or filtered, sorted comma list, at most 500 characters. |
| `uptime.dnsbl.listed` | boolean | base, `list` | `true` | Listing finding from a completed list check. Read with that list's result; no new boolean is emitted on error. |
| `uptime.dns.addresses` | string | base, dns | `192.0.2.10, 2001:db8::10` | Sorted addresses; empty after failure. |
| `uptime.http.status_code` | integer | base, http | `204` | Response code, or 0 without a response. |
| `uptime.http.final_url` | string | base, http | `https://example.com/health` | URL after redirects; empty without a response. |
| `uptime.http.protocol` | string | base, http | `HTTP/2.0` | Response protocol; empty without a response. |
| `uptime.tls.server_name` | string | base, http or tls | `example.com` | SNI/verification name for TLS checks; certificate common name for HTTP. |
| `uptime.tls.issuer` | string | base, http or tls | `Example CA` | Leaf certificate issuer common name; empty without a certificate. |
| `uptime.tls.expires_at` | string | base, http or tls | `2026-12-01T00:00:00Z` | Leaf expiry in UTC; empty without a certificate. |

`base` means `probe`, `endpoint`, `check_type`, plus configured global dimensions. `list` is the configured DNSBL zone. Addresses and diagnostic messages remain state values rather than dimensions.

## Check behavior and failures

**Ports:** `range` defaults to `1-10000`, inclusive; `expected` ports must fall inside it. Each scan starts at most `rate_per_second` connections per second (default `200`; must be positive), with at most 64 concurrent connections, a two-second connect timeout, and a total `timeout_seconds` budget of 900 seconds by default. Override the target's `timeout_seconds` to change that budget. A successful TCP connection means open, a refusal means closed, and a connect timeout means filtered. Closed and filtered ports count as not open. An unexpected open port or an expected port that is closed or filtered makes the result `failed`; the latter appears in `uptime.ports.missing`.

Resolution or routing errors and a budget exhausted before the whole range is checked make the result `error`. A scan with zero open ports and zero refusals also returns `error`: it cannot distinguish a host that is down from one with every port filtered. Nonempty partial findings update the port lists, but an error never clears a previous finding with an empty list. Read the lists with the result state; only a completed scan without a scan error confirms that an empty list is current. Open-count, filtered-count, and success gauges are absent on scan errors. A hostname must resolve to exactly one address; use separate IP targets for hosts with multiple addresses. Both IPv4 and bracketed IPv6 are supported.

**DNS records:** `records` supports A, AAAA, MX, NS, TXT, and CNAME. `expect` compares sets, ignoring ordering and duplicates. IP addresses use canonical form; DNS names ignore case and trailing dots. MX values use `priority hostname`. TXT values retain case and punctuation; `contains` accepts a substring in any returned TXT value and cannot be combined with `expect`. Split SPF and DMARC names into separate targets. Missing answers, NXDOMAIN, SERVFAIL, REFUSED, malformed replies, and timeouts are `error`; a returned set that differs is `failed`.

**Resolvers:** `dns_records` and `dnsbl` accept `resolver = "ip:port"`, including `[IPv6]:port`. When omitted, they use nameservers from `/etc/resolv.conf`, querying the full configured name without search suffixes. UDP replies truncated by the server are retried over TCP. Use an explicit resolver for split DNS or DNSBL services that restrict public recursive resolvers.

**TLS:** The connection uses SNI from the address hostname, or optional `server_name`. The certificate chain and hostname are verified against system roots, or the certificates in `ca_file`. Verification failure and expiry below `warn_days` (default 14) are `failed`; connection/handshake timeouts, a missing certificate, and an unreadable CA file are `error`. A refused connection is `failed`. Certificate expiry and issuer are retained for verification failures. TLS checks send no application data.

**DNSBL:** Before querying the reversed target IPv4, each list must answer `2.0.0.127.<list>` with only `127.0.0.x` addresses. A failed reference query makes that list `error`. After a working reference, target NXDOMAIN is `ok`; a `127.0.0.x` listing is `failed`; empty replies, other address ranges, resolver failures, and timeouts are `error`. The overall target is `error` if any list could not be checked, otherwise `failed` if any lists report a listing. Individual results retain confirmed findings.

**Existing checks:** HTTP uses GET, reads at most 64 KiB, and follows at most three redirects. Status mismatches and refused TCP/HTTP connections are `failed`. ICMP uses the round loss threshold described above. Missing/broken ping execution, DNS errors or empty answers, request/read errors, and timeouts are `error`. A failed target does not suppress other targets.

The module emits no events for target findings. The runner emits `ingestor.collector.ok` and `ingestor.collector.failed` for collector failures. [Uptime ingestor](/en/ingestor-uptime) explains source heartbeat monitoring and provides complete configurations.

## Signal migration

- Metric `uptime.check.availability` becomes `uptime.check.success` (gauge, 1/0).
- Boolean states `uptime.check.measured` and `uptime.check.success` are replaced by `uptime.check.result`.
- State `uptime.check.error` becomes `uptime.check.message`.
- Collector dimensions change from `collector=uptime` to `collector=uptime-60s`, `uptime-3600s`, or `uptime-86400s` (custom cadences use their seconds). Update collector-health queries accordingly.
- ICMP no longer emits `uptime.check.duration`; replace ICMP duration panels with `uptime.icmp.rtt.median`. Other check kinds retain elapsed duration. Existing DNS, HTTP, and TLS signal names remain unchanged; HTTP and standalone TLS share one expiry metric definition.
