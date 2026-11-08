import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react';
import { api } from '@/utils/apiClient';

export type Locale = 'en' | 'fr' | 'zh-CN';
export type SupportedLocale = Locale;

const STORAGE_KEY = 'ovh_nexus_locale';

const translations: Record<Locale, Record<string, string>> = {
  en: {
    // Brand
    'brand.name': 'OVH Nexus',
    'brand.subtitle': 'OVHcloud Control & Procurement Console',
    'brand.shortSubtitle': 'OVHcloud Control Console',
    'brand.disclaimer': 'Unofficial third-party tool. Not affiliated with OVH SAS / OVHcloud.',

    // Auth / Login
    'auth.title': 'Sign in to OVH Nexus',
    'auth.subtitle': 'Enter your console access key to manage OVHcloud infrastructure and procurement.',
    'auth.placeholder': 'Enter API Secret Key...',
    'auth.submit': 'Sign In',
    'auth.verifying': 'Authenticating...',
    'auth.error.empty': 'Please enter your access key',
    'auth.error.invalid': 'Invalid access key, please try again',
    'auth.error.network': 'Authentication failed, please check backend connection',

    // Navigation Groups
    'nav.group.overview': 'Overview',
    'nav.group.fleet': 'Cloud Fleet',
    'nav.group.procurement': 'Procurement & Stock',
    'nav.group.system': 'Account & System',

    // Navigation Items
    'nav.dashboard': 'Dashboard',
    'nav.dedicated': 'Dedicated Servers',
    'nav.vps': 'VPS & Cloud Services',
    'nav.servers': 'Server Catalog',
    'nav.availability': 'Live Availability',
    'nav.queue': 'Order Queue',
    'nav.monitor': 'Stock Monitor',
    'nav.vpsMonitor': 'VPS Monitor',
    'nav.history': 'Purchase History',
    'nav.account': 'Account, Orders & SSH',
    'nav.notifications': 'Notification Center',
    'nav.logs': 'System Logs',
    'nav.settings': 'Settings & API',

    // Sidebar Footer
    'sidebar.apiStatus': 'OVHcloud API',
    'sidebar.connected': 'Connected',
    'sidebar.notConfigured': 'Unconfigured',
    'sidebar.logout': 'Lock Console',
    'sidebar.collapse': 'Collapse',
    'sidebar.expand': 'Expand',
    'theme.light': 'Light',
    'theme.dark': 'Dark',
    'theme.system': 'Auto',

    // Dashboard
    'dashboard.title': 'OVH Nexus Overview',
    'dashboard.subtitle': 'Unified OVHcloud Fleet Operations & Automated Server Procurement',
    'dashboard.refresh': 'Refresh Status',
    'dashboard.refreshing': 'Refreshing...',
    'dashboard.apiNotConfigured': 'OVHcloud API Not Configured',
    'dashboard.apiNotConfiguredDesc': 'Configure your OVHcloud API credentials in Settings to unlock server control, account orders, and automated procurement.',
    'dashboard.configureApi': 'Configure API',
    'dashboard.stat.servers': 'Catalog Models',
    'dashboard.stat.serversLoading': 'Loading catalog...',
    'dashboard.stat.serversReady': 'Dedicated server plans indexed',
    'dashboard.stat.activeQueues': 'Active Queues',
    'dashboard.stat.totalQueues': 'of {total} total tasks',
    'dashboard.stat.successOrders': 'Completed Orders',
    'dashboard.stat.failedOrders': '{failed} failed attempts',
    'dashboard.stat.apiNode': 'API Endpoint',
    'dashboard.stat.apiReady': 'Authenticated & Ready',
    'dashboard.stat.apiMissing': 'Credentials Required',
    'dashboard.quickAccess': 'Cloud Fleet & Procurement Shortcuts',
    'dashboard.shortcut.dedicated': 'Dedicated Fleet Control',
    'dashboard.shortcut.dedicatedDesc': 'IPMI/KVM, reboot, OS reinstall with SSH key, and Edge Firewall',
    'dashboard.shortcut.orders': 'Orders & Delivery Tracker',
    'dashboard.shortcut.ordersDesc': 'Track unpaid orders, payment links, and 4-step server delivery progress',
    'dashboard.shortcut.renewals': 'Services & Renewals',
    'dashboard.shortcut.renewalsDesc': 'Monitor expiration countdowns and auto-renewal status across fleet',
    'dashboard.shortcut.vps': 'VPS Fleet Console',
    'dashboard.shortcut.vpsDesc': 'Inspect VPS state, vCore/RAM specs, IPv4/IPv6, and trigger reboots',
    'dashboard.shortcut.availability': 'Live Datacenter Stock',
    'dashboard.shortcut.availabilityDesc': 'Real-time availability matrix across global OVHcloud datacenters',
    'dashboard.shortcut.ssh': 'SSH Key Registry',
    'dashboard.shortcut.sshDesc': 'Manage `/me/sshKey` public keys for automated OS installations',
    'dashboard.queueSection': 'Active Procurement Queue',
    'dashboard.viewAll': 'View All',
    'dashboard.noQueues': 'No active procurement tasks',
    'dashboard.newQueue': 'Create Task',
    'dashboard.historySection': 'Recent Procurement History',
    'dashboard.noHistory': 'No purchase records yet',

    // Account & Cloud Fleet Page
    'account.title': 'Cloud Fleet & Account Hub',
    'account.subtitle': 'Manage OVHcloud orders, service renewals, SSH keys, VPS instances, emails, and refunds',
    'account.tab.orders': 'Orders & Delivery',
    'account.tab.services': 'Services & Renewals',
    'account.tab.ssh': 'SSH Keys',
    'account.tab.vps': 'VPS Fleet',
    'account.tab.emails': 'Email History',
    'account.tab.refunds': 'Credit Notes & Refunds',
  },
  fr: {
    // Brand
    'brand.name': 'OVH Nexus',
    'brand.subtitle': 'Console de Contrôle & Approvisionnement OVHcloud',
    'brand.shortSubtitle': 'Console OVHcloud',
    'brand.disclaimer': 'Outil tiers non officiel. Non affilié à OVH SAS / OVHcloud.',

    // Auth / Login
    'auth.title': 'Connexion à OVH Nexus',
    'auth.subtitle': 'Saisissez votre clé d’accès pour gérer votre infrastructure et vos commandes OVHcloud.',
    'auth.placeholder': 'Clé secrète API...',
    'auth.submit': 'Se connecter',
    'auth.verifying': 'Vérification...',
    'auth.error.empty': 'Veuillez saisir votre clé d’accès',
    'auth.error.invalid': 'Clé d’accès invalide, veuillez réessayer',
    'auth.error.network': 'Échec de connexion au serveur backend',

    // Navigation Groups
    'nav.group.overview': 'Vue d’ensemble',
    'nav.group.fleet': 'Flotte Cloud',
    'nav.group.procurement': 'Approvisionnement & Stock',
    'nav.group.system': 'Compte & Système',

    // Navigation Items
    'nav.dashboard': 'Tableau de bord',
    'nav.dedicated': 'Serveurs Dédiés',
    'nav.vps': 'VPS & Services Cloud',
    'nav.servers': 'Catalogue Serveurs',
    'nav.availability': 'Disponibilité Live',
    'nav.queue': 'File de Commande',
    'nav.monitor': 'Moniteur de Stock',
    'nav.vpsMonitor': 'Moniteur VPS',
    'nav.history': 'Historique d’Achats',
    'nav.account': 'Compte, Commandes & SSH',
    'nav.notifications': 'Centre de Notifications',
    'nav.logs': 'Journaux Système',
    'nav.settings': 'Paramètres & API',

    // Sidebar Footer
    'sidebar.apiStatus': 'API OVHcloud',
    'sidebar.connected': 'Connecté',
    'sidebar.notConfigured': 'Non configuré',
    'sidebar.logout': 'Verrouiller',
    'sidebar.collapse': 'Réduire',
    'sidebar.expand': 'Développer',
    'theme.light': 'Clair',
    'theme.dark': 'Sombre',
    'theme.system': 'Auto',

    // Dashboard
    'dashboard.title': 'Vue d’ensemble OVH Nexus',
    'dashboard.subtitle': 'Gestion unifiée de flotte OVHcloud & approvisionnement automatisé de serveurs',
    'dashboard.refresh': 'Actualiser',
    'dashboard.refreshing': 'Actualisation...',
    'dashboard.apiNotConfigured': 'API OVHcloud non configurée',
    'dashboard.apiNotConfiguredDesc': 'Configurez vos identifiants API OVHcloud dans Paramètres pour activer la gestion de flotte et les commandes.',
    'dashboard.configureApi': 'Configurer l’API',
    'dashboard.stat.servers': 'Modèles Catalogue',
    'dashboard.stat.serversLoading': 'Chargement...',
    'dashboard.stat.serversReady': 'Serveurs dédiés indexés',
    'dashboard.stat.activeQueues': 'Tâches Actives',
    'dashboard.stat.totalQueues': 'sur {total} tâches au total',
    'dashboard.stat.successOrders': 'Commandes Réussies',
    'dashboard.stat.failedOrders': '{failed} tentatives échouées',
    'dashboard.stat.apiNode': 'Nœud API',
    'dashboard.stat.apiReady': 'Authentifié & Prêt',
    'dashboard.stat.apiMissing': 'Identifiants requis',
    'dashboard.quickAccess': 'Accès Rapide Flotte Cloud & Stock',
    'dashboard.shortcut.dedicated': 'Contrôle Serveurs Dédiés',
    'dashboard.shortcut.dedicatedDesc': 'IPMI/KVM, redémarrage, réinstallation OS avec clé SSH et pare-feu IP',
    'dashboard.shortcut.orders': 'Suivi Commandes & Livraison',
    'dashboard.shortcut.ordersDesc': 'Commandes en attente, liens de paiement et suivi de livraison en 4 étapes',
    'dashboard.shortcut.renewals': 'Services & Renouvellements',
    'dashboard.shortcut.renewalsDesc': 'Échéances d’expiration et statut de renouvellement automatique',
    'dashboard.shortcut.vps': 'Console Flotte VPS',
    'dashboard.shortcut.vpsDesc': 'État des VPS, ressources vCore/RAM, IPv4/IPv6 et redémarrage rapide',
    'dashboard.shortcut.availability': 'Stock Datacenters en Direct',
    'dashboard.shortcut.availabilityDesc': 'Disponibilité en temps réel dans les datacenters OVHcloud',
    'dashboard.shortcut.ssh': 'Registre de Clés SSH',
    'dashboard.shortcut.sshDesc': 'Gérez vos clés publiques `/me/sshKey` pour l’installation OS',
    'dashboard.queueSection': 'File d’Approvisionnement Active',
    'dashboard.viewAll': 'Tout voir',
    'dashboard.noQueues': 'Aucune tâche active',
    'dashboard.newQueue': 'Nouvelle tâche',
    'dashboard.historySection': 'Historique Récent des Achats',
    'dashboard.noHistory': 'Aucun historique d’achat',

    // Account & Cloud Fleet Page
    'account.title': 'Flotte Cloud & Compte OVHcloud',
    'account.subtitle': 'Gérez vos commandes, renouvellements de services, clés SSH, instances VPS, e-mails et remboursements',
    'account.tab.orders': 'Commandes & Livraison',
    'account.tab.services': 'Services & Échéances',
    'account.tab.ssh': 'Clés SSH',
    'account.tab.vps': 'Flotte VPS',
    'account.tab.emails': 'E-mails',
    'account.tab.refunds': 'Avoirs & Remboursements',
  },
  'zh-CN': {
    // Brand
    'brand.name': 'OVH Nexus',
    'brand.subtitle': 'OVHcloud 云资产控制与自动化采购控制台',
    'brand.shortSubtitle': 'OVHcloud 综合控制台',
    'brand.disclaimer': '非官方第三方开源工具，与 OVH SAS / OVHcloud 官方无隶属关系。',

    // Auth / Login
    'auth.title': '登录 OVH Nexus',
    'auth.subtitle': '输入控制台访问密钥以管理您的 OVHcloud 云资产与自动化采购任务。',
    'auth.placeholder': '请输入 API 访问密钥...',
    'auth.submit': '进入控制台',
    'auth.verifying': '验证中...',
    'auth.error.empty': '请输入访问密钥',
    'auth.error.invalid': '访问密钥无效，请重试',
    'auth.error.network': '验证失败，请检查后端服务连接',

    // Navigation Groups
    'nav.group.overview': '全局概览',
    'nav.group.fleet': '云资产管理',
    'nav.group.procurement': '库存监控与抢购',
    'nav.group.system': '账户与系统',

    // Navigation Items
    'nav.dashboard': '仪表盘概览',
    'nav.dedicated': '独服管理 (Dedicated)',
    'nav.vps': 'VPS 与服务续费',
    'nav.servers': '服务器目录',
    'nav.availability': '实时库存矩阵',
    'nav.queue': '自动抢购队列',
    'nav.monitor': '独服库存监控',
    'nav.vpsMonitor': 'VPS 库存监控',
    'nav.history': '采购历史与交付',
    'nav.account': '账户、订单与 SSH',
    'nav.notifications': '通知中心',
    'nav.logs': '系统运行日志',
    'nav.settings': 'API 与系统设置',

    // Sidebar Footer
    'sidebar.apiStatus': 'OVHcloud API',
    'sidebar.connected': '已连接',
    'sidebar.notConfigured': '未配置',
    'sidebar.logout': '锁定控制台',
    'sidebar.collapse': '收起导航',
    'sidebar.expand': '展开导航',
    'theme.light': '浅色',
    'theme.dark': '深色',
    'theme.system': '自动',

    // Dashboard
    'dashboard.title': 'OVH Nexus 全局概览',
    'dashboard.subtitle': '已购云资产运维管控与自动化服务器监控采购一体化平台',
    'dashboard.refresh': '刷新状态',
    'dashboard.refreshing': '刷新中...',
    'dashboard.apiNotConfigured': '尚未配置 OVHcloud API 凭证',
    'dashboard.apiNotConfiguredDesc': '请前往「API 与系统设置」配置您的 OVH 密钥，以启用独服/VPS 管理、订单交付追踪与自动抢购功能。',
    'dashboard.configureApi': '立即配置 API',
    'dashboard.stat.servers': '目录机型数',
    'dashboard.stat.serversLoading': '正在加载目录...',
    'dashboard.stat.serversReady': '已索引独立服务器型号',
    'dashboard.stat.activeQueues': '运行中队列',
    'dashboard.stat.totalQueues': '共 {total} 个抢购任务',
    'dashboard.stat.successOrders': '成功下单数',
    'dashboard.stat.failedOrders': '{failed} 次失败尝试',
    'dashboard.stat.apiNode': 'API 区域节点',
    'dashboard.stat.apiReady': '已认证并就绪',
    'dashboard.stat.apiMissing': '待配置凭证',
    'dashboard.quickAccess': '云资产管控与采购快捷入口',
    'dashboard.shortcut.dedicated': '独立服务器控制',
    'dashboard.shortcut.dedicatedDesc': 'IPMI/KVM 控制台、硬件重启、重装系统自动注入 SSH 密钥、IP 边缘防火墙',
    'dashboard.shortcut.orders': '订单与实时交付追踪',
    'dashboard.shortcut.ordersDesc': '查看待支付订单付款链接，实时追踪机器创建、验证、交付与装机进度',
    'dashboard.shortcut.renewals': '服务到期与自动续费',
    'dashboard.shortcut.renewalsDesc': '一屏统览名下独服与 VPS 的到期倒计时、自动续费与到期销毁策略',
    'dashboard.shortcut.vps': '已购 VPS 实例管控',
    'dashboard.shortcut.vpsDesc': '查看已购 VPS 运行状态、vCore/内存/磁盘规格、公网 IP 与一键重启',
    'dashboard.shortcut.availability': '全球机房实时库存',
    'dashboard.shortcut.availabilityDesc': '无需登录即可查询 OVHcloud 全球数据中心可用库存状态',
    'dashboard.shortcut.ssh': '账户 SSH 密钥管理',
    'dashboard.shortcut.sshDesc': '管理 `/me/sshKey` 公钥库，支持在独服重装系统时一键注入',
    'dashboard.queueSection': '活跃采购队列',
    'dashboard.viewAll': '查看全部',
    'dashboard.noQueues': '暂无活跃的抢购队列任务',
    'dashboard.newQueue': '新建抢购任务',
    'dashboard.historySection': '最近采购与订单记录',
    'dashboard.noHistory': '暂无采购记录',

    // Account & Cloud Fleet Page
    'account.title': '云资产与账户中心',
    'account.subtitle': '统一管理 OVHcloud 订单交付、服务到期续费、SSH 密钥库、已购 VPS、账户邮件与退订账单',
    'account.tab.orders': '订单与交付追踪',
    'account.tab.services': '服务到期与续费',
    'account.tab.ssh': 'SSH 密钥管理',
    'account.tab.vps': '已购 VPS 管控',
    'account.tab.emails': '账户邮件记录',
    'account.tab.refunds': '退订与退款记录',
  },
};

