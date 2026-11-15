import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

function nexusSeoSitemapIntegration() {
  return {
    name: 'nexus-seo-sitemap',
    hooks: {
      'astro:build:done': async ({ dir, logger }) => {
        const outDir = fileURLToPath(dir);
        const sitemap0Path = path.join(outDir, 'sitemap-0.xml');
        const sitemapIdxPath = path.join(outDir, 'sitemap-index.xml');
        const sitemapAliasPath = path.join(outDir, 'sitemap.xml');
        const lastmod = new Date().toISOString().split('T')[0];

        try {
          let xml0 = await fs.readFile(sitemap0Path, 'utf-8');
          // Add x-default alternate link mirroring the English canonical URL and inject <lastmod>
          xml0 = xml0.replace(
            /<url><loc>(https:\/\/ovh-nexus\.ellery\.me\/[^<]*)<\/loc>((?:<xhtml:link[^>]*\/>)*)<\/url>/g,
            (_match, loc, links) => {
              const enMatch = links.match(/hreflang="en"\s+href="([^"]+)"/);
              const xDefault =
                enMatch && !links.includes('hreflang="x-default"')
                  ? `<xhtml:link rel="alternate" hreflang="x-default" href="${enMatch[1]}"/>`
                  : '';
              return `<url><loc>${loc}</loc><lastmod>${lastmod}</lastmod>${links}${xDefault}</url>`;
            }
          );
          await fs.writeFile(sitemap0Path, xml0, 'utf-8');
          // Also publish `/sitemap.xml` directly so Search Console & crawlers never hit 404
          await fs.writeFile(sitemapAliasPath, xml0, 'utf-8');

          let xmlIdx = await fs.readFile(sitemapIdxPath, 'utf-8');
          xmlIdx = xmlIdx.replace(
            /<sitemap><loc>([^<]+)<\/loc><\/sitemap>/g,
            `<sitemap><loc>$1</loc><lastmod>${lastmod}</lastmod></sitemap>`
          );
          await fs.writeFile(sitemapIdxPath, xmlIdx, 'utf-8');
          logger.info(`Generated /sitemap.xml and enriched sitemaps with <lastmod>${lastmod}</lastmod>`);
        } catch (err) {
          logger.warn(`Failed to enrich sitemap.xml: ${err}`);
        }
      },
    },
  };
}

export default defineConfig({
  site: 'https://ovh-nexus.ellery.me',
  integrations: [
    starlight({
      title: 'OVH Nexus',
      description:
        'Unified OVHcloud Control & Automated Bare-Metal / VPS Procurement Console Documentation.',
      logo: {
        src: './src/assets/logo.svg',
      },
      favicon: '/favicon.svg',
      customCss: ['./src/styles/custom.css'],
      head: [
        {
          tag: 'meta',
          attrs: {
            name: 'robots',
            content: 'index, follow, max-image-preview:large, max-snippet:-1, max-video-preview:-1',
          },
        },
        {
          tag: 'link',
          attrs: {
            rel: 'sitemap',
            type: 'application/xml',
            href: '/sitemap.xml',
          },
        },
        {
          tag: 'meta',
          attrs: {
            property: 'og:image',
            content: 'https://ovh-nexus.ellery.me/og-image.jpg',
          },
        },
        {
          tag: 'meta',
          attrs: {
            property: 'og:image:width',
            content: '1280',
          },
        },
        {
          tag: 'meta',
          attrs: {
            property: 'og:image:height',
            content: '640',
          },
        },
        {
          tag: 'meta',
          attrs: {
            name: 'twitter:image',
            content: 'https://ovh-nexus.ellery.me/og-image.jpg',
          },
        },
        {
          tag: 'script',
          content: `
            (() => {
              const applyNexusEnhancements = () => {
                const year = String(new Date().getFullYear());
                document.querySelectorAll('[data-nexus-year]').forEach((el) => {
                  el.textContent = year;
                });
                document.querySelectorAll('a[href^="http://"], a[href^="https://"]').forEach((a) => {
                  try {
                    const url = new URL(a.href, window.location.origin);
                    if (url.hostname !== window.location.hostname && url.hostname !== 'ovh-nexus.ellery.me') {
                      a.setAttribute('target', '_blank');
                      a.setAttribute('rel', 'noopener noreferrer');
                    }
                  } catch {}
                });
              };
              if (document.readyState === 'loading') {
                document.addEventListener('DOMContentLoaded', applyNexusEnhancements);
              } else {
                applyNexusEnhancements();
              }
              document.addEventListener('astro:page-load', applyNexusEnhancements);
            })();
          `,
        },
      ],
      defaultLocale: 'root',
      locales: {
        root: {
          label: 'English',
          lang: 'en',
        },
        fr: {
          label: 'Français',
          lang: 'fr',
        },
        'zh-cn': {
          label: '简体中文',
          lang: 'zh-CN',
        },
      },
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/ElleryHe/OVH-Nexus',
        },
      ],
      editLink: {
        baseUrl: 'https://github.com/ElleryHe/OVH-Nexus/edit/main/website/',
      },
      lastUpdated: false,
      sidebar: [
        {
          label: 'Getting Started',
          translations: {
            fr: 'Démarrage',
            'zh-CN': '快速上手',
          },
          items: [
            { slug: 'getting-started/introduction' },
            { slug: 'getting-started/quick-start' },
            { slug: 'getting-started/installation' },
            { slug: 'getting-started/reverse-proxy' },
          ],
        },
        {
          label: 'Configuration',
          translations: {
            fr: 'Configuration',
            'zh-CN': '配置参考',
          },
          items: [
            { slug: 'configuration/environment-variables' },
            { slug: 'configuration/ovh-api-credentials' },
            { slug: 'configuration/multi-account' },
            { slug: 'configuration/data-persistence' },
          ],
        },
        {
          label: 'Feature Guides',
          translations: {
            fr: 'Guides des Fonctionnalités',
            'zh-CN': '功能指南',
          },
          items: [
            { slug: 'features/dedicated-servers' },
            { slug: 'features/vps' },
            { slug: 'features/orders-delivery' },
            { slug: 'features/renewals' },
            { slug: 'features/availability-matrix' },
            { slug: 'features/auto-order-queue' },
            { slug: 'features/sku-watcher' },
            { slug: 'features/notification-center' },
            { slug: 'features/mcp-gateway' },
          ],
        },
        {
          label: 'Operations & Security',
          translations: {
            fr: 'Exploitation & Sécurité',
            'zh-CN': '运维与安全',
          },
          items: [
            { slug: 'operations/security' },
            { slug: 'operations/troubleshooting' },
            { slug: 'operations/faq' },
          ],
        },
        {
          label: 'About',
          translations: {
            fr: 'À propos',
            'zh-CN': '关于项目',
          },
          items: [
            { slug: 'about/changelog' },
            { slug: 'about/contributing' },
            { slug: 'about/license' },
          ],
        },
      ],
    }),
    nexusSeoSitemapIntegration(),
  ],
});
