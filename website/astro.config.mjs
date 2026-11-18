import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const SITE_URL = 'https://ovh-nexus.ellery.me';

const FALLBACK_RELEASE_DATES = {
  '1.3.4': '2026-11-18',
  '1.3.3': '2026-11-17',
  '1.3.2': '2026-11-17',
  '1.3.1': '2026-11-17',
  '1.3.0': '2026-11-16',
  '1.2.3': '2026-11-16',
  '1.2.2': '2026-11-15',
  '1.2.1': '2026-11-15',
  '1.2.0': '2026-11-15',
  '1.1.1': '2026-11-12',
  '1.1.0': '2026-11-09',
  '1.0.1': '2026-11-09',
  '1.0.0': '2026-11-08',
};

function escapeXml(str) {
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&apos;');
}

function renderInlineMarkdownToHtml(text, localePrefix = '') {
  const codeTokens = [];
  let working = String(text).replace(/`([^`]+)`/g, (_m, code) => {
    const idx = codeTokens.length;
    codeTokens.push(`<code>${escapeXml(code)}</code>`);
    return `\x00CODE_${idx}\x00`;
  });

  working = working
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');

  working = working.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_m, label, rawHref) => {
    let href = rawHref.trim();
    if (href.startsWith('/')) {
      href = `${SITE_URL}${href}`;
    } else if (!/^https?:\/\//i.test(href) && !href.startsWith('#')) {
      const base = localePrefix ? `${SITE_URL}/${localePrefix}` : SITE_URL;
      href = `${base}/${href.replace(/^\.\//, '')}`;
    }
    return `<a href="${href.replace(/"/g, '&quot;')}">${label}</a>`;
  });

  working = working.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');

  return working.replace(/\x00CODE_(\d+)\x00/g, (_m, idx) => codeTokens[Number(idx)] || '');
}

function markdownReleaseNotesToHtml(markdownBody, localePrefix = '') {
  const cleaned = markdownBody
    .replace(/^---[ \t]*$/gm, '')
    .replace(
      /^:::(note|tip|caution|warning|danger)(?:\[([^\]]*)\])?[ \t]*\r?\n([\s\S]*?)\r?\n:::[ \t]*$/gm,
      (_m, type, customTitle, inner) => {
        const label = customTitle ? customTitle.trim() : type.toUpperCase();
        const bodyLine = inner.trim().replace(/\r?\n/g, ' ');
        return `> **${label}**: ${bodyLine}`;
      }
    )
    .trim();

  const lines = cleaned.split(/\r?\n/);
  const htmlLines = [];
  let inTopUl = false;
  let inTopLi = false;
  let inSubUl = false;

  const closeSubUl = () => {
    if (inSubUl) {
      htmlLines.push('</ul>');
      inSubUl = false;
    }
  };
  const closeTopLi = () => {
    closeSubUl();
    if (inTopLi) {
      htmlLines.push('</li>');
      inTopLi = false;
    }
  };
  const closeTopUl = () => {
    closeTopLi();
    if (inTopUl) {
      htmlLines.push('</ul>');
      inTopUl = false;
    }
  };

  for (const rawLine of lines) {
    const trimmed = rawLine.trim();
    if (!trimmed) continue;

    const h3Match = rawLine.match(/^###\s+(.+)$/);
    if (h3Match) {
      closeTopUl();
      htmlLines.push(`<h3>${renderInlineMarkdownToHtml(h3Match[1].trim(), localePrefix)}</h3>`);
      continue;
    }

    const quoteMatch = rawLine.match(/^>\s+(.+)$/);
    if (quoteMatch) {
      closeTopUl();
      htmlLines.push(
        `<blockquote><p>${renderInlineMarkdownToHtml(quoteMatch[1].trim(), localePrefix)}</p></blockquote>`
      );
      continue;
    }

    const subBulletMatch = rawLine.match(/^[ \t]{2,}-\s+(.+)$/);
    if (subBulletMatch) {
      if (!inTopUl) {
        inTopUl = true;
        htmlLines.push('<ul>');
      }
      if (!inTopLi) {
        inTopLi = true;
        htmlLines.push('<li>');
      }
      if (!inSubUl) {
        inSubUl = true;
        htmlLines.push('<ul>');
      }
      htmlLines.push(`<li>${renderInlineMarkdownToHtml(subBulletMatch[1].trim(), localePrefix)}</li>`);
      continue;
    }

    const topBulletMatch = rawLine.match(/^-\s+(.+)$/);
    if (topBulletMatch) {
      if (!inTopUl) {
        inTopUl = true;
        htmlLines.push('<ul>');
      } else {
        closeTopLi();
      }
      inTopLi = true;
      htmlLines.push(`<li>${renderInlineMarkdownToHtml(topBulletMatch[1].trim(), localePrefix)}`);
      continue;
    }

    closeTopUl();
    htmlLines.push(`<p>${renderInlineMarkdownToHtml(trimmed, localePrefix)}</p>`);
  }

  closeTopUl();
  return htmlLines.join('\n');
}

