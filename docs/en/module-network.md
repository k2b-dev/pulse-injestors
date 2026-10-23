---
title: Network module
navTitle: Network
section: Modules
order: 340
description: Linux host network interface counters.
tags: [network, modules, reference]
updated: 2026-10-23
---

# Network module

The network module reads Linux interface counters from procfs.

In `pulse-linux`, this collector runs every 300 seconds by default. Override `network` in `[runner.cadence]`. States are sent on change and refreshed after six hours when checked again; table cadences describe collection. Other binaries retain their own schedules.

## Resource types

| Resource type | Resource key | Used for |
|---|---|---|
| `host` | `host:<host-id>` | Network availability. |
| `network-interface` | `network-interface:<host-id>:<interface>` | Counter metrics for one network interface. |

## Metrics

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.network.rx` | counter | bytes | `interface` | `123456789` | Received bytes for one host network interface. Emitted on `network-interface`. | 300 s |
| `system.network.rx_errors` | counter | — | `interface` | `0` | Receive error count for one host network interface. Emitted on `network-interface`. | 300 s |
| `system.network.rx_dropped` | counter | — | `interface` | `12` | Dropped receive packet count for one host network interface. Emitted on `network-interface`. | 300 s |
| `system.network.tx` | counter | bytes | `interface` | `987654321` | Transmitted bytes for one host network interface. Emitted on `network-interface`. | 300 s |
| `system.network.tx_errors` | counter | — | `interface` | `0` | Transmit error count for one host network interface. Emitted on `network-interface`. | 300 s |
| `system.network.tx_dropped` | counter | — | `interface` | `3` | Dropped transmit packet count for one host network interface. Emitted on `network-interface`. | 300 s |

## States

| Signal | Type | Unit | Dimensions | Example | Meaning | Cadence |
|---|---|---|---|---|---|---|
| `system.network.available` | state boolean | — | — | `true` | Whether `/proc/net/dev` was readable and parseable. | 300 s; change / 6 h |

## Events

This module does not emit events directly. If procfs network data is missing, it returns a collector failure after sending `system.network.available=false`.

## Dimensions

| Name | Example | Applies to | Meaning |
|---|---|---|---|
| `interface` | `eth0` | interface metrics | Network interface name. |

## Requirements

- Linux `/proc/net/dev` access through `host.proc_root`.

## Failure behavior

Missing network procfs data emits `system.network.available=false` and returns an error because the module cannot collect interface counters.

## Interface selection

By default, exclude `lo`, `veth*`, `docker*`, `br-*`, `virbr*`, `cni*`, `flannel*`, `cali*`, `vxlan*`, `tap*`, `fwbr*`, `fwpr*`, `fwln*`, `kube-*`, `cilium*`, and `lxc*`. Host bridges (`vmbr*`), bonds, physical NICs, WireGuard (`wg*`), and tunnels (`tun*`) remain visible.

`[linux] network_exclude` replaces that entire list. Set `network_exclude = []` to include all interfaces. Each retained interface sends six cumulative counters; calculate `rate()` or `increase()` in Pulse. Packet counters are no longer emitted.
