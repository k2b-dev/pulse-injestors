---
title: Network module
navTitle: Network
section: Modules
order: 340
description: Linux host network interface counters.
tags: [network, modules, reference]
updated: 2026-06-11
---

# Network module

The network module reads Linux interface counters from procfs.

## Entity types

| Entity type | Entity id | Used for |
|---|---|---|
| `host` | `host:<host-id>` | Network availability. |
| `network-interface` | `network-interface:<host-id>:<interface>` | Counter metrics for one network interface. |

## Metrics

| Name | Type | Unit | Dimensions | Example | Meaning |
|---|---|---:|---|---:|---|
| `system.network.rx` | counter | bytes | `host`, `interface` | `123456789` | Received bytes for one host network interface. Emitted on `network-interface`. |
| `system.network.rx_packets` | counter | count | `host`, `interface` | `812345` | Received packet count for one host network interface. Emitted on `network-interface`. |
| `system.network.rx_errors` | counter | count | `host`, `interface` | `0` | Receive error count for one host network interface. Emitted on `network-interface`. |
| `system.network.rx_dropped` | counter | count | `host`, `interface` | `12` | Dropped receive packet count for one host network interface. Emitted on `network-interface`. |
| `system.network.tx` | counter | bytes | `host`, `interface` | `987654321` | Transmitted bytes for one host network interface. Emitted on `network-interface`. |
| `system.network.tx_packets` | counter | count | `host`, `interface` | `734211` | Transmitted packet count for one host network interface. Emitted on `network-interface`. |
| `system.network.tx_errors` | counter | count | `host`, `interface` | `0` | Transmit error count for one host network interface. Emitted on `network-interface`. |
| `system.network.tx_dropped` | counter | count | `host`, `interface` | `3` | Dropped transmit packet count for one host network interface. Emitted on `network-interface`. |

## States

| Key | Value type | Dimensions | Example | Meaning |
|---|---|---|---|---|
| `system.network.available` | boolean | `host` | `true` | Whether `/proc/net/dev` was readable and parseable. |

## Events

This module does not emit events directly. If procfs network data is missing, it returns a collector failure after sending `system.network.available=false`.

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `host` | `server-01` | all signals | Stable host id from configuration. |
| `interface` | `eth0` | interface metrics | Network interface name. |

## Requirements

- Linux `/proc/net/dev` access through `host.proc_root`.

## Failure behavior

Missing network procfs data emits `system.network.available=false` and returns an error because the module cannot collect interface counters.
