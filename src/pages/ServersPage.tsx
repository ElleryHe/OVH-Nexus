import { useState, useEffect, useRef, useMemo, useCallback } from 'react';
import { useAPI, apiEvents } from '@/context/APIContext';
import { api } from '@/utils/apiClient';
import { toast } from 'sonner';
import { Card, CardContent } from '@/components/ui/card';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import {
  Cpu,
  HardDrive,
  Database,
  Wifi,
  Settings,
  ArrowRightLeft,
  Clock,
  Bell,
  Grid,
  List,
  Check,
  Sparkles,
  ShoppingCart,
  RefreshCw,
  SlidersHorizontal,
  X,
  Zap,
} from 'lucide-react';
import { useIsMobile } from '@/hooks/use-mobile';
import { useLocale } from '@/context/LocaleContext';
import { DCLabel } from '@/components/DCLabel';
import {
  QuickMonitorDialog,
  QuickMonitorInitialConfig,
} from '@/components/QuickMonitorDialog';
import { CatalogPromoBanner } from '@/components/CatalogPromoBanner';
import { OrderPromoDebugPanel } from '@/components/OrderPromoDebugPanel';
import {
  OVH_DATACENTERS,
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
  buildConfigKey,
  formatConfigLabel,
  getCurrencySymbol,
  resolvePricingModeDuration,
  synthesizePricingModes,
  resolvePricingModeEntry,
} from '@/utils/hardwareFormat';
import type {
  CatalogPromotionsResponse,
  ListingState,
  OVHAvailabilityRow,
  PlanConfigOption,
  PlanPricingMode,
  ServerOption,
  ServerPlan,
} from '@/types';

interface UnifiedCardConfig {
  configKey: string;
  memory: string;
  storage: string;
  label: string;
  options: string[];
  isDefault: boolean;
  estimatedMonthlyPrice: number | null;
  originalMonthlyPrice: number | null;
  setupFee: number | null;
  originalSetupFee: number | null;
  freeSetup: boolean;
  hasPromo: boolean;
  promoCode?: string;
  promoBadge?: string;
  promoDescription?: string;
  pricingModes: PlanPricingMode[];
  currency: string;
  dcStatuses: Record<string, string>;
  inStockDcs: string[];
}

type ListingFilterTab =
  | 'all'
  | 'listed'
  | 'promo'
  | 'announced'
  | 'presale'
  | 'new';

const isAvailStatus = (status?: string): boolean =>
  Boolean(status && status !== 'unavailable' && status !== 'unknown');

const resolveServerListingState = (server: ServerPlan): ListingState => {
  if (server.listingState === 'announced') return 'announced';
  if (
    server.listingState === 'presale' ||
    server.catalogSource === 'presale'
  ) {
    return 'presale';
  }
  if (server.listingState === 'listed') return 'listed';
  if (server.orderable === false) {
    if (!server.datacenters || server.datacenters.length === 0) {
      return 'announced';
    }
    return 'presale';
  }
  return 'listed';
};

const isLicenseOrSoftwareOption = (option: ServerOption): boolean => {
  const v = (option.value || '').toLowerCase();
  const l = (option.label || '').toLowerCase();
  return (
    v.includes('windows-server') ||
    v.includes('sql-server') ||
    v.includes('cpanel-license') ||
    v.includes('plesk-') ||
    v.includes('-license-') ||
    v.startsWith('os-') ||
    v.includes('control-panel') ||
    v.includes('panel') ||
    l.includes('license') ||
    l.includes('许可证') ||
    l.includes('许可') ||
    v.includes('security') ||
    v.includes('antivirus') ||
    v.includes('firewall')
  );
};

const classifyOptionGroup = (
  option: ServerOption
): 'memory' | 'storage' | 'bandwidth' | 'vrack' | 'other' => {
  const f = (option.family || '').toLowerCase();
  const v = (option.value || '').toLowerCase();
  const l = (option.label || '').toLowerCase();

  if (
    f.includes('memory') ||
    f.includes('ram') ||
    v.startsWith('ram-') ||
    l.includes('ddr')
  ) {
    return 'memory';
  }
  if (
    f.includes('storage') ||
    f.includes('disk') ||
    v.startsWith('softraid-') ||
    v.startsWith('hardraid-') ||
    v.startsWith('hybridsoftraid-') ||
    v.startsWith('noraid-') ||
    v.includes('nvme') ||
    v.includes('ssd') ||
    v.includes('hdd')
  ) {
    return 'storage';
  }
  if (f.includes('vrack') || v.includes('vrack') || l.includes('内网')) {
    return 'vrack';
  }
  if (
    f.includes('bandwidth') ||
    f.includes('traffic') ||
    v.includes('bandwidth') ||
    v.includes('traffic')
  ) {
    return 'bandwidth';
  }
  return 'other';
};

