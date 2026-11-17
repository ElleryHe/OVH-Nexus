export const frV132Translations: Record<string, string> = {
  // Listing states & filter tabs (v1.3.2)
  'listing.filter.all': 'Tous',
  'listing.filter.listed': 'En vente',
  'listing.filter.announced': 'Annoncé',
  'listing.filter.presale': 'Prévente',
  'listing.filter.new': 'Nouveau',
  'listed.badge': 'En vente',
  'announced.badge': 'Annoncé',
  'announced.noStockFeedHint':
    'Publié au catalogue OVH avec tarif ; le flux de disponibilité par datacenter n’est pas encore actif.',
  'announced.skuAdded': '📢 Annoncé',

  // SKU Change badges & timeline (v1.3.2)
  'sku.saleStarted': '🚀 Ouverture des ventes',
  'sku.replaced': '🔄 Remplacé : {oldCode} → {newCode}',
  'sku.removedBadge': '📤 Retiré',
  'sku.configChangedBadge': '🔧 Config modifiée',

  // Compact Availability Matrix (v1.3.2)
  'availability.allConfigs': 'Toutes les configurations ({count})',
  'availability.inStockDcCount': '{count} DC',
  'availability.multiConfigBadge': '{count} cfg',
  'availability.fromPrice': 'dès {price}/mois',
  'availability.perMonth': '/mois',
  'availability.configSelectorLabel': 'Configuration :',
  'availability.bestStatusTooltip': 'Configurations en stock à {dc} : {configs}',

  // Compact Collapsible Server Catalog Cards (v1.3.2)
  'catalog.fromPrice': 'dès {price}/mois',
  'catalog.defaultConfigBadge': 'Défaut',
  'catalog.unifiedConfigsTitle': 'Configurations matérielles',
  'catalog.showAllInventory': 'Afficher tout l’inventaire (y compris rupture)',
  'catalog.advancedOptions': 'Options avancées (Bande passante / vRack)',
  'catalog.expandCard': 'Configs & Stock',
  'catalog.collapseCard': 'Réduire',
  'catalog.noInStockConfigsNotice':
    'Actuellement en rupture dans tous les datacenters (configuration par défaut affichée). Cochez « Afficher tout l’inventaire » pour tout voir.',
  'catalog.inStockDcsCount': '{count} DC en stock',
  'catalog.outOfStockBadge': 'Rupture',
  'catalog.addToQueue': 'File d’achat',

  // Multi-Target Config-Level QuickMonitorDialog & MonitorPage (v1.3.2)
  'quickMonitor.targetsSectionTitle': 'Cibles de surveillance par configuration',
  'quickMonitor.addTargetButton': '+ Ajouter une cible de configuration',
  'quickMonitor.removeTargetButton': 'Supprimer la cible',
  'quickMonitor.targetTitle': 'Cible #{index}',
  'quickMonitor.configSelectLabel': 'Configuration matérielle (Mémoire + Stockage)',
  'quickMonitor.anyConfigNotifyOnly': 'Toute configuration (Notification seule)',
  'quickMonitor.autoOrderConfigRequiredWarning':
    'La sélection d’une configuration spécifique est obligatoire pour activer la commande automatique afin d’éviter tout montant imprévu.',
  'quickMonitor.maxMonthlyPriceLabel': 'Plafond mensuel max (HT)',
  'quickMonitor.maxMonthlyPriceHint':
    'Comparé au tarif mensuel HT en temps réel du panier avant commande ; les frais d’installation uniques sont exclus de ce plafond.',
  'quickMonitor.quantityLabel': 'Quantité',
  'monitor.legacyAutoOrderPausedBanner':
    'Commande auto héritée en pause : veuillez sélectionner une configuration spécifique et un plafond mensuel pour réactiver la commande automatique',
  'monitor.selectConfigAction': 'Choisir la configuration',
  'monitor.editSubscription': 'Modifier les cibles',
  'monitor.targetCountBadge': '{count} cibles',
  'monitor.priceCapBadge': 'Plafond : {price}/mois (HT)',
  'monitor.autoOrderQtyBadge': 'Commande auto ×{qty}',
};

export default frV132Translations;
