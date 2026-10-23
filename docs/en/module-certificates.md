---
title: Certificate files module
navTitle: Certificate files
section: Modules
order: 421
description: Track expiration of configured local PEM certificates.
tags: [certificates, linux, modules, reference]
updated: 2026-10-23
---

# Certificate files module

Set `linux.certificate_paths` to local PEM regular-file paths or globs. The default is empty. The collector reads the first certificate block in each file every 3600 seconds; override `certificates` in `[runner.cadence]`. Overlapping globs do not duplicate a file.

```toml
[linux]
certificate_paths = ["/etc/ssl/local/*.pem", "/etc/service/tls.crt"]
```

Each certificate uses resource type `certificate`, ID `entity.ID(host, path)`, and subject CN as its label, falling back to the filename. If the resource key would exceed 500 characters, the ID uses the host and the first 16 hexadecimal characters of the path’s SHA-256 hash; the path becomes the label. Labels are limited to 240 characters. Paths remain local: this collector makes no network requests and does not validate a remote endpoint or its certificate chain.

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.certificate.expires_in` | gauge | seconds | — | `86400` | Time until NotAfter; negative values mean expired. | 3600 s |
| `system.certificate.not_after` | state string | — | — | `2026-12-01T00:00:00Z` | Certificate expiry as RFC3339 UTC. | 3600 s; change / 6 h |
| `system.certificate.collect.failed` | event | — | — | `path=/etc/service/tls.crt` | Host-level error for unreadable or invalid files, or invalid glob patterns; attributes path and a generic error, never certificate content. | on failure at 3600 s |

Globs matching no files are silently skipped. Files larger than 1 MiB are skipped with a collector error naming the path. The service needs read access to the configured files. A failed file does not stop other certificates. The collector sends no expiry metric for that file.
