import { useState, useEffect, useMemo, useCallback, useRef } from 'react';
import { motion } from 'framer-motion';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import {
  RefreshCw,
  Search,
  Database,
  Filter,
  Download,
  TrendingUp,
  ChevronLeft,
  ChevronRight,
  Sparkles,
  ChevronDown,
  ChevronUp,
  History,
  Bell,
} from 'lucide-react';
import axios from 'axios';
import apiClient from '@/utils/apiClient';
import { useIsMobile } from '@/hooks/use-mobile';
import { useLocale } from '@/context/LocaleContext';
import { DCLabel } from '@/components/DCLabel';
import {
  QuickMonitorDialog,
  QuickMonitorInitialConfig,
} from '@/components/QuickMonitorDialog';
import {
  formatDatacenter,
  normalizeDatacenterCode,
  inferPlanGeneration,
} from '@/config/ovhConstants';
import {
  useCatalogDictionary,
  resolvePlanDisplay,
  resolveConfigPrice,
  formatMemoryCode,
  formatStorageCode,
  formatGpuCode,
  buildConfigKey,
  formatConfigLabel,
  getCurrencySymbol,
  parsePlanRegion,
} from '@/utils/hardwareFormat';
import type {
  ListingState,
  OVHAvailabilityRow,
  PlanConfigOption,
  PlanPricingMode,
  SkuChangeRecord,
} from '@/types';

interface DatacenterInfo {
  datacenter: string;
  availability: string;
}

interface MergedConfigEntry {
  configKey: string;
  fqn: string;
  memory: string;
  storage: string;
  systemStorage?: string;
  gpu?: string;
  options: string[];
  isDefault: boolean;
  label: string;
  estimatedMonthlyPrice: number | null;
  originalMonthlyPrice: number | null;
  setupFee: number | null;
  originalSetupFee: number | null;
  freeSetup: boolean;
  hasPromo: boolean;
  promoCode?: string;
  promoBadge?: string;
  promoDescription?: string;
  pricingModes?: PlanPricingMode[];
  currency: string;
  datacenters: DatacenterInfo[];
  dcMap: Record<string, string>;
  inStockCount: number;
}

interface MergedDatacenterCell {
  datacenter: string;
  bestAvailability: string;
  inStockConfigsCount: number;
  inStockConfigLabels: string[];
}

interface MergedPlanRow {
  planCode: string;
  server: string;
  listingState: ListingState;
  orderable: boolean;
  generation?: number;
  isRecentlyAdded: boolean;
  startingPrice: number | null;
  originalMonthlyPrice: number | null;
  setupFee: number | null;
  originalSetupFee: number | null;
  freeSetup: boolean;
  hasPromo: boolean;
  promoCode?: string;
  promoBadge?: string;
  promoDescription?: string;
  pricingModes?: PlanPricingMode[];
  currency: string;
  configs: MergedConfigEntry[];
  allDatacenters: MergedDatacenterCell[];
  totalInStockDcs: number;
  has1HStock: boolean;
}

type ListingFilterTab =
  | 'all'
  | 'listed'
  | 'promo'
  | 'announced'
  | 'presale'
  | 'new';

const isStatusInStock = (status?: string): boolean =>
  Boolean(status && status !== 'unavailable' && status !== 'unknown');

const rankAvailabilityStatus = (status?: string): number => {
  if (!status || status === 'unknown') return 0;
  if (status === 'unavailable') return 1;
  switch (status) {
    case '1H-high':
      return 100;
    case '1H-low':
      return 90;
    case '24H':
      return 80;
    case '72H':
      return 70;
    case '240H':
      return 60;
    case '480H':
      return 50;
    default:
      return 65;
  }
};

