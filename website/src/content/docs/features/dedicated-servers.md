---
title: Dedicated Server Fleet Control
description: Manage OVHcloud bare-metal servers, IPMI/KVM consoles, OS reinstallation, SSH keys, and IP Edge Firewall.
---

The **Dedicated Server Control** module (`/server-control`) provides day-to-day operational control over all bare-metal servers (`Kimsufi`, `So you Start`, `Rise`, `Advance`, `Scale`, `High Grade`) under your active OVHcloud account.

## Hardware Inventory & MRTG Telemetry

Selecting a server displays:

- **Hardware Specifications**: Commercial model name, CPU model, RAM capacity, disk layout, datacenter code, and rack ID.
- **Network & Bandwidth**: Public IPv4/IPv6 addresses, reverse DNS, and real-time **MRTG bandwidth graphs** (`/dedicated/server/{serviceName}/statistics`) for both upload and download traffic across `hourly`, `daily`, `weekly`, `monthly`, and `yearly` windows.
- **Power & Rescue Control**: Trigger hardware reboots (`/reboot`) or boot into netboot / rescue mode (`rescue64-pro`).

## IPMI / KVM Remote Access

For servers equipped with a BMC/IPMI module:

- **Serial over LAN (SOL) & xterm.js**: Launch an interactive browser-based terminal session or retrieve SSH SOL access credentials.
- **KVM over IP / HTML5 Console**: Request temporary KVM access from `/dedicated/server/{serviceName}/features/ipmi/access` (automatically Whitelisting your public IP) and track IPMI task readiness in real time.

## Automated OS Reinstallation & SSH Key Injection

OVH Nexus streamlines bare-metal OS provisioning (`/dedicated/server/{serviceName}/install/start`):

1. **OS Template Discovery**: Lists compatible OVHcloud installation templates (Debian, Ubuntu, Rocky Linux, AlmaLinux, Proxmox VE, Windows Server, etc.).
2. **Custom Partitioning & RAID / ZFS**:
   - Configure hardware or software RAID levels (`RAID 0`, `RAID 1`, `RAID 5`, `RAID 10`, or `JBOD`).
   - One-click **Proxmox VE ZFS** root filesystem profile generation.
   - Customize root/data partition sizes and custom hostname.
3. **Account SSH Key Management (`/me/sshKey`)**:
   - View, add, or delete SSH public keys stored in your OVHcloud account directly from the **Account SSH Keys** modal.
   - Select an `sshKeyName` during OS reinstallation so OVHcloud injects your public key into `root` / default user `authorized_keys` during initial provisioning.

## IP Edge Firewall & Anti-DDoS Mitigation

Manage OVHcloud's network-level Edge Firewall (`/ip/{ip}/firewall`) and VAC Anti-DDoS protection without logging into the OVHcloud Manager:

- **Edge Firewall Toggle**: Enable or disable the hardware edge firewall on any IPv4 address attached to the server.
- **Rule Management (Sequence `0–19`)**: Add or delete rules with `permit` or `deny` action, protocol (`tcp`, `udp`, `icmp`, `ipv4`), source CIDR (or any), destination port, and TCP flags (`syn`, `established`).
- **Permanent Anti-DDoS Mitigation**: View mitigation status (`auto` vs `permanent`) and toggle permanent VAC mitigation (`/ip/{ip}/mitigation`) for latency-sensitive or game workloads.
