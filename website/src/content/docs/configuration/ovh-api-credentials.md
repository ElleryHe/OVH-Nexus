---
title: OVHcloud API Credentials
description: How to create OVHcloud App Key, App Secret, and Consumer Key across Europe, Canada, and US endpoints.
---

While public features (**Live Availability Matrix** and **Public SKU Change Watcher**) work without any OVHcloud credentials, managing purchased servers, viewing orders, or placing automated purchases requires an OVHcloud API key pair (`App Key`, `App Secret`, and `Consumer Key`).

## 1. Open the Regional Token Generator

Select the token creation page corresponding to the OVHcloud region where your account is registered:

| Region | Endpoint ID | Token Creation URL |
| :--- | :--- | :--- |
| **Europe** | `ovh-eu` | `https://eu.api.ovh.com/createToken/` |
| **Canada / Asia-Pacific** | `ovh-ca` | `https://ca.api.ovh.com/createToken/` |
| **United States** | `ovh-us` | `https://api.us.ovhcloud.com/createToken/` |

## 2. Fill Out the Token Form

1. **Account ID / NIC-handle or Email** & **Password**: Sign in with your OVHcloud account credentials (and 2FA if enabled).
2. **Script Name**: `OVH Nexus`
3. **Script Description**: `Self-hosted OVH Nexus control and procurement console`
4. **Validity**: Choose `Unlimited` (recommended for unattended stock monitoring and renewal tracking) or `30 days`.
5. **Rights**: Add the HTTP methods and path patterns required for your use case (see below).

## 3. Recommended Permission Rules

### Option A: Full Console Functionality (Simplest)

If you use OVH Nexus for both automated server ordering and full fleet operations (OS reinstall, SSH keys, IP firewall, VPS reboot), add these four rules:

| Method | Path Pattern |
| :--- | :--- |
| `GET` | `/*` |
| `POST` | `/*` |
| `PUT` | `/*` |
| `DELETE` | `/*` |

### Option B: Scoped Least-Privilege Rules

If you prefer to restrict the token strictly to the API namespaces used by OVH Nexus:

| Feature Group | Methods | Path Patterns |
| :--- | :--- | :--- |
| **Account, Orders & SSH Keys** | `GET`, `POST`, `DELETE` | `/me`, `/me/*` |
| **Shopping Cart & Auto-Ordering** | `GET`, `POST`, `PUT`, `DELETE` | `/order/*` |
| **Dedicated Server Control & IPMI** | `GET`, `POST`, `PUT` | `/dedicated/server`, `/dedicated/server/*` |
| **OS Installation Templates** | `GET` | `/dedicated/installationTemplate`, `/dedicated/installationTemplate/*` |
| **VPS Fleet Control** | `GET`, `POST` | `/vps`, `/vps/*` |
| **IP Edge Firewall & Anti-DDoS** | `GET`, `POST`, `PUT`, `DELETE` | `/ip`, `/ip/*` |

## 4. Add Credentials in OVH Nexus

1. Log in to your OVH Nexus WebUI.
2. Navigate to **API Accounts** (or **Settings**).
3. Enter your **App Key**, **App Secret**, **Consumer Key**, select the matching **Endpoint** (`ovh-eu`, `ovh-ca`, or `ovh-us`), and verify the **Zone / Subsidiary** (e.g., `IE`, `FR`, `DE`, `CA`, `SG`, `US`) is auto-detected or set to match your billing subsidiary.
4. Click **Verify / Save**. OVH Nexus will query `/me` to confirm connectivity and display your `nichandle` and billing subsidiary.
