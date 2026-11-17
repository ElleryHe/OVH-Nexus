import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react';
import { api } from '@/utils/apiClient';
import { enV132Translations } from '@/i18n/locales/en';
import { frV132Translations } from '@/i18n/locales/fr';
import { zhV132Translations } from '@/i18n/locales/zh';

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
    'auth.totpPlaceholder': '6-digit 2FA code or 8-char recovery code...',
    'auth.totpPrompt': 'Two-Factor Authentication is enabled. Please enter your 6-digit authenticator code or recovery code.',
    'auth.totpEmpty': 'Please enter your 2FA code or recovery code',
    'auth.backToPassword': 'Back to password',
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
    'nav.mcp': 'MCP AI Gateway',
    'nav.notifications': 'Notification Center',
    'nav.logs': 'System Logs',
    'nav.apiAccounts': 'OVH API Accounts',
    'nav.settings': 'System & Security',

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
    'dashboard.apiNotConfiguredDesc': 'Add your OVHcloud API credentials in OVH API Accounts to unlock server control, account orders, and automated procurement.',
    'dashboard.configureApi': 'Configure API',
    'dashboard.dismissNotice': 'Dismiss',
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

    // System Logs Console
    'logs.title': 'System Logs',
    'logs.subtitle': 'Real-time structured observability stream, module filters, and diagnostic export',
    'logs.searchPlaceholder': 'Search message, module, or attributes...',
    'logs.searchPlaceholderMobile': 'Search logs...',
    'logs.filter.allLevels': 'All Levels',
    'logs.filter.allSources': 'All Modules',
    'logs.source.server': 'server · Core Server',
    'logs.source.http': 'http · HTTP Router',
    'logs.source.auth': 'auth · Session Auth',
    'logs.source.totp': 'totp · 2FA Gatekeeper',
    'logs.source.vault': 'vault · Crypto Vault',
    'logs.source.ovh': 'ovh · OVH API Client',
    'logs.source.catalog': 'catalog · Catalog & Pricing',
    'logs.source.queue': 'queue · Order Queue',
    'logs.source.stock-monitor': 'stock-monitor · Dedicated Stock',
    'logs.source.vps-monitor': 'vps-monitor · VPS Stock',
    'logs.source.sku-watcher': 'sku-watcher · SKU Watcher',
    'logs.source.fleet': 'fleet · Cloud Fleet',
    'logs.source.notify': 'notify · Notifications',
    'logs.source.mcp': 'mcp · MCP Gateway',
    'logs.liveStream': 'Live Stream',
    'logs.liveStreamShort': 'Live',
    'logs.status.live': 'SSE Live',
    'logs.status.connecting': 'Connecting...',
    'logs.status.reconnecting': 'Reconnecting...',
    'logs.status.paused': 'Paused',
    'logs.refresh': 'Refresh',
    'logs.export': 'Export',
    'logs.exportJsonl': 'Export .jsonl',
    'logs.exportLog': 'Export .log',
    'logs.exportSuccess': 'Logs exported ({format})',
    'logs.exportFailed': 'Failed to export logs',
    'logs.clear': 'Clear',
    'logs.clearConfirmTitle': 'Clear System Logs',
    'logs.clearConfirmMessage': 'Are you sure you want to clear all system logs?\nThis action cannot be undone.',
    'logs.clearConfirmButton': 'Clear All',
    'logs.cancelButton': 'Cancel',
    'logs.clearSuccess': 'System logs cleared',
    'logs.clearFailed': 'Failed to clear logs',
    'logs.fetchFailed': 'Failed to fetch logs',
    'logs.panelTitle': 'System Log Stream',
    'logs.count': '{count} entries',
    'logs.filteredCount': '{shown} / {total} entries',
    'logs.latest': 'Latest: {time}',
    'logs.empty': 'No matching log entries',
    'logs.resetFilters': 'Reset Filters',
    'logs.col.index': '#',
    'logs.col.time': 'Local Time',
    'logs.col.level': 'Level',
    'logs.col.source': 'Module',
    'logs.col.message': 'Message & Attributes',
    'logs.autoScroll': 'Auto-scroll',

    // Notification Center Language & Outbound Settings
    'notifications.lang.title': 'Notification Language',
    'notifications.lang.desc': 'Language used for outbound Telegram and SMTP email notification templates.',
    'notifications.lang.label': 'Notification Language',
    'notifications.lang.option.en': 'English (Default)',
    'notifications.lang.option.fr': 'Français',
    'notifications.lang.option.zhCN': '简体中文',
    'notifications.lang.hint': 'Applies to outbound Telegram and SMTP alerts after saving.',
    'notifications.panelUrl.title': 'Outbound Console Link (Optional)',
    'notifications.panelUrl.desc': 'When set, outbound Telegram and SMTP email alerts include an "Open OVH Nexus Console" link (login still required; no passwordless action tokens are embedded).',

    // Presale & Generation (v1.3.1)
    'presale.badge': 'Presale / Unlisted',
    'presale.filterOnly': 'Presale Only',
    'presale.generation': '{year} Gen',
    'presale.pricingTbd': 'Pricing TBD (Presale)',
    'presale.notOrderableHint': 'Presale model: not yet listed in public catalog for direct ordering, but stock can be monitored.',
    'presale.skuAdded': '🟣 Presale Detected',
    'sku.newListed': '🆕 Newly Listed',

    // Quick Monitor Modal (v1.3.1)
    'quickMonitor.button': 'Quick Monitor',
    'quickMonitor.titleAdd': 'Quick Stock Monitor',
    'quickMonitor.titleUpdate': 'Update Monitor Subscription',
    'quickMonitor.subtitle': 'Configure real-time datacenter stock alerts and optional auto-order rules for {planCode}.',
    'quickMonitor.datacentersLabel': 'Target Datacenters',
    'quickMonitor.allDatacentersHint': 'All datacenters monitored by default when none or all are selected',
    'quickMonitor.selectAll': 'Select All (Monitor All DCs)',
    'quickMonitor.clearAll': 'Clear Selection',
    'quickMonitor.notifySection': 'Alert Notifications',
    'quickMonitor.notifyAvailable': 'In-Stock / Restock Alert (notifyAvailable)',
    'quickMonitor.notifyUnavailable': 'Out-of-Stock Alert (notifyUnavailable)',
    'quickMonitor.autoOrderSection': 'Automated Procurement',
    'quickMonitor.autoOrderToggle': 'Auto-Order when stock becomes available',
    'quickMonitor.autoOrderQtyLabel': 'Quantity per Datacenter',
    'quickMonitor.autoOrderQtyHint': 'Number of unpaid orders to create per datacenter when restocked (default: 1)',
    'quickMonitor.presaleCallout': 'This model is currently in Presale / Unlisted state: only restock notifications will be sent before official listing; auto-order will activate automatically once OVH publishes catalog pricing.',
    'quickMonitor.configSection': 'Hardware Configuration Filter (Optional)',
    'quickMonitor.memoryFilter': 'Memory Configuration',
    'quickMonitor.storageFilter': 'Storage Configuration',
    'quickMonitor.anyConfig': 'Any Configuration',
    'quickMonitor.autoStartMonitor': 'Start stock monitor engine immediately after saving',
    'quickMonitor.saveAdd': 'Save & Subscribe',
    'quickMonitor.saveUpdate': 'Update Subscription',
    'quickMonitor.saving': 'Saving...',
    'quickMonitor.cancel': 'Cancel',
    'quickMonitor.viewInMonitor': 'View in Stock Monitor →',
    'quickMonitor.savedToast': 'Monitor subscription saved for {planCode}',

    // Monitor <-> Queue Linkage (v1.3.1)
    'monitor.viewQueueTasks': 'View Queue Tasks →',
    'monitor.filterPlanCodeBanner': 'Filtering subscription by SKU: {planCode}',
    'monitor.clearFilter': 'Show All',
    'queue.sourceStockMonitor': 'From: Stock Monitor',
    'queue.sourceMcp': 'MCP AI',
    'queue.filterPlanCodeBanner': 'Filtering queue tasks by SKU: {planCode}',
    'queue.clearFilter': 'Show All',

    // MCP Client Snippet Switcher (v1.3.1)
    'mcp.snippet.modeEntryOnly': 'Server Entry Only (Merge into existing config)',
    'mcp.snippet.modeFullFile': 'Full Config File',
    'mcp.snippet.mergeHint': 'Tip: When pasting this entry into an existing `mcpServers` object, make sure the closing brace `}` of the previous server entry has a trailing comma `,` to avoid JSON syntax errors.',
    ...enV132Translations,
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
    'auth.totpPlaceholder': 'Code 2FA à 6 chiffres ou code de secours...',
    'auth.totpPrompt': 'L’authentification 2FA est activée. Saisissez le code à 6 chiffres ou un code de récupération.',
    'auth.totpEmpty': 'Veuillez saisir votre code 2FA',
    'auth.backToPassword': 'Retour au mot de passe',
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
    'nav.mcp': 'Passerelle IA MCP',
    'nav.notifications': 'Centre de Notifications',
    'nav.logs': 'Journaux Système',
    'nav.apiAccounts': 'Comptes API OVH',
    'nav.settings': 'Système & Sécurité',

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
    'dashboard.apiNotConfiguredDesc': 'Ajoutez vos identifiants dans Comptes API OVH pour activer la gestion de flotte et les commandes.',
    'dashboard.configureApi': 'Configurer l’API',
    'dashboard.dismissNotice': 'Masquer',
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

    // System Logs Console
    'logs.title': 'Journaux Système',
    'logs.subtitle': 'Flux structuré en temps réel, filtres par module et export diagnostique',
    'logs.searchPlaceholder': 'Rechercher message, module ou attributs...',
    'logs.searchPlaceholderMobile': 'Rechercher...',
    'logs.filter.allLevels': 'Tous les niveaux',
    'logs.filter.allSources': 'Tous les modules',
    'logs.source.server': 'server · Serveur Core',
    'logs.source.http': 'http · Routeur HTTP',
    'logs.source.auth': 'auth · Authentification',
    'logs.source.totp': 'totp · Gardien 2FA',
    'logs.source.vault': 'vault · Coffre Chiffré',
    'logs.source.ovh': 'ovh · Client API OVH',
    'logs.source.catalog': 'catalog · Catalogue & Prix',
    'logs.source.queue': 'queue · File de Commande',
    'logs.source.stock-monitor': 'stock-monitor · Stock Dédié',
    'logs.source.vps-monitor': 'vps-monitor · Stock VPS',
    'logs.source.sku-watcher': 'sku-watcher · Veille SKU',
    'logs.source.fleet': 'fleet · Flotte Cloud',
    'logs.source.notify': 'notify · Notifications',
    'logs.source.mcp': 'mcp · Passerelle MCP',
    'logs.liveStream': 'Flux Direct',
    'logs.liveStreamShort': 'Direct',
    'logs.status.live': 'SSE Actif',
    'logs.status.connecting': 'Connexion...',
    'logs.status.reconnecting': 'Reconnexion...',
    'logs.status.paused': 'En pause',
    'logs.refresh': 'Actualiser',
    'logs.export': 'Exporter',
    'logs.exportJsonl': 'Exporter .jsonl',
    'logs.exportLog': 'Exporter .log',
    'logs.exportSuccess': 'Journaux exportés ({format})',
    'logs.exportFailed': 'Échec de l’export des journaux',
    'logs.clear': 'Vider',
    'logs.clearConfirmTitle': 'Vider les journaux système',
    'logs.clearConfirmMessage': 'Voulez-vous vraiment supprimer tous les journaux système ?\nCette action est irréversible.',
    'logs.clearConfirmButton': 'Confirmer',
    'logs.cancelButton': 'Annuler',
    'logs.clearSuccess': 'Journaux système effacés',
    'logs.clearFailed': 'Échec de la suppression des journaux',
    'logs.fetchFailed': 'Échec du chargement des journaux',
    'logs.panelTitle': 'Flux des journaux système',
    'logs.count': '{count} entrées',
    'logs.filteredCount': '{shown} / {total} entrées',
    'logs.latest': 'Dernier : {time}',
    'logs.empty': 'Aucune entrée de journal correspondante',
    'logs.resetFilters': 'Réinitialiser',
    'logs.col.index': '#',
    'logs.col.time': 'Heure Locale',
    'logs.col.level': 'Niveau',
    'logs.col.source': 'Module',
    'logs.col.message': 'Message & Attributs',
    'logs.autoScroll': 'Défilement auto',

    // Notification Center Language & Outbound Settings
    'notifications.lang.title': 'Langue des Notifications',
    'notifications.lang.desc': 'Langue utilisée pour les modèles d’alertes sortantes Telegram et e-mail SMTP.',
    'notifications.lang.label': 'Langue des notifications',
    'notifications.lang.option.en': 'English (Par défaut)',
    'notifications.lang.option.fr': 'Français',
    'notifications.lang.option.zhCN': '简体中文',
    'notifications.lang.hint': 'S’applique aux alertes sortantes Telegram et SMTP après enregistrement.',
    'notifications.panelUrl.title': 'Lien Console Sortant (Optionnel)',
    'notifications.panelUrl.desc': 'Une fois défini, les alertes sortantes Telegram et e-mail SMTP incluent un lien « Ouvrir OVH Nexus » (authentification requise, aucun jeton sans mot de passe intégré).',

    // Presale & Generation (v1.3.1)
    'presale.badge': 'Prévente / Non listé',
    'presale.filterOnly': 'Prévente uniquement',
    'presale.generation': 'Gén. {year}',
    'presale.pricingTbd': 'Tarif à venir (Prévente)',
    'presale.notOrderableHint': 'Modèle en prévente : non encore listé au catalogue public pour commande directe, mais le suivi de stock est disponible.',
    'presale.skuAdded': '🟣 Prévente détectée',
    'sku.newListed': '🆕 Nouvellement listé',

    // Quick Monitor Modal (v1.3.1)
    'quickMonitor.button': 'Suivi rapide',
    'quickMonitor.titleAdd': 'Suivi de Stock Rapide',
    'quickMonitor.titleUpdate': 'Mettre à jour l’abonnement',
    'quickMonitor.subtitle': 'Configurez les alertes de stock par datacenter et la commande automatique pour {planCode}.',
    'quickMonitor.datacentersLabel': 'Datacenters cibles',
    'quickMonitor.allDatacentersHint': 'Tous les datacenters sont surveillés par défaut si aucun ou tous sont sélectionnés',
    'quickMonitor.selectAll': 'Tout sélectionner (Tous les DC)',
    'quickMonitor.clearAll': 'Effacer la sélection',
    'quickMonitor.notifySection': 'Alertes de notification',
    'quickMonitor.notifyAvailable': 'Alerte de réapprovisionnement (notifyAvailable)',
    'quickMonitor.notifyUnavailable': 'Alerte de rupture de stock (notifyUnavailable)',
    'quickMonitor.autoOrderSection': 'Approvisionnement automatique',
    'quickMonitor.autoOrderToggle': 'Commande automatique dès disponibilité',
    'quickMonitor.autoOrderQtyLabel': 'Quantité par datacenter',
    'quickMonitor.autoOrderQtyHint': 'Nombre de commandes non payées à créer par datacenter lors du réapprovisionnement (par défaut : 1)',
    'quickMonitor.presaleCallout': 'Ce modèle est actuellement en prévente / non listé : seules les notifications de disponibilité seront envoyées avant la mise en ligne officielle ; la commande automatique s’activera dès qu’OVH publiera les tarifs au catalogue.',
    'quickMonitor.configSection': 'Filtre de configuration matérielle (Optionnel)',
    'quickMonitor.memoryFilter': 'Configuration mémoire',
    'quickMonitor.storageFilter': 'Configuration stockage',
    'quickMonitor.anyConfig': 'Toutes configurations',
    'quickMonitor.autoStartMonitor': 'Démarrer immédiatement le moteur de surveillance après l’enregistrement',
    'quickMonitor.saveAdd': 'Enregistrer et suivre',
    'quickMonitor.saveUpdate': 'Mettre à jour l’abonnement',
    'quickMonitor.saving': 'Enregistrement...',
    'quickMonitor.cancel': 'Annuler',
    'quickMonitor.viewInMonitor': 'Voir dans le Moniteur de Stock →',
    'quickMonitor.savedToast': 'Abonnement de surveillance enregistré pour {planCode}',

    // Monitor <-> Queue Linkage (v1.3.1)
    'monitor.viewQueueTasks': 'Voir les tâches de file →',
    'monitor.filterPlanCodeBanner': 'Filtrage de l’abonnement par SKU : {planCode}',
    'monitor.clearFilter': 'Tout afficher',
    'queue.sourceStockMonitor': 'Source : Moniteur de stock',
    'queue.sourceMcp': 'MCP IA',
    'queue.filterPlanCodeBanner': 'Filtrage des tâches par SKU : {planCode}',
    'queue.clearFilter': 'Tout afficher',

    // MCP Client Snippet Switcher (v1.3.1)
    'mcp.snippet.modeEntryOnly': 'Entrée serveur seule (Fusionner dans une config existante)',
    'mcp.snippet.modeFullFile': 'Fichier JSON complet',
    'mcp.snippet.mergeHint': 'Astuce : Si vous collez cette entrée dans un objet `mcpServers` existant, assurez-vous que l’accolade fermante `}` de l’entrée précédente est suivie d’une virgule `,` pour éviter toute erreur de syntaxe JSON.',
    ...frV132Translations,
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
    'auth.totpPlaceholder': '输入 6 位 2FA 动态码或 8 位应急恢复码...',
    'auth.totpPrompt': '已启用双因素认证 (2FA)，请输入验证器 App 上的 6 位动态码或应急恢复码。',
    'auth.totpEmpty': '请输入 6 位 2FA 动态码或应急恢复码',
    'auth.backToPassword': '返回重新输入密码',
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
    'nav.mcp': 'MCP AI 网关',
    'nav.notifications': '通知中心',
    'nav.logs': '系统运行日志',
    'nav.apiAccounts': 'OVH API 账户',
    'nav.settings': '系统与安全设置',

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
    'dashboard.apiNotConfiguredDesc': '请前往「OVH API 账户」添加您的 OVH 密钥，以启用独服/VPS 管理、订单交付追踪与自动抢购功能。',
    'dashboard.configureApi': '立即配置 API',
    'dashboard.dismissNotice': '暂时收起',
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

    // System Logs Console
    'logs.title': '系统运行日志',
    'logs.subtitle': '实时结构化日志流、模块筛选与诊断导出控制台',
    'logs.searchPlaceholder': '搜索日志消息、模块或属性...',
    'logs.searchPlaceholderMobile': '搜索日志...',
    'logs.filter.allLevels': '所有级别',
    'logs.filter.allSources': '所有模块',
    'logs.source.server': 'server · 核心服务',
    'logs.source.http': 'http · HTTP 路由',
    'logs.source.auth': 'auth · 会话认证',
    'logs.source.totp': 'totp · 2FA 门禁',
    'logs.source.vault': 'vault · 密钥保险库',
    'logs.source.ovh': 'ovh · OVH API 客户端',
    'logs.source.catalog': 'catalog · 目录与价格',
    'logs.source.queue': 'queue · 自动下单队列',
    'logs.source.stock-monitor': 'stock-monitor · 独服库存监控',
    'logs.source.vps-monitor': 'vps-monitor · VPS 库存监控',
    'logs.source.sku-watcher': 'sku-watcher · SKU 货架监控',
    'logs.source.fleet': 'fleet · 云资产控制',
    'logs.source.notify': 'notify · 出站通知',
    'logs.source.mcp': 'mcp · MCP AI 网关',
    'logs.liveStream': '实时日志流',
    'logs.liveStreamShort': '实时',
    'logs.status.live': 'SSE 实时连接中',
    'logs.status.connecting': '正在连接...',
    'logs.status.reconnecting': '断开重连中...',
    'logs.status.paused': '已暂停实时流',
    'logs.refresh': '刷新',
    'logs.export': '导出',
    'logs.exportJsonl': '导出 .jsonl',
    'logs.exportLog': '导出 .log',
    'logs.exportSuccess': '日志已导出 ({format})',
    'logs.exportFailed': '导出日志失败',
    'logs.clear': '清空',
    'logs.clearConfirmTitle': '确认清空系统日志',
    'logs.clearConfirmMessage': '确定要清空所有系统日志吗？\n此操作不可撤销。',
    'logs.clearConfirmButton': '确认清空',
    'logs.cancelButton': '取消',
    'logs.clearSuccess': '系统日志已清空',
    'logs.clearFailed': '清空日志失败',
    'logs.fetchFailed': '获取日志失败',
    'logs.panelTitle': '系统日志控制台',
    'logs.count': '共 {count} 条',
    'logs.filteredCount': '显示 {shown} / {total} 条',
    'logs.latest': '最新: {time}',
    'logs.empty': '暂无匹配的日志记录',
    'logs.resetFilters': '重置筛选',
    'logs.col.index': '#',
    'logs.col.time': '本地时间',
    'logs.col.level': '级别',
    'logs.col.source': '模块',
    'logs.col.message': '消息与结构化属性',
    'logs.autoScroll': '自动滚动',

    // Notification Center Language & Outbound Settings
    'notifications.lang.title': '通知推送语言',
    'notifications.lang.desc': '选择通过 Telegram 与 SMTP 邮件发送出站告警通知时使用的模板语言。',
    'notifications.lang.label': '推送模板语言',
    'notifications.lang.option.en': 'English (默认)',
    'notifications.lang.option.fr': 'Français',
    'notifications.lang.option.zhCN': '简体中文',
    'notifications.lang.hint': '保存全部配置后应用于后续所有 Telegram 与邮件出站推送。',
    'notifications.panelUrl.title': '出站通知控制台链接（可选）',
    'notifications.panelUrl.desc': '填写后，Telegram 与 SMTP 邮件通知将附带「打开 OVH Nexus 控制台」跳转链接（打开后仍需输入访问密钥，不附带免密操作令牌）。',

    // Presale & Generation (v1.3.1)
    'presale.badge': '预售 / 待上架',
    'presale.filterOnly': '仅看预售',
    'presale.generation': '{year} 款',
    'presale.pricingTbd': '暂无公开定价（待上架）',
    'presale.notOrderableHint': '该机型处于预售/待上架状态：暂未开放公开目录直接下单，但可立即配置到货监控。',
    'presale.skuAdded': '🟣 预售待上架',
    'sku.newListed': '🆕 新上架',

    // Quick Monitor Modal (v1.3.1)
    'quickMonitor.button': '快速监控',
    'quickMonitor.titleAdd': '快速配置库存监控',
    'quickMonitor.titleUpdate': '更新监控订阅',
    'quickMonitor.subtitle': '为 {planCode} 配置数据中心实时库存通知与自动抢购策略。',
    'quickMonitor.datacentersLabel': '监控机房范围（多选）',
    'quickMonitor.allDatacentersHint': '默认全选（监控所有数据中心）；也可单独勾选特定机房',
    'quickMonitor.selectAll': '全选（监控全部机房）',
    'quickMonitor.clearAll': '清空选择',
    'quickMonitor.notifySection': '库存变动通知',
    'quickMonitor.notifyAvailable': '有货补货通知 (notifyAvailable)',
    'quickMonitor.notifyUnavailable': '售罄无货通知 (notifyUnavailable)',
    'quickMonitor.autoOrderSection': '有货自动下单',
    'quickMonitor.autoOrderToggle': '检测到有货时自动触发下单入队 (autoOrder)',
    'quickMonitor.autoOrderQtyLabel': '每机房自动下单数量 (autoOrderQuantity)',
    'quickMonitor.autoOrderQtyHint': '留空或 1 表示每机房有货时自动抢购 1 台（仅生成未付款订单或按队列策略执行）',
    'quickMonitor.presaleCallout': '该机型当前处于预售/待上架状态：正式上架前仅推送有货通知；待 OVH 公开目录开放定价后，自动下单将自动生效。',
    'quickMonitor.configSection': '可选硬件配置精确匹配（默认不限配置）',
    'quickMonitor.memoryFilter': '内存规格筛选',
    'quickMonitor.storageFilter': '存储规格筛选',
    'quickMonitor.anyConfig': '不限配置 / Any Configuration',
    'quickMonitor.autoStartMonitor': '保存后立即启动库存监控引擎',
    'quickMonitor.saveAdd': '保存并开启监控',
    'quickMonitor.saveUpdate': '更新监控订阅',
    'quickMonitor.saving': '保存中...',
    'quickMonitor.cancel': '取消',
    'quickMonitor.viewInMonitor': '前往库存监控查看 →',
    'quickMonitor.savedToast': '已保存 {planCode} 的库存监控订阅',

    // Monitor <-> Queue Linkage (v1.3.1)
    'monitor.viewQueueTasks': '查看关联抢购队列 →',
    'monitor.filterPlanCodeBanner': '当前按型号筛选监控订阅：{planCode}',
    'monitor.clearFilter': '显示全部',
    'queue.sourceStockMonitor': '来源：库存监控',
    'queue.sourceMcp': 'MCP AI',
    'queue.filterPlanCodeBanner': '当前按型号筛选抢购队列：{planCode}',
    'queue.clearFilter': '显示全部',

    // MCP Client Snippet Switcher (v1.3.1)
    'mcp.snippet.modeEntryOnly': '仅服务器条目（合并到已有配置）',
    'mcp.snippet.modeFullFile': '完整配置文件',
    'mcp.snippet.mergeHint': '提示：若将条目粘贴到已有的 `mcpServers` 对象中，请确保前一个服务器条目的右花括号 `}` 后带有英文逗号 `,`，否则会导致 JSON 解析报错。',
    ...zhV132Translations,
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