interface LocaleContextType {
  locale: Locale;
  setLocale: (loc: Locale) => void;
  t: (key: string, params?: Record<string, string | number>) => string;
}

const LocaleContext = createContext<LocaleContextType>({
  locale: 'en',
  setLocale: () => {},
  t: (key: string) => key,
});

function normalizeLocale(raw?: string | null): Locale {
  if (!raw) return 'en';
  const cleaned = raw.trim().toLowerCase();
  if (cleaned.startsWith('fr')) return 'fr';
  if (cleaned.startsWith('zh')) return 'zh-CN';
  return 'en';
}

export const LocaleProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const [locale, setLocaleState] = useState<Locale>(() => {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved === 'en' || saved === 'fr' || saved === 'zh-CN') {
      return saved;
    }
    return 'en';
  });

  useEffect(() => {
    // If user hasn't explicitly chosen a language in localStorage, sync with server DEFAULT_LOCALE
    const saved = localStorage.getItem(STORAGE_KEY);
    if (!saved) {
      api
        .get('/endpoint-config')
        .then((res) => {
          if (res.data?.defaultLocale) {
            const serverLocale = normalizeLocale(res.data.defaultLocale);
            setLocaleState(serverLocale);
          }
        })
        .catch(() => {
          // Ignore error when unauthenticated or offline; default 'en' remains active
        });
    }
  }, []);

  const setLocale = (loc: Locale) => {
    setLocaleState(loc);
    localStorage.setItem(STORAGE_KEY, loc);
    document.documentElement.lang = loc;
  };

  const t = (key: string, params?: Record<string, string | number>): string => {
    const dict = translations[locale] || translations.en;
    let text = dict[key] || translations.en[key] || key;
    if (params) {
      Object.entries(params).forEach(([k, v]) => {
        text = text.replace(`{${k}}`, String(v));
      });
    }
    return text;
  };

  return (
    <LocaleContext.Provider value={{ locale, setLocale, t }}>
      {children}
    </LocaleContext.Provider>
  );
};

export const useLocale = () => useContext(LocaleContext);
export default LocaleContext;
