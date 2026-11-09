---
title: Frequently Asked Questions (FAQ)
description: Answers to common questions about OVH Nexus, API keys, auto-ordering, pricing, and notifications.
---

### Does OVH Nexus require an OVHcloud API key?

**Not for public stock and SKU monitoring.** You can deploy OVH Nexus and immediately use the **Live Availability Matrix** (including commercial model names, hardware specs, and configuration-level monthly pricing), the **Public SKU Change Watcher**, and **Telegram / SMTP notifications** for `stock.available` and `sku.changed` without an OVHcloud API key.

An OVHcloud API key is only required for account-specific features: **Dedicated Server Control**, **Purchased VPS Control**, **Order & Delivery Tracking**, **Renewal Matrix**, and **Automated Order Placement**.

---

### Will automated ordering charge my credit card or PayPal automatically?

By default, OVH Nexus generates an official order via `/order/cart/{cartId}/checkout` with `autoPayWithPreferredPaymentMethod: false` (unless auto-pay is explicitly requested/supported by your account setup). This locks in the server allocation and sends you an `order.success` notification containing the **official OVHcloud payment link** so you can review and complete payment in your browser.

---

### Why do some newly racked SKUs in the SKU Watcher show no price yet?

When OVHcloud engineers first register a brand-new server code (for example `27skle01-v1`) in `/dedicated/server/datacenter/availabilities`, it often appears in the datacenter availability feed before OVHcloud publishes its commercial pricing in `/order/catalog/public/eco` or `/baremetal`.

During this pre-release window, OVH Nexus uses its deterministic fallback parser (`infer_plan_from_code`) to infer the commercial family (`KS-LE-1`) and hardware specs (`64 GB DDR4 ECC`, `2 × 512 GB NVMe`), and automatically fills in the monthly price once OVHcloud adds the plan to the public catalog.

---

### Can I monitor OVH Europe (`ovh-eu`), Canada (`ovh-ca`), and US (`ovh-us`) at the same time?

Yes. You can add accounts from any combination of `ovh-eu`, `ovh-ca`, and `ovh-us` in **API Accounts** and switch the active account context at any time from the top navigation bar.

---

### How do I change the default language of the WebUI?

Set `DEFAULT_LOCALE` in your `.env` file to `en` (English), `fr` (Français), or `zh-CN` (简体中文), or click the `EN | FR | 中文` language selector in the login screen or sidebar at any time (saved per browser).
