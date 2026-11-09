---
title: Services Expiration & Renewal Matrix
description: Monitor expiration countdowns and renewal modes across all Dedicated Servers and VPS instances.
---

When managing dozens of Dedicated Servers and VPS instances across multiple OVHcloud accounts, missing a manual renewal deadline can result in permanent server termination and data loss.

The **Services Expiration & Renewal Overview** panel (located on the **Dashboard** and powered by `/api/ovh/services/renewals`) aggregates `/dedicated/server/{name}/serviceInfos` and `/vps/{name}/serviceInfos` into a single sorted matrix.

## Expiration Health Tiers

Services are automatically sorted by shortest remaining time (`daysLeft` ascending) and color-coded into three urgency tiers:

| Tier | Condition | Visual Badge | Action Needed |
| :--- | :--- | :--- | :--- |
| **Critical** | `daysLeft <= 7` | Red Alert | Renew immediately if set to Manual or Terminate at expiry. |
| **Warning** | `8 <= daysLeft <= 30` | Amber Warning | Verify billing method or upcoming renewal schedule. |
| **Healthy** | `daysLeft > 30` | Emerald Healthy | No immediate action required. |

## Renewal Modes Explained

For every Dedicated Server and VPS, OVH Nexus inspects the `renew` object in `serviceInfos` and classifies the service into one of three modes:

- **Auto Renew (`automatic: true`)**: OVHcloud will automatically bill your default payment method at the renewal cycle (`period` in months).
- **Manual Renew (`manualPayment: true`)**: You must manually pay the renewal order before the `expiration` date.
- **Terminate at Expiry (`deleteAtExpiration: true`)**: Cancellation at expiration has been requested; the server will be decommissioned when the billing period ends.

## Expiration Alert Notifications (`service.expiring`)

In the **Notification Center**, enable the `service.expiring` event and configure the **Expiration Alert Threshold (`expiryDays`, default `7` days, range `1–90` days)**. OVH Nexus will push an alert whenever a service approaches expiration within your configured window.