function parseChangelogReleases(rawMarkdown, localePrefix = '', fallbackDate = '2026-11-18') {
  const { body } = parseDocFrontmatter(rawMarkdown);
  const headingRegex = /^##\s+(?:\[?v?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)\]?)(.*)$/gm;
  const matches = [];
  let match;
  while ((match = headingRegex.exec(body)) !== null) {
    matches.push({
      version: match[1].trim(),
      rest: (match[2] || '').trim(),
      headerStart: match.index,
      contentStart: match.index + match[0].length,
    });
  }

  const changelogUrl = localePrefix
    ? `${SITE_URL}/${localePrefix}/about/changelog/`
    : `${SITE_URL}/about/changelog/`;
  const localeKey = localePrefix || 'en';

  return matches.map((entry, idx) => {
    const contentEnd = idx + 1 < matches.length ? matches[idx + 1].headerStart : body.length;
    const sectionMarkdown = body.slice(entry.contentStart, contentEnd).trim();

    let dateStr = FALLBACK_RELEASE_DATES[entry.version] || fallbackDate;
    let subtitle = entry.rest;
    const dateMatch = entry.rest.match(/^[-—–]\s*(\d{4}-\d{2}-\d{2})(?:\s*[-—–]\s*(.*))?$/);
    if (dateMatch) {
      dateStr = dateMatch[1];
      subtitle = (dateMatch[2] || '').trim();
    } else {
      subtitle = entry.rest.replace(/^[-—–]\s*/, '').trim();
    }

    const cleanSubtitle = subtitle.replace(/`([^`]+)`/g, '$1').trim();
    const title = cleanSubtitle ? `OVH Nexus v${entry.version} — ${cleanSubtitle}` : `OVH Nexus v${entry.version}`;
    const anchor = entry.version.replace(/[^0-9a-zA-Z]/g, '');
    const pubDate = new Date(`${dateStr}T00:00:00Z`).toUTCString();
    const htmlDescription = markdownReleaseNotesToHtml(sectionMarkdown, localePrefix).replace(
      /]]>/g,
      ']]]]><![CDATA[>'
    );

    return {
      version: entry.version,
      date: dateStr,
      pubDate,
      title,
      link: `${changelogUrl}#${anchor}`,
      guid: `ovh-nexus-${localeKey}-v${entry.version}`,
      htmlDescription,
    };
  });
}

function buildRssXml({ title, description, link, selfUrl, language, items }) {
  const lastBuildDate = items.length > 0 ? items[0].pubDate : new Date().toUTCString();
  const itemXml = items
    .map(
      (item) => `    <item>
      <title>${escapeXml(item.title)}</title>
      <link>${escapeXml(item.link)}</link>
      <guid isPermaLink="false">${escapeXml(item.guid)}</guid>
      <pubDate>${item.pubDate}</pubDate>
      <description><![CDATA[${item.htmlDescription}]]></description>
    </item>`
    )
    .join('\n');

  return `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">
  <channel>
    <title>${escapeXml(title)}</title>
    <link>${escapeXml(link)}</link>
    <description>${escapeXml(description)}</description>
    <language>${escapeXml(language)}</language>
    <lastBuildDate>${lastBuildDate}</lastBuildDate>
    <atom:link href="${escapeXml(selfUrl)}" rel="self" type="application/rss+xml" />
${itemXml}
  </channel>
</rss>
`;
}

const sidebar = [
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
];

function parseDocFrontmatter(raw) {
  const match = raw.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n?([\s\S]*)$/);
  if (!match) {
    return { title: '', description: '', body: raw.trim() };
  }
  const fm = match[1];
  const body = match[2];
  const extractScalar = (key) => {
    const lineMatch = fm.match(new RegExp(`^${key}:\\s*(.+)$`, 'm'));
    if (!lineMatch) return '';
    return lineMatch[1].trim().replace(/^['"]|['"]$/g, '');
  };
  return {
    title: extractScalar('title'),
    description: extractScalar('description'),
    body: body.trim(),
  };
}

function cleanDocMarkdown(body) {
  return body
    .replace(/^import\s+[^\r\n]+[\r\n]*/gm, '')
    .replace(/^[ \t]*<CardGrid[^>]*>[ \t]*\r?\n?/gm, '')
    .replace(/^[ \t]*<\/CardGrid>[ \t]*\r?\n?/gm, '')
    .replace(/^[ \t]*<Card\s+title="([^"]+)"[^>]*>([\s\S]*?)<\/Card>/gm, (_m, title, content) => {
      const cleaned = content.trim().replace(/\s+/g, ' ');
      return `- **${title}**: ${cleaned}`;
    })
    .replace(/<footer[\s\S]*?<\/footer>/g, '')
    .replace(/\n{3,}/g, '\n\n')
    .trim();
}