const ServersPage = () => {
  const isMobile = useIsMobile();
  const { locale, t } = useLocale();
  const catalogDict = useCatalogDictionary();
  const { isAuthenticated } = useAPI();

  const [showBatchMonitorDialog, setShowBatchMonitorDialog] = useState(false);
  const [listingFilterTab, setListingFilterTab] =
    useState<ListingFilterTab>('all');
  const [promotions, setPromotions] =
    useState<CatalogPromotionsResponse | null>(null);
  const [selectedPromoCode, setSelectedPromoCode] = useState<string | null>(
    null
  );
  const [recentSkuMap, setRecentSkuMap] = useState<Record<string, string>>({});

  const [quickMonitorTarget, setQuickMonitorTarget] = useState<{
    server: ServerPlan;
    initialConfig?: QuickMonitorInitialConfig | null;
    initialConfigs?: PlanConfigOption[];
    initialSelectedDcs?: string[];
  } | null>(null);

  const [pendingQueueTarget, setPendingQueueTarget] = useState<{
    server: ServerPlan;
    datacenters: string[];
    selectedConfig: UnifiedCardConfig | null;
    extraOptions: string[];
    pricingMode: string;
    duration: string;
    coupon: string;
  } | null>(null);
  const [queueAutoPay, setQueueAutoPay] = useState(false);
  const [queueWaiveRetractation, setQueueWaiveRetractation] = useState(false);

  const [servers, setServers] = useState<ServerPlan[]>([]);
  const [rawAvailItems, setRawAvailItems] = useState<OVHAvailabilityRow[]>([]);
  const [planConfigsCache, setPlanConfigsCache] = useState<
    Record<string, PlanConfigOption[]>
  >({});
  const [loadingPlanConfigs, setLoadingPlanConfigs] = useState<
    Record<string, boolean>
  >({});

  const [isLoading, setIsLoading] = useState(true);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [searchTerm, setSearchTerm] = useState('');
  const [selectedDatacenter, setSelectedDatacenter] = useState<string>('all');
  const [isCheckingAvailability, setIsCheckingAvailability] = useState(false);
  const [selectedServer, setSelectedServer] = useState<string | null>(null);
  const [availability, setAvailability] = useState<
    Record<string, Record<string, string>>
  >({});

  // Focused Configuration & Order Modal server
  const [configModalServer, setConfigModalServer] = useState<ServerPlan | null>(
    null
  );
  const [isQuickOrdering, setIsQuickOrdering] = useState(false);
  // Per-card "Show all inventory (including out-of-stock)" toggle: default false
  const [showAllInventoryMap, setShowAllInventoryMap] = useState<
    Record<string, boolean>
  >({});
  // Per-card selected configuration key: planCode -> configKey
  const [selectedConfigKeyMap, setSelectedConfigKeyMap] = useState<
    Record<string, string>
  >({});
  // Per-card selected billing / commitment pricingMode: planCode -> pricingMode
  const [selectedPricingModeMap, setSelectedPricingModeMap] = useState<
    Record<string, string>
  >({});
  // Per-card custom coupon code: planCode -> coupon
  const [selectedCouponMap, setSelectedCouponMap] = useState<
    Record<string, string>
  >({});
  // Per-card selected advanced addon options (bandwidth / vrack): planCode -> optionValue[]
  const [selectedAddonOptionsMap, setSelectedAddonOptionsMap] = useState<
    Record<string, string[]>
  >({});
  // Per-card selected datacenters: planCode -> dcCode[] (lowercase normalized)
  const [selectedDcsMap, setSelectedDcsMap] = useState<
    Record<string, string[]>
  >({});

  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  const hasLoadedFromCache = useRef(false);
  const [isActuallyFetching, setIsActuallyFetching] = useState(false);

  const [viewMode, setViewMode] = useState<'grid' | 'list'>(() => {
    if (isMobile) return 'grid';
    try {
      const saved = localStorage.getItem('ovh_view_mode');
      return saved === 'list' || saved === 'grid' ? saved : 'grid';
    } catch {
      return 'grid';
    }
  });

  useEffect(() => {
    if (isMobile && viewMode === 'list') {
      setViewMode('grid');
    }
  }, [isMobile, viewMode]);

  useEffect(() => {
    if (!isMobile) {
      try {
        localStorage.setItem('ovh_view_mode', viewMode);
      } catch {
        // ignore
      }
    }
  }, [viewMode, isMobile]);

  const [subscribedServers, setSubscribedServers] = useState<Set<string>>(() => {
    try {
      const cached = localStorage.getItem('ovh_subscribed_servers');
      if (cached) {
        return new Set(JSON.parse(cached));
      }
    } catch {
      // ignore
    }
    return new Set();
  });
  const subscribedServersRef = useRef<Set<string>>(subscribedServers);

  const formatServerSpec = useCallback(
    (value: string, type: string): string => {
      if (!value || value === 'N/A') return '暂无数据';
      const trimmed = value.trim();

      if (type === 'CPU') {
        if (
          trimmed.toLowerCase().includes('intel') ||
          trimmed.toLowerCase().includes('amd') ||
          trimmed.toLowerCase().includes('ryzen') ||
          trimmed.toLowerCase().includes('xeon') ||
          trimmed.toLowerCase().includes('epyc')
        ) {
          return trimmed;
        }
        const cpuNameMatch = trimmed.match(/cpu-([a-z0-9-]+)/i);
        if (cpuNameMatch) {
          const cpuName = cpuNameMatch[1]
            .replace(/-/g, ' ')
            .replace(/(\d+)c(\d+)t/i, '$1核$2线程')
            .replace(/(\d+)c/i, '$1核')
            .replace(/i(\d+)/i, 'Intel Core i$1');
          return cpuName.charAt(0).toUpperCase() + cpuName.slice(1);
        }
        return trimmed;
      }

      if (type === '内存') {
        if (trimmed.toLowerCase().startsWith('ram-')) {
          return formatMemoryCode(trimmed, locale) || trimmed;
        }
        const ramMatch = trimmed.match(/ram-(\d+)g/i);
        if (ramMatch) return `${ramMatch[1]} GB`;
        return trimmed;
      }

      if (type === '存储') {
        const low = trimmed.toLowerCase();
        if (
          low.startsWith('softraid-') ||
          low.startsWith('hardraid-') ||
          low.startsWith('hybridsoftraid-') ||
          low.startsWith('noraid-')
        ) {
          return formatStorageCode(trimmed, locale, false) || trimmed;
        }
        return trimmed;
      }

      if (type === '带宽') {
        const combinedTrafficMatch = trimmed.match(
          /traffic-(\d+)(tb|gb|mb)-(\d+)/i
        );
        if (combinedTrafficMatch) {
          return `${combinedTrafficMatch[3]} Mbps / ${
            combinedTrafficMatch[1]
          } ${combinedTrafficMatch[2].toUpperCase()}`;
        }
        if (trimmed.toLowerCase().includes('unlimited')) {
          return locale === 'zh-CN' ? '无限流量' : 'Unlimited';
        }
        const bandwidthMatch = trimmed.match(/bandwidth-(\d+)/i);
        if (bandwidthMatch) {
          const bwValue = parseInt(bandwidthMatch[1], 10);
          return bwValue >= 1000 ? `${bwValue / 1000} Gbps` : `${bwValue} Mbps`;
        }
        return trimmed;
      }

      if (type === '内网带宽') {
        const vrackBwMatch = trimmed.match(/vrack-bandwidth-(\d+)/i);
        if (vrackBwMatch) {
          const bwValue = parseInt(vrackBwMatch[1], 10);
          return bwValue >= 1000
            ? `${bwValue / 1000} Gbps vRack`
            : `${bwValue} Mbps vRack`;
        }
        return trimmed;
      }

      return trimmed;
    },
    [locale]
  );

  const fetchPlanConfigsForCard = useCallback(
    async (planCode: string) => {
      if (planConfigsCache[planCode] || loadingPlanConfigs[planCode]) return;
      setLoadingPlanConfigs((prev) => ({ ...prev, [planCode]: true }));
      try {
        const res = await api.getPlanConfigs(planCode);
        if (Array.isArray(res.data?.configs) && res.data.configs.length > 0) {
          setPlanConfigsCache((prev) => ({
            ...prev,
            [planCode]: res.data.configs,
          }));
        }
      } catch {
        // Fallback to synthesized configs from rawAvailItems / server options
      } finally {
        setLoadingPlanConfigs((prev) => ({ ...prev, [planCode]: false }));
      }
    },
    [planConfigsCache, loadingPlanConfigs]
  );

  const fetchServers = async (forceRefresh = false, overrideAuth?: boolean) => {
    if (isActuallyFetching) return;
    const authState =
      overrideAuth !== undefined ? overrideAuth : isAuthenticated;

    if (forceRefresh && servers.length > 0) {
      setIsRefreshing(true);
    } else {
      setIsLoading(true);
    }
    setIsActuallyFetching(true);

    try {
      const [response, availRes] = await Promise.allSettled([
        api.get('/servers', {
          params: {
            showApiServers: authState,
            forceRefresh,
          },
        }),
        api.get('/availability'),
      ]);

      if (availRes.status === 'fulfilled' && Array.isArray(availRes.value?.data)) {
        setRawAvailItems(availRes.value.data);
      }

      if (response.status !== 'fulfilled') {
        throw response.reason;
      }

      const resData = response.value.data;
      if (resData?.cacheInfo?.usingExpiredCache) {
        const ageMinutes = resData.cacheInfo.cacheAgeMinutes;
        const ageHours = Math.floor(ageMinutes / 60);
        const ageText = ageHours > 0 ? `${ageHours}小时` : `${ageMinutes}分钟`;
        toast.warning(
          `⚠️ 正在使用过期缓存数据（${ageText}前），OVH API 暂时无法访问`,
          { duration: 5000 }
        );
      }

      let serversList: ServerPlan[] = [];
      if (Array.isArray(resData)) {
        serversList = resData;
      } else if (Array.isArray(resData?.servers)) {
        serversList = resData.servers;
      }

      const formattedServers: ServerPlan[] = serversList.map((server) => ({
        ...server,
        planCode: server.planCode || '未知',
        name: server.name || server.planCode || '未命名服务器',
        description: server.description || '',
        cpu: server.cpu || 'N/A',
        memory: server.memory || 'N/A',
        storage: server.storage || 'N/A',
        bandwidth: server.bandwidth || 'N/A',
        vrackBandwidth: server.vrackBandwidth || 'N/A',
        defaultOptions: Array.isArray(server.defaultOptions)
          ? server.defaultOptions
          : [],
        availableOptions: Array.isArray(server.availableOptions)
          ? server.availableOptions
          : [],
        datacenters: Array.isArray(server.datacenters) ? server.datacenters : [],
      }));

      const seededAvail: Record<string, Record<string, string>> = {};
      formattedServers.forEach((srv) => {
        if (Array.isArray(srv.datacenters) && srv.datacenters.length > 0) {
          const dcMap: Record<string, string> = {};
          srv.datacenters.forEach((d) => {
            const norm = normalizeDatacenterCode(d.datacenter);
            if (norm && d.availability) {
              dcMap[norm] = d.availability;
            }
          });
          if (Object.keys(dcMap).length > 0) {
            seededAvail[srv.planCode] = dcMap;
          }
        }
      });

      setAvailability((prev) => ({ ...seededAvail, ...prev }));
      setServers(formattedServers);
      setLastUpdated(new Date());
      hasLoadedFromCache.current = true;
    } catch (error: any) {
      let errorMessage = '获取服务器列表失败';
      if (error?.code === 'ECONNABORTED' || error?.message?.includes('timeout')) {
        errorMessage = '请求超时，请点击"刷新"按钮重试';
      } else if (error?.message) {
        errorMessage = `获取服务器列表失败: ${error.message}`;
      }
      toast.error(errorMessage);
    } finally {
      setIsLoading(false);
      setIsRefreshing(false);
      setIsActuallyFetching(false);
    }
  };

  const loadSubscribedServers = async (): Promise<Set<string>> => {
    if (!isAuthenticated) return subscribedServersRef.current;
    try {
      const response = await api.get('/monitor/subscriptions');
      const subscriptions = Array.isArray(response.data) ? response.data : [];
      const planCodes = new Set<string>(
        subscriptions.map((sub: any) => sub.planCode as string)
      );
      subscribedServersRef.current = planCodes;
      setSubscribedServers(planCodes);
      try {
        localStorage.setItem(
          'ovh_subscribed_servers',
          JSON.stringify(Array.from(planCodes))
        );
      } catch {
        // ignore
      }
      return planCodes;
    } catch {
      return subscribedServersRef.current;
    }
  };

  const fetchPromotions = useCallback(async (forceRefresh = false) => {
    try {
      const res = await api.get('/catalog/promotions', {
        params: forceRefresh ? { refresh: '1' } : undefined,
      });
      if (res.data && Array.isArray(res.data.campaigns)) {
        setPromotions(res.data as CatalogPromotionsResponse);
      }
    } catch {
      // non-fatal: banner simply hides if promotions endpoint is unavailable
    }
  }, []);

  useEffect(() => {
    const loadInitialData = async () => {
      if (isAuthenticated) {
        await loadSubscribedServers();
      }
      api
        .get('/sku/changes?limit=50')
        .then((res) => {
          if (res.data?.status?.recentFirstSeen) {
            setRecentSkuMap(res.data.status.recentFirstSeen);
          }
        })
        .catch(() => {});
      fetchPromotions(false);
      await fetchServers(false);
    };

    loadInitialData();

    const unsubscribe = apiEvents.onAuthChanged(async (newAuthState) => {
      if (newAuthState) {
        await loadSubscribedServers();
      }
      fetchPromotions(true);
      await fetchServers(true, newAuthState);
    });

    return () => {
      unsubscribe();
    };
  }, [fetchPromotions]);

  // Build unified configuration list for a server card (merging default config + available configs + live availability)
  const buildUnifiedConfigsForServer = useCallback(
    (server: ServerPlan): UnifiedCardConfig[] => {
      const planMeta = resolvePlanDisplay(
        server.planCode,
        '',
        locale,
        catalogDict
      );
      const baseCurrency = server.currency || planMeta.currency || 'EUR';
      const basePrice =
        typeof server.startingPrice === 'number' && server.startingPrice > 0
          ? server.startingPrice
          : typeof server.monthlyPrice === 'number' && server.monthlyPrice > 0
          ? server.monthlyPrice
          : planMeta.basePrice ?? null;
      const baseOrigMonthly =
        typeof server.originalMonthlyPrice === 'number' &&
        server.originalMonthlyPrice > 0
          ? server.originalMonthlyPrice
          : typeof server.originalPrice === 'number' && server.originalPrice > 0
          ? server.originalPrice
          : planMeta.originalMonthlyPrice ?? null;
      const baseSetupFee =
        typeof server.setupFee === 'number'
          ? server.setupFee
          : typeof planMeta.setupFee === 'number'
          ? planMeta.setupFee
          : null;
      const baseOrigSetupFee =
        typeof server.originalSetupFee === 'number'
          ? server.originalSetupFee
          : typeof planMeta.originalSetupFee === 'number'
          ? planMeta.originalSetupFee
          : null;
      const baseFreeSetupPromo =
        baseSetupFee === 0 && (baseOrigSetupFee ?? 0) > 0;
      const baseFreeSetup = Boolean(
        server.freeSetup ||
          planMeta.freeSetup ||
          baseSetupFee === 0 ||
          baseFreeSetupPromo
      );
      const baseHasPromo = Boolean(
        server.hasPromo ||
          planMeta.hasPromo ||
          (server.pricingModes || []).some((m) => m.hasPromo) ||
          (planMeta.pricingModes || []).some((m) => m.hasPromo) ||
          baseFreeSetupPromo ||
          (baseOrigMonthly && basePrice && baseOrigMonthly > basePrice + 0.01)
      );
      const basePricingModes =
        Array.isArray(server.pricingModes) && server.pricingModes.length > 0
          ? server.pricingModes
          : Array.isArray(planMeta.pricingModes) &&
            planMeta.pricingModes.length > 0
          ? planMeta.pricingModes
          : undefined;

      const buildAdjustedModes = (
        rawModes: PlanPricingMode[] | undefined,
        estMonthly: number | null,
        origMonthly: number | null,
        setup: number | null,
        origSetup: number | null,
        freeSetupFlag: boolean,
        hasPromoFlag: boolean,
        promoCode?: string,
        promoBadge?: string,
        promoDescription?: string
      ): PlanPricingMode[] => {
        if (Array.isArray(rawModes) && rawModes.length > 0) {
          return rawModes;
        }
        if (Array.isArray(basePricingModes) && basePricingModes.length > 0) {
          const monthlyDelta =
            estMonthly !== null && basePrice !== null
              ? Math.max(0, Number((estMonthly - basePrice).toFixed(2)))
              : 0;
          return basePricingModes.map((m) => {
            const interval = m.intervalMonths > 0 ? m.intervalMonths : 1;
            const periodDelta = Number((monthlyDelta * interval).toFixed(2));
            return {
              ...m,
              periodPrice: Number((m.periodPrice + periodDelta).toFixed(2)),
              originalPeriodPrice: Number(
                (m.originalPeriodPrice + periodDelta).toFixed(2)
              ),
              monthlyEquivalentPrice: Number(
                (m.monthlyEquivalentPrice + monthlyDelta).toFixed(2)
              ),
              originalMonthlyPrice: Number(
                (m.originalMonthlyPrice + monthlyDelta).toFixed(2)
              ),
            };
          });
        }
        return synthesizePricingModes({
          estimatedMonthlyPrice: estMonthly,
          originalMonthlyPrice: origMonthly,
          setupFee: setup,
          originalSetupFee: origSetup,
          freeSetup: freeSetupFlag,
          hasPromo: hasPromoFlag,
          promoCode,
          promoBadge,
          promoDescription,
        });
      };

      const serverDcMap: Record<string, string> = {
        ...(availability[server.planCode] || {}),
      };
      (server.datacenters || []).forEach((d) => {
        const norm = normalizeDatacenterCode(d.datacenter);
        if (norm && !serverDcMap[norm]) {
          serverDcMap[norm] = d.availability;
        }
      });

      const configMap = new Map<string, UnifiedCardConfig>();

      // Default memory & storage codes from server.defaultOptions or server.memory/storage
      const hwDefaultOpts = (server.defaultOptions || []).filter(
        (o) => !isLicenseOrSoftwareOption(o)
      );
      const defMemOpt = hwDefaultOpts.find(
        (o) => classifyOptionGroup(o) === 'memory'
      );
      const defStoOpt = hwDefaultOpts.find(
        (o) => classifyOptionGroup(o) === 'storage'
      );
      const defMemCode =
        defMemOpt?.value ||
        (server.memory !== 'N/A' ? server.memory : '') ||
        '';
      const defStoCode =
        defStoOpt?.value ||
        (server.storage !== 'N/A' ? server.storage : '') ||
        '';
      const defaultKey =
        server.defaultConfigKey || buildConfigKey(defMemCode, defStoCode);

      // 1. From GET /api/catalog/plans/{planCode}/configs if loaded
      const apiConfigs = planConfigsCache[server.planCode];
      if (Array.isArray(apiConfigs) && apiConfigs.length > 0) {
        apiConfigs.forEach((c) => {
          const mem = String(c.memory || '').trim();
          const sto = String(c.storage || '').trim();
          const key =
            String(c.configKey || '').trim() || buildConfigKey(mem, sto);
          const dcStatuses: Record<string, string> = {};
          if (Array.isArray(c.datacenters)) {
            c.datacenters.forEach((d) => {
              const norm = normalizeDatacenterCode(d.datacenter);
              if (norm) dcStatuses[norm] = d.availability;
            });
          } else if (c.datacenters && typeof c.datacenters === 'object') {
            Object.entries(c.datacenters).forEach(([dc, st]) => {
              const norm = normalizeDatacenterCode(dc);
              if (norm && typeof st === 'string') dcStatuses[norm] = st;
            });
          }
          const inStockDcs = Object.entries(dcStatuses)
            .filter(([_, st]) => isAvailStatus(st))
            .map(([dc]) => dc);

          const priceCalc = resolveConfigPrice(planMeta, {
            memory: mem,
            storage: sto,
          });
          const estPrice =
            typeof c.estimatedMonthlyPrice === 'number' &&
            c.estimatedMonthlyPrice > 0
              ? c.estimatedMonthlyPrice
              : typeof c.price === 'number' && c.price > 0
              ? c.price
              : priceCalc.totalPrice ?? basePrice;
          const isDefCfg = Boolean(c.isDefault || key === defaultKey);
          const origMonthly =
            typeof c.originalMonthlyPrice === 'number' &&
            c.originalMonthlyPrice > 0
              ? c.originalMonthlyPrice
              : typeof c.originalPrice === 'number' && c.originalPrice > 0
              ? c.originalPrice
              : isDefCfg || apiConfigs.length === 1
              ? baseOrigMonthly
              : null;
          const setupFee =
            typeof c.setupFee === 'number' ? c.setupFee : baseSetupFee;
          const origSetupFee =
            typeof c.originalSetupFee === 'number'
              ? c.originalSetupFee
              : baseOrigSetupFee;
          const freeSetupPromo =
            setupFee === 0 && (origSetupFee ?? 0) > 0;
          const freeSetup = Boolean(
            c.freeSetup || setupFee === 0 || freeSetupPromo
          );
          const hasPromo = Boolean(
            c.hasPromo ||
              (c.pricingModes || []).some((m) => m.hasPromo) ||
              freeSetupPromo ||
              (origMonthly && estPrice && origMonthly > estPrice + 0.01)
          );
          const promoCode = c.promoCode || server.promoCode || planMeta.promoCode;
          const promoBadge =
            c.promoBadge || server.promoBadge || planMeta.promoBadge;
          const promoDescription =
            c.promoDescription ||
            server.promoDescription ||
            planMeta.promoDescription;

          configMap.set(key, {
            configKey: key,
            memory: mem,
            storage: sto,
            label:
              String(c.label || '').trim() ||
              formatConfigLabel(mem, sto, locale),
            options:
              Array.isArray(c.options) && c.options.length > 0
                ? c.options
                : [mem, sto].filter((x) => x && x !== 'N/A'),
            isDefault: Boolean(c.isDefault || key === defaultKey),
            estimatedMonthlyPrice: estPrice,
            originalMonthlyPrice: origMonthly,
            setupFee,
            originalSetupFee: origSetupFee,
            freeSetup,
            hasPromo,
            promoCode,
            promoBadge,
            promoDescription,
            pricingModes: buildAdjustedModes(
              c.pricingModes,
              estPrice,
              origMonthly,
              setupFee,
              origSetupFee,
              freeSetup,
              hasPromo,
              promoCode,
              promoBadge,
              promoDescription
            ),
            currency: c.currency || baseCurrency,
            dcStatuses,
            inStockDcs,
          });
        });
      }

      // 2. Merge from rawAvailItems matching server.planCode
      const matchingAvailRows = rawAvailItems.filter(
        (r) =>
          r &&
          typeof r.planCode === 'string' &&
          r.planCode.toLowerCase() === server.planCode.toLowerCase()
      );
      matchingAvailRows.forEach((row) => {
        const mem = String(row.memory || '').trim();
        const sto = String(row.storage || '').trim();
        const key = row.configKey || buildConfigKey(mem, sto);
        const dcStatuses: Record<string, string> = {};
        (row.datacenters || []).forEach((d) => {
          const norm = normalizeDatacenterCode(d.datacenter);
          if (norm) dcStatuses[norm] = d.availability;
        });
        const inStockDcs = Object.entries(dcStatuses)
          .filter(([_, st]) => isAvailStatus(st))
          .map(([dc]) => dc);

        const priceCalc = resolveConfigPrice(planMeta, {
          memory: mem,
          storage: sto,
          systemStorage: row.systemStorage,
          gpu: row.gpu,
        });
        const estPrice =
          typeof row.estimatedMonthlyPrice === 'number' &&
          row.estimatedMonthlyPrice > 0
            ? row.estimatedMonthlyPrice
            : typeof row.price === 'number' && row.price > 0
            ? row.price
            : priceCalc.totalPrice ?? basePrice;
        const isDefRow = Boolean(row.isDefault || key === defaultKey);
        const origMonthly =
          typeof row.originalMonthlyPrice === 'number' &&
          row.originalMonthlyPrice > 0
            ? row.originalMonthlyPrice
            : typeof row.originalPrice === 'number' && row.originalPrice > 0
            ? row.originalPrice
            : isDefRow || matchingAvailRows.length === 1
            ? baseOrigMonthly
            : null;
        const setupFee =
          typeof row.setupFee === 'number' ? row.setupFee : baseSetupFee;
        const origSetupFee =
          typeof row.originalSetupFee === 'number'
            ? row.originalSetupFee
            : baseOrigSetupFee;
        const freeSetupPromo =
          setupFee === 0 && (origSetupFee ?? 0) > 0;
        const freeSetup = Boolean(
          row.freeSetup || setupFee === 0 || freeSetupPromo
        );
        const hasPromo = Boolean(
          row.hasPromo ||
            (row.pricingModes || []).some((m) => m.hasPromo) ||
            freeSetupPromo ||
            (origMonthly && estPrice && origMonthly > estPrice + 0.01)
        );
        const promoCode =
          row.promoCode || server.promoCode || planMeta.promoCode;
        const promoBadge =
          row.promoBadge || server.promoBadge || planMeta.promoBadge;
        const promoDescription =
          row.promoDescription ||
          server.promoDescription ||
          planMeta.promoDescription;

        const existing = configMap.get(key);
        if (!existing) {
          configMap.set(key, {
            configKey: key,
            memory: mem,
            storage: sto,
            label: formatConfigLabel(mem, sto, locale),
            options:
              Array.isArray(row.options) && row.options.length > 0
                ? row.options
                : [mem, sto].filter((x) => x && x !== 'N/A'),
            isDefault: Boolean(row.isDefault || key === defaultKey),
            estimatedMonthlyPrice: estPrice,
            originalMonthlyPrice: origMonthly,
            setupFee,
            originalSetupFee: origSetupFee,
            freeSetup,
            hasPromo,
            promoCode,
            promoBadge,
            promoDescription,
            pricingModes: buildAdjustedModes(
              row.pricingModes,
              estPrice,
              origMonthly,
              setupFee,
              origSetupFee,
              freeSetup,
              hasPromo,
              promoCode,
              promoBadge,
              promoDescription
            ),
            currency: row.currency || baseCurrency,
            dcStatuses,
            inStockDcs,
          });
        } else {
          Object.entries(dcStatuses).forEach(([dc, st]) => {
            if (!existing.dcStatuses[dc] || isAvailStatus(st)) {
              existing.dcStatuses[dc] = st;
            }
          });
          existing.inStockDcs = Object.entries(existing.dcStatuses)
            .filter(([_, st]) => isAvailStatus(st))
            .map(([dc]) => dc);
          if (
            (!existing.pricingModes || existing.pricingModes.length === 0) &&
            Array.isArray(row.pricingModes) &&
            row.pricingModes.length > 0
          ) {
            existing.pricingModes = row.pricingModes;
          }
        }
      });

      // 3. Ensure the plan's default configuration is always present in the unified list
      if (defaultKey && !configMap.has(defaultKey)) {
        const priceCalc = resolveConfigPrice(planMeta, {
          memory: defMemCode,
          storage: defStoCode,
        });
        const inStockDcs = Object.entries(serverDcMap)
          .filter(([_, st]) => isAvailStatus(st))
          .map(([dc]) => dc);
        const estPrice = priceCalc.totalPrice ?? basePrice;
        const promoCode = server.promoCode || planMeta.promoCode;
        const promoBadge = server.promoBadge || planMeta.promoBadge;
        const promoDescription =
          server.promoDescription || planMeta.promoDescription;

        configMap.set(defaultKey, {
          configKey: defaultKey,
          memory: defMemCode,
          storage: defStoCode,
          label: formatConfigLabel(defMemCode, defStoCode, locale),
          options: [defMemCode, defStoCode].filter(
            (x) => x && x !== 'N/A' && x !== '暂无数据'
          ),
          isDefault: true,
          estimatedMonthlyPrice: estPrice,
          originalMonthlyPrice: baseOrigMonthly,
          setupFee: baseSetupFee,
          originalSetupFee: baseOrigSetupFee,
          freeSetup: baseFreeSetup,
          hasPromo: baseHasPromo,
          promoCode,
          promoBadge,
          promoDescription,
          pricingModes: buildAdjustedModes(
            basePricingModes,
            estPrice,
            baseOrigMonthly,
            baseSetupFee,
            baseOrigSetupFee,
            baseFreeSetup,
            baseHasPromo,
            promoCode,
            promoBadge,
            promoDescription
          ),
          currency: baseCurrency,
          dcStatuses: serverDcMap,
          inStockDcs,
        });
      }

      // If no config was marked isDefault yet, mark the first one
      const list = Array.from(configMap.values());
      if (list.length > 0 && !list.some((c) => c.isDefault)) {
        list[0].isDefault = true;
      }

      return list.sort((a, b) => {
        if (a.isDefault && !b.isDefault) return -1;
        if (!a.isDefault && b.isDefault) return 1;
        if (b.inStockDcs.length !== a.inStockDcs.length) {
          return b.inStockDcs.length - a.inStockDcs.length;
        }
        return (
          (a.estimatedMonthlyPrice ?? 999999) -
          (b.estimatedMonthlyPrice ?? 999999)
        );
      });
    },
    [locale, catalogDict, availability, planConfigsCache, rawAvailItems]
  );

  // Extract advanced options (Bandwidth, vRack, etc.) for a server
  const getAdvancedOptionsForServer = useCallback(
    (server: ServerPlan): { bandwidth: ServerOption[]; vrack: ServerOption[] } => {
      const bandwidth: ServerOption[] = [];
      const vrack: ServerOption[] = [];
      const seen = new Set<string>();

      (server.availableOptions || []).forEach((opt) => {
        if (isLicenseOrSoftwareOption(opt) || seen.has(opt.value)) return;
        const grp = classifyOptionGroup(opt);
        if (grp === 'bandwidth') {
          seen.add(opt.value);
          bandwidth.push(opt);
        } else if (grp === 'vrack') {
          seen.add(opt.value);
          vrack.push(opt);
        }
      });

      return { bandwidth, vrack };
    },
    []
  );

  const openConfigModal = (server: ServerPlan) => {
    setConfigModalServer(server);
    fetchPlanConfigsForCard(server.planCode);
  };

  useEffect(() => {
    if (!configModalServer) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !quickMonitorTarget && !pendingQueueTarget) {
        setConfigModalServer(null);
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [configModalServer, quickMonitorTarget, pendingQueueTarget]);

  const toggleDcForServer = (planCode: string, dcCode: string) => {
    const norm = normalizeDatacenterCode(dcCode);
    if (!norm) return;
    setSelectedDcsMap((prev) => {
      const current = prev[planCode] || [];
      const next = current.includes(norm)
        ? current.filter((d) => d !== norm)
        : [...current, norm];
      return { ...prev, [planCode]: next };
    });
  };

  const toggleAdvancedOptionForServer = (
    server: ServerPlan,
    optionValue: string,
    group: 'bandwidth' | 'vrack'
  ) => {
    const adv = getAdvancedOptionsForServer(server);
    const groupValues = new Set(adv[group].map((o) => o.value));
    setSelectedAddonOptionsMap((prev) => {
      const cur = prev[server.planCode] || [];
      const already = cur.includes(optionValue);
      const withoutGroup = cur.filter((v) => !groupValues.has(v));
      return {
        ...prev,
        [server.planCode]: already ? withoutGroup : [...withoutGroup, optionValue],
      };
    });
  };

  const checkAvailability = async (
    planCode: string,
    activeCfg: UnifiedCardConfig | null
  ) => {
    if (!isAuthenticated) {
      toast.error('请先配置 API 设置');
      return;
    }

    setIsCheckingAvailability(true);
    setSelectedServer(planCode);
    toast.info(`正在检测 ${planCode} 的实时可用性...`, { duration: 2000 });

    try {
      const extraOpts = selectedAddonOptionsMap[planCode] || [];
      const cfgOpts = activeCfg?.options || [];
      const response = await api.post(
        `/availability/${encodeURIComponent(planCode)}`,
        { options: [...cfgOpts, ...extraOpts] },
        { timeout: 120000 }
      );

      const normalizedData: Record<string, string> = {};
      if (response.data && typeof response.data === 'object') {
        Object.entries(response.data).forEach(([dc, status]) => {
          const normalizedDc = normalizeDatacenterCode(dc) || dc;
          normalizedData[normalizedDc] = status as string;
        });
      }

      setAvailability((prev) => ({
        ...prev,
        [planCode]: normalizedData,
      }));
      toast.success(`已更新 ${planCode} 可用性信息`);
    } catch (error: any) {
      toast.error(
        `获取 ${planCode} 可用性失败: ${error?.message || '请求错误'}`
      );
    } finally {
      setIsCheckingAvailability(false);
      setSelectedServer(null);
    }
  };

  const convertDisplayDcToApiDc = (displayDc: string): string => {
    const dcMap: Record<string, string> = {
      mum: 'ynm',
    };
    return dcMap[displayDc.toLowerCase()] || displayDc.toLowerCase();
  };

  const sanitizeCouponForSubmit = useCallback(
    (planCode: string, rawCoupon?: string): string => {
      const trimmed = (rawCoupon || '').trim();
      if (!trimmed) return '';
      const matched = promotions?.autoAppliedCodes?.find(
        (c) => c.toLowerCase() === trimmed.toLowerCase()
      );
      if (matched) {
        setSelectedCouponMap((prev) =>
          prev[planCode] ? { ...prev, [planCode]: '' } : prev
        );
        toast.info(t('promoDebug.autoCouponStripped', { code: matched }));
        return '';
      }
      return trimmed;
    },
    [promotions, t]
  );

  const handleOpenAddToQueue = (
    server: ServerPlan,
    activeConfig: UnifiedCardConfig | null,
    inStockDcs: string[]
  ) => {
    if (!isAuthenticated) {
      toast.error('请先配置 API 设置');
      return;
    }
    const userSelectedDcs = selectedDcsMap[server.planCode] || [];
    const effectiveDcs =
      userSelectedDcs.length > 0 ? userSelectedDcs : inStockDcs;

    if (effectiveDcs.length === 0) {
      setConfigModalServer(server);
      setShowAllInventoryMap((prev) => ({ ...prev, [server.planCode]: true }));
      fetchPlanConfigsForCard(server.planCode);
      toast.info('请在配置弹窗中选择目标机房后再加入抢购队列');
      return;
    }

    const mode = selectedPricingModeMap[server.planCode] || 'default';
    const modeEntry = resolvePricingModeEntry(
      activeConfig?.pricingModes || server.pricingModes,
      mode
    );
    const duration =
      modeEntry?.duration ||
      resolvePricingModeDuration(
        mode,
        activeConfig?.pricingModes || server.pricingModes
      );
    const coupon = sanitizeCouponForSubmit(
      server.planCode,
      selectedCouponMap[server.planCode]
    );

    setQueueAutoPay(false);
    setQueueWaiveRetractation(false);
    setPendingQueueTarget({
      server,
      datacenters: effectiveDcs,
      selectedConfig: activeConfig,
      extraOptions: selectedAddonOptionsMap[server.planCode] || [],
      pricingMode: mode,
      duration,
      coupon,
    });
  };

  const quickOrderFromModal = async (server: ServerPlan) => {
    if (!isAuthenticated) {
      toast.error('请先配置 API 设置');
      return;
    }

    const unifiedConfigs = buildUnifiedConfigsForServer(server);
    const defaultCfg =
      unifiedConfigs.find((c) => c.isDefault) || unifiedConfigs[0] || null;
    const activeCfgKey =
      selectedConfigKeyMap[server.planCode] || defaultCfg?.configKey || '';
    const activeCfg =
      unifiedConfigs.find((c) => c.configKey === activeCfgKey) || defaultCfg;

    const allInStockDcSet = new Set<string>();
    unifiedConfigs.forEach((c) => {
      c.inStockDcs.forEach((dc) => allInStockDcSet.add(dc));
    });
    (server.datacenters || []).forEach((d) => {
      if (isAvailStatus(d.availability)) {
        const norm = normalizeDatacenterCode(d.datacenter);
        if (norm) allInStockDcSet.add(norm);
      }
    });
    const allInStockDcs = Array.from(allInStockDcSet);

    const cfgInStockDcs =
      activeCfg && activeCfg.inStockDcs.length > 0
        ? activeCfg.inStockDcs
        : allInStockDcs;
    const userSelectedDcs = selectedDcsMap[server.planCode] || [];
    const selectedInStock = userSelectedDcs.filter((d) =>
      cfgInStockDcs.includes(d)
    );
    const targetDc =
      selectedInStock[0] || cfgInStockDcs[0] || userSelectedDcs[0] || '';

    if (!targetDc) {
      toast.error('请先选择至少一个可用机房');
      return;
    }

    const configOpts =
      activeCfg && activeCfg.options.length > 0
        ? activeCfg.options
        : (server.defaultOptions || [])
            .filter((o) => !isLicenseOrSoftwareOption(o))
            .map((o) => o.value);
    const extraOptions = selectedAddonOptionsMap[server.planCode] || [];
    const mergedOptions = Array.from(new Set([...configOpts, ...extraOptions]));
    const apiDc = convertDisplayDcToApiDc(targetDc);

    const mode = selectedPricingModeMap[server.planCode] || 'default';
    const modeEntry = resolvePricingModeEntry(
      activeCfg?.pricingModes || server.pricingModes,
      mode
    );
    const duration =
      modeEntry?.duration ||
      resolvePricingModeDuration(
        mode,
        activeCfg?.pricingModes || server.pricingModes
      );
    const coupon = sanitizeCouponForSubmit(
      server.planCode,
      selectedCouponMap[server.planCode]
    );

    setIsQuickOrdering(true);
    try {
      const res = await api.post('/orders/quick-order', {
        planCode: server.planCode,
        datacenter: apiDc,
        options: mergedOptions,
        configKey: activeCfg?.configKey || undefined,
        configLabel: activeCfg?.label || undefined,
        currency: activeCfg?.currency || server.currency || undefined,
        pricingMode: mode,
        duration,
        coupon: coupon || undefined,
      });
      const cfgLabel = activeCfg?.label ? ` (${activeCfg.label})` : '';
      toast.success(
        res.data?.message ||
          `已提交 ${server.planCode}${cfgLabel} [${targetDc.toUpperCase()}] 快捷下单任务`
      );
    } catch (err: any) {
      toast.error(
        err?.response?.data?.message ||
          err?.response?.data?.error ||
          '快捷下单失败'
      );
    } finally {
      setIsQuickOrdering(false);
    }
  };

  const confirmAddToQueue = async () => {
    if (!pendingQueueTarget) return;
    const {
      server,
      datacenters,
      selectedConfig,
      extraOptions,
      pricingMode,
      duration,
      coupon,
    } = pendingQueueTarget;

    try {
      const configOpts =
        selectedConfig && selectedConfig.options.length > 0
          ? selectedConfig.options
          : (server.defaultOptions || [])
              .filter((o) => !isLicenseOrSoftwareOption(o))
              .map((o) => o.value);
      const mergedOptions = Array.from(new Set([...configOpts, ...extraOptions]));
      const apiDatacenters = datacenters.map((dc) => convertDisplayDcToApiDc(dc));
      const cleanCoupon = sanitizeCouponForSubmit(server.planCode, coupon);

      await api.post('/queue', {
        planCode: server.planCode,
        datacenters: apiDatacenters,
        options: mergedOptions,
        configKey: selectedConfig?.configKey || undefined,
        configLabel: selectedConfig?.label || undefined,
        currency: selectedConfig?.currency || server.currency || undefined,
        pricingMode: pricingMode || 'default',
        duration: duration || 'P1M',
        coupon: cleanCoupon || undefined,
        auto_pay: queueAutoPay,
        waive_retractation_period: queueWaiveRetractation,
      });

      setPendingQueueTarget(null);
      const cfgLabel = selectedConfig?.label
        ? ` (${selectedConfig.label})`
        : '';
      toast.success(
        `已将 ${server.planCode}${cfgLabel} 添加到 ${datacenters.length} 个机房的抢购队列`
      );
    } catch {
      toast.error('添加到抢购队列失败');
    }
  };

  const confirmBatchAddAllServersToMonitor = async () => {
    setShowBatchMonitorDialog(false);
    try {
      const response = await api.post('/monitor/subscriptions/batch-add-all', {
        notifyAvailable: true,
        notifyUnavailable: true,
      });
      const result = response.data;
      await loadSubscribedServers();
      setServers((prev) => [...prev]);
      toast.success(
        `✅ 批量添加完成：新增 ${result.added} 款，跳过 ${result.skipped || 0} 款`
      );
    } catch (error: any) {
      toast.error(error?.response?.data?.message || '批量添加监控失败');
    }
  };

  const isServerPromo = useCallback((server: ServerPlan): boolean => {
    return Boolean(
      server.hasPromo || (server.pricingModes || []).some((m) => m.hasPromo)
    );
  }, []);

  // Tab counts
  const tabCounts = useMemo(() => {
    const counts = {
      all: servers.length,
      listed: 0,
      promo: 0,
      announced: 0,
      presale: 0,
      new: 0,
    };
    servers.forEach((srv) => {
      const st = resolveServerListingState(srv);
      if (st === 'listed') counts.listed += 1;
      else if (st === 'announced') counts.announced += 1;
      else if (st === 'presale') counts.presale += 1;
      if (isServerPromo(srv)) counts.promo += 1;
      const isNew = Boolean(
        st === 'listed' &&
          (recentSkuMap[srv.planCode] ||
            (srv.listedAt &&
              Date.now() - Date.parse(srv.listedAt) <= 7 * 24 * 3600 * 1000))
      );
      if (isNew) counts.new += 1;
    });
    return counts;
  }, [servers, recentSkuMap, isServerPromo]);

  const filteredServers = useMemo(() => {
    let filtered = [...servers];

    if (listingFilterTab === 'listed') {
      filtered = filtered.filter(
        (s) => resolveServerListingState(s) === 'listed'
      );
    } else if (listingFilterTab === 'promo') {
      filtered = filtered.filter((s) => isServerPromo(s));
    } else if (listingFilterTab === 'announced') {
      filtered = filtered.filter(
        (s) => resolveServerListingState(s) === 'announced'
      );
    } else if (listingFilterTab === 'presale') {
      filtered = filtered.filter(
        (s) => resolveServerListingState(s) === 'presale'
      );
    } else if (listingFilterTab === 'new') {
      filtered = filtered.filter(
        (s) =>
          resolveServerListingState(s) === 'listed' &&
          Boolean(
            recentSkuMap[s.planCode] ||
              (s.listedAt &&
                Date.now() - Date.parse(s.listedAt) <= 7 * 24 * 3600 * 1000)
          )
      );
    }

    if (selectedPromoCode) {
      const targetPromoLow = selectedPromoCode.toLowerCase();
      const targetCamp = promotions?.campaigns.find(
        (c) => c.promoCode.toLowerCase() === targetPromoLow
      );
      const codeSet = new Set(
        (targetCamp?.planCodes || []).map((c) => c.toLowerCase())
      );
      filtered = filtered.filter((srv) => {
        if (codeSet.size > 0 && codeSet.has(srv.planCode.toLowerCase())) {
          return true;
        }
        if (srv.promoCode?.toLowerCase() === targetPromoLow) return true;
        return (srv.pricingModes || []).some(
          (m) =>
            m.promoCode?.toLowerCase() === targetPromoLow ||
            m.renewPromo?.code?.toLowerCase() === targetPromoLow ||
            m.setupPromo?.code?.toLowerCase() === targetPromoLow ||
            (m.voucherPromos || []).some(
              (v) => v.code?.toLowerCase() === targetPromoLow
            )
        );
      });
    }

    if (searchTerm) {
      const term = searchTerm.toLowerCase();
      filtered = filtered.filter(
        (server) =>
          server.planCode.toLowerCase().includes(term) ||
          server.name.toLowerCase().includes(term) ||
          server.cpu.toLowerCase().includes(term) ||
          server.memory.toLowerCase().includes(term) ||
          server.storage.toLowerCase().includes(term)
      );
    }

    if (selectedDatacenter !== 'all') {
      const normDc = normalizeDatacenterCode(selectedDatacenter);
      filtered = filtered.filter((srv) =>
        (srv.datacenters || []).some(
          (d) =>
            normalizeDatacenterCode(d.datacenter) === normDc &&
            isAvailStatus(d.availability)
        )
      );
    }

    const currentSubscribed = subscribedServersRef.current;
    filtered.sort((a, b) => {
      const aSub = currentSubscribed.has(a.planCode);
      const bSub = currentSubscribed.has(b.planCode);
      if (aSub && !bSub) return -1;
      if (!aSub && bSub) return 1;
      return 0;
    });

    return filtered;
  }, [servers, listingFilterTab, selectedPromoCode, promotions, searchTerm, selectedDatacenter, recentSkuMap, subscribedServers]);

  const formatDateTime = (date: Date | null): string => {
    if (!date) return '未知';
    const diffMins = Math.floor((Date.now() - date.getTime()) / 60000);
    if (diffMins < 1) return '刚刚';
    if (diffMins < 60) return `${diffMins} 分钟前`;
    return `今天 ${date.getHours().toString().padStart(2, '0')}:${date
      .getMinutes()
      .toString()
      .padStart(2, '0')}`;
  };

  return (
    <div
      className="space-y-5 w-full"
      style={{ maxWidth: '100%', overflowX: 'hidden' }}
    >
      <div className="mb-1">
        <h1
          className={`${
            isMobile ? 'text-2xl' : 'text-3xl'
          } font-bold mb-1 cyber-glow-text`}
        >
          服务器目录
        </h1>
        <p className="text-cyber-muted text-xs sm:text-sm">
          模块化固定规格卡片，点击「选择配置 / 下单」查看完整硬件配置（内存 + 硬盘）、实时库存并一键下单
        </p>
      </div>

      {/* 批量监控确认对话框 */}
      <AlertDialog
        open={showBatchMonitorDialog}
        onOpenChange={setShowBatchMonitorDialog}
      >
        <AlertDialogContent className="bg-slate-900 border-cyber-accent/30">
          <AlertDialogHeader>
            <AlertDialogTitle className="text-cyber-accent text-lg font-bold">
              批量添加监控
            </AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div className="space-y-3 mt-2 text-slate-200">
                <p className="text-base font-medium text-white">
                  确定要将所有 {servers.length} 台服务器添加到监控吗？
                </p>
                <ul className="space-y-1.5 text-sm text-slate-300 pl-4">
                  <li>• 将监控所有机房</li>
                  <li>• 自动启用有货和无货提醒</li>
                  <li>• 已订阅的服务器将被跳过</li>
                </ul>
              </div>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel className="bg-slate-800 border-slate-700 text-slate-300 hover:bg-slate-700 hover:text-white">
              取消
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={confirmBatchAddAllServersToMonitor}
              className="bg-blue-500 hover:bg-blue-600 text-white border-blue-600"
            >
              确定
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Live OVH Promotions Radar Banner (Collapsible) */}
      <CatalogPromoBanner
        promotions={promotions}
        selectedPromoCode={selectedPromoCode}
        onSelectPromoCode={(code) => {
          setSelectedPromoCode(code);
          if (code && listingFilterTab !== 'all' && listingFilterTab !== 'promo') {
            setListingFilterTab('all');
          }
        }}
      />

      {/* Filters & Controls */}
      <div className="cyber-panel p-3 w-full space-y-2.5">
        <div className="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-2.5 w-full">
          <div className="flex flex-wrap items-center gap-2 flex-1">
            <div className="relative flex-1 min-w-[200px] sm:max-w-md">
              <input
                type="text"
                placeholder={
                  isMobile
                    ? '搜索...'
                    : '搜索型号、planCode、CPU、内存、存储...'
                }
                value={searchTerm}
                onChange={(e) => setSearchTerm(e.target.value)}
                className="cyber-input pl-3 w-full text-xs sm:text-sm py-1.5"
              />
            </div>

            <select
              value={selectedDatacenter}
              onChange={(e) => setSelectedDatacenter(e.target.value)}
              className="cyber-input text-xs py-1.5 px-2.5 w-auto"
            >
              <option value="all">有货机房: 全部</option>
              {OVH_DATACENTERS.map((dc) => (
                <option key={dc.code} value={dc.code}>
                  {formatDatacenter(dc.code, locale, 'full')}
                </option>
              ))}
            </select>
          </div>

          <div className="flex items-center justify-between sm:justify-end gap-2 flex-wrap">
            <div className="flex items-center flex-shrink-0 text-[11px] text-cyber-muted">
              <Clock size={12} className="mr-1" />
              <span>{formatDateTime(lastUpdated)}</span>
            </div>

            {!isMobile && (
              <div className="flex items-center gap-1 border border-cyber-accent/30 rounded-md p-0.5">
                <button
                  onClick={() => setViewMode('grid')}
                  className={`p-1 rounded transition-all ${
                    viewMode === 'grid'
                      ? 'bg-cyber-accent/20 text-cyber-accent'
                      : 'text-cyber-muted hover:text-cyber-text'
                  }`}
                  title="紧凑网格视图"
                >
                  <Grid size={14} />
                </button>
                <button
                  onClick={() => setViewMode('list')}
                  className={`p-1 rounded transition-all ${
                    viewMode === 'list'
                      ? 'bg-cyber-accent/20 text-cyber-accent'
                      : 'text-cyber-muted hover:text-cyber-text'
                  }`}
                  title="列表视图"
                >
                  <List size={14} />
                </button>
              </div>
            )}

            <div className="flex items-center gap-1.5">
              <button
                onClick={() => setShowBatchMonitorDialog(true)}
                disabled={!isAuthenticated || servers.length === 0}
                className="px-2.5 py-1.5 bg-blue-500/10 hover:bg-blue-500/20 text-blue-400 border border-blue-500/30 rounded-md text-xs font-medium disabled:opacity-50 flex items-center gap-1"
              >
                <Bell size={12} />
                <span>一键监控全部</span>
              </button>

              <button
                onClick={() => {
                  fetchServers(true);
                  fetchPromotions(true);
                }}
                disabled={isLoading || isRefreshing}
                className="cyber-button text-xs flex items-center gap-1.5 px-2.5 py-1.5"
              >
                <RefreshCw
                  size={12}
                  className={isRefreshing ? 'animate-spin' : ''}
                />
                <span>刷新</span>
              </button>
            </div>
          </div>
        </div>

        {/* Listing State Filter Tabs */}
        <div className="flex flex-wrap items-center gap-1.5 pt-1 border-t border-cyber-grid/40">
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
                  'bg-emerald-500/20 text-emerald-700 dark:text-emerald-300 border-emerald-500/50',
              },
              {
                id: 'promo',
                label: t('listing.filter.promo'),
                count: tabCounts.promo,
                activeCls:
                  'bg-rose-500/20 text-rose-600 dark:text-rose-300 border-rose-500/50 shadow-[0_0_10px_rgba(244,63,94,0.2)]',
              },
              {
                id: 'announced',
                label: t('listing.filter.announced'),
                count: tabCounts.announced,
                activeCls:
                  'bg-amber-500/20 text-amber-700 dark:text-amber-300 border-amber-500/50',
              },
              {
                id: 'presale',
                label: t('listing.filter.presale'),
                count: tabCounts.presale,
                activeCls:
                  'bg-purple-500/20 text-purple-700 dark:text-purple-300 border-purple-500/50',
              },
              {
                id: 'new',
                label: t('listing.filter.new'),
                count: tabCounts.new,
                activeCls:
                  'bg-cyan-500/20 text-cyan-700 dark:text-cyan-300 border-cyan-500/50',
              },
            ] as const
          ).map((tab) => {
            const isActive = listingFilterTab === tab.id;
            return (
              <button
                key={tab.id}
                type="button"
                onClick={() => setListingFilterTab(tab.id)}
                className={`px-2.5 py-1 rounded-md text-xs font-semibold border transition-all flex items-center gap-1.5 whitespace-nowrap ${
                  isActive
                    ? tab.activeCls
                    : tab.id === 'promo'
                    ? 'bg-rose-500/10 text-rose-600 dark:text-rose-300 border-rose-500/30 hover:bg-rose-500/20'
                    : 'bg-cyber-surface text-cyber-muted border-cyber-grid hover:text-cyber-text'
                }`}
              >
                <span>{tab.label}</span>
                <span className="px-1.5 py-0.2 rounded-full text-[10px] font-mono bg-black/10 dark:bg-black/25 text-current">
                  {tab.count}
                </span>
              </button>
            );
          })}
        </div>
      </div>

      {/* Loading State */}
      {isLoading && !hasLoadedFromCache.current ? (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4 items-stretch">
          {[...Array(6)].map((_, i) => (
            <Card key={i} className="animate-pulse border-cyber-accent/30 p-4">
              <div className="h-5 bg-cyber-grid/30 rounded w-1/2 mb-3"></div>
              <div className="grid grid-cols-2 gap-2 mb-3">
                <div className="h-12 bg-cyber-grid/20 rounded"></div>
                <div className="h-12 bg-cyber-grid/20 rounded"></div>
                <div className="h-12 bg-cyber-grid/20 rounded"></div>
                <div className="h-12 bg-cyber-grid/20 rounded"></div>
              </div>
              <div className="h-4 bg-cyber-grid/20 rounded w-2/3"></div>
            </Card>
          ))}
        </div>
      ) : filteredServers.length === 0 ? (
        <Card className="border-cyber-accent/30 py-10">
          <CardContent className="flex flex-col items-center justify-center">
            <p className="text-cyber-muted mb-3">没有找到匹配的服务器</p>
            <button
              onClick={() => {
                setSearchTerm('');
                setSelectedDatacenter('all');
                setListingFilterTab('all');
                setSelectedPromoCode(null);
              }}
              className="px-4 py-1.5 bg-cyber-accent/15 text-cyber-accent border border-cyber-accent/40 rounded-md text-xs font-semibold"
            >
              清除筛选
            </button>
          </CardContent>
        </Card>
      ) : (
        <div
          className={
            viewMode === 'grid'
              ? 'grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4 items-stretch'
              : 'space-y-3'
          }
        >
          {filteredServers.map((server) => {
            const listingState = resolveServerListingState(server);
            const isPresale = listingState === 'presale';
            const isAnnounced = listingState === 'announced';
            const generation = inferPlanGeneration(
              server.planCode,
              server.generation
            );
            const isRecentlyAdded = Boolean(
              listingState === 'listed' &&
                (recentSkuMap[server.planCode] ||
                  (server.listedAt &&
                    Date.now() - Date.parse(server.listedAt) <=
                      7 * 24 * 3600 * 1000))
            );

            const unifiedConfigs = buildUnifiedConfigsForServer(server);
            const defaultCfg =
              unifiedConfigs.find((c) => c.isDefault) || unifiedConfigs[0] || null;
            const activeCfgKey =
              selectedConfigKeyMap[server.planCode] ||
              defaultCfg?.configKey ||
              '';
            const activeCfg =
              unifiedConfigs.find((c) => c.configKey === activeCfgKey) ||
              defaultCfg;

            // Aggregate all in-stock DCs across configs for the card badge row
            const allInStockDcSet = new Set<string>();
            unifiedConfigs.forEach((c) => {
              c.inStockDcs.forEach((dc) => allInStockDcSet.add(dc));
            });
            (server.datacenters || []).forEach((d) => {
              if (isAvailStatus(d.availability)) {
                const norm = normalizeDatacenterCode(d.datacenter);
                if (norm) allInStockDcSet.add(norm);
              }
            });
            const allInStockDcs = Array.from(allInStockDcSet);

            const startingPrice =
              typeof server.startingPrice === 'number' &&
              server.startingPrice > 0
                ? server.startingPrice
                : typeof server.monthlyPrice === 'number' &&
                  server.monthlyPrice > 0
                ? server.monthlyPrice
                : defaultCfg?.estimatedMonthlyPrice ?? null;

            const originalMonthlyPrice =
              typeof server.originalMonthlyPrice === 'number' &&
              server.originalMonthlyPrice > 0
                ? server.originalMonthlyPrice
                : typeof server.originalPrice === 'number' &&
                  server.originalPrice > 0
                ? server.originalPrice
                : defaultCfg?.originalMonthlyPrice ?? null;

            const cardSetupFee =
              typeof server.setupFee === 'number'
                ? server.setupFee
                : typeof defaultCfg?.setupFee === 'number'
                ? defaultCfg.setupFee
                : null;
            const cardOrigSetupFee =
              typeof server.originalSetupFee === 'number'
                ? server.originalSetupFee
                : typeof defaultCfg?.originalSetupFee === 'number'
                ? defaultCfg.originalSetupFee
                : null;
            const cardFreeSetupPromo =
              cardSetupFee === 0 && (cardOrigSetupFee ?? 0) > 0;
            const cardFreeSetup = Boolean(
              server.freeSetup ||
                defaultCfg?.freeSetup ||
                cardSetupFee === 0 ||
                cardFreeSetupPromo
            );
            const cardPricingModes =
              (server.pricingModes && server.pricingModes.length > 0
                ? server.pricingModes
                : defaultCfg?.pricingModes) || [];
            const hasAnnualFreeSetup = cardPricingModes.some(
              (m) =>
                (m.mode === 'upfront12' || m.mode === 'upfront24') &&
                m.setupFee === 0
            );
            const cardHasPromo = Boolean(
              server.hasPromo ||
                defaultCfg?.hasPromo ||
                cardPricingModes.some((m) => m.hasPromo) ||
                cardFreeSetupPromo ||
                (originalMonthlyPrice !== null &&
                  startingPrice !== null &&
                  originalMonthlyPrice > startingPrice + 0.01)
            );
            const cardPromoBadge =
              server.promoBadge ||
              defaultCfg?.promoBadge ||
              cardPricingModes.find((m) => m.promoBadge)?.promoBadge;
            const cardPromoDescription =
              server.promoDescription ||
              defaultCfg?.promoDescription ||
              cardPricingModes.find((m) => m.promoDescription)?.promoDescription;

            const symbol = getCurrencySymbol(
              activeCfg?.currency || server.currency
            );

            const cpuShort = formatServerSpec(server.cpu, 'CPU');
            const ramShort = formatServerSpec(server.memory, '内存');
            const stoShort = formatServerSpec(server.storage, '存储');
            const bwShort = formatServerSpec(server.bandwidth, '带宽');
            const selectedDcsForCard = selectedDcsMap[server.planCode] || [];

            return (
              <Card
                key={server.planCode}
                className={`h-full flex flex-col justify-between overflow-hidden transition-all ${
                  isPresale
                    ? 'border-purple-500/40 hover:border-purple-500/60'
                    : isAnnounced
                    ? 'border-amber-500/40 hover:border-amber-500/60'
                    : cardHasPromo
                    ? 'border-rose-500/35 hover:border-rose-500/60'
                    : 'border-cyber-accent/30 hover:border-cyber-accent/55'
                }`}
              >
                <div className="p-4 h-full flex flex-col justify-between gap-3">
                  {/* Zone 1: Header & Badges (Fixed 2-Row Structure) */}
                  <div className="space-y-1.5">
                    {/* Row 1: Model Name + planCode badge on Left, Starting Monthly Price on Right */}
                    <div className="flex items-center justify-between gap-2 h-6">
                      <div className="flex items-center gap-1.5 min-w-0 flex-1">
                        <span
                          className="font-bold text-sm text-cyber-accent truncate"
                          title={server.name}
                        >
                          {server.name}
                        </span>
                        <span className="px-1.5 py-0.5 text-[10px] font-mono rounded bg-cyber-grid/40 text-cyber-muted flex-shrink-0">
                          {server.planCode}
                        </span>
                      </div>

                      <div className="flex-shrink-0 text-right">
                        {isPresale ? (
                          <span
                            title={t('presale.notOrderableHint')}
                            className="px-2 py-0.5 rounded text-[10px] font-medium bg-purple-500/10 text-purple-700 dark:text-purple-300 border border-purple-500/30 whitespace-nowrap"
                          >
                            {t('presale.pricingTbd')}
                          </span>
                        ) : startingPrice !== null && startingPrice > 0 ? (
                          <span className="px-2 py-0.5 rounded text-xs font-bold bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 border border-emerald-500/30 whitespace-nowrap inline-flex items-center">
                            <span>
                              {t('catalog.fromPrice', {
                                price: `${symbol}${startingPrice.toFixed(2)}`,
                              })}
                            </span>
                            {originalMonthlyPrice !== null &&
                              originalMonthlyPrice > startingPrice + 0.01 && (
                                <span className="line-through text-[10px] opacity-70 ml-1 font-mono">
                                  {symbol}
                                  {originalMonthlyPrice.toFixed(2)}
                                </span>
                              )}
                          </span>
                        ) : (
                          <span className="px-2 py-0.5 rounded text-[10px] font-medium bg-cyber-grid/30 text-cyber-muted border border-cyber-grid whitespace-nowrap">
                            {t('presale.pricingTbd')}
                          </span>
                        )}
                      </div>
                    </div>

                    {/* Row 2: Promo + Status + Generation + Newly Listed + Subscribed Badges */}
                    <div className="flex items-center gap-1.5 h-6 overflow-hidden">
                      {cardHasPromo && (
                        <span
                          title={cardPromoDescription}
                          className="px-1.5 py-0.5 text-[10px] font-bold rounded bg-rose-500/15 text-rose-600 dark:text-rose-300 border border-rose-500/40 whitespace-nowrap shrink-0 inline-flex items-center gap-0.5"
                        >
                          <span>🔥 {cardPromoBadge || t('promo.badge')}</span>
                        </span>
                      )}
                      {isAnnounced && (
                        <span className="px-1.5 py-0.5 text-[10px] font-semibold rounded bg-amber-500/15 text-amber-700 dark:text-amber-300 border border-amber-500/30 whitespace-nowrap shrink-0 inline-flex items-center gap-1">
                          <Sparkles className="w-2.5 h-2.5 shrink-0" />
                          {t('announced.badge')}
                        </span>
                      )}
                      {isPresale && (
                        <span className="px-1.5 py-0.5 text-[10px] font-semibold rounded bg-purple-500/15 text-purple-700 dark:text-purple-300 border border-purple-500/30 whitespace-nowrap shrink-0 inline-flex items-center gap-1">
                          <Sparkles className="w-2.5 h-2.5 shrink-0" />
                          {t('presale.badge')}
                        </span>
                      )}
                      {!isPresale && !isAnnounced && (
                        <span className="px-1.5 py-0.5 text-[10px] font-medium rounded bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 border border-emerald-500/25 whitespace-nowrap shrink-0 inline-flex items-center">
                          {t('listed.badge')}
                        </span>
                      )}
                      {generation && (
                        <span className="px-1.5 py-0.5 text-[10px] font-mono rounded bg-indigo-500/15 text-indigo-700 dark:text-indigo-300 border border-indigo-500/30 whitespace-nowrap shrink-0 inline-flex items-center">
                          {t('presale.generation', { year: generation })}
                        </span>
                      )}
                      {isRecentlyAdded && (
                        <span className="px-1.5 py-0.5 text-[10px] font-bold rounded bg-emerald-500/15 text-emerald-700 dark:text-emerald-300 border border-emerald-500/30 whitespace-nowrap shrink-0 inline-flex items-center">
                          {t('sku.newListed')}
                        </span>
                      )}
                      {subscribedServers.has(server.planCode) && (
                        <span
                          className="px-1.5 py-0.5 rounded text-[10px] font-semibold bg-cyan-500/20 text-cyan-700 dark:text-cyan-300 border border-cyan-500/40 whitespace-nowrap shrink-0 inline-flex items-center gap-1"
                          title="已订阅监控"
                        >
                          <Bell size={10} />
                        </span>
                      )}
                    </div>
                  </div>

                  {/* Zone 2: 2x2 Modular Hardware Spec Grid */}
                  <div className="grid grid-cols-2 gap-2">
                    {/* 1. CPU */}
                    <div
                      className="bg-cyber-bg/60 border border-cyber-grid/50 rounded-lg px-2.5 py-2 flex flex-col justify-center min-h-[54px]"
                      title={cpuShort}
                    >
                      <div className="flex items-center gap-1 text-[10px] text-cyber-muted mb-0.5">
                        <Cpu
                          size={11}
                          className="text-cyber-accent flex-shrink-0"
                        />
                        <span className="truncate">{t('catalog.specCpu')}</span>
                      </div>
                      <div className="text-xs font-medium text-cyber-text truncate">
                        {cpuShort}
                      </div>
                    </div>

                    {/* 2. RAM */}
                    <div
                      className="bg-cyber-bg/60 border border-cyber-grid/50 rounded-lg px-2.5 py-2 flex flex-col justify-center min-h-[54px]"
                      title={ramShort}
                    >
                      <div className="flex items-center justify-between gap-1 text-[10px] text-cyber-muted mb-0.5">
                        <span className="flex items-center gap-1 min-w-0 truncate">
                          <Database
                            size={11}
                            className="text-cyber-accent flex-shrink-0"
                          />
                          <span className="truncate">
                            {t('catalog.specRam')}
                          </span>
                        </span>
                        {unifiedConfigs.length > 1 && (
                          <span className="px-1 py-0.2 rounded bg-cyber-accent/15 text-cyber-accent text-[9px] font-semibold flex-shrink-0">
                            {t('catalog.multiOptionBadge', {
                              count: unifiedConfigs.length,
                            })}
                          </span>
                        )}
                      </div>
                      <div className="text-xs font-medium text-cyber-text truncate">
                        {ramShort}
                      </div>
                    </div>

                    {/* 3. Storage */}
                    <div
                      className="bg-cyber-bg/60 border border-cyber-grid/50 rounded-lg px-2.5 py-2 flex flex-col justify-center min-h-[54px]"
                      title={stoShort}
                    >
                      <div className="flex items-center gap-1 text-[10px] text-cyber-muted mb-0.5">
                        <HardDrive
                          size={11}
                          className="text-cyber-accent flex-shrink-0"
                        />
                        <span className="truncate">
                          {t('catalog.specStorage')}
                        </span>
                      </div>
                      <div className="text-xs font-medium text-cyber-text truncate">
                        {stoShort}
                      </div>
                    </div>

                    {/* 4. Bandwidth */}
                    <div
                      className="bg-cyber-bg/60 border border-cyber-grid/50 rounded-lg px-2.5 py-2 flex flex-col justify-center min-h-[54px]"
                      title={bwShort}
                    >
                      <div className="flex items-center gap-1 text-[10px] text-cyber-muted mb-0.5">
                        <Wifi
                          size={11}
                          className="text-cyber-accent flex-shrink-0"
                        />
                        <span className="truncate">
                          {t('catalog.specBandwidth')}
                        </span>
                      </div>
                      <div className="text-xs font-medium text-cyber-text truncate">
                        {bwShort}
                      </div>
                    </div>
                  </div>

                  {/* Zone 3: Dedicated Datacenter Availability & Setup Fee Row */}
                  <div className="min-h-[30px] flex items-center justify-between gap-2 px-1">
                    <div className="flex items-center gap-1 min-w-0 overflow-hidden">
                      {isAnnounced ? (
                        <span className="px-2 py-0.5 rounded bg-amber-500/10 border border-amber-500/25 text-amber-700 dark:text-amber-300 text-[10px] font-medium truncate">
                          {t('announced.badge')}
                        </span>
                      ) : allInStockDcs.length > 0 ? (
                        <>
                          {allInStockDcs.slice(0, 4).map((dc) => (
                            <span
                              key={dc}
                              className="px-1.5 py-0.5 rounded bg-emerald-500/15 border border-emerald-500/30 text-emerald-700 dark:text-emerald-300 text-[10px] font-semibold shrink-0 whitespace-nowrap"
                            >
                              <DCLabel code={dc} mode="compact" />
                            </span>
                          ))}
                          {allInStockDcs.length > 4 && (
                            <span className="px-1.5 py-0.5 rounded bg-cyber-grid/40 text-cyber-muted text-[10px] font-mono flex-shrink-0">
                              +{allInStockDcs.length - 4}
                            </span>
                          )}
                        </>
                      ) : (
                        <span className="px-2 py-0.5 rounded bg-red-500/10 border border-red-500/20 text-rose-600 dark:text-red-400/80 text-[10px] font-medium flex-shrink-0 whitespace-nowrap">
                          {t('catalog.allOutOfStock')}
                        </span>
                      )}
                    </div>

                    <div className="flex items-center gap-1 flex-shrink-0">
                      {!isPresale &&
                        (cardSetupFee === 0 || cardFreeSetup ? (
                          <span className="px-1.5 py-0.5 rounded bg-emerald-500/15 border border-emerald-500/35 text-emerald-700 dark:text-emerald-300 text-[10px] font-semibold inline-flex items-center whitespace-nowrap">
                            <span>{t('promo.freeSetup')}</span>
                            {cardOrigSetupFee !== null &&
                              cardOrigSetupFee > 0 && (
                                <span className="line-through opacity-70 ml-1 font-mono text-[9px]">
                                  {symbol}
                                  {cardOrigSetupFee.toFixed(2)}
                                </span>
                              )}
                          </span>
                        ) : cardSetupFee !== null && cardSetupFee > 0 ? (
                          <span
                            title={
                              hasAnnualFreeSetup
                                ? t('promo.annualWaivesSetupHint')
                                : undefined
                            }
                            className="px-1.5 py-0.5 rounded bg-cyber-grid/40 border border-cyber-grid text-cyber-muted text-[10px] font-mono inline-flex items-center gap-1 whitespace-nowrap"
                          >
                            <span>
                              {t('promo.setupFee', {
                                price: `${symbol}${cardSetupFee.toFixed(2)}`,
                              })}
                            </span>
                            {hasAnnualFreeSetup && (
                              <span className="text-emerald-700 dark:text-emerald-400 font-sans">
                                · {t('promo.annualFreeSetupShort')}
                              </span>
                            )}
                          </span>
                        ) : null)}
                      <span className="px-2 py-0.5 rounded bg-cyber-grid/40 text-cyber-muted text-[10px] font-mono flex-shrink-0 whitespace-nowrap">
                        {t('catalog.configCount', {
                          count: unifiedConfigs.length,
                        })}
                      </span>
                    </div>
                  </div>

                  {/* Zone 4: Dedicated Bottom Action Footer */}
                  <div className="pt-2.5 border-t border-cyber-grid/40 flex items-center justify-between gap-2">
                    <button
                      type="button"
                      onClick={() =>
                        setQuickMonitorTarget({
                          server,
                          initialConfig: activeCfg
                            ? {
                                configKey: activeCfg.configKey,
                                options: activeCfg.options,
                                label: activeCfg.label,
                                estimatedMonthlyPrice:
                                  activeCfg.estimatedMonthlyPrice ?? undefined,
                                originalMonthlyPrice:
                                  activeCfg.originalMonthlyPrice ?? undefined,
                                setupFee: activeCfg.setupFee ?? undefined,
                                originalSetupFee:
                                  activeCfg.originalSetupFee ?? undefined,
                                freeSetup: activeCfg.freeSetup,
                                hasPromo: activeCfg.hasPromo,
                                promoCode: activeCfg.promoCode,
                                promoBadge: activeCfg.promoBadge,
                                promoDescription: activeCfg.promoDescription,
                                pricingModes: activeCfg.pricingModes,
                                currency: activeCfg.currency,
                                memory: activeCfg.memory,
                                storage: activeCfg.storage,
                              }
                            : null,
                          initialConfigs: unifiedConfigs.map((c) => ({
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
                            originalSetupFee: c.originalSetupFee ?? undefined,
                            freeSetup: c.freeSetup,
                            hasPromo: c.hasPromo,
                            promoCode: c.promoCode,
                            promoBadge: c.promoBadge,
                            promoDescription: c.promoDescription,
                            pricingModes: c.pricingModes,
                            currency: c.currency,
                            datacenters: c.dcStatuses,
                            inStockCount: c.inStockDcs.length,
                          })),
                          initialSelectedDcs: selectedDcsForCard,
                        })
                      }
                      disabled={!isAuthenticated}
                      className="px-2.5 py-1.5 rounded-md bg-blue-500/15 hover:bg-blue-500/25 text-blue-400 border border-blue-500/35 text-xs font-semibold flex items-center gap-1.5 disabled:opacity-50 transition-colors"
                      title={t('quickMonitor.button')}
                    >
                      <Bell size={12} />
                      <span>{t('quickMonitor.button')}</span>
                    </button>

                    <button
                      type="button"
                      onClick={() => openConfigModal(server)}
                      className="flex-1 sm:flex-initial px-3 py-1.5 rounded-md bg-cyber-accent/20 hover:bg-cyber-accent/30 text-cyber-accent border border-cyber-accent/50 text-xs font-semibold flex items-center justify-center gap-1.5 transition-colors"
                    >
                      <SlidersHorizontal size={12} />
                      <span>{t('catalog.configureAndOrder')}</span>
                    </button>
                  </div>
                </div>
              </Card>
            );
          })}
        </div>
      )}

      {/* Focused Configuration & Order Modal */}
      {(() => {
        if (!configModalServer) return null;
        const modalServer =
          servers.find((s) => s.planCode === configModalServer.planCode) ||
          configModalServer;
        const listingState = resolveServerListingState(modalServer);
        const isPresale = listingState === 'presale';
        const isAnnounced = listingState === 'announced';
        const generation = inferPlanGeneration(
          modalServer.planCode,
          modalServer.generation
        );
        const showAllInventory = Boolean(
          showAllInventoryMap[modalServer.planCode]
        );

        const unifiedConfigs = buildUnifiedConfigsForServer(modalServer);
        const defaultCfg =
          unifiedConfigs.find((c) => c.isDefault) || unifiedConfigs[0] || null;
        const activeCfgKey =
          selectedConfigKeyMap[modalServer.planCode] ||
          defaultCfg?.configKey ||
          '';
        const activeCfg =
          unifiedConfigs.find((c) => c.configKey === activeCfgKey) ||
          defaultCfg;

        const selectedPricingMode =
          selectedPricingModeMap[modalServer.planCode] || 'default';
        const selectedCoupon =
          selectedCouponMap[modalServer.planCode] || '';

        // Available pricing modes for the active configuration
        const rawModes =
          activeCfg && activeCfg.pricingModes.length > 0
            ? activeCfg.pricingModes
            : modalServer.pricingModes && modalServer.pricingModes.length > 0
            ? modalServer.pricingModes
            : synthesizePricingModes({
                estimatedMonthlyPrice:
                  activeCfg?.estimatedMonthlyPrice ??
                  modalServer.startingPrice ??
                  modalServer.monthlyPrice ??
                  0,
                originalMonthlyPrice:
                  activeCfg?.originalMonthlyPrice ??
                  modalServer.originalMonthlyPrice ??
                  0,
                setupFee: activeCfg?.setupFee ?? modalServer.setupFee ?? 0,
                originalSetupFee:
                  activeCfg?.originalSetupFee ??
                  modalServer.originalSetupFee ??
                  0,
                freeSetup: Boolean(
                  activeCfg?.freeSetup || modalServer.freeSetup
                ),
                hasPromo: Boolean(activeCfg?.hasPromo || modalServer.hasPromo),
                promoCode: activeCfg?.promoCode || modalServer.promoCode,
                promoBadge: activeCfg?.promoBadge || modalServer.promoBadge,
                promoDescription:
                  activeCfg?.promoDescription || modalServer.promoDescription,
              });

        const availablePricingModes: PlanPricingMode[] =
          rawModes.length > 1
            ? rawModes
            : rawModes.length === 1
            ? [
                rawModes[0],
                {
                  ...rawModes[0],
                  mode: 'upfront12',
                  duration: 'P12M',
                  intervalMonths: 12,
                  commitmentMonths: 12,
                  periodPrice: Number(
                    (rawModes[0].monthlyEquivalentPrice * 12).toFixed(2)
                  ),
                  originalPeriodPrice: Number(
                    (rawModes[0].originalMonthlyPrice * 12).toFixed(2)
                  ),
                  setupFee: 0,
                  freeSetup: true,
                },
                {
                  ...rawModes[0],
                  mode: 'upfront24',
                  duration: 'P24M',
                  intervalMonths: 24,
                  commitmentMonths: 24,
                  periodPrice: Number(
                    (rawModes[0].monthlyEquivalentPrice * 24).toFixed(2)
                  ),
                  originalPeriodPrice: Number(
                    (rawModes[0].originalMonthlyPrice * 24).toFixed(2)
                  ),
                  setupFee: 0,
                  freeSetup: true,
                },
              ]
            : [];

        const activeModeEntry =
          resolvePricingModeEntry(availablePricingModes, selectedPricingMode) ||
          availablePricingModes[0] ||
          null;

        const allInStockDcSet = new Set<string>();
        unifiedConfigs.forEach((c) => {
          c.inStockDcs.forEach((dc) => allInStockDcSet.add(dc));
        });
        (modalServer.datacenters || []).forEach((d) => {
          if (isAvailStatus(d.availability)) {
            const norm = normalizeDatacenterCode(d.datacenter);
            if (norm) allInStockDcSet.add(norm);
          }
        });
        const allInStockDcs = Array.from(allInStockDcSet);
        const hasAnyStock = allInStockDcs.length > 0;

        const visibleConfigs =
          showAllInventory || !hasAnyStock
            ? unifiedConfigs
            : unifiedConfigs.filter((c) => c.inStockDcs.length > 0);

        const startingPrice =
          typeof modalServer.startingPrice === 'number' &&
          modalServer.startingPrice > 0
            ? modalServer.startingPrice
            : typeof modalServer.monthlyPrice === 'number' &&
              modalServer.monthlyPrice > 0
            ? modalServer.monthlyPrice
            : defaultCfg?.estimatedMonthlyPrice ?? null;

        const symbol = getCurrencySymbol(
          activeCfg?.currency || modalServer.currency
        );

        const selectedDcsForModal =
          selectedDcsMap[modalServer.planCode] || [];
        const advOptions = getAdvancedOptionsForServer(modalServer);
        const hasAdvOptions =
          advOptions.bandwidth.length > 0 || advOptions.vrack.length > 0;
        const selectedAdvOpts =
          selectedAddonOptionsMap[modalServer.planCode] || [];

        const activeCfgDcEntries = activeCfg
          ? Object.entries(activeCfg.dcStatuses)
          : [];
        const allCandidateDcs: [string, string][] =
          activeCfgDcEntries.length > 0
            ? activeCfgDcEntries
            : OVH_DATACENTERS.filter((dc) => {
                const low = modalServer.planCode.toLowerCase();
                if (low.includes('-sgp')) return dc.code === 'sgp';
                if (low.includes('-syd')) return dc.code === 'syd';
                if (low.includes('-mum')) return dc.code === 'mum';
                return true;
              }).map(
                (dc) =>
                  [
                    dc.code,
                    activeCfg?.dcStatuses[dc.code] || 'unavailable',
                  ] as [string, string]
              );

        const visibleDcForActiveCfg =
          showAllInventory || (activeCfg?.inStockDcs.length ?? 0) === 0
            ? allCandidateDcs
            : allCandidateDcs.filter(([_, st]) => isAvailStatus(st));

        const canQuickOrder =
          !isPresale &&
          !isAnnounced &&
          isAuthenticated &&
          ((activeCfg?.inStockDcs.length ?? 0) > 0 ||
            allInStockDcs.length > 0 ||
            selectedDcsForModal.length > 0);

        const modalHasPromo = Boolean(
          modalServer.hasPromo ||
            activeModeEntry?.hasPromo ||
            activeCfg?.hasPromo
        );
        const modalPromoBadge =
          activeModeEntry?.promoBadge ||
          activeCfg?.promoBadge ||
          modalServer.promoBadge;

        return (
          <div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 backdrop-blur-sm p-4"
            onClick={() => setConfigModalServer(null)}
          >
            <div
              className="w-full max-w-3xl max-h-[90vh] flex flex-col rounded-xl bg-cyber-surface border border-cyber-accent/40 shadow-2xl overflow-hidden text-cyber-text"
              onClick={(e) => e.stopPropagation()}
            >
              {/* Modal Header */}
              <div className="px-5 py-4 border-b border-cyber-grid/60 flex items-center justify-between gap-3 bg-cyber-bg/40">
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <h2 className="text-base sm:text-lg font-bold text-cyber-accent truncate">
                      {modalServer.name}
                    </h2>
                    <span className="px-2 py-0.5 text-xs font-mono rounded bg-cyber-grid/50 text-cyber-muted">
                      {modalServer.planCode}
                    </span>
                    {modalHasPromo && (
                      <span
                        title={
                          activeModeEntry?.promoDescription ||
                          modalServer.promoDescription
                        }
                        className="px-2 py-0.5 text-[11px] font-bold rounded bg-rose-500/15 text-rose-600 dark:text-rose-300 border border-rose-500/40 whitespace-nowrap shrink-0 inline-flex items-center"
                      >
                        🔥 {modalPromoBadge || t('promo.badge')}
                      </span>
                    )}
                    {isAnnounced && (
                      <span className="px-2 py-0.5 text-[11px] font-semibold rounded bg-amber-500/15 text-amber-700 dark:text-amber-300 border border-amber-500/30 whitespace-nowrap shrink-0 inline-flex items-center gap-1">
                        <Sparkles className="w-3 h-3 shrink-0" />
                        {t('announced.badge')}
                      </span>
                    )}
                    {isPresale && (
                      <span className="px-2 py-0.5 text-[11px] font-semibold rounded bg-purple-500/15 text-purple-700 dark:text-purple-300 border border-purple-500/30 whitespace-nowrap shrink-0 inline-flex items-center gap-1">
                        <Sparkles className="w-3 h-3 shrink-0" />
                        {t('presale.badge')}
                      </span>
                    )}
                    {!isPresale && !isAnnounced && (
                      <span className="px-2 py-0.5 text-[11px] font-medium rounded bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 border border-emerald-500/25 whitespace-nowrap shrink-0 inline-flex items-center">
                        {t('listed.badge')}
                      </span>
                    )}
                    {generation && (
                      <span className="px-2 py-0.5 text-[11px] font-mono rounded bg-indigo-500/15 text-indigo-700 dark:text-indigo-300 border border-indigo-500/30 whitespace-nowrap shrink-0 inline-flex items-center">
                        {t('presale.generation', { year: generation })}
                      </span>
                    )}
                  </div>
                </div>

                <div className="flex items-center gap-2.5 flex-shrink-0">
                  {startingPrice !== null && startingPrice > 0 && !isPresale && (
                    <span className="px-2.5 py-1 rounded text-xs font-bold bg-emerald-500/10 text-emerald-400 border border-emerald-500/30 whitespace-nowrap">
                      {t('catalog.fromPrice', {
                        price: `${symbol}${startingPrice.toFixed(2)}`,
                      })}
                    </span>
                  )}
                  <button
                    type="button"
                    onClick={() => setConfigModalServer(null)}
                    className="p-1.5 rounded-lg bg-cyber-bg hover:bg-cyber-grid/50 text-cyber-muted hover:text-cyber-text border border-cyber-grid transition-colors"
                    aria-label="Close"
                  >
                    <X size={16} />
                  </button>
                </div>
              </div>

              {/* Default Spec Summary Bar */}
              <div className="grid grid-cols-2 sm:grid-cols-4 gap-2 px-5 py-3 bg-cyber-bg/60 border-b border-cyber-grid/50">
                <div className="flex items-center gap-2 min-w-0">
                  <Cpu size={14} className="text-cyber-accent flex-shrink-0" />
                  <div className="min-w-0">
                    <div className="text-[10px] text-cyber-muted">
                      {t('catalog.specCpu')}
                    </div>
                    <div
                      className="text-xs font-medium text-cyber-text truncate"
                      title={formatServerSpec(modalServer.cpu, 'CPU')}
                    >
                      {formatServerSpec(modalServer.cpu, 'CPU')}
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-2 min-w-0">
                  <Database
                    size={14}
                    className="text-cyber-accent flex-shrink-0"
                  />
                  <div className="min-w-0">
                    <div className="text-[10px] text-cyber-muted">
                      {t('catalog.specRam')}
                    </div>
                    <div
                      className="text-xs font-medium text-cyber-text truncate"
                      title={formatServerSpec(modalServer.memory, '内存')}
                    >
                      {formatServerSpec(modalServer.memory, '内存')}
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-2 min-w-0">
                  <HardDrive
                    size={14}
                    className="text-cyber-accent flex-shrink-0"
                  />
                  <div className="min-w-0">
                    <div className="text-[10px] text-cyber-muted">
                      {t('catalog.specStorage')}
                    </div>
                    <div
                      className="text-xs font-medium text-cyber-text truncate"
                      title={formatServerSpec(modalServer.storage, '存储')}
                    >
                      {formatServerSpec(modalServer.storage, '存储')}
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-2 min-w-0">
                  <Wifi size={14} className="text-cyber-accent flex-shrink-0" />
                  <div className="min-w-0">
                    <div className="text-[10px] text-cyber-muted">
                      {t('catalog.specBandwidth')}
                    </div>
                    <div
                      className="text-xs font-medium text-cyber-text truncate"
                      title={formatServerSpec(modalServer.bandwidth, '带宽')}
                    >
                      {formatServerSpec(modalServer.bandwidth, '带宽')}
                    </div>
                  </div>
                </div>
              </div>

              {/* Scrollable Modal Body */}
              <div className="flex-1 overflow-y-auto p-5 space-y-4">
                {/* Billing / Commitment Mode Selector + Coupon Bar */}
                {!isPresale && availablePricingModes.length > 0 && (
                  <div className="p-3.5 rounded-xl border border-cyber-accent/30 bg-cyber-bg/50 space-y-3">
                    <div className="space-y-1.5">
                      <div className="text-xs font-bold text-cyber-accent">
                        {t('billing.modeSelectorTitle')}
                      </div>
                      <div className="grid grid-cols-1 sm:grid-cols-3 gap-2">
                        {availablePricingModes.map((m) => {
                          const isSelectedMode =
                            (activeModeEntry?.mode || selectedPricingMode) ===
                            m.mode;
                          const modeLabelKey = `billing.mode.${m.mode}`;
                          const modeTitle =
                            t(modeLabelKey) !== modeLabelKey
                              ? t(modeLabelKey)
                              : m.mode;
                          return (
                            <button
                              key={m.mode}
                              type="button"
                              onClick={() =>
                                setSelectedPricingModeMap((prev) => ({
                                  ...prev,
                                  [modalServer.planCode]: m.mode,
                                }))
                              }
                              className={`p-2.5 rounded-lg border text-left transition-all flex flex-col justify-between gap-1 ${
                                isSelectedMode
                                  ? 'bg-cyber-accent/15 border-cyber-accent text-cyber-text ring-1 ring-cyber-accent/40'
                                  : 'bg-cyber-surface/60 border-cyber-grid text-cyber-muted hover:border-cyber-accent/40 hover:text-cyber-text'
                              }`}
                            >
                              <div className="flex items-center justify-between gap-1">
                                <span className="text-xs font-bold truncate">
                                  {modeTitle}
                                </span>
                                {m.hasPromo && (
                                  <span
                                    title={m.promoDescription}
                                    className="px-1.5 py-0.2 rounded bg-rose-500/15 text-rose-300 border border-rose-500/35 text-[10px] font-bold flex-shrink-0"
                                  >
                                    🔥 {m.promoBadge || t('promo.badge')}
                                  </span>
                                )}
                              </div>

                              <div className="flex items-baseline gap-1 font-mono">
                                {m.monthlyEquivalentPrice > 0 ? (
                                  <>
                                    <span className="text-sm font-bold text-emerald-400">
                                      {symbol}
                                      {m.monthlyEquivalentPrice.toFixed(2)}
                                      {t('availability.perMonth')}
                                    </span>
                                    {m.originalMonthlyPrice >
                                      m.monthlyEquivalentPrice + 0.01 && (
                                      <span className="line-through text-[10px] text-cyber-muted">
                                        {symbol}
                                        {m.originalMonthlyPrice.toFixed(2)}
                                      </span>
                                    )}
                                  </>
                                ) : (
                                  <span className="text-xs text-cyber-muted">
                                    {m.duration}
                                  </span>
                                )}
                              </div>

                              <div className="flex flex-wrap items-center justify-between gap-1 text-[10px]">
                                {m.intervalMonths > 1 && m.periodPrice > 0 ? (
                                  <span className="text-cyber-muted font-mono">
                                    {t('billing.periodTotal', {
                                      price: `${symbol}${m.periodPrice.toFixed(
                                        2
                                      )}`,
                                      months: m.intervalMonths,
                                    })}
                                  </span>
                                ) : (
                                  <span />
                                )}

                                {m.setupFee === 0 || m.freeSetup ? (
                                  <span className="px-1.5 py-0.2 rounded bg-emerald-500/15 text-emerald-300 border border-emerald-500/30 font-semibold font-mono">
                                    {t('promo.freeSetupWithZero', {
                                      zero: `${symbol}0.00`,
                                    })}
                                    {m.originalSetupFee > 0 && (
                                      <span className="line-through opacity-70 ml-1">
                                        {symbol}
                                        {m.originalSetupFee.toFixed(2)}
                                      </span>
                                    )}
                                  </span>
                                ) : (
                                  <span className="px-1.5 py-0.2 rounded bg-cyber-grid/40 text-cyber-muted font-mono">
                                    {t('promo.setupFee', {
                                      price: `${symbol}${m.setupFee.toFixed(2)}`,
                                    })}
                                  </span>
                                )}
                              </div>
                            </button>
                          );
                        })}
                      </div>
                    </div>

                    {/* Optional Custom Coupon Code Input */}
                    <div className="flex flex-col sm:flex-row sm:items-center gap-2 pt-2 border-t border-cyber-grid/40">
                      <label className="text-xs font-semibold text-cyber-muted flex-shrink-0">
                        {t('coupon.label')}:
                      </label>
                      <div className="relative flex-1">
                        <input
                          type="text"
                          value={selectedCoupon}
                          onChange={(e) =>
                            setSelectedCouponMap((prev) => ({
                              ...prev,
                              [modalServer.planCode]: e.target.value,
                            }))
                          }
                          placeholder={t('coupon.placeholder')}
                          className="cyber-input w-full text-xs py-1.5 pr-7 font-mono"
                        />
                        {selectedCoupon && (
                          <button
                            type="button"
                            onClick={() =>
                              setSelectedCouponMap((prev) => ({
                                ...prev,
                                [modalServer.planCode]: '',
                              }))
                            }
                            className="absolute right-2 top-1/2 -translate-y-1/2 text-cyber-muted hover:text-cyber-text"
                          >
                            <X size={12} />
                          </button>
                        )}
                      </div>
                    </div>

                    {/* Promo Hit & Pricing Debug + Auto-Promo Guard + Live Cart Probe */}
                    <OrderPromoDebugPanel
                      server={modalServer}
                      promotions={promotions}
                      activeModeKey={activeModeEntry?.mode || selectedPricingMode}
                      activeDuration={resolvePricingModeDuration(
                        activeModeEntry?.mode || selectedPricingMode,
                        availablePricingModes
                      )}
                      activeConfigMode={activeModeEntry}
                      basePlanModes={modalServer.pricingModes || []}
                      configModes={availablePricingModes}
                      selectedDatacenter={
                        selectedDcsForModal[0] ||
                        activeCfg?.inStockDcs[0] ||
                        allInStockDcs[0] ||
                        'gra'
                      }
                      selectedOptions={[
                        ...(activeCfg?.options || []),
                        ...selectedAdvOpts,
                      ]}
                      coupon={selectedCoupon}
                      isAuthenticated={isAuthenticated}
                      onClearCoupon={() =>
                        setSelectedCouponMap((prev) => ({
                          ...prev,
                          [modalServer.planCode]: '',
                        }))
                      }
                      onSwitchMode={(mode) =>
                        setSelectedPricingModeMap((prev) => ({
                          ...prev,
                          [modalServer.planCode]: mode,
                        }))
                      }
                    />
                  </div>
                )}

                {/* Unified Configuration Selector Section */}
                <div className="space-y-2.5">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                      <span className="text-xs font-bold text-cyber-accent">
                        {t('catalog.unifiedConfigsTitle')}
                      </span>
                      <span className="px-1.5 py-0.2 rounded-full text-[10px] font-mono bg-cyber-accent/15 text-cyber-accent">
                        {visibleConfigs.length}/{unifiedConfigs.length}
                      </span>
                    </div>

                    <div className="flex flex-wrap items-center gap-3">
                      <label className="flex items-center gap-1.5 text-xs text-cyber-muted hover:text-cyber-text cursor-pointer select-none">
                        <input
                          type="checkbox"
                          checked={showAllInventory}
                          onChange={(e) =>
                            setShowAllInventoryMap((prev) => ({
                              ...prev,
                              [modalServer.planCode]: e.target.checked,
                            }))
                          }
                          className="h-3.5 w-3.5 accent-cyber-accent"
                        />
                        <span>{t('catalog.showAllInventory')}</span>
                      </label>

                      <button
                        type="button"
                        onClick={() =>
                          checkAvailability(modalServer.planCode, activeCfg)
                        }
                        disabled={isCheckingAvailability || !isAuthenticated}
                        className="px-2.5 py-1 rounded bg-cyber-accent/10 hover:bg-cyber-accent/20 text-cyber-accent border border-cyber-accent/30 text-xs font-medium flex items-center gap-1 disabled:opacity-50"
                      >
                        <RefreshCw
                          size={11}
                          className={
                            isCheckingAvailability &&
                            selectedServer === modalServer.planCode
                              ? 'animate-spin'
                              : ''
                          }
                        />
                        <span>实时检测</span>
                      </button>
                    </div>
                  </div>

                  {!hasAnyStock && !isAnnounced && (
                    <div className="p-2.5 rounded-lg bg-amber-500/10 border border-amber-500/25 text-amber-300 text-xs">
                      {t('catalog.noInStockConfigsNotice')}
                    </div>
                  )}

                  {isAnnounced && (
                    <div className="p-2.5 rounded-lg bg-amber-500/10 border border-amber-500/25 text-amber-300 text-xs">
                      {t('announced.noStockFeedHint')}
                    </div>
                  )}

                  {/* Configuration Cards */}
                  <div className="space-y-2 max-h-64 overflow-y-auto pr-1">
                    {visibleConfigs.map((cfg) => {
                      const isSelectedCfg =
                        activeCfg?.configKey === cfg.configKey;
                      const cfgSymbol = getCurrencySymbol(cfg.currency);
                      const cfgMode =
                        resolvePricingModeEntry(
                          cfg.pricingModes,
                          selectedPricingMode
                        ) || null;
                      const cfgMonthlyPrice =
                        cfgMode && cfgMode.monthlyEquivalentPrice > 0
                          ? cfgMode.monthlyEquivalentPrice
                          : cfg.estimatedMonthlyPrice;
                      const cfgOrigMonthly =
                        cfgMode && cfgMode.originalMonthlyPrice > 0
                          ? cfgMode.originalMonthlyPrice
                          : cfg.originalMonthlyPrice;
                      const cfgSetupFee =
                        typeof cfgMode?.setupFee === 'number'
                          ? cfgMode.setupFee
                          : cfg.setupFee;
                      const cfgOrigSetupFee =
                        typeof cfgMode?.originalSetupFee === 'number'
                          ? cfgMode.originalSetupFee
                          : cfg.originalSetupFee;
                      const cfgFreeSetup = Boolean(
                        cfgMode?.freeSetup ||
                          cfg.freeSetup ||
                          (cfgSetupFee === 0 && (cfgOrigSetupFee ?? 0) > 0)
                      );
                      const cfgHasPromo = Boolean(
                        cfgMode?.hasPromo || cfg.hasPromo
                      );
                      const cfgPromoBadge =
                        cfgMode?.promoBadge || cfg.promoBadge;

                      const allCfgDcEntries = Object.entries(cfg.dcStatuses);
                      const cfgVisibleDcs =
                        showAllInventory || cfg.inStockDcs.length === 0
                          ? allCfgDcEntries
                          : allCfgDcEntries.filter(([_, st]) =>
                              isAvailStatus(st)
                            );

                      return (
                        <div
                          key={cfg.configKey}
                          onClick={() =>
                            setSelectedConfigKeyMap((prev) => ({
                              ...prev,
                              [modalServer.planCode]: cfg.configKey,
                            }))
                          }
                          className={`p-3 rounded-lg border cursor-pointer transition-all space-y-2 ${
                            isSelectedCfg
                              ? 'bg-cyber-accent/12 border-cyber-accent/70 ring-1 ring-cyber-accent/30'
                              : 'bg-cyber-bg/40 border-cyber-grid hover:border-cyber-accent/30'
                          }`}
                        >
                          <div className="flex items-center justify-between gap-2">
                            <div className="flex flex-wrap items-center gap-2 min-w-0">
                              <span
                                className={`w-4 h-4 rounded-full border flex items-center justify-center flex-shrink-0 ${
                                  isSelectedCfg
                                    ? 'border-cyber-accent bg-cyber-accent text-white'
                                    : 'border-cyber-grid bg-cyber-bg'
                                }`}
                              >
                                {isSelectedCfg && (
                                  <Check size={10} strokeWidth={3} />
                                )}
                              </span>
                              <span className="text-xs sm:text-sm font-semibold text-cyber-text">
                                {cfg.label}
                              </span>
                              {cfg.isDefault && (
                                <span className="px-1.5 py-0.2 rounded text-[10px] font-bold bg-cyber-accent/20 text-cyber-accent border border-cyber-accent/40 flex-shrink-0">
                                  {t('catalog.defaultConfigBadge')}
                                </span>
                              )}
                              {cfgHasPromo && (
                                <span className="px-1.5 py-0.2 rounded text-[10px] font-bold bg-rose-500/15 text-rose-300 border border-rose-500/35 flex-shrink-0">
                                  🔥 {cfgPromoBadge || t('promo.badge')}
                                </span>
                              )}
                              {(cfg.memory || cfg.storage) && (
                                <span className="text-[10px] font-mono text-cyber-muted">
                                  {[cfg.memory, cfg.storage]
                                    .filter(Boolean)
                                    .join(' + ')}
                                </span>
                              )}
                            </div>

                            <div className="flex flex-wrap items-center justify-end gap-2 flex-shrink-0">
                              {cfgSetupFee === 0 || cfgFreeSetup ? (
                                <span className="px-1.5 py-0.5 rounded bg-emerald-500/15 text-emerald-300 border border-emerald-500/30 text-[10px] font-semibold font-mono">
                                  {t('promo.freeSetup')}
                                  {cfgOrigSetupFee !== null &&
                                    cfgOrigSetupFee > 0 && (
                                      <span className="line-through opacity-70 ml-1">
                                        {cfgSymbol}
                                        {cfgOrigSetupFee.toFixed(2)}
                                      </span>
                                    )}
                                </span>
                              ) : cfgSetupFee !== null && cfgSetupFee > 0 ? (
                                <span className="px-1.5 py-0.5 rounded bg-cyber-grid/40 text-cyber-muted border border-cyber-grid text-[10px] font-mono">
                                  {t('promo.setupFee', {
                                    price: `${cfgSymbol}${cfgSetupFee.toFixed(
                                      2
                                    )}`,
                                  })}
                                </span>
                              ) : null}

                              {cfgMonthlyPrice !== null &&
                                cfgMonthlyPrice > 0 && (
                                  <div className="text-right font-mono">
                                    <span className="text-xs sm:text-sm font-bold text-emerald-400">
                                      {cfgSymbol}
                                      {cfgMonthlyPrice.toFixed(2)}
                                      {t('availability.perMonth')}
                                    </span>
                                    {cfgOrigMonthly !== null &&
                                      cfgOrigMonthly >
                                        cfgMonthlyPrice + 0.01 && (
                                        <span className="line-through text-[10px] text-cyber-muted ml-1">
                                          {cfgSymbol}
                                          {cfgOrigMonthly.toFixed(2)}
                                        </span>
                                      )}
                                    {cfgMode &&
                                      cfgMode.intervalMonths > 1 &&
                                      cfgMode.periodPrice > 0 && (
                                        <div className="text-[10px] text-cyber-muted">
                                          {t('billing.periodTotal', {
                                            price: `${cfgSymbol}${cfgMode.periodPrice.toFixed(
                                              2
                                            )}`,
                                            months: cfgMode.intervalMonths,
                                          })}
                                        </div>
                                      )}
                                  </div>
                                )}
                            </div>
                          </div>

                          {cfgVisibleDcs.length > 0 && (
                            <div className="flex flex-wrap items-center gap-1.5 pl-6">
                              {cfgVisibleDcs.map(([dcCode, st]) => {
                                const norm =
                                  normalizeDatacenterCode(dcCode) || dcCode;
                                const inStock = isAvailStatus(st);
                                return (
                                  <span
                                    key={norm}
                                    className={`px-2 py-0.5 rounded border text-[10px] font-medium inline-flex items-center gap-1 ${
                                      inStock
                                        ? 'bg-emerald-500/10 border-emerald-500/30 text-emerald-300'
                                        : 'bg-slate-800/50 border-slate-700 text-slate-400'
                                    }`}
                                  >
                                    <DCLabel code={norm} mode="compact" />
                                    <span
                                      className={
                                        inStock
                                          ? 'text-emerald-400 font-semibold'
                                          : 'text-red-400/70'
                                      }
                                    >
                                      {inStock
                                        ? st
                                        : locale === 'zh-CN'
                                        ? '无'
                                        : 'Out'}
                                    </span>
                                  </span>
                                );
                              })}
                            </div>
                          )}
                        </div>
                      );
                    })}
                  </div>
                </div>

                {/* Datacenter Selection Section (for the selected configuration) */}
                {visibleDcForActiveCfg.length > 0 && (
                  <div className="space-y-2 pt-2 border-t border-cyber-grid/40">
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <span className="text-xs font-bold text-cyber-accent">
                        {t('catalog.dcSelectionTitle')}
                      </span>
                      <div className="flex items-center gap-2">
                        <button
                          type="button"
                          onClick={() => {
                            const inStockList =
                              activeCfg && activeCfg.inStockDcs.length > 0
                                ? activeCfg.inStockDcs
                                : allInStockDcs;
                            setSelectedDcsMap((prev) => ({
                              ...prev,
                              [modalServer.planCode]: inStockList,
                            }));
                          }}
                          className="text-[11px] text-cyber-accent hover:underline"
                        >
                          {t('catalog.selectAllInStockDcs')}
                        </button>
                        <span className="text-cyber-grid">|</span>
                        <button
                          type="button"
                          onClick={() =>
                            setSelectedDcsMap((prev) => ({
                              ...prev,
                              [modalServer.planCode]: [],
                            }))
                          }
                          className="text-[11px] text-cyber-muted hover:text-cyber-text"
                        >
                          {t('catalog.clearSelectedDcs')}
                        </button>
                      </div>
                    </div>

                    <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-2">
                      {visibleDcForActiveCfg.map(([dcCode, st]) => {
                        const norm = normalizeDatacenterCode(dcCode) || dcCode;
                        const inStock = isAvailStatus(st);
                        const isDcSelected =
                          selectedDcsForModal.includes(norm);

                        return (
                          <button
                            key={norm}
                            type="button"
                            onClick={() =>
                              toggleDcForServer(modalServer.planCode, norm)
                            }
                            className={`px-3 py-2 rounded-lg border text-xs font-medium flex items-center justify-between gap-2 transition-all ${
                              isDcSelected
                                ? 'bg-cyber-accent/20 border-cyber-accent text-cyber-text ring-1 ring-cyber-accent/50'
                                : inStock
                                ? 'bg-emerald-500/10 border-emerald-500/30 text-emerald-300 hover:border-emerald-400'
                                : 'bg-cyber-bg/50 border-cyber-grid text-cyber-muted hover:border-cyber-accent/30'
                            }`}
                          >
                            <span className="flex items-center gap-1.5 min-w-0 truncate">
                              {isDcSelected && (
                                <Check
                                  size={12}
                                  strokeWidth={3}
                                  className="text-cyber-accent flex-shrink-0"
                                />
                              )}
                              <DCLabel code={norm} mode="full" />
                            </span>
                            <span
                              className={`text-[10px] font-mono px-1.5 py-0.5 rounded flex-shrink-0 ${
                                inStock
                                  ? 'bg-emerald-500/20 text-emerald-300 font-semibold'
                                  : 'bg-red-500/15 text-red-400/80'
                              }`}
                            >
                              {inStock
                                ? st
                                : locale === 'zh-CN'
                                ? '无货'
                                : 'Out'}
                            </span>
                          </button>
                        );
                      })}
                    </div>
                  </div>
                )}

                {/* Collapsible Advanced Options (<details>) */}
                {hasAdvOptions && (
                  <details className="rounded-lg border border-cyber-grid bg-cyber-bg/30 text-xs">
                    <summary className="px-3.5 py-2.5 cursor-pointer font-semibold text-cyber-muted hover:text-cyber-text flex items-center gap-1.5 select-none">
                      <Settings size={13} className="text-cyber-accent" />
                      <span>{t('catalog.advancedOptions')}</span>
                      {selectedAdvOpts.length > 0 && (
                        <span className="px-1.5 py-0.2 rounded-full bg-cyber-accent/20 text-cyber-accent text-[10px] font-mono">
                          +{selectedAdvOpts.length}
                        </span>
                      )}
                    </summary>
                    <div className="p-3.5 pt-2 border-t border-cyber-grid/50 space-y-3">
                      {advOptions.bandwidth.length > 0 && (
                        <div>
                          <div className="text-[11px] font-semibold text-cyber-accent mb-1.5 flex items-center gap-1">
                            <Wifi size={12} />
                            <span>公网带宽 / 流量</span>
                          </div>
                          <div className="flex flex-wrap gap-1.5">
                            {advOptions.bandwidth.map((opt) => {
                              const sel = selectedAdvOpts.includes(opt.value);
                              return (
                                <button
                                  key={opt.value}
                                  type="button"
                                  onClick={() =>
                                    toggleAdvancedOptionForServer(
                                      modalServer,
                                      opt.value,
                                      'bandwidth'
                                    )
                                  }
                                  className={`px-2.5 py-1 rounded border text-xs transition-colors ${
                                    sel
                                      ? 'bg-cyber-accent/20 border-cyber-accent text-cyber-accent font-semibold'
                                      : 'bg-cyber-bg border-cyber-grid text-cyber-muted hover:text-cyber-text'
                                  }`}
                                >
                                  {formatServerSpec(opt.value, '带宽')}
                                </button>
                              );
                            })}
                          </div>
                        </div>
                      )}

                      {advOptions.vrack.length > 0 && (
                        <div>
                          <div className="text-[11px] font-semibold text-cyber-accent mb-1.5 flex items-center gap-1">
                            <ArrowRightLeft size={12} />
                            <span>vRack 内网带宽</span>
                          </div>
                          <div className="flex flex-wrap gap-1.5">
                            {advOptions.vrack.map((opt) => {
                              const sel = selectedAdvOpts.includes(opt.value);
                              return (
                                <button
                                  key={opt.value}
                                  type="button"
                                  onClick={() =>
                                    toggleAdvancedOptionForServer(
                                      modalServer,
                                      opt.value,
                                      'vrack'
                                    )
                                  }
                                  className={`px-2.5 py-1 rounded border text-xs transition-colors ${
                                    sel
                                      ? 'bg-cyber-accent/20 border-cyber-accent text-cyber-accent font-semibold'
                                      : 'bg-cyber-bg border-cyber-grid text-cyber-muted hover:text-cyber-text'
                                  }`}
                                >
                                  {formatServerSpec(opt.value, '内网带宽')}
                                </button>
                              );
                            })}
                          </div>
                        </div>
                      )}
                    </div>
                  </details>
                )}
              </div>

              {/* Modal Footer */}
              <div className="px-5 py-3.5 border-t border-cyber-grid/60 bg-cyber-bg/60 space-y-2.5">
                {(() => {
                  const periodPrice =
                    activeModeEntry?.periodPrice ??
                    activeCfg?.estimatedMonthlyPrice ??
                    0;
                  const origPeriodPrice =
                    activeModeEntry?.originalPeriodPrice ??
                    activeCfg?.originalMonthlyPrice ??
                    periodPrice;
                  const monthlyEq =
                    activeModeEntry?.monthlyEquivalentPrice ??
                    activeCfg?.estimatedMonthlyPrice ??
                    0;
                  const origMonthlyEq =
                    activeModeEntry?.originalMonthlyPrice ??
                    activeCfg?.originalMonthlyPrice ??
                    monthlyEq;
                  const setupFee =
                    typeof activeModeEntry?.setupFee === 'number'
                      ? activeModeEntry.setupFee
                      : activeCfg?.setupFee ?? 0;
                  const origSetupFee =
                    typeof activeModeEntry?.originalSetupFee === 'number'
                      ? activeModeEntry.originalSetupFee
                      : activeCfg?.originalSetupFee ?? setupFee;
                  const isFreeSetup = Boolean(
                    activeModeEntry?.freeSetup ||
                      activeCfg?.freeSetup ||
                      setupFee === 0
                  );
                  const firstTotal = Number((periodPrice + setupFee).toFixed(2));

                  return (
                    <div className="flex flex-col lg:flex-row lg:items-center justify-between gap-3">
                      <div className="space-y-1 text-xs min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-cyber-muted">
                            {t('catalog.selectedConfigSummary')}:
                          </span>
                          <span className="font-semibold text-cyber-text truncate">
                            {activeCfg?.label || modalServer.planCode}
                          </span>
                          {selectedCoupon.trim() && (
                            <span className="px-1.5 py-0.2 rounded bg-amber-500/15 text-amber-300 border border-amber-500/35 text-[10px] font-mono">
                              {t('coupon.badge', {
                                coupon: selectedCoupon.trim(),
                              })}
                            </span>
                          )}
                        </div>

                        {!isPresale && periodPrice > 0 && (
                          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] font-mono">
                            <span>
                              <span className="text-cyber-muted font-sans">
                                {t('billing.periodFeeLabel')}:{' '}
                              </span>
                              <span className="text-emerald-400 font-bold">
                                {symbol}
                                {periodPrice.toFixed(2)}
                              </span>
                              {origPeriodPrice > periodPrice + 0.01 && (
                                <span className="line-through text-cyber-muted ml-1">
                                  {symbol}
                                  {origPeriodPrice.toFixed(2)}
                                </span>
                              )}
                              <span className="text-cyber-muted ml-1">
                                (
                                {t('billing.monthlyEquivalent', {
                                  price: `${symbol}${monthlyEq.toFixed(2)}`,
                                })}
                                {origMonthlyEq > monthlyEq + 0.01
                                  ? ` / ${symbol}${origMonthlyEq.toFixed(2)}`
                                  : ''}
                                )
                              </span>
                            </span>

                            <span>
                              <span className="text-cyber-muted font-sans">
                                {t('promo.setupFeeLabel')}:{' '}
                              </span>
                              {isFreeSetup ? (
                                <span className="text-emerald-300 font-semibold">
                                  {t('promo.freeSetupWithZero', {
                                    zero: `${symbol}0.00`,
                                  })}
                                  {origSetupFee > 0 && (
                                    <span className="line-through text-cyber-muted ml-1">
                                      {symbol}
                                      {origSetupFee.toFixed(2)}
                                    </span>
                                  )}
                                </span>
                              ) : (
                                <span className="text-cyber-text">
                                  {symbol}
                                  {setupFee.toFixed(2)}
                                </span>
                              )}
                            </span>

                            <span>
                              <span className="text-cyber-muted font-sans">
                                {t('billing.firstPaymentTotalLabel')}:{' '}
                              </span>
                              <span className="text-cyber-accent font-bold text-xs">
                                {symbol}
                                {firstTotal.toFixed(2)}
                              </span>
                            </span>
                          </div>
                        )}
                      </div>

                      <div className="flex flex-wrap items-center justify-end gap-2 flex-shrink-0">
                        <button
                          type="button"
                          onClick={() =>
                            setQuickMonitorTarget({
                              server: modalServer,
                              initialConfig: activeCfg
                                ? {
                                    configKey: activeCfg.configKey,
                                    options: activeCfg.options,
                                    label: activeCfg.label,
                                    estimatedMonthlyPrice:
                                      monthlyEq > 0
                                        ? monthlyEq
                                        : activeCfg.estimatedMonthlyPrice ??
                                          undefined,
                                    originalMonthlyPrice:
                                      origMonthlyEq > 0
                                        ? origMonthlyEq
                                        : activeCfg.originalMonthlyPrice ??
                                          undefined,
                                    setupFee,
                                    originalSetupFee: origSetupFee,
                                    freeSetup: isFreeSetup,
                                    hasPromo: Boolean(
                                      activeModeEntry?.hasPromo ||
                                        activeCfg.hasPromo
                                    ),
                                    promoCode:
                                      activeModeEntry?.promoCode ||
                                      activeCfg.promoCode,
                                    promoBadge:
                                      activeModeEntry?.promoBadge ||
                                      activeCfg.promoBadge,
                                    promoDescription:
                                      activeModeEntry?.promoDescription ||
                                      activeCfg.promoDescription,
                                    pricingModes: activeCfg.pricingModes,
                                    pricingMode:
                                      activeModeEntry?.mode ||
                                      selectedPricingMode,
                                    duration:
                                      activeModeEntry?.duration ||
                                      resolvePricingModeDuration(
                                        selectedPricingMode,
                                        activeCfg.pricingModes
                                      ),
                                    coupon: selectedCoupon.trim() || undefined,
                                    currency: activeCfg.currency,
                                    memory: activeCfg.memory,
                                    storage: activeCfg.storage,
                                  }
                                : null,
                              initialConfigs: unifiedConfigs.map((c) => ({
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
                                datacenters: c.dcStatuses,
                                inStockCount: c.inStockDcs.length,
                              })),
                              initialSelectedDcs: selectedDcsForModal,
                            })
                          }
                          disabled={!isAuthenticated}
                          className="px-3 py-1.5 rounded-md bg-blue-500/15 hover:bg-blue-500/25 text-blue-400 border border-blue-500/35 text-xs font-semibold flex items-center gap-1.5 disabled:opacity-50 transition-colors"
                        >
                          <Bell size={13} />
                          <span>{t('catalog.monitorThisConfig')}</span>
                        </button>

                        <button
                          type="button"
                          onClick={() =>
                            handleOpenAddToQueue(
                              modalServer,
                              activeCfg,
                              activeCfg?.inStockDcs || allInStockDcs
                            )
                          }
                          disabled={
                            !isAuthenticated || isPresale || isAnnounced
                          }
                          className="px-3 py-1.5 rounded-md bg-cyber-accent/15 hover:bg-cyber-accent/25 text-cyber-accent border border-cyber-accent/40 text-xs font-semibold flex items-center gap-1.5 disabled:opacity-50 transition-colors"
                        >
                          <ShoppingCart size={13} />
                          <span>{t('catalog.addToQueueBtn')}</span>
                        </button>

                        <button
                          type="button"
                          onClick={() => quickOrderFromModal(modalServer)}
                          disabled={!canQuickOrder || isQuickOrdering}
                          className="px-3.5 py-1.5 rounded-md bg-emerald-500 hover:bg-emerald-600 text-white text-xs font-semibold flex items-center gap-1.5 disabled:opacity-50 transition-colors shadow-sm"
                        >
                          <Zap size={13} />
                          <span>
                            {isQuickOrdering
                              ? t('catalog.quickOrdering')
                              : t('catalog.quickOrderNow')}
                          </span>
                        </button>
                      </div>
                    </div>
                  );
                })()}
              </div>
            </div>
          </div>
        );
      })()}

      <QuickMonitorDialog
        open={Boolean(quickMonitorTarget)}
        onOpenChange={(open) => {
          if (!open) setQuickMonitorTarget(null);
        }}
        planCode={quickMonitorTarget?.server.planCode || ''}
        serverName={quickMonitorTarget?.server.name}
        listingState={
          quickMonitorTarget
            ? resolveServerListingState(quickMonitorTarget.server)
            : 'listed'
        }
        isPresale={
          quickMonitorTarget
            ? resolveServerListingState(quickMonitorTarget.server) === 'presale'
            : false
        }
        initialDatacenters={quickMonitorTarget?.server.datacenters}
        initialSelectedDcs={quickMonitorTarget?.initialSelectedDcs}
        initialConfig={quickMonitorTarget?.initialConfig}
        initialConfigs={quickMonitorTarget?.initialConfigs}
        autoAppliedCodes={promotions?.autoAppliedCodes}
        onSuccess={(planCode) => {
          const nextSet = new Set([...subscribedServersRef.current, planCode]);
          subscribedServersRef.current = nextSet;
          setSubscribedServers(nextSet);
          setServers((prev) => [...prev]);
        }}
      />

      {/* 抢购任务支付与欧盟 14 天撤回权策略确认弹窗 */}
      <AlertDialog
        open={!!pendingQueueTarget}
        onOpenChange={(open) => {
          if (!open) setPendingQueueTarget(null);
        }}
      >
        <AlertDialogContent className="bg-cyber-surface border border-cyber-accent/40 text-cyber-text max-w-lg">
          <AlertDialogHeader>
            <AlertDialogTitle className="text-base font-bold text-cyber-text">
              创建抢购队列任务 — {pendingQueueTarget?.server.planCode}
            </AlertDialogTitle>
            <AlertDialogDescription asChild>
              <div className="space-y-3 pt-2 text-xs text-cyber-muted">
                {pendingQueueTarget?.selectedConfig && (
                  <div className="p-2.5 rounded-md border border-cyber-accent/30 bg-cyber-accent/10 text-cyber-text flex flex-wrap items-center justify-between gap-2">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="text-cyber-muted">已选硬件配置：</span>
                      <span className="font-semibold text-cyber-accent">
                        {pendingQueueTarget.selectedConfig.label}
                      </span>
                      {pendingQueueTarget.pricingMode && (
                        <span className="px-1.5 py-0.2 rounded bg-cyber-grid/50 text-cyber-text text-[10px] font-semibold">
                          {t(
                            `billing.modeShort.${pendingQueueTarget.pricingMode}`
                          )}
                        </span>
                      )}
                      {pendingQueueTarget.coupon && (
                        <span className="px-1.5 py-0.2 rounded bg-amber-500/15 text-amber-300 border border-amber-500/35 text-[10px] font-mono">
                          {t('coupon.badge', {
                            coupon: pendingQueueTarget.coupon,
                          })}
                        </span>
                      )}
                    </div>
                    {(() => {
                      const modeEntry = resolvePricingModeEntry(
                        pendingQueueTarget.selectedConfig.pricingModes,
                        pendingQueueTarget.pricingMode
                      );
                      const moPrice =
                        modeEntry?.monthlyEquivalentPrice ??
                        pendingQueueTarget.selectedConfig.estimatedMonthlyPrice;
                      if (!moPrice || moPrice <= 0) return null;
                      return (
                        <span className="font-mono font-bold text-emerald-400">
                          {getCurrencySymbol(
                            pendingQueueTarget.selectedConfig.currency
                          )}
                          {moPrice.toFixed(2)}
                          {t('availability.perMonth')}
                        </span>
                      );
                    })()}
                  </div>
                )}

                <div className="flex flex-wrap items-center gap-1">
                  <span>已选机房优先级：</span>
                  {pendingQueueTarget?.datacenters.map((d, idx) => (
                    <span
                      key={d}
                      className="inline-flex items-center gap-1 font-mono text-cyber-accent"
                    >
                      {idx > 0 && <span>&gt;</span>}
                      <DCLabel code={d} mode="compact" />
                    </span>
                  ))}
                </div>

                <div className="space-y-2.5 pt-1">
                  <label className="flex items-start gap-2.5 p-3 rounded-md border border-cyber-grid bg-cyber-bg/60 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={queueAutoPay}
                      onChange={(e) => setQueueAutoPay(e.target.checked)}
                      className="mt-0.5 h-4 w-4 accent-cyber-accent"
                    />
                    <div className="space-y-0.5">
                      <div className="font-semibold text-cyber-text flex items-center gap-1.5">
                        <span>抢购命中后自动扣款 (Auto-Pay)</span>
                        <span className="text-[10px] px-1.5 py-0.2 rounded bg-amber-500/15 text-amber-400 border border-amber-500/30">
                          需 2FA
                        </span>
                      </div>
                      <p className="text-[11px] text-cyber-muted leading-relaxed">
                        默认关闭（仅生成未付款订单，需手动前往官网或面板付款）。勾选后将调用
                        OVH 默认支付方式尝试自动扣款。
                      </p>
                    </div>
                  </label>

                  <label className="flex items-start gap-2.5 p-3 rounded-md border border-cyber-grid bg-cyber-bg/60 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={queueWaiveRetractation}
                      onChange={(e) =>
                        setQueueWaiveRetractation(e.target.checked)
                      }
                      className="mt-0.5 h-4 w-4 accent-cyber-accent"
                    />
                    <div className="space-y-0.5">
                      <div className="font-semibold text-cyber-text flex items-center gap-1.5">
                        <span>
                          放弃欧盟 14 天无理由撤回权以加速交付 (
                          <code>waiveRetractationPeriod</code>)
                        </span>
                        <span className="text-[10px] px-1.5 py-0.2 rounded bg-amber-500/15 text-amber-400 border border-amber-500/30">
                          需 2FA
                        </span>
                      </div>
                      <p className="text-[11px] text-cyber-muted leading-relaxed">
                        默认关闭（保留欧盟消费者 14 天法定撤回权）。若希望付款后立即装机且自愿放弃无理由撤回权，请勾选此项。
                      </p>
                    </div>
                  </label>
                </div>
              </div>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel className="bg-cyber-bg border-cyber-grid text-cyber-muted hover:text-cyber-text text-xs">
              取消
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault();
                confirmAddToQueue();
              }}
              className="bg-cyber-accent text-white hover:opacity-90 text-xs font-semibold"
            >
              确认加入抢购队列
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
};

export default ServersPage;
