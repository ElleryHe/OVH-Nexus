export const enV132Translations: Record<string, string> = {
  // Listing states & filter tabs (v1.3.2)
  'listing.filter.all': 'All',
  'listing.filter.listed': 'Listed',
  'listing.filter.announced': 'Announced',
  'listing.filter.presale': 'Presale',
  'listing.filter.new': 'New',
  'listed.badge': 'Listed',
  'announced.badge': 'Announced',
  'announced.noStockFeedHint':
    'Published in OVH public catalog with pricing; datacenter availability feed is not yet active.',
  'announced.skuAdded': '📢 Announced',

  // SKU Change badges & timeline (v1.3.2)
  'sku.saleStarted': '🚀 Sale Started',
  'sku.replaced': '🔄 Replaced: {oldCode} → {newCode}',
  'sku.removedBadge': '📤 Discontinued',
  'sku.configChangedBadge': '🔧 Config Updated',

  // Compact Availability Matrix (v1.3.2)
  'availability.allConfigs': 'All Configurations ({count})',
  'availability.inStockDcCount': '{count} DCs',
  'availability.multiConfigBadge': '{count} cfg',
  'availability.fromPrice': 'from {price}/mo',
  'availability.perMonth': '/mo',
  'availability.configSelectorLabel': 'Configuration:',
  'availability.bestStatusTooltip': 'In-stock configurations at {dc}: {configs}',

  // Compact Collapsible Server Catalog Cards (v1.3.2)
  'catalog.fromPrice': 'from {price}/mo',
  'catalog.defaultConfigBadge': 'Default',
  'catalog.unifiedConfigsTitle': 'Hardware Configurations',
  'catalog.showAllInventory': 'Show all inventory (including out-of-stock)',
  'catalog.advancedOptions': 'Advanced Options (Bandwidth / vRack)',
  'catalog.expandCard': 'Configs & Stock',
  'catalog.collapseCard': 'Collapse',
  'catalog.noInStockConfigsNotice':
    'Currently out of stock across all datacenters (showing default configuration). Check "Show all inventory" to view all options.',
  'catalog.inStockDcsCount': '{count} DCs in stock',
  'catalog.outOfStockBadge': 'Out of Stock',
  'catalog.addToQueue': 'Add to Queue',

  // Multi-Target Config-Level QuickMonitorDialog & MonitorPage (v1.3.2)
  'quickMonitor.targetsSectionTitle': 'Configuration Monitor Targets',
  'quickMonitor.addTargetButton': '+ Add Configuration Target',
  'quickMonitor.removeTargetButton': 'Remove Target',
  'quickMonitor.targetTitle': 'Target #{index}',
  'quickMonitor.configSelectLabel': 'Hardware Configuration (Memory + Storage)',
  'quickMonitor.anyConfigNotifyOnly': 'Any Configuration (Notify Only)',
  'quickMonitor.autoOrderConfigRequiredWarning':
    'Selecting a specific configuration is required to enable Auto-Order to avoid unknown order costs.',
  'quickMonitor.maxMonthlyPriceLabel': 'Max Monthly Price Cap (ex-VAT)',
  'quickMonitor.maxMonthlyPriceHint':
    'Checked against live cart ex-VAT monthly recurring price before ordering; one-time setup fee is excluded from this monthly cap.',
  'quickMonitor.quantityLabel': 'Order Quantity',
  'monitor.legacyAutoOrderPausedBanner':
    'Legacy auto-order paused: please select a specific configuration and price cap to re-enable auto-order',
  'monitor.selectConfigAction': 'Select Configuration',
  'monitor.editSubscription': 'Edit Targets',
  'monitor.targetCountBadge': '{count} targets',
  'monitor.priceCapBadge': 'Cap: {price}/mo (ex-VAT)',
  'monitor.autoOrderQtyBadge': 'Auto-Order ×{qty}',
};

export default enV132Translations;
