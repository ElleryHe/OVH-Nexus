export type ListingState = 'listed' | 'announced' | 'presale';

export interface PromoDetail {
  code: string;
  description?: string;
  context: string; // 'standard' (auto-applied) | 'voucher'
  autoApplied: boolean;
  capacity: 'renew' | 'installation' | string;
  type?: string;
  value?: number;
  discountAmount?: number;
  duration?: number;
  minimumDuration?: number;
  startDate?: string;
  endDate?: string;
  isGlobalQuantityLimited?: boolean;
  globalQuantity?: number | null;
  quantity?: number | null;
}

export interface PlanPricingMode {
  mode: string; // 'default' | 'upfront12' | 'upfront24' | 'degressivity12' | 'degressivity24'
  duration: string; // 'P1M' | 'P12M' | 'P24M'
  intervalMonths: number;
  commitmentMonths: number;
  periodPrice: number;
  originalPeriodPrice: number;
  monthlyEquivalentPrice: number;
  originalMonthlyPrice: number;
  setupFee: number;
  originalSetupFee: number;
  freeSetup: boolean;
  hasPromo: boolean;
  promoCode?: string;
  promoBadge?: string;
  promoDescription?: string;
  promoValue?: number;
  promoType?: string;
  promoDuration?: number;
  promoMinimumDuration?: number;
  promoContext?: string;
  autoApplied?: boolean;
  promoStartDate?: string;
  promoEndDate?: string;
  renewPromo?: PromoDetail | null;
  setupPromo?: PromoDetail | null;
}

export interface CatalogPromoCampaign {
  promoCode: string;
  context: string;
  autoApplied: boolean;
  capacities: string[];
  supportedModes: string[];
  discountType?: string;
  discountValue?: number;
  badge: string;
  description?: string;
  durationMonths?: number;
  minimumDurationMonths?: number;
  startDate?: string;
  endDate?: string;
  remainingHours?: number | null;
  isGlobalQuantityLimited?: boolean;
  globalQuantity?: number | null;
  catalogSources: string[];
  planCount: number;
  inStockPlanCount: number;
  maxSavingsAmount: number;
  currency: string;
  planCodes: string[];
  samplePlans: string[];
}

export interface CatalogPromoPlan {
  planCode: string;
  name: string;
  range?: string;
  cpu?: string;
  catalogSource?: string;
  listingState: string;
  currency: string;
  startingPrice: number;
  originalMonthlyPrice: number;
  setupFee: number;
  originalSetupFee: number;
  monthlySavings: number;
  setupSavings: number;
  hasRecurringPromo: boolean;
  hasSetupPromo: boolean;
  autoApplied: boolean;
  promoCodes: string[];
  promoBadge?: string;
  promoDescription?: string;
  supportedPromoModes: string[];
  missedPromoModes?: string[];
  addonsDiscounted: boolean;
  inStockDatacenters: string[];
  inStockCount: number;
  pricingModes: PlanPricingMode[];
}

export interface CatalogPromotionsResponse {
  updatedAt: number;
  endpoint: string;
  zone: string;
  currency: string;
  totalCampaigns: number;
  totalPromotedPlans: number;
  inStockPromotedPlans: number;
  autoAppliedCodes: string[];
  campaigns: CatalogPromoCampaign[];
  plans: CatalogPromoPlan[];
}

export interface CartPriceLine {
  detailType?: string;
  description?: string;
  totalWithoutTax?: number | null;
  originalWithoutTax?: number | null;
  currencyCode?: string;
}

export interface ServerPriceProbeResult {
  success: boolean;
  code?: string;
  planCode?: string;
  datacenter?: string;
  options?: string[];
  pricingMode?: string;
  duration?: string;
  coupon?: string;
  autoAppliedPromoDetected?: boolean;
  autoAppliedPromoCode?: string;
  autoAppliedPromoHint?: string;
  error?: string;
  price?: {
    pricingMode?: string;
    duration?: string;
    intervalMonths?: number;
    monthlyEquivalentWithoutTax?: number | null;
    setupFeeWithoutTax?: number | null;
    recurringWithoutTax?: number | null;
    reductionWithoutTax?: number | null;
    prices?: {
      withTax?: number | null;
      withoutTax?: number | null;
      tax?: number | null;
      currencyCode?: string;
    };
    details?: CartPriceLine[];
  } | null;
}

export interface DatacenterAvailabilityItem {
  datacenter: string;
  availability: string;
  dcName?: string;
  region?: string;
  countryCode?: string;
  flag?: string;
}

export interface OVHAvailabilityRow {
  fqn: string;
  planCode: string;
  server: string;
  memory: string;
  storage: string;
  systemStorage?: string;
  gpu?: string;
  listingState?: ListingState | 'delisted' | string;
  catalogSource?: string;
  orderable?: boolean;
  generation?: number;
  listedAt?: string;
  configKey?: string;
  options?: string[];
  isDefault?: boolean;
  estimatedMonthlyPrice?: number;
  price?: number;
  originalPrice?: number;
  originalMonthlyPrice?: number;
  setupFee?: number;
  originalSetupFee?: number;
  freeSetup?: boolean;
  hasPromo?: boolean;
  promoCode?: string;
  promoBadge?: string;
  promoDescription?: string;
  pricingModes?: PlanPricingMode[];
  currency?: string;
  datacenters: DatacenterAvailabilityItem[];
}