async function resolveDocFile(docsRoot, slug) {
  for (const ext of ['.md', '.mdx']) {
    const candidate = path.join(docsRoot, `${slug}${ext}`);
    try {
      await fs.access(candidate);
      return candidate;
    } catch {
      // try next extension
    }
  }
  return null;
}

async function walkHtmlFiles(dir) {
  const entries = await fs.readdir(dir, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    const fullPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await walkHtmlFiles(fullPath)));
    } else if (entry.isFile() && entry.name.endsWith('.html')) {
      files.push(fullPath);
    }
  }
  return files;
}

function nexusSeoSitemapIntegration() {
  return {
    name: 'nexus-seo-sitemap',
    hooks: {
      'astro:build:done': async ({ dir, logger }) => {
        const outDir = fileURLToPath(dir);
        const docsRoot = fileURLToPath(new URL('./src/content/docs', import.meta.url));
        const sitemap0Path = path.join(outDir, 'sitemap-0.xml');
        const sitemapIdxPath = path.join(outDir, 'sitemap-index.xml');
        const sitemapAliasPath = path.join(outDir, 'sitemap.xml');
        const lastmod = new Date().toISOString().split('T')[0];

        // 1. Enrich sitemaps with <lastmod> and hreflang="x-default"
        try {
          let xml0 = await fs.readFile(sitemap0Path, 'utf-8');
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

        // 2. Preview environment X-Robots-Tag: noindex, nofollow injection
        const isPreviewEnv =
          (process.env.CF_PAGES === '1' &&
            Boolean(process.env.CF_PAGES_BRANCH) &&
            process.env.CF_PAGES_BRANCH !== 'main') ||
          process.env.VERCEL_ENV === 'preview' ||
          process.env.CONTEXT === 'deploy-preview';

        if (isPreviewEnv) {
          try {
            const headersPath = path.join(outDir, '_headers');
            let headersContent = await fs.readFile(headersPath, 'utf-8');
            if (!headersContent.includes('X-Robots-Tag: noindex, nofollow')) {
              headersContent = headersContent.replace(
                /^\/\*\r?\n/m,
                '/*\n  X-Robots-Tag: noindex, nofollow\n'
              );
              await fs.writeFile(headersPath, headersContent, 'utf-8');
              logger.info('Injected preview-environment X-Robots-Tag: noindex, nofollow into dist/_headers');
            }
          } catch (err) {
            logger.warn(`Failed to update dist/_headers for preview environment: ${err}`);
          }
        }

        // 3. Historical Version Isolation pass for legacy/archived routes
        try {
          const htmlFiles = await walkHtmlFiles(outDir);
          const legacyRouteRegex = /^\/(?:(fr|zh-cn)\/)?(v\d+(?:\.\d+)*|archive|legacy|old)\/(.*)$/i;
          let isolatedCount = 0;

          for (const htmlFile of htmlFiles) {
            const relPath = '/' + path.relative(outDir, htmlFile).replace(/\\/g, '/');
            const routePath = relPath.replace(/\/index\.html$/i, '/');
            const match = routePath.match(legacyRouteRegex);
            if (!match) continue;

            const localePrefix = match[1] ? `/${match[1].toLowerCase()}` : '';
            const restPath = match[3] || '';
            const latestCanonical = `${SITE_URL}${localePrefix}/${restPath}`.replace(/\/+/g, '/').replace('https:/', 'https://');

            let html = await fs.readFile(htmlFile, 'utf-8');
            html = html.replace(
              /<meta\s+name="robots"\s+content="[^"]*"\s*\/?>/i,
              '<meta name="robots" content="noindex, follow"/>'
            );
            html = html.replace(
              /<link\s+rel="canonical"\s+href="[^"]*"\s*\/?>/i,
              `<link rel="canonical" href="${latestCanonical}"/>`
            );
            await fs.writeFile(htmlFile, html, 'utf-8');
            isolatedCount++;
          }
          if (isolatedCount > 0) {
            logger.info(`Isolated ${isolatedCount} legacy/archived HTML page(s) with noindex, follow and latest canonical URL`);
          }
        } catch (err) {
          logger.warn(`Historical version isolation pass failed: ${err}`);
        }

        // 4. Auto-generate /llms.txt and /llms-full.txt (English canonical docs)
        try {
          const llmsLines = [
            '# OVH Nexus',
            '',
            '> Self-hosted, multi-account OVHcloud infrastructure control, real-time bare-metal/VPS stock monitoring, public SKU change watcher, automated server procurement console, and privacy-shielded MCP AI Gateway (Single static Go binary + embedded React SPA + SQLite WAL).',
            '',
            `- Official Documentation: ${SITE_URL}/`,
            `- Full LLM Context (Single-File Markdown): ${SITE_URL}/llms-full.txt`,
            `- Changelog RSS 2.0 Feed: ${SITE_URL}/rss.xml (Alias: ${SITE_URL}/feed.xml · 简体中文: ${SITE_URL}/zh-cn/rss.xml · Français: ${SITE_URL}/fr/rss.xml)`,
            '- GitHub Repository: https://github.com/ElleryHe/OVH-Nexus',
            '- Docker Hub Image: https://hub.docker.com/r/elleryhe/ovh-nexus',
            `- Translations: [简体中文](${SITE_URL}/zh-cn/) · [Français](${SITE_URL}/fr/)`,
            '',
          ];

          const fullSections = [
            [
              '# OVH Nexus — Full Documentation Context',
              '',
              '> Self-hosted, multi-account OVHcloud infrastructure control, real-time bare-metal/VPS stock monitoring, public SKU change watcher, automated server procurement console, and privacy-shielded MCP AI Gateway (Single static Go binary + embedded React SPA + SQLite WAL).',
              '',
              `Official Documentation: ${SITE_URL}/`,
              `Summary Index: ${SITE_URL}/llms.txt`,
              `Changelog RSS Feed: ${SITE_URL}/rss.xml`,
              'GitHub Repository: https://github.com/ElleryHe/OVH-Nexus',
            ].join('\n'),
          ];

          const indexFile = await resolveDocFile(docsRoot, 'index');
          if (indexFile) {
            const rawIndex = await fs.readFile(indexFile, 'utf-8');
            const parsedIndex = parseDocFrontmatter(rawIndex);
            const cleanedIndex = cleanDocMarkdown(parsedIndex.body);
            if (cleanedIndex) {
              const headerLines = [
                `# ${parsedIndex.title || 'OVH Nexus Documentation'}`,
                `Canonical URL: ${SITE_URL}/`,
              ];
              if (parsedIndex.description) {
                headerLines.push(`Description: ${parsedIndex.description}`);
              }
              fullSections.push(`${headerLines.join('\n')}\n\n${cleanedIndex}`);
            }
          }

          for (const section of sidebar) {
            llmsLines.push(`## ${section.label}`, '');
            for (const item of section.items) {
              const filePath = await resolveDocFile(docsRoot, item.slug);
              if (!filePath) continue;
              const raw = await fs.readFile(filePath, 'utf-8');
              const { title, description, body } = parseDocFrontmatter(raw);
              const pageTitle = title || item.slug;
              const pageUrl = `${SITE_URL}/${item.slug}/`;
              const descSuffix = description ? `: ${description}` : '';
              llmsLines.push(`- [${pageTitle}](${pageUrl})${descSuffix}`);

              const cleanedBody = cleanDocMarkdown(body);
              const headerLines = [
                `# ${pageTitle}`,
                `Canonical URL: ${pageUrl}`,
                `Section: ${section.label}`,
              ];
              if (description) {
                headerLines.push(`Description: ${description}`);
              }
              fullSections.push(`${headerLines.join('\n')}\n\n${cleanedBody}`);
            }
            llmsLines.push('');
          }

          const llmsTxtPath = path.join(outDir, 'llms.txt');
          const llmsFullTxtPath = path.join(outDir, 'llms-full.txt');
          await fs.writeFile(llmsTxtPath, llmsLines.join('\n').trim() + '\n', 'utf-8');
          await fs.writeFile(llmsFullTxtPath, fullSections.join('\n\n---\n\n').trim() + '\n', 'utf-8');
          logger.info('Generated /llms.txt and /llms-full.txt for AI search and LLM agents');
        } catch (err) {
          logger.warn(`Failed to generate /llms.txt and /llms-full.txt: ${err}`);
        }

        // 5. Auto-generate multi-lingual RSS 2.0 Changelog feeds (/rss.xml, /feed.xml, /zh-cn/rss.xml, /fr/rss.xml)
        //    and enrich changelog HTML headings with short #134 version anchor IDs
        try {
          const feedConfigs = [
            {
              localePrefix: '',
              docRelPath: 'about/changelog.md',
              outPaths: [path.join(outDir, 'rss.xml'), path.join(outDir, 'feed.xml')],
              selfUrl: `${SITE_URL}/rss.xml`,
              link: `${SITE_URL}/about/changelog/`,
              language: 'en',
              title: 'OVH Nexus Changelog',
              description:
                'Official release history, new features, security advisories, and notable changes for OVH Nexus.',
              htmlPath: path.join(outDir, 'about/changelog/index.html'),
            },
            {
              localePrefix: 'zh-cn',
              docRelPath: 'zh-cn/about/changelog.md',
              outPaths: [path.join(outDir, 'zh-cn/rss.xml')],
              selfUrl: `${SITE_URL}/zh-cn/rss.xml`,
              link: `${SITE_URL}/zh-cn/about/changelog/`,
              language: 'zh-CN',
              title: 'OVH Nexus 版本更新日志 (Changelog)',
              description: 'OVH Nexus 官方版本发布历史、新增功能与安全修复订阅源。',
              htmlPath: path.join(outDir, 'zh-cn/about/changelog/index.html'),
            },
            {
              localePrefix: 'fr',
              docRelPath: 'fr/about/changelog.md',
              outPaths: [path.join(outDir, 'fr/rss.xml')],
              selfUrl: `${SITE_URL}/fr/rss.xml`,
              link: `${SITE_URL}/fr/about/changelog/`,
              language: 'fr',
              title: 'OVH Nexus — Journal des Modifications (Changelog)',
              description:
                "Historique officiel des versions, nouvelles fonctionnalités et correctifs de sécurité d'OVH Nexus.",
              htmlPath: path.join(outDir, 'fr/about/changelog/index.html'),
            },
          ];

          for (const cfg of feedConfigs) {
            const mdPath = path.join(docsRoot, cfg.docRelPath);
            const rawMd = await fs.readFile(mdPath, 'utf-8');
            const items = parseChangelogReleases(rawMd, cfg.localePrefix, lastmod);
            const rssXml = buildRssXml({
              title: cfg.title,
              description: cfg.description,
              link: cfg.link,
              selfUrl: cfg.selfUrl,
              language: cfg.language,
              items,
            });

            for (const targetPath of cfg.outPaths) {
              await fs.mkdir(path.dirname(targetPath), { recursive: true });
              await fs.writeFile(targetPath, rssXml, 'utf-8');
            }

            try {
              let changelogHtml = await fs.readFile(cfg.htmlPath, 'utf-8');
              changelogHtml = changelogHtml.replace(
                /<div class="sl-heading-wrapper level-h2"><h2 id="((\d{3,})(?:---[^"]*)?)">/g,
                '<div class="sl-heading-wrapper level-h2" id="$2"><h2 id="$1">'
              );
              await fs.writeFile(cfg.htmlPath, changelogHtml, 'utf-8');
            } catch {
              // ignore if changelog HTML not present
            }
          }
          logger.info('Generated /rss.xml, /feed.xml, /zh-cn/rss.xml, and /fr/rss.xml Changelog RSS 2.0 feeds');
        } catch (err) {
          logger.warn(`Failed to generate RSS 2.0 Changelog feeds: ${err}`);
        }
      },
    },
  };
}

export default defineConfig({
  site: SITE_URL,
  integrations: [
    starlight({
      title: 'OVH Nexus',
      description:
        'Unified OVHcloud Control & Automated Bare-Metal / VPS Procurement Console Documentation.',
      logo: {
        src: './src/assets/logo.png',
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
          tag: 'link',
          attrs: {
            rel: 'alternate',
            type: 'application/rss+xml',
            title: 'OVH Nexus Changelog (RSS)',
            href: 'https://ovh-nexus.ellery.me/rss.xml',
          },
        },
        {
          tag: 'link',
          attrs: {
            rel: 'alternate',
            type: 'application/rss+xml',
            title: 'OVH Nexus 版本更新日志 (RSS - 简体中文)',
            hreflang: 'zh-CN',
            href: 'https://ovh-nexus.ellery.me/zh-cn/rss.xml',
          },
        },
        {
          tag: 'link',
          attrs: {
            rel: 'alternate',
            type: 'application/rss+xml',
            title: 'OVH Nexus Journal des Modifications (RSS - Français)',
            hreflang: 'fr',
            href: 'https://ovh-nexus.ellery.me/fr/rss.xml',
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
      sidebar,
    }),
    nexusSeoSitemapIntegration(),
  ],
});
