import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

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
  ],
});