export interface PlanConfigOption {
  label: string;
  value?: string;
  family?: string;
  configKey?: string;
  memory?: string;
  storage?: string;
  systemStorage?: string;
  gpu?: string;
  memoryLabel?: string;
  storageLabel?: string;
  options?: string[];
  isDefault?: boolean;
  estimatedMonthlyPrice?: number;
  price?: number;
  originalPrice?: number;
  originalMonthlyPrice?: number;
  setupFee?: number;
  originalSetupFee?: number;
  freeSetup?: boolean;
  hasPromo?: boolean;
  promoCode?: string;
  promoBadge?: string;
  promoDescription?: string;
  pricingModes?: PlanPricingMode[];
  currency?: string;
  datacenters?: Record<string, string> | DatacenterAvailabilityItem[];
  inStockCount?: number;
}

export type PlanConfigItem = PlanConfigOption;

export interface PlanConfigsResponse {
  planCode: string;
  name?: string;
  listingState?: ListingState | string;
  basePrice?: number | null;
  startingPrice?: number | null;
  originalPrice?: number;
  originalMonthlyPrice?: number;
  setupFee?: number;
  originalSetupFee?: number;
  freeSetup?: boolean;
  hasPromo?: boolean;
  promoCode?: string;
  promoBadge?: string;
  promoDescription?: string;
  pricingModes?: PlanPricingMode[];
  currency?: string;
  defaultConfigKey?: string;
  configs: PlanConfigOption[];
}

export interface ServerOption {
  label: string;
  value: string;
  family?: string;
  configKey?: string;
  options?: string[];
  isDefault?: boolean;
  estimatedMonthlyPrice?: number;
  price?: number;
  originalPrice?: number;
  originalMonthlyPrice?: number;
  setupFee?: number;
  originalSetupFee?: number;
  freeSetup?: boolean;
  hasPromo?: boolean;
  promoCode?: string;
  promoBadge?: string;
  promoDescription?: string;
  pricingModes?: PlanPricingMode[];
  currency?: string;
}

export interface ServerPlan {
  planCode: string;
  name: string;
  description?: string;
  range?: string;
  cpu: string;
  memory: string;
  storage: string;
  bandwidth: string;
  vrackBandwidth: string;
  monthlyPrice?: number | null;
  startingPrice?: number | null;
  originalPrice?: number;
  originalMonthlyPrice?: number;
  setupFee?: number;
  originalSetupFee?: number;
  freeSetup?: boolean;
  hasPromo?: boolean;
  promoCode?: string;
  promoBadge?: string;
  promoDescription?: string;
  pricingModes?: PlanPricingMode[];
  defaultConfigKey?: string;
  currency?: string;
  catalogSource?: string;
  listingState?: ListingState | 'delisted' | string;
  listedAt?: string;
  orderable?: boolean;
  generation?: number;
  defaultOptions: ServerOption[];
  availableOptions: ServerOption[];
  datacenters: {
    datacenter: string;
    dcName: string;
    region: string;
    availability: string;
    countryCode: string;
    flag?: string;
  }[];
}

export interface StockTarget {
  id: string;
  configKey: string;
  options: string[];
  label: string;
  datacenters: string[];
  notifyAvailable: boolean;
  notifyUnavailable: boolean;
  autoOrder: boolean;
  quantity: number;
  maxMonthlyPrice?: number;
  pricingMode?: string;
  duration?: string;
  coupon?: string;
  currency?: string;
  lastStatus?: Record<string, string>;
}

export interface StockSubscription {
  accountId?: string;
  planCode: string;
  serverName?: string;
  datacenters: string[];
  notifyAvailable: boolean;
  notifyUnavailable: boolean;
  autoOrder?: boolean;
  autoOrderQuantity?: number;
  targets?: StockTarget[];
  needsConfigSelection?: boolean;
  lastStatus: Record<string, string>;
  createdAt: string;
}

export interface QueueItem {
  id: string;
  planCode: string;
  datacenter?: string;
  datacenters?: string[];
  options: string[];
  configKey?: string;
  configLabel?: string;
  currency?: string;
  maxMonthlyPrice?: number;
  pricingMode?: string;
  duration?: string;
  coupon?: string;
  status: 'pending' | 'running' | 'paused' | 'completed' | 'failed';
  createdAt: string;
  updatedAt: string;
  retryInterval: number;
  retryCount: number;
  accountId?: string;
  quantity?: number;
  purchased?: number;
  failureCount?: number;
  nextAttemptAt?: number;
  auto_pay?: boolean;
  waive_retractation_period?: boolean;
  source?: 'manual' | 'stock-monitor' | 'mcp' | string;
  sourceRef?: string;
}

export interface QuickOrderRequest {
  planCode: string;
  datacenter: string;
  options?: string[];
  configKey?: string;
  configLabel?: string;
  currency?: string;
  maxMonthlyPrice?: number;
  pricingMode?: string;
  duration?: string;
  coupon?: string;
  auto_pay?: boolean;
  waive_retractation_period?: boolean;
}

export interface SkuChangeRecord {
  id: string;
  type: 'added' | 'removed' | 'replaced' | 'config_changed' | 'presale' | 'listed';
  listingState?: ListingState | string;
  saleStarted?: boolean;
  replacedFrom?: string;
  generation?: number;
  timestamp: string;
  skuKey: string;
  planCode?: string;
  planCodes?: string[];
  name: string;
  range?: string;
  endpoint?: string;
  price?: number | null;
  currency?: string;
  details?: string;
  cpuText?: string;
  memorySummary?: string;
  storageSummary?: string;
  stockState?: string;
  addedConfigs?: string[];
  removedConfigs?: string[];
}