const OVHAvailabilityPage = () => {
  const isMobile = useIsMobile();
  const { locale, t } = useLocale();
  const catalogDict = useCatalogDictionary();
  const [availabilities, setAvailabilities] = useState<OVHAvailabilityRow[]>([]);
  const [skuChanges, setSkuChanges] = useState<SkuChangeRecord[]>([]);
  const [recentSkuMap, setRecentSkuMap] = useState<Record<string, string>>({});
  const [presaleSkuMap, setPresaleSkuMap] = useState<Record<string, string>>({});
  const [announcedSkuMap, setAnnouncedSkuMap] = useState<Record<string, string>>({});
  const [serverListingMap, setServerListingMap] = useState<
    Record<
      string,
      {
        listingState?: string;
        listedAt?: string;
        orderable?: boolean;
        catalogSource?: string;
        memory?: string;
        storage?: string;
        monthlyPrice?: number | null;
        startingPrice?: number | null;
        originalMonthlyPrice?: number | null;
        setupFee?: number | null;
        originalSetupFee?: number | null;
        freeSetup?: boolean;
        hasPromo?: boolean;
        promoCode?: string;
        promoBadge?: string;
        promoDescription?: string;
        pricingModes?: PlanPricingMode[];
        currency?: string;
      }
    >
  >({});
  const [skuTotalIndexed, setSkuTotalIndexed] = useState<number>(0);
  const [showSkuTimeline, setShowSkuTimeline] = useState<boolean>(false);
  const [isCheckingSku, setIsCheckingSku] = useState<boolean>(false);
  const [isLoading, setIsLoading] = useState(false);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [, setIsConfigLoading] = useState(true);
  const [endpoint, setEndpoint] = useState<string>('');
  const [apiBaseUrl, setApiBaseUrl] = useState<string>('');
  const prevAvailabilitiesRef = useRef<OVHAvailabilityRow[]>([]);

  // Per-row configuration selector state: planCode -> 'all' | configKey
  const [selectedRowConfigs, setSelectedRowConfigs] = useState<
    Record<string, string>
  >({});

  // Search and filters
  const [searchTerm, setSearchTerm] = useState('');
  const [debouncedSearch, setDebouncedSearch] = useState('');
  const [filterDatacenter, setFilterDatacenter] = useState('all');
  const [filterAvailability, setFilterAvailability] = useState('all');
  const [filterMemory, setFilterMemory] = useState('all');
  const [listingFilterTab, setListingFilterTab] =
    useState<ListingFilterTab>('all');

  const [quickMonitorTarget, setQuickMonitorTarget] = useState<{
    planCode: string;
    serverName?: string;
    listingState?: ListingState;
    isPresale?: boolean;
    datacenters?: DatacenterInfo[];
    initialConfig?: QuickMonitorInitialConfig | null;
    initialConfigs?: PlanConfigOption[];
  } | null>(null);

  // Sorting & pagination
  const [sortBy, setSortBy] = useState<
    'planCode' | 'memory' | 'availability' | 'price'
  >('planCode');
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('asc');
  const [currentPage, setCurrentPage] = useState(1);
  const itemsPerPage = isMobile ? 20 : 40;

  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedSearch(searchTerm);
      setCurrentPage(1);
    }, 300);
    return () => clearTimeout(timer);
  }, [searchTerm]);

  useEffect(() => {
    setCurrentPage(1);
  }, [
    filterDatacenter,
    filterAvailability,
    filterMemory,
    listingFilterTab,
    sortBy,
    sortOrder,
  ]);

  const getApiBaseUrl = (ep: string): string => {
    switch (ep) {
      case 'ovh-us':
        return 'https://api.us.ovhcloud.com';
      case 'ovh-ca':
        return 'https://ca.api.ovh.com';
      case 'ovh-eu':
      default:
        return 'https://eu.api.ovh.com';
    }
  };

  const fetchAvailabilities = useCallback(
    async (isRefresh = false) => {
      if (!apiBaseUrl) return;

      const hasExistingData = prevAvailabilitiesRef.current.length > 0;
      if (isRefresh && hasExistingData) {
        setIsRefreshing(true);
      } else {
        setIsLoading(true);
      }
      try {
        const apiUrl = `${apiBaseUrl}/v1/dedicated/server/datacenter/availabilities`;
        if (!isRefresh) {
          toast.info('正在从 OVH 公开 API 获取数据...', { duration: 2000 });
        }

        let newData: OVHAvailabilityRow[] = [];
        try {
          const backendRes = await apiClient.get('/availability', {
            timeout: 30000,
          });
          if (Array.isArray(backendRes.data) && backendRes.data.length > 0) {
            newData = backendRes.data;
          }
        } catch {
          // Fallback to direct OVH API below
        }
        if (newData.length === 0) {
          const response = await axios.get(apiUrl, { timeout: 30000 });
          newData = Array.isArray(response.data) ? response.data : [];
        }

        prevAvailabilitiesRef.current = newData;
        setAvailabilities(newData);

        if (!isRefresh && newData.length > 0) {
          toast.success(`成功获取 ${newData.length} 条配置可用性记录`);
        }
      } catch (error: any) {
        let errorMessage = '获取数据失败';
        if (error.code === 'ECONNABORTED' || error.message?.includes('timeout')) {
          errorMessage = '请求超时，请重试';
        } else if (error.message) {
          errorMessage = `获取数据失败: ${error.message}`;
        }
        toast.error(errorMessage);
      } finally {
        setIsLoading(false);
        setIsRefreshing(false);
      }
    },
    [apiBaseUrl]
  );

  const fetchEndpointConfig = useCallback(async () => {
    setIsConfigLoading(true);
    try {
      const response = await apiClient.get('/endpoint-config');
      const configEndpoint = response.data.endpoint || 'ovh-eu';
      setEndpoint(configEndpoint);
      setApiBaseUrl(getApiBaseUrl(configEndpoint));
    } catch {
      setEndpoint('ovh-eu');
      setApiBaseUrl('https://eu.api.ovh.com');
    } finally {
      setIsConfigLoading(false);
    }
  }, []);

  const fetchSkuChanges = useCallback(async () => {
    try {
      const res = await apiClient.get('/sku/changes?limit=200');
      if (res.data) {
        setSkuChanges(res.data.changes || []);
        setRecentSkuMap(res.data.status?.recentFirstSeen || {});
        setPresaleSkuMap(res.data.status?.presaleSkus || {});
        setAnnouncedSkuMap(res.data.status?.announcedSkus || {});
        setSkuTotalIndexed(res.data.status?.totalSkus || 0);
      }
    } catch {
      // ignore if backend not reachable
    }
  }, []);

  const fetchServerListingStates = useCallback(async () => {
    try {
      const res = await apiClient.get('/servers');
      const list = Array.isArray(res.data) ? res.data : res.data?.servers || [];
      if (Array.isArray(list) && list.length > 0) {
        const map: Record<
          string,
          {
            listingState?: string;
            listedAt?: string;
            orderable?: boolean;
            catalogSource?: string;
            memory?: string;
            storage?: string;
            monthlyPrice?: number | null;
            startingPrice?: number | null;
            originalMonthlyPrice?: number | null;
            setupFee?: number | null;
            originalSetupFee?: number | null;
            freeSetup?: boolean;
            hasPromo?: boolean;
            promoCode?: string;
            promoBadge?: string;
            promoDescription?: string;
            pricingModes?: PlanPricingMode[];
            currency?: string;
          }
        > = {};
        list.forEach((srv: any) => {
          if (srv && srv.planCode) {
            map[srv.planCode] = {
              listingState: srv.listingState,
              listedAt: srv.listedAt,
              orderable: srv.orderable,
              catalogSource: srv.catalogSource,
              memory: srv.memory,
              storage: srv.storage,
              monthlyPrice: srv.monthlyPrice,
              startingPrice: srv.startingPrice,
              originalMonthlyPrice:
                srv.originalMonthlyPrice ?? srv.originalPrice ?? null,
              setupFee:
                typeof srv.setupFee === 'number' ? srv.setupFee : null,
              originalSetupFee:
                typeof srv.originalSetupFee === 'number'
                  ? srv.originalSetupFee
                  : null,
              freeSetup: Boolean(srv.freeSetup),
              hasPromo: Boolean(srv.hasPromo),
              promoCode: srv.promoCode,
              promoBadge: srv.promoBadge,
              promoDescription: srv.promoDescription,
              pricingModes: Array.isArray(srv.pricingModes)
                ? srv.pricingModes
                : undefined,
              currency: srv.currency,
            };
          }
        });
        setServerListingMap(map);
      }
    } catch {
      // ignore
    }
  }, []);

  const handleManualSkuCheck = async () => {
    setIsCheckingSku(true);
    try {
      const res = await apiClient.post('/sku/check');
      if (res.data) {
        setSkuChanges(res.data.changes || []);
        setRecentSkuMap(res.data.status?.recentFirstSeen || {});
        setPresaleSkuMap(res.data.status?.presaleSkus || {});
        setAnnouncedSkuMap(res.data.status?.announcedSkus || {});
        setSkuTotalIndexed(res.data.status?.totalSkus || 0);
      }
      toast.success('已同步最新货架 SKU 基线与变动');
    } catch (err: any) {
      toast.error(
        '同步 SKU 变动失败: ' + (err.response?.data?.message || err.message)
      );
    } finally {
      setIsCheckingSku(false);
    }
  };

  useEffect(() => {
    fetchEndpointConfig();
    fetchSkuChanges();
    fetchServerListingStates();
  }, [fetchEndpointConfig, fetchSkuChanges, fetchServerListingStates]);

  useEffect(() => {
    if (apiBaseUrl) {
      fetchAvailabilities();
    }
  }, [apiBaseUrl, fetchAvailabilities]);

  // Resolve listingState ('listed' | 'announced' | 'presale') for an availability item
  const resolveItemListingState = useCallback(
    (item: OVHAvailabilityRow): ListingState => {
      if (item.listingState === 'announced') return 'announced';
      if (
        item.listingState === 'presale' ||
        item.catalogSource === 'presale'
      ) {
        return 'presale';
      }
      if (item.listingState === 'listed') return 'listed';

      const srvMeta = serverListingMap[item.planCode];
      if (srvMeta) {
        if (srvMeta.listingState === 'announced') return 'announced';
        if (
          srvMeta.listingState === 'presale' ||
          srvMeta.catalogSource === 'presale'
        ) {
          return 'presale';
        }
        if (srvMeta.listingState === 'listed') return 'listed';
      }
      if (presaleSkuMap[item.planCode]) {
        return 'presale';
      }
      if (
        announcedSkuMap[item.planCode] &&
        (!item.datacenters || item.datacenters.length === 0)
      ) {
        return 'announced';
      }
      const realPlans = catalogDict?.realPlans || catalogDict?.plans || {};
      if (Object.keys(realPlans).length > 0) {
        const { baseCode } = parsePlanRegion(item.planCode);
        const directPlan = realPlans[item.planCode] || realPlans[baseCode];
        if (!directPlan || directPlan.catalogSource === 'presale') {
          return 'presale';
        }
      }
      if (!item.datacenters || item.datacenters.length === 0) {
        return 'announced';
      }
      return 'listed';
    },
    [serverListingMap, presaleSkuMap, announcedSkuMap, catalogDict]
  );

  // Merge availability rows by exact planCode (regional variants like -sgp / -mum stay separate rows)
  const mergedRows = useMemo<MergedPlanRow[]>(() => {
    const groups = new Map<string, OVHAvailabilityRow[]>();
    const seenBaseCodesInAvail = new Set<string>();

    availabilities.forEach((item) => {
      if (!item || !item.planCode) return;
      const code = item.planCode.trim();
      if (!groups.has(code)) {
        groups.set(code, []);
      }
      groups.get(code)!.push(item);
      if (item.datacenters && item.datacenters.length > 0) {
        const { baseCode } = parsePlanRegion(code);
        seenBaseCodesInAvail.add(code.toLowerCase());
        if (baseCode) seenBaseCodesInAvail.add(baseCode.toLowerCase());
      }
    });

    // Synthesize announced plans from serverListingMap or catalogDict.realPlans if not already in availabilities
    Object.entries(serverListingMap).forEach(([planCode, meta]) => {
      if (meta.listingState === 'announced' && !groups.has(planCode)) {
        groups.set(planCode, [
          {
            fqn: `${planCode}.announced`,
            planCode,
            server: planCode,
            memory: meta.memory || '',
            storage: meta.storage || '',
            listingState: 'announced',
            orderable: false,
            estimatedMonthlyPrice:
              typeof meta.startingPrice === 'number'
                ? meta.startingPrice
                : typeof meta.monthlyPrice === 'number'
                ? meta.monthlyPrice
                : undefined,
            currency: meta.currency || catalogDict?.currency || 'EUR',
            datacenters: [],
          },
        ]);
      }
    });

    if (catalogDict?.realPlans) {
      Object.entries(catalogDict.realPlans).forEach(([planCode, entry]) => {
        if (groups.has(planCode)) return;
        const { baseCode } = parsePlanRegion(planCode);
        if (
          seenBaseCodesInAvail.has(planCode.toLowerCase()) ||
          (baseCode && seenBaseCodesInAvail.has(baseCode.toLowerCase()))
        ) {
          return;
        }
        groups.set(planCode, [
          {
            fqn: `${planCode}.announced`,
            planCode,
            server: entry.product || planCode,
            memory: entry.defaultSpecs?.memory || '',
            storage: entry.defaultSpecs?.storage || '',
            listingState: 'announced',
            orderable: false,
            estimatedMonthlyPrice:
              typeof entry.price === 'number' ? entry.price : undefined,
            currency: entry.currency || catalogDict.currency || 'EUR',
            datacenters: [],
          },
        ]);
      });
    }

    const result: MergedPlanRow[] = [];

    groups.forEach((items, planCode) => {
      const first = items[0];
      const srvMeta = serverListingMap[planCode];
      const planMeta = resolvePlanDisplay(
        planCode,
        first.server,
        locale,
        catalogDict
      );
      const listingState = resolveItemListingState(first);
      const generation = inferPlanGeneration(planCode, first.generation);
      const listedAtStr =
        first.listedAt || srvMeta?.listedAt || '';
      const isListedWithin7d = Boolean(
        listedAtStr &&
          Date.now() - Date.parse(listedAtStr) <= 7 * 24 * 3600 * 1000
      );
      const isRecentlyAdded = Boolean(
        listingState === 'listed' &&
          (recentSkuMap[planCode] ||
            recentSkuMap[planMeta.baseCode] ||
            isListedWithin7d)
      );

      const configMap = new Map<string, MergedConfigEntry>();
      const dcAggregate = new Map<
        string,
        {
          bestAvailability: string;
          inStockConfigsCount: number;
          inStockConfigLabels: string[];
        }
      >();

      let minPrice: number | null =
        typeof srvMeta?.startingPrice === 'number' && srvMeta.startingPrice > 0
          ? srvMeta.startingPrice
          : typeof planMeta.basePrice === 'number' && planMeta.basePrice > 0
          ? planMeta.basePrice
          : null;
      const baseOrigMonthly =
        typeof first.originalMonthlyPrice === 'number' &&
        first.originalMonthlyPrice > 0
          ? first.originalMonthlyPrice
          : typeof srvMeta?.originalMonthlyPrice === 'number' &&
            srvMeta.originalMonthlyPrice > 0
          ? srvMeta.originalMonthlyPrice
          : planMeta.originalMonthlyPrice ?? null;
      const baseSetupFee =
        typeof first.setupFee === 'number'
          ? first.setupFee
          : typeof srvMeta?.setupFee === 'number'
          ? srvMeta.setupFee
          : planMeta.setupFee ?? null;
      const baseOrigSetupFee =
        typeof first.originalSetupFee === 'number'
          ? first.originalSetupFee
          : typeof srvMeta?.originalSetupFee === 'number'
          ? srvMeta.originalSetupFee
          : planMeta.originalSetupFee ?? null;
      const baseFreeSetup = Boolean(
        first.freeSetup ||
          srvMeta?.freeSetup ||
          planMeta.freeSetup ||
          (baseSetupFee === 0 && (baseOrigSetupFee ?? 0) > 0)
      );
      const basePricingModes =
        (Array.isArray(first.pricingModes) && first.pricingModes.length > 0
          ? first.pricingModes
          : Array.isArray(srvMeta?.pricingModes) &&
            srvMeta.pricingModes.length > 0
          ? srvMeta.pricingModes
          : planMeta.pricingModes) || undefined;
      const baseHasPromo = Boolean(
        first.hasPromo ||
          srvMeta?.hasPromo ||
          planMeta.hasPromo ||
          (basePricingModes || []).some((m) => m.hasPromo) ||
          baseFreeSetup ||
          (baseOrigMonthly && minPrice && baseOrigMonthly > minPrice + 0.01)
      );
      const basePromoCode =
        first.promoCode || srvMeta?.promoCode || planMeta.promoCode;
      const basePromoBadge =
        first.promoBadge ||
        srvMeta?.promoBadge ||
        planMeta.promoBadge ||
        (basePricingModes || []).find((m) => m.promoBadge)?.promoBadge;
      const basePromoDescription =
        first.promoDescription ||
        srvMeta?.promoDescription ||
        planMeta.promoDescription ||
        (basePricingModes || []).find((m) => m.promoDescription)
          ?.promoDescription;

      const currency =
        first.currency || srvMeta?.currency || planMeta.currency || 'EUR';

      items.forEach((it, idx) => {
        const cfgKey =
          it.configKey || buildConfigKey(it.memory, it.storage) || `cfg-${idx}`;
        const priceInfo = resolveConfigPrice(planMeta, it);
        const estPrice =
          typeof it.estimatedMonthlyPrice === 'number' &&
          it.estimatedMonthlyPrice > 0
            ? it.estimatedMonthlyPrice
            : typeof it.price === 'number' && it.price > 0
            ? it.price
            : priceInfo.totalPrice;

        if (estPrice !== null && estPrice > 0) {
          if (minPrice === null || estPrice < minPrice) {
            minPrice = estPrice;
          }
        }

        const itOrigMonthly =
          typeof it.originalMonthlyPrice === 'number' &&
          it.originalMonthlyPrice > 0
            ? it.originalMonthlyPrice
            : typeof it.originalPrice === 'number' && it.originalPrice > 0
            ? it.originalPrice
            : baseOrigMonthly;
        const itSetupFee =
          typeof it.setupFee === 'number' ? it.setupFee : baseSetupFee;
        const itOrigSetupFee =
          typeof it.originalSetupFee === 'number'
            ? it.originalSetupFee
            : baseOrigSetupFee;
        const itFreeSetup = Boolean(
          it.freeSetup ||
            baseFreeSetup ||
            (itSetupFee === 0 && (itOrigSetupFee ?? 0) > 0)
        );
        const itPricingModes =
          Array.isArray(it.pricingModes) && it.pricingModes.length > 0
            ? it.pricingModes
            : basePricingModes;
        const itHasPromo = Boolean(
          it.hasPromo ||
            baseHasPromo ||
            (itPricingModes || []).some((m) => m.hasPromo) ||
            itFreeSetup ||
            (itOrigMonthly && estPrice && itOrigMonthly > estPrice + 0.01)
        );

        const label = formatConfigLabel(it.memory, it.storage, locale);
        const dcMap: Record<string, string> = {};
        const dcs: DatacenterInfo[] = [];

        (it.datacenters || []).forEach((dc) => {
          const norm = normalizeDatacenterCode(dc.datacenter) || dc.datacenter;
          dcMap[norm] = dc.availability;
          dcs.push({ datacenter: norm, availability: dc.availability });
        });

        const inStockCount = dcs.filter((d) =>
          isStatusInStock(d.availability)
        ).length;

        const existingCfg = configMap.get(cfgKey);
        if (!existingCfg) {
          configMap.set(cfgKey, {
            configKey: cfgKey,
            fqn: it.fqn || `${planCode}.${cfgKey}`,
            memory: it.memory || '',
            storage: it.storage || '',
            systemStorage: it.systemStorage,
            gpu: it.gpu,
            options:
              Array.isArray(it.options) && it.options.length > 0
                ? it.options
                : [it.memory, it.storage].filter(
                    (x) => x && x !== 'N/A' && x !== 'none'
                  ),
            isDefault: Boolean(it.isDefault || idx === 0),
            label,
            estimatedMonthlyPrice: estPrice ?? null,
            originalMonthlyPrice: itOrigMonthly,
            setupFee: itSetupFee,
            originalSetupFee: itOrigSetupFee,
            freeSetup: itFreeSetup,
            hasPromo: itHasPromo,
            promoCode: it.promoCode || basePromoCode,
            promoBadge: it.promoBadge || basePromoBadge,
            promoDescription: it.promoDescription || basePromoDescription,
            pricingModes: itPricingModes,
            currency: it.currency || currency,
            datacenters: dcs,
            dcMap,
            inStockCount,
          });
        } else {
          dcs.forEach((d) => {
            const cur = existingCfg.dcMap[d.datacenter];
            if (
              !cur ||
              rankAvailabilityStatus(d.availability) >
                rankAvailabilityStatus(cur)
            ) {
              existingCfg.dcMap[d.datacenter] = d.availability;
            }
          });
          existingCfg.datacenters = Object.entries(existingCfg.dcMap).map(
            ([datacenter, availability]) => ({ datacenter, availability })
          );
          existingCfg.inStockCount = existingCfg.datacenters.filter((d) =>
            isStatusInStock(d.availability)
          ).length;
        }
      });

      const configsList = Array.from(configMap.values()).sort((a, b) => {
        if (a.isDefault && !b.isDefault) return -1;
        if (!a.isDefault && b.isDefault) return 1;
        if (b.inStockCount !== a.inStockCount) {
          return b.inStockCount - a.inStockCount;
        }
        return (
          (a.estimatedMonthlyPrice ?? 999999) -
          (b.estimatedMonthlyPrice ?? 999999)
        );
      });

      // Compute best availability and in-stock config count per datacenter across all configs of this planCode
      configsList.forEach((cfg) => {
        cfg.datacenters.forEach((d) => {
          const prev = dcAggregate.get(d.datacenter);
          const inStock = isStatusInStock(d.availability);
          if (!prev) {
            dcAggregate.set(d.datacenter, {
              bestAvailability: d.availability,
              inStockConfigsCount: inStock ? 1 : 0,
              inStockConfigLabels: inStock ? [cfg.label] : [],
            });
          } else {
            if (
              rankAvailabilityStatus(d.availability) >
              rankAvailabilityStatus(prev.bestAvailability)
            ) {
              prev.bestAvailability = d.availability;
            }
            if (inStock) {
              prev.inStockConfigsCount += 1;
              if (!prev.inStockConfigLabels.includes(cfg.label)) {
                prev.inStockConfigLabels.push(cfg.label);
              }
            }
          }
        });
      });

      const allDatacenters: MergedDatacenterCell[] = Array.from(
        dcAggregate.entries()
      ).map(([datacenter, info]) => ({
        datacenter,
        bestAvailability: info.bestAvailability,
        inStockConfigsCount: info.inStockConfigsCount,
        inStockConfigLabels: info.inStockConfigLabels,
      }));

      const totalInStockDcs = allDatacenters.filter((d) =>
        isStatusInStock(d.bestAvailability)
      ).length;
      const has1HStock = allDatacenters.some(
        (d) =>
          d.bestAvailability === '1H-low' || d.bestAvailability === '1H-high'
      );

      const rowHasPromo = Boolean(
        baseHasPromo ||
          configsList.some(
            (c) => c.hasPromo || (c.pricingModes || []).some((m) => m.hasPromo)
          )
      );

      result.push({
        planCode,
        server: first.server || planCode,
        listingState,
        orderable: listingState === 'listed',
        generation,
        isRecentlyAdded,
        startingPrice: minPrice,
        originalMonthlyPrice: baseOrigMonthly,
        setupFee: baseSetupFee,
        originalSetupFee: baseOrigSetupFee,
        freeSetup: baseFreeSetup,
        hasPromo: rowHasPromo,
        promoCode: basePromoCode,
        promoBadge: basePromoBadge,
        promoDescription: basePromoDescription,
        pricingModes: basePricingModes,
        currency,
        configs: configsList,
        allDatacenters,
        totalInStockDcs,
        has1HStock,
      });
    });

    return result;
  }, [availabilities, serverListingMap, catalogDict, locale, resolveItemListingState, recentSkuMap]);

  // Tab counts
  const tabCounts = useMemo(() => {
    const counts = {
      all: mergedRows.length,
      listed: 0,
      promo: 0,
      announced: 0,
      presale: 0,
      new: 0,
    };
    mergedRows.forEach((r) => {
      if (r.listingState === 'listed') counts.listed += 1;
      else if (r.listingState === 'announced') counts.announced += 1;
      else if (r.listingState === 'presale') counts.presale += 1;
      if (
        r.hasPromo ||
        r.configs.some(
          (c) => c.hasPromo || (c.pricingModes || []).some((m) => m.hasPromo)
        )
      ) {
        counts.promo += 1;
      }
      if (r.isRecentlyAdded) counts.new += 1;
    });
    return counts;
  }, [mergedRows]);

  const filteredData = useMemo(() => {
    let filtered = [...mergedRows];

    if (listingFilterTab === 'listed') {
      filtered = filtered.filter((r) => r.listingState === 'listed');
    } else if (listingFilterTab === 'promo') {
      filtered = filtered.filter(
        (r) =>
          r.hasPromo ||
          r.configs.some(
            (c) => c.hasPromo || (c.pricingModes || []).some((m) => m.hasPromo)
          )
      );
    } else if (listingFilterTab === 'announced') {
      filtered = filtered.filter((r) => r.listingState === 'announced');
    } else if (listingFilterTab === 'presale') {
      filtered = filtered.filter((r) => r.listingState === 'presale');
    } else if (listingFilterTab === 'new') {
      filtered = filtered.filter((r) => r.isRecentlyAdded);
    }

    if (debouncedSearch) {
      const term = debouncedSearch.toLowerCase();
      filtered = filtered.filter((row) => {
        const planMeta = resolvePlanDisplay(
          row.planCode,
          row.server,
          locale,
          catalogDict
        );
        if (
          row.planCode.toLowerCase().includes(term) ||
          row.server.toLowerCase().includes(term) ||
          planMeta.name.toLowerCase().includes(term) ||
          planMeta.cpuText.toLowerCase().includes(term)
        ) {
          return true;
        }
        return row.configs.some(
          (c) =>
            c.memory.toLowerCase().includes(term) ||
            c.storage.toLowerCase().includes(term) ||
            c.label.toLowerCase().includes(term) ||
            c.fqn.toLowerCase().includes(term)
        );
      });
    }

    if (filterDatacenter !== 'all') {
      const targetDc = normalizeDatacenterCode(filterDatacenter);
      filtered = filtered.filter((row) =>
        row.allDatacenters.some(
          (dc) => normalizeDatacenterCode(dc.datacenter) === targetDc
        )
      );
    }

    if (filterAvailability !== 'all') {
      filtered = filtered.filter((row) => {
        if (filterAvailability === 'available') {
          return row.totalInStockDcs > 0;
        } else if (filterAvailability === 'unavailable') {
          return row.totalInStockDcs === 0;
        } else if (filterAvailability === '1h') {
          return row.has1HStock;
        }
        return true;
      });
    }

    if (filterMemory !== 'all') {
      filtered = filtered.filter((row) =>
        row.configs.some((c) => {
          const memMatch = c.memory.match(/(\d+)g/i);
          if (!memMatch) return false;
          const memSize = parseInt(memMatch[1], 10);
          switch (filterMemory) {
            case '<=128':
              return memSize <= 128;
            case '256':
              return memSize >= 128 && memSize <= 256;
            case '512':
              return memSize >= 256 && memSize <= 512;
            case '>=1024':
              return memSize >= 1024;
            default:
              return true;
          }
        })
      );
    }

    filtered.sort((a, b) => {
      let compareValue = 0;
      switch (sortBy) {
        case 'planCode':
          compareValue = a.planCode.localeCompare(b.planCode);
          break;
        case 'memory': {
          const aMemMatch = a.configs[0]?.memory.match(/(\d+)g/i);
          const bMemMatch = b.configs[0]?.memory.match(/(\d+)g/i);
          const aMem = aMemMatch ? parseInt(aMemMatch[1], 10) : 0;
          const bMem = bMemMatch ? parseInt(bMemMatch[1], 10) : 0;
          compareValue = aMem - bMem;
          break;
        }
        case 'availability':
          compareValue = a.totalInStockDcs - b.totalInStockDcs;
          break;
        case 'price': {
          const aPrice = a.startingPrice ?? 999999;
          const bPrice = b.startingPrice ?? 999999;
          compareValue = aPrice - bPrice;
          break;
        }
      }
      return sortOrder === 'asc' ? compareValue : -compareValue;
    });

    return filtered;
  }, [
    mergedRows,
    listingFilterTab,
    debouncedSearch,
    filterDatacenter,
    filterAvailability,
    filterMemory,
    sortBy,
    sortOrder,
    locale,
    catalogDict,
  ]);

  const paginatedData = useMemo(() => {
    const startIndex = (currentPage - 1) * itemsPerPage;
    return filteredData.slice(startIndex, startIndex + itemsPerPage);
  }, [filteredData, currentPage, itemsPerPage]);

  const totalPages = Math.ceil(filteredData.length / itemsPerPage);

  const exportData = () => {
    const dataStr = JSON.stringify(filteredData, null, 2);
    const dataBlob = new Blob([dataStr], { type: 'application/json' });
    const url = URL.createObjectURL(dataBlob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `ovh-availability-${new Date().toISOString().split('T')[0]}.json`;
    link.click();
    URL.revokeObjectURL(url);
    toast.success('数据已导出');
  };

  const stats = {
    total: filteredData.length,
    available: filteredData.filter((r) => r.totalInStockDcs > 0).length,
    oneHour: filteredData.filter((r) => r.has1HStock).length,
  };

  const getAvailabilityInfo = (availability: string) => {
    switch (availability) {
      case '1H-low':
        return {
          text: '1H-Low',
          color: 'text-yellow-400',
          bg: 'bg-yellow-500/10',
          border: 'border-yellow-500/30',
        };
      case '1H-high':
        return {
          text: '1H-High',
          color: 'text-green-400',
          bg: 'bg-green-500/10',
          border: 'border-green-500/30',
        };
      case '72H':
        return {
          text: '72H',
          color: 'text-blue-400',
          bg: 'bg-blue-500/10',
          border: 'border-blue-500/30',
        };
      case '480H':
        return {
          text: '480H',
          color: 'text-purple-400',
          bg: 'bg-purple-500/10',
          border: 'border-purple-500/30',
        };
      case 'unavailable':
        return {
          text: locale === 'zh-CN' ? '无货' : locale === 'fr' ? 'Rupture' : 'Out',
          color: 'text-red-400/80',
          bg: 'bg-red-500/5',
          border: 'border-red-500/20',
        };
      case 'unknown':
        return {
          text: '-',
          color: 'text-gray-400',
          bg: 'bg-gray-500/10',
          border: 'border-gray-500/30',
        };
      default:
        return {
          text: availability,
          color: 'text-cyan-400',
          bg: 'bg-cyan-500/10',
          border: 'border-cyan-500/30',
        };
    }
  };

  const renderSkuChangeBadge = (chg: SkuChangeRecord) => {
    if (chg.type === 'replaced') {
      return (
        <span className="px-2 py-0.5 rounded font-semibold bg-amber-500/15 text-amber-400 border border-amber-500/30">
          {t('sku.replaced', {
            oldCode: chg.replacedFrom || 'old',
            newCode: chg.planCode || chg.skuKey,
          })}
        </span>
      );
    }
    if (chg.saleStarted) {
      return (
        <span className="px-2 py-0.5 rounded font-semibold bg-cyan-500/15 text-cyan-300 border border-cyan-500/30">
          {t('sku.saleStarted')}
        </span>
      );
    }
    if (chg.listingState === 'announced') {
      return (
        <span className="px-2 py-0.5 rounded font-semibold bg-amber-500/15 text-amber-300 border border-amber-500/30">
          {t('announced.skuAdded')}
        </span>
      );
    }
    if (
      chg.type === 'presale' ||
      (chg.type === 'added' && chg.listingState === 'presale')
    ) {
      return (
        <span className="px-2 py-0.5 rounded font-semibold bg-purple-500/15 text-purple-400 border border-purple-500/30">
          {t('presale.skuAdded')}
        </span>
      );
    }
    if (chg.type === 'added' || chg.type === 'listed') {
      return (
        <span className="px-2 py-0.5 rounded font-semibold bg-emerald-500/15 text-emerald-400 border border-emerald-500/30">
          {t('sku.newListed')}
        </span>
      );
    }
    if (chg.type === 'removed') {
      return (
        <span className="px-2 py-0.5 rounded font-semibold bg-red-500/15 text-red-400 border border-red-500/30">
          {t('sku.removedBadge')}
        </span>
      );
    }
    return (
      <span className="px-2 py-0.5 rounded font-semibold bg-cyber-accent/15 text-cyber-accent border border-cyber-accent/30">
        {t('sku.configChangedBadge')}
      </span>
    );
  };

  return (
    <div className="space-y-5">
      <motion.div
        initial={{ opacity: 0, y: -20 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3 }}
      >
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
          <div>
            <h1
              className={`${
                isMobile ? 'text-2xl' : 'text-3xl'
              } font-bold mb-1 cyber-glow-text`}
            >
              OVH 实时可用性矩阵
            </h1>
            <p className="text-cyber-muted text-sm">
              按型号合并展示（区域变体独立成行），支持行内按配置筛选价格与机房实时库存
            </p>
          </div>

          <div className="flex gap-2">
            <Button
              onClick={exportData}
              disabled={filteredData.length === 0}
              variant="cyber"
              size="sm"
              className="flex items-center gap-2 text-xs sm:text-sm"
            >
              <Download className="w-3 h-3 sm:w-4 sm:h-4" />
              {!isMobile && '导出JSON'}
            </Button>
            <Button
              onClick={() => fetchAvailabilities(true)}
              disabled={isLoading || isRefreshing}
              variant="cyber"
              size="sm"
              className="flex items-center gap-2 text-xs sm:text-sm"
            >
              <RefreshCw
                className={`w-3 h-3 sm:w-4 sm:h-4 flex-shrink-0 ${
                  isRefreshing ? 'animate-spin' : ''
                }`}
              />
              <span className="min-w-[2.5rem]">刷新</span>
            </Button>
          </div>
        </div>
      </motion.div>

      {/* SKU 货架变动记录折叠面板 */}
      <div className="cyber-panel p-3.5 sm:p-4 border-cyber-accent/30">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <button
            type="button"
            onClick={() => setShowSkuTimeline((prev) => !prev)}
            className="flex items-center gap-2 text-left font-semibold text-sm sm:text-base text-cyber-text hover:text-cyber-accent transition-colors"
          >
            <History className="w-4 h-4 text-cyber-accent" />
            <span>货架 SKU 变动时间线</span>
            <span className="px-2 py-0.5 text-xs rounded-full bg-cyber-accent/15 text-cyber-accent font-mono">
              {skuChanges.length} 条记录
            </span>
            {skuTotalIndexed > 0 && (
              <span className="text-xs text-cyber-muted font-normal hidden sm:inline">
                （当前基线已收录 {skuTotalIndexed} 款在架型号）
              </span>
            )}
            {showSkuTimeline ? (
              <ChevronUp className="w-4 h-4 text-cyber-muted" />
            ) : (
              <ChevronDown className="w-4 h-4 text-cyber-muted" />
            )}
          </button>

          <Button
            onClick={handleManualSkuCheck}
            disabled={isCheckingSku}
            variant="cyber"
            size="sm"
            className="text-xs flex items-center gap-1.5"
          >
            <RefreshCw
              className={`w-3.5 h-3.5 ${isCheckingSku ? 'animate-spin' : ''}`}
            />
            {isCheckingSku ? '检测中...' : '立即检测 SKU 变动'}
          </Button>
        </div>

        {showSkuTimeline && (
          <div className="mt-3 pt-3 border-t border-cyber-grid space-y-2.5 max-h-80 overflow-y-auto">
            {skuChanges.length === 0 ? (
              <div className="text-xs sm:text-sm text-cyber-muted py-4 text-center">
                暂无货架 SKU 增删变动记录（首次运行仅建立基线，后续检测到新型号上架、开售、换代或移除将在此展示并推送通知）。
              </div>
            ) : (
              skuChanges.map((chg) => (
                <div
                  key={chg.id}
                  className="p-2.5 rounded-lg border border-cyber-grid bg-cyber-bg flex flex-col sm:flex-row sm:items-center justify-between gap-2 text-xs"
                >
                  <div className="space-y-0.5">
                    <div className="flex flex-wrap items-center gap-2">
                      {renderSkuChangeBadge(chg)}
                      <span className="font-bold text-cyber-text text-sm">
                        {chg.name}
                      </span>
                      <span className="font-mono text-cyber-muted">
                        (
                        {chg.type === 'replaced' && chg.replacedFrom
                          ? `${chg.replacedFrom} → ${chg.planCode || chg.skuKey}`
                          : chg.planCode || chg.skuKey}
                        )
                      </span>
                      {chg.cpuText && (
                        <span className="text-cyber-muted">· {chg.cpuText}</span>
                      )}
                    </div>
                    {(chg.memorySummary || chg.storageSummary) && (
                      <div className="text-cyber-muted">
                        {chg.memorySummary && (
                          <span>内存: {chg.memorySummary} </span>
                        )}
                        {chg.storageSummary && (
                          <span>· 存储: {chg.storageSummary}</span>
                        )}
                      </div>
                    )}
                  </div>
                  <div className="flex sm:flex-col items-end justify-between gap-1 text-right flex-shrink-0">
                    {chg.stockState && (
                      <span className="px-2 py-0.5 rounded bg-cyber-surface border border-cyber-grid text-cyber-text font-medium">
                        {chg.stockState}
                      </span>
                    )}
                    <span className="text-cyber-muted font-mono text-[11px]">
                      {new Date(chg.timestamp).toLocaleString('zh-CN')}
                    </span>
                  </div>
                </div>
              ))
            )}
          </div>
        )}
      </div>

      {/* 统计卡片 */}
      {mergedRows.length > 0 && (
        <div className="grid grid-cols-3 gap-2 sm:gap-4">
          <div className="cyber-panel p-2 sm:p-3.5 backdrop-blur-0 overflow-visible">
            <div className="flex items-center gap-1 sm:gap-2 text-cyber-muted text-xs mb-0.5">
              <Database className="w-3.5 h-3.5" />
              <span>机型总数 (按 planCode)</span>
            </div>
            <div className="text-lg sm:text-2xl font-bold text-cyber-accent">
              {stats.total}
            </div>
          </div>
          <div className="cyber-panel p-2 sm:p-3.5 backdrop-blur-0 overflow-visible">
            <div className="flex items-center gap-1 sm:gap-2 text-cyber-muted text-xs mb-0.5">
              <TrendingUp className="w-3.5 h-3.5" />
              <span>有货机型</span>
            </div>
            <div className="text-lg sm:text-2xl font-bold text-green-400">
              {stats.available}
            </div>
          </div>
          <div className="cyber-panel p-2 sm:p-3.5 backdrop-blur-0 overflow-visible">
            <div className="flex items-center gap-1 sm:gap-2 text-cyber-muted text-xs mb-0.5">
              <Filter className="w-3.5 h-3.5" />
              <span>1 小时内急速交付</span>
            </div>
            <div className="text-lg sm:text-2xl font-bold text-yellow-400">
              {stats.oneHour}
            </div>
          </div>
        </div>
      )}

      {/* 搜索、过滤与状态分页签 */}
      {mergedRows.length > 0 && (
        <div className="cyber-panel p-3 sm:p-4 backdrop-blur-0 overflow-visible space-y-3">
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
            <div className="relative sm:col-span-2 lg:col-span-1">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-cyber-muted" />
              <input
                type="text"
                placeholder={
                  isMobile ? '搜索...' : '搜索型号、planCode、CPU、内存、存储...'
                }
                value={searchTerm}
                onChange={(e) => setSearchTerm(e.target.value)}
                className="cyber-input pl-9 w-full text-sm"
              />
            </div>

            <select
              value={filterDatacenter}
              onChange={(e) => setFilterDatacenter(e.target.value)}
              className="cyber-input w-full text-sm"
            >
              <option value="all">所有数据中心</option>
              <optgroup label="🇪🇺 欧洲 (Europe)">
                {[
                  'rbx',
                  'sbg',
                  'gra',
                  'par',
                  'mrs',
                  'lim',
                  'fra',
                  'lon',
                  'eri',
                  'waw',
                ].map((dc) => (
                  <option key={dc} value={dc}>
                    {formatDatacenter(dc, locale, 'full')}
                  </option>
                ))}
              </optgroup>
              <optgroup label="🇨🇦 🇺🇸 北美 (North America)">
                {['bhs', 'tor', 'hil', 'vin'].map((dc) => (
                  <option key={dc} value={dc}>
                    {formatDatacenter(dc, locale, 'full')}
                  </option>
                ))}
              </optgroup>
              <optgroup label="🌏 亚太 (Asia Pacific)">
                {['sgp', 'syd', 'mum'].map((dc) => (
                  <option key={dc} value={dc}>
                    {formatDatacenter(dc, locale, 'full')}
                  </option>
                ))}
              </optgroup>
            </select>

            <select
              value={filterAvailability}
              onChange={(e) => setFilterAvailability(e.target.value)}
              className="cyber-input w-full text-sm"
            >
              <option value="all">所有库存状态</option>
              <option value="available">有货</option>
              <option value="1h">1小时内 (1H)</option>
              <option value="unavailable">无货</option>
            </select>

            <select
              value={filterMemory}
              onChange={(e) => setFilterMemory(e.target.value)}
              className="cyber-input w-full text-sm"
            >
              <option value="all">所有内存</option>
              <option value="<=128">≤ 128GB</option>
              <option value="256">128GB - 256GB</option>
              <option value="512">256GB - 512GB</option>
              <option value=">=1024">≥ 1TB</option>
            </select>
          </div>

          {/* 排序按钮与状态 Filter Tabs (All, Listed, Announced, Presale, New) */}
          <div className="flex flex-wrap items-center justify-between gap-2 text-xs sm:text-sm pt-1 border-t border-cyber-grid/50">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-cyber-muted text-xs">排序：</span>
              {(
                [
                  ['planCode', '型号'],
                  ['memory', '内存'],
                  ['availability', '有货机房'],
                  ['price', '起始月价'],
                ] as const
              ).map(([key, label]) => (
                <Button
                  key={key}
                  onClick={() => {
                    if (sortBy === key) {
                      setSortOrder(sortOrder === 'asc' ? 'desc' : 'asc');
                    } else {
                      setSortBy(key);
                      setSortOrder(key === 'availability' ? 'desc' : 'asc');
                    }
                  }}
                  variant="cyber"
                  size="sm"
                  className={`text-xs h-7 px-2.5 ${
                    sortBy === key ? 'bg-cyber-accent/20' : ''
                  }`}
                >
                  {label} {sortBy === key && (sortOrder === 'asc' ? '↑' : '↓')}
                </Button>
              ))}
            </div>

            {/* Listing State Filter Tabs */}
            <div className="flex flex-wrap items-center gap-1.5">
              {(
                [
                  {
                    id: 'all',
                    label: t('listing.filter.all'),
                    count: tabCounts.all,
                    activeCls:
                      'bg-cyber-accent/20 text-cyber-accent border-cyber-accent/50',
                  },
                  {
                    id: 'listed',
                    label: t('listing.filter.listed'),
                    count: tabCounts.listed,
                    activeCls:
                      'bg-emerald-500/20 text-emerald-300 border-emerald-500/50',
                  },
                  {
                    id: 'promo',
                    label: t('listing.filter.promo'),
                    count: tabCounts.promo,
                    activeCls:
                      'bg-rose-500/20 text-rose-300 border-rose-500/50 shadow-[0_0_10px_rgba(244,63,94,0.2)]',
                  },
                  {
                    id: 'announced',
                    label: t('listing.filter.announced'),
                    count: tabCounts.announced,
                    activeCls:
                      'bg-amber-500/20 text-amber-300 border-amber-500/50',
                  },
                  {
                    id: 'presale',
                    label: t('listing.filter.presale'),
                    count: tabCounts.presale,
                    activeCls:
                      'bg-purple-500/20 text-purple-300 border-purple-500/50',
                  },
                  {
                    id: 'new',
                    label: t('listing.filter.new'),
                    count: tabCounts.new,
                    activeCls:
                      'bg-cyan-500/20 text-cyan-300 border-cyan-500/50',
                  },
                ] as const
              ).map((tab) => {
                const isActive = listingFilterTab === tab.id;
                return (
                  <button
                    key={tab.id}
                    type="button"
                    onClick={() => setListingFilterTab(tab.id)}
                    className={`px-2.5 py-1 rounded-md text-xs font-semibold border transition-all flex items-center gap-1.5 ${
                      isActive
                        ? tab.activeCls
                        : tab.id === 'promo'
                        ? 'bg-rose-500/10 text-rose-300/90 border-rose-500/30 hover:bg-rose-500/20'
                        : 'bg-cyber-surface text-cyber-muted border-cyber-grid hover:text-cyber-text'
                    }`}
                  >
                    <span>{tab.label}</span>
                    <span className="px-1.5 py-0.2 rounded-full text-[10px] font-mono bg-black/20">
                      {tab.count}
                    </span>
                  </button>
                );
              })}
            </div>
          </div>
        </div>
      )}

      {/* 紧凑型实时库存矩阵列表（每款 planCode 一行） */}
      {filteredData.length === 0 && mergedRows.length > 0 ? (
        <div className="cyber-panel p-8 text-center backdrop-blur-0 overflow-visible">
          <Filter className="w-12 h-12 text-cyber-muted mx-auto mb-3 opacity-50" />
          <p className="text-cyber-muted mb-1">没有匹配的服务器型号</p>
          <p className="text-xs text-slate-500">尝试切换状态筛选或搜索关键字</p>
        </div>
      ) : filteredData.length > 0 ? (
        <>
          <div className="space-y-2">
            {paginatedData.map((row) => {
              const planMeta = resolvePlanDisplay(
                row.planCode,
                row.server,
                locale,
                catalogDict
              );
              const selectedCfgKey = selectedRowConfigs[row.planCode] || 'all';
              const activeConfig =
                selectedCfgKey !== 'all'
                  ? row.configs.find((c) => c.configKey === selectedCfgKey) ||
                    null
                  : null;

              const symbol = getCurrencySymbol(
                activeConfig?.currency || row.currency
              );
              const displayPrice = activeConfig
                ? activeConfig.estimatedMonthlyPrice
                : row.startingPrice;
              const displayOrigPrice = activeConfig
                ? activeConfig.originalMonthlyPrice
                : row.originalMonthlyPrice;
              const displaySetupFee = activeConfig
                ? activeConfig.setupFee
                : row.setupFee;
              const displayOrigSetupFee = activeConfig
                ? activeConfig.originalSetupFee
                : row.originalSetupFee;
              const displayFreeSetup = Boolean(
                activeConfig ? activeConfig.freeSetup : row.freeSetup
              );
              const displayHasPromo = Boolean(
                activeConfig ? activeConfig.hasPromo : row.hasPromo
              );
              const displayPromoBadge =
                (activeConfig ? activeConfig.promoBadge : row.promoBadge) ||
                row.promoBadge;
              const displayPromoDesc =
                (activeConfig
                  ? activeConfig.promoDescription
                  : row.promoDescription) || row.promoDescription;

              const activeGpuText = activeConfig?.gpu
                ? formatGpuCode(activeConfig.gpu, locale)
                : '';

              const dcCellsToRender: {
                datacenter: string;
                availability: string;
                inStockConfigsCount: number;
                tooltip?: string;
              }[] = activeConfig
                ? activeConfig.datacenters.map((d) => ({
                    datacenter: d.datacenter,
                    availability: d.availability,
                    inStockConfigsCount: isStatusInStock(d.availability) ? 1 : 0,
                  }))
                : row.allDatacenters.map((d) => ({
                    datacenter: d.datacenter,
                    availability: d.bestAvailability,
                    inStockConfigsCount: d.inStockConfigsCount,
                    tooltip:
                      d.inStockConfigLabels.length > 0
                        ? t('availability.bestStatusTooltip', {
                            dc: d.datacenter.toUpperCase(),
                            configs: d.inStockConfigLabels.join(' | '),
                          })
                        : undefined,
                  }));

              return (
                <div
                  key={row.planCode}
                  className="cyber-panel px-3 py-2.5 sm:px-4 sm:py-3 hover:border-cyber-accent/50 transition-colors backdrop-blur-0 overflow-visible"
                >
                  {/* Top Row: Model, Badges, Config Selector Dropdown, Price & Quick Monitor */}
                  <div className="flex flex-col lg:flex-row lg:items-center justify-between gap-2.5">
                    {/* Left: Model identity & CPU */}
                    <div className="flex-1 min-w-0">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <h3 className="text-sm sm:text-base font-bold text-cyber-accent truncate">
                          {planMeta.name}
                        </h3>
                        {planMeta.regionLabel && (
                          <span className="px-1.5 py-0.5 text-[10px] font-medium rounded bg-cyber-accent/15 text-cyber-accent border border-cyber-accent/30">
                            {planMeta.regionLabel}
                          </span>
                        )}
                        <span className="px-1.5 py-0.5 text-[11px] font-mono rounded bg-cyber-grid/40 text-cyber-muted">
                          {row.planCode}
                        </span>
                        {displayHasPromo && (
                          <span
                            title={displayPromoDesc}
                            className="px-1.5 py-0.5 text-[10px] font-bold rounded bg-rose-500/15 text-rose-300 border border-rose-500/40"
                          >
                            🔥 {displayPromoBadge || t('promo.badge')}
                          </span>
                        )}
                        {row.listingState === 'announced' && (
                          <span className="px-2 py-0.5 text-[10px] font-semibold rounded bg-amber-500/15 text-amber-300 border border-amber-500/30 flex items-center gap-1">
                            <Sparkles className="w-2.5 h-2.5" />
                            {t('announced.badge')}
                          </span>
                        )}
                        {row.listingState === 'presale' && (
                          <span className="px-2 py-0.5 text-[10px] font-semibold rounded bg-purple-500/15 text-purple-400 border border-purple-500/30 flex items-center gap-1">
                            <Sparkles className="w-2.5 h-2.5" />
                            {t('presale.badge')}
                          </span>
                        )}
                        {row.listingState === 'listed' && (
                          <span className="px-1.5 py-0.5 text-[10px] font-medium rounded bg-emerald-500/10 text-emerald-400 border border-emerald-500/25">
                            {t('listed.badge')}
                          </span>
                        )}
                        {row.generation && (
                          <span className="px-1.5 py-0.5 text-[10px] font-mono rounded bg-indigo-500/15 text-indigo-300 border border-indigo-500/30">
                            {t('presale.generation', { year: row.generation })}
                          </span>
                        )}
                        {row.isRecentlyAdded && (
                          <span className="px-1.5 py-0.5 text-[10px] font-bold rounded bg-emerald-500/15 text-emerald-400 border border-emerald-500/30 flex items-center gap-1">
                            <Sparkles className="w-2.5 h-2.5" />
                            {t('sku.newListed')}
                          </span>
                        )}
                        {planMeta.cpuText && (
                          <span className="text-xs text-cyber-muted truncate max-w-md">
                            · {planMeta.cpuText}
                          </span>
                        )}
                        {activeGpuText && (
                          <span className="text-xs text-cyber-accent font-medium">
                            · {activeGpuText}
                          </span>
                        )}
                      </div>
                    </div>

                    {/* Right: Inline Configuration Dropdown + Price + Quick Monitor Button */}
                    <div className="flex flex-wrap items-center gap-2 flex-shrink-0">
                      {row.configs.length > 0 && (
                        <div className="flex items-center gap-1.5 min-w-[220px] sm:min-w-[280px] max-w-full">
                          <select
                            aria-label={t('availability.configSelectorLabel')}
                            value={selectedCfgKey}
                            onChange={(e) =>
                              setSelectedRowConfigs((prev) => ({
                                ...prev,
                                [row.planCode]: e.target.value,
                              }))
                            }
                            className="cyber-input text-xs py-1 px-2 w-full"
                          >
                            <option value="all">
                              {t('availability.allConfigs', {
                                count: row.configs.length,
                              })}
                            </option>
                            {row.configs.map((cfg) => {
                              const cfgSym = getCurrencySymbol(cfg.currency);
                              const promoTag = cfg.hasPromo
                                ? `🔥${cfg.promoBadge || t('promo.badge')} `
                                : '';
                              const pricePart =
                                cfg.estimatedMonthlyPrice &&
                                cfg.estimatedMonthlyPrice > 0
                                  ? ` — ${cfgSym}${cfg.estimatedMonthlyPrice.toFixed(
                                      2
                                    )}${t('availability.perMonth')}`
                                  : '';
                              const setupPart =
                                cfg.setupFee === 0 || cfg.freeSetup
                                  ? ` · ${t('promo.freeSetup')}`
                                  : typeof cfg.setupFee === 'number' &&
                                    cfg.setupFee > 0
                                  ? ` · ${t('promo.setupFee', {
                                      price: `${cfgSym}${cfg.setupFee.toFixed(
                                        2
                                      )}`,
                                    })}`
                                  : '';
                              const dcPart = ` (${t(
                                'availability.inStockDcCount',
                                { count: cfg.inStockCount }
                              )})`;
                              return (
                                <option
                                  key={cfg.configKey}
                                  value={cfg.configKey}
                                >
                                  {`${promoTag}${cfg.label}${pricePart}${setupPart}${dcPart}`}
                                </option>
                              );
                            })}
                          </select>
                        </div>
                      )}

                      {/* Setup Fee Pill */}
                      {row.listingState !== 'presale' &&
                        (displaySetupFee === 0 || displayFreeSetup ? (
                          <div className="px-2 py-1 rounded-md bg-emerald-500/15 border border-emerald-500/35 text-emerald-300 font-semibold text-xs leading-none whitespace-nowrap inline-flex items-center">
                            <span>{t('promo.freeSetup')}</span>
                            {displayOrigSetupFee !== null &&
                              displayOrigSetupFee > 0 && (
                                <span className="line-through text-[10px] opacity-70 ml-1 font-mono">
                                  {symbol}
                                  {displayOrigSetupFee.toFixed(2)}
                                </span>
                              )}
                          </div>
                        ) : displaySetupFee !== null && displaySetupFee > 0 ? (
                          <div className="px-2 py-1 rounded-md bg-cyber-grid/40 border border-cyber-grid text-cyber-muted font-mono text-xs leading-none whitespace-nowrap">
                            {t('promo.setupFee', {
                              price: `${symbol}${displaySetupFee.toFixed(2)}`,
                            })}
                          </div>
                        ) : null)}

                      {/* Price Pill */}
                      {displayPrice !== null &&
                      displayPrice > 0 &&
                      row.listingState !== 'presale' ? (
                        <div className="px-2.5 py-1 rounded-md bg-cyber-accent/10 border border-cyber-accent/30 text-cyber-accent font-bold text-xs sm:text-sm leading-none whitespace-nowrap inline-flex items-center">
                          <span>
                            {activeConfig
                              ? `${symbol}${displayPrice.toFixed(2)}${t(
                                  'availability.perMonth'
                                )}`
                              : t('availability.fromPrice', {
                                  price: `${symbol}${displayPrice.toFixed(2)}`,
                                })}
                          </span>
                          {displayOrigPrice !== null &&
                            displayOrigPrice > displayPrice + 0.01 && (
                              <span className="line-through text-[10px] opacity-70 ml-1 font-mono">
                                {symbol}
                                {displayOrigPrice.toFixed(2)}
                              </span>
                            )}
                        </div>
                      ) : row.listingState === 'presale' ? (
                        <div
                          title={t('presale.notOrderableHint')}
                          className="px-2 py-1 rounded bg-purple-500/10 border border-purple-500/30 text-purple-300 text-xs font-medium whitespace-nowrap"
                        >
                          {t('presale.pricingTbd')}
                        </div>
                      ) : null}

                      {/* Quick Monitor Button */}
                      <button
                        type="button"
                        onClick={() =>
                          setQuickMonitorTarget({
                            planCode: row.planCode,
                            serverName: planMeta.name,
                            listingState: row.listingState,
                            isPresale: row.listingState === 'presale',
                            datacenters: activeConfig
                              ? activeConfig.datacenters
                              : row.allDatacenters.map((d) => ({
                                  datacenter: d.datacenter,
                                  availability: d.bestAvailability,
                                })),
                            initialConfig: activeConfig
                              ? {
                                  configKey: activeConfig.configKey,
                                  options: activeConfig.options,
                                  label: activeConfig.label,
                                  estimatedMonthlyPrice:
                                    activeConfig.estimatedMonthlyPrice ??
                                    undefined,
                                  originalMonthlyPrice:
                                    activeConfig.originalMonthlyPrice ??
                                    undefined,
                                  setupFee: activeConfig.setupFee ?? undefined,
                                  originalSetupFee:
                                    activeConfig.originalSetupFee ?? undefined,
                                  freeSetup: activeConfig.freeSetup,
                                  hasPromo: activeConfig.hasPromo,
                                  promoCode: activeConfig.promoCode,
                                  promoBadge: activeConfig.promoBadge,
                                  promoDescription:
                                    activeConfig.promoDescription,
                                  pricingModes: activeConfig.pricingModes,
                                  currency: activeConfig.currency,
                                  memory: activeConfig.memory,
                                  storage: activeConfig.storage,
                                }
                              : null,
                            initialConfigs: row.configs.map((c) => ({
                              configKey: c.configKey,
                              label: c.label,
                              memory: c.memory,
                              storage: c.storage,
                              options: c.options,
                              isDefault: c.isDefault,
                              estimatedMonthlyPrice:
                                c.estimatedMonthlyPrice ?? undefined,
                              originalMonthlyPrice:
                                c.originalMonthlyPrice ?? undefined,
                              setupFee: c.setupFee ?? undefined,
                              originalSetupFee:
                                c.originalSetupFee ?? undefined,
                              freeSetup: c.freeSetup,
                              hasPromo: c.hasPromo,
                              promoCode: c.promoCode,
                              promoBadge: c.promoBadge,
                              promoDescription: c.promoDescription,
                              pricingModes: c.pricingModes,
                              currency: c.currency,
                              datacenters: c.dcMap,
                              inStockCount: c.inStockCount,
                            })),
                          })
                        }
                        className="px-2.5 py-1 rounded-md bg-cyber-accent/15 hover:bg-cyber-accent/25 border border-cyber-accent/40 text-cyber-accent text-xs font-semibold flex items-center gap-1 transition-colors whitespace-nowrap"
                      >
                        <Bell className="w-3.5 h-3.5" />
                        <span>{t('quickMonitor.button')}</span>
                      </button>
                    </div>
                  </div>

                  {/* Datacenter Cells Row */}
                  <div className="mt-2 pt-2 border-t border-cyber-grid/40">
                    {row.listingState === 'announced' &&
                    dcCellsToRender.length === 0 ? (
                      <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-amber-300/90 bg-amber-500/10 border border-amber-500/25 rounded px-2.5 py-1.5">
                        <span>{t('announced.noStockFeedHint')}</span>
                        {row.configs[0] &&
                          (row.configs[0].memory || row.configs[0].storage) && (
                            <span className="font-mono text-[11px] text-cyber-muted">
                              {row.configs[0].label}
                            </span>
                          )}
                      </div>
                    ) : (
                      <div className="grid grid-cols-3 sm:grid-cols-4 md:grid-cols-6 lg:grid-cols-8 xl:grid-cols-10 gap-1.5">
                        {dcCellsToRender.map((dc) => {
                          const availInfo = getAvailabilityInfo(
                            dc.availability
                          );
                          return (
                            <div
                              key={dc.datacenter}
                              title={dc.tooltip}
                              className={`${availInfo.bg} ${availInfo.border} border rounded px-2 py-1 flex items-center justify-between gap-1 text-xs`}
                            >
                              <div className="font-semibold text-slate-200 text-[11px] truncate">
                                <DCLabel code={dc.datacenter} mode="compact" />
                              </div>
                              <div className="flex items-center gap-1 flex-shrink-0">
                                <span
                                  className={`${availInfo.color} text-[10px] font-semibold`}
                                >
                                  {availInfo.text}
                                </span>
                                {!activeConfig &&
                                  dc.inStockConfigsCount > 1 && (
                                    <span className="px-1 py-0.1 rounded bg-emerald-500/20 text-emerald-300 text-[9px] font-mono">
                                      {t('availability.multiConfigBadge', {
                                        count: dc.inStockConfigsCount,
                                      })}
                                    </span>
                                  )}
                              </div>
                            </div>
                          );
                        })}
                      </div>
                    )}
                  </div>
                </div>
              );
            })}
          </div>

          {/* 分页控件 */}
          {totalPages > 1 && (
            <div className="cyber-panel p-3 sm:p-4 mt-3 sm:mt-4 backdrop-blur-0 overflow-visible">
              <div className="flex flex-col sm:flex-row items-center justify-between gap-3 sm:gap-4">
                <div className="text-xs sm:text-sm text-cyber-muted">
                  {isMobile ? (
                    <>
                      {currentPage}/{totalPages}
                    </>
                  ) : (
                    <>
                      显示 {(currentPage - 1) * itemsPerPage + 1} -{' '}
                      {Math.min(currentPage * itemsPerPage, filteredData.length)}{' '}
                      / 共 {filteredData.length} 款机型
                    </>
                  )}
                </div>

                <div className="flex items-center gap-1.5 sm:gap-2">
                  {!isMobile && (
                    <Button
                      onClick={() => setCurrentPage(1)}
                      disabled={currentPage === 1}
                      variant="cyber"
                      size="sm"
                      className="px-3 text-xs"
                    >
                      首页
                    </Button>
                  )}

                  <Button
                    onClick={() =>
                      setCurrentPage((prev) => Math.max(1, prev - 1))
                    }
                    disabled={currentPage === 1}
                    variant="cyber"
                    size="sm"
                    className="px-2"
                  >
                    <ChevronLeft className="w-3 h-3 sm:w-4 sm:h-4" />
                  </Button>

                  <div className="flex items-center gap-1.5 sm:gap-2 px-2 sm:px-3 py-1 bg-cyber-grid/30 rounded text-xs sm:text-sm">
                    <span className="text-cyber-accent font-medium">
                      {currentPage}
                    </span>
                    <span className="text-cyber-muted">/</span>
                    <span className="text-cyber-muted">{totalPages}</span>
                  </div>

                  <Button
                    onClick={() =>
                      setCurrentPage((prev) => Math.min(totalPages, prev + 1))
                    }
                    disabled={currentPage === totalPages}
                    variant="cyber"
                    size="sm"
                    className="px-2"
                  >
                    <ChevronRight className="w-3 h-3 sm:w-4 sm:h-4" />
                  </Button>

                  {!isMobile && (
                    <Button
                      onClick={() => setCurrentPage(totalPages)}
                      disabled={currentPage === totalPages}
                      variant="cyber"
                      size="sm"
                      className="px-3 text-xs"
                    >
                      末页
                    </Button>
                  )}
                </div>
              </div>
            </div>
          )}
        </>
      ) : null}

      <QuickMonitorDialog
        open={Boolean(quickMonitorTarget)}
        onOpenChange={(open) => {
          if (!open) setQuickMonitorTarget(null);
        }}
        planCode={quickMonitorTarget?.planCode || ''}
        serverName={quickMonitorTarget?.serverName}
        listingState={quickMonitorTarget?.listingState}
        isPresale={quickMonitorTarget?.isPresale}
        initialDatacenters={quickMonitorTarget?.datacenters}
        initialConfig={quickMonitorTarget?.initialConfig}
        initialConfigs={quickMonitorTarget?.initialConfigs}
      />
    </div>
  );
};

export default OVHAvailabilityPage;
