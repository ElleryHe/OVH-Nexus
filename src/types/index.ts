export type ListingState = 'listed' | 'announced' | 'presale';

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
  currency?: string;
  datacenters?: Record<string, string> | DatacenterAvailabilityItem[];
  inStockCount?: number;
}

export interface PlanConfigsResponse {
  planCode: string;
  name?: string;
  listingState?: ListingState | string;
  basePrice?: number | null;
  startingPrice?: number | null;
  currency?: string;
  defaultConfigKey?: string;
  configs: PlanConfigOption[];
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
