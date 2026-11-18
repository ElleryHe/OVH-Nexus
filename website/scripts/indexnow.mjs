#!/usr/bin/env node
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const HOST = 'ovh-nexus.ellery.me';
const SITE_ORIGIN = `https://${HOST}`;
const INDEXNOW_KEY = 'd4e8f2a9c1b74e6095a2c3d8e1f0b6a4';
const KEY_LOCATION = `${SITE_ORIGIN}/${INDEXNOW_KEY}.txt`;
const INDEXNOW_ENDPOINT = 'https://api.indexnow.org/indexnow';

const args = new Set(process.argv.slice(2));
const waitForDeploy = args.has('--wait-for-deploy');
const dryRun = args.has('--dry-run');

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const localSitemapPath = path.resolve(__dirname, '../dist/sitemap-0.xml');

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function loadSitemapXml() {
  try {
    return await fs.readFile(localSitemapPath, 'utf-8');
  } catch {
    const res = await fetch(`${SITE_ORIGIN}/sitemap-0.xml`);
    if (!res.ok) {
      throw new Error(`Failed to fetch remote sitemap-0.xml: HTTP ${res.status}`);
    }
    return await res.text();
  }
}

function extractUrls(xml) {
  const urls = new Set();
  const regex = /<loc>(https:\/\/ovh-nexus\.ellery\.me\/[^<]*)<\/loc>/g;
  let match;
  while ((match = regex.exec(xml)) !== null) {
    urls.add(match[1].trim());
  }
  urls.add(`${SITE_ORIGIN}/llms.txt`);
  urls.add(`${SITE_ORIGIN}/llms-full.txt`);
  urls.add(`${SITE_ORIGIN}/rss.xml`);
  urls.add(`${SITE_ORIGIN}/feed.xml`);
  urls.add(`${SITE_ORIGIN}/zh-cn/rss.xml`);
  urls.add(`${SITE_ORIGIN}/fr/rss.xml`);
  return Array.from(urls);
}

async function verifyKeyLive() {
  const maxAttempts = waitForDeploy ? 12 : 1;
  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    try {
      const res = await fetch(`${KEY_LOCATION}?t=${Date.now()}`, {
        headers: { 'Cache-Control': 'no-cache' },
      });
      if (res.ok) {
        const body = (await res.text()).trim();
        if (body === INDEXNOW_KEY) {
          console.log(`[IndexNow] Verified live key file at ${KEY_LOCATION} (attempt ${attempt}/${maxAttempts}).`);
          return true;
        }
      }
      console.log(
        `[IndexNow] Key file not yet live at ${KEY_LOCATION} (HTTP ${res.status}, attempt ${attempt}/${maxAttempts}).`
      );
    } catch (err) {
      console.log(`[IndexNow] Key check attempt ${attempt}/${maxAttempts} failed: ${err.message}`);
    }
    if (attempt < maxAttempts) {
      await sleep(15_000);
    }
  }
  return false;
}

async function main() {
  const xml = await loadSitemapXml();
  const urlList = extractUrls(xml);
  console.log(`[IndexNow] Extracted ${urlList.length} canonical URLs for ${HOST}.`);

  if (dryRun) {
    console.log('[IndexNow] Dry run enabled. Sample URLs:', urlList.slice(0, 5));
    return;
  }

  const keyReady = await verifyKeyLive();
  if (!keyReady) {
    console.warn(
      `[IndexNow] Key file ${KEY_LOCATION} is not reachable on production yet; skipping IndexNow ping.`
    );
    return;
  }

  const payload = {
    host: HOST,
    key: INDEXNOW_KEY,
    keyLocation: KEY_LOCATION,
    urlList,
  };

  const res = await fetch(INDEXNOW_ENDPOINT, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json; charset=utf-8' },
    body: JSON.stringify(payload),
  });

  if (res.ok || res.status === 202) {
    console.log(`[IndexNow] Successfully submitted ${urlList.length} URLs (HTTP ${res.status}).`);
  } else {
    const errText = await res.text().catch(() => '');
    throw new Error(`IndexNow submission failed with HTTP ${res.status}: ${errText}`);
  }
}

main().catch((err) => {
  console.error(`[IndexNow] Error: ${err.message}`);
  process.exit(1);
});
