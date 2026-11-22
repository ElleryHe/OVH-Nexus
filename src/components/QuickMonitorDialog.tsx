import React, { useState, useEffect, useMemo } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate } from 'react-router-dom';
import { motion, AnimatePresence } from 'framer-motion';
import { toast } from 'sonner';
import {
  Bell,
  X,
  Check,
  Sparkles,
  ShoppingCart,
  AlertTriangle,
  Cpu,
  RefreshCw,
  ExternalLink,
  Plus,
  Trash2,
  ShieldAlert,
} from 'lucide-react';
import { api } from '@/utils/apiClient';
import { useLocale } from '@/context/LocaleContext';
import { DCLabel } from '@/components/DCLabel';
import {
  normalizeDatacenterCode,
  inferPlanGeneration,
  OVH_DATACENTERS,
} from '@/config/ovhConstants';
import {
  useCatalogDictionary,
  resolvePlanDisplay,
  resolveConfigPrice,
  buildConfigKey,
  formatConfigLabel,
  getCurrencySymbol,
  resolvePricingModeDuration,
  synthesizePricingModes,
  resolvePricingModeEntry,
  findAutoAppliedPromoCodeForPlanMode,
} from '@/utils/hardwareFormat';
import type {
  CatalogPromotionsResponse,
  ListingState,
  PlanConfigOption,
  PlanPricingMode,
  StockTarget,
  StockSubscription,
} from '@/types';

export interface QuickMonitorInitialConfig {
  configKey?: string;
  options?: string[];
  label?: string;
  estimatedMonthlyPrice?: number;
  originalMonthlyPrice?: number;
  setupFee?: number;
  originalSetupFee?: number;
  freeSetup?: boolean;
  hasPromo?: boolean;
  promoCode?: string;
  promoBadge?: string;
  promoDescription?: string;
  pricingModes?: PlanPricingMode[];
  pricingMode?: string;
  duration?: string;
  coupon?: string;
  currency?: string;
  memory?: string;
  storage?: string;
}

export interface QuickMonitorDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  planCode: string;
  serverName?: string;
  listingState?: ListingState | string;
  isPresale?: boolean;
  subsidiary?: string;
  initialDatacenters?: { datacenter: string; availability: string }[];
  initialSelectedDcs?: string[];
  initialMemory?: string;
  initialStorage?: string;
  initialConfig?: QuickMonitorInitialConfig | null;
  initialConfigs?: PlanConfigOption[];
  existingSubscription?: StockSubscription | null;
  promotions?: CatalogPromotionsResponse | null;
  onSuccess?: (planCode: string) => void;
}

interface NormalizedConfigChoice {
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
  inStockCount: number;
}

const isAvailStatus = (st?: string): boolean =>
  Boolean(st && st !== 'unavailable' && st !== 'unknown');

export const QuickMonitorDialog: React.FC<QuickMonitorDialogProps> = ({
  open,
  onOpenChange,
  planCode,
  serverName,
  listingState,
  isPresale = false,
  subsidiary,
  initialDatacenters = [],
  initialSelectedDcs = [],
  initialMemory = '',
  initialStorage = '',
  initialConfig = null,
  initialConfigs = [],
  existingSubscription = null,
  promotions = null,
  onSuccess,
}) => {
  const { locale, t } = useLocale();
  const navigate = useNavigate();
  const catalogDict = useCatalogDictionary();

  const [isLoading, setIsLoading] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [isExistingSub, setIsExistingSub] = useState(false);
  const [needsConfigSelection, setNeedsConfigSelection] = useState(false);
  const [monitorRunning, setMonitorRunning] = useState(true);
  const [autoStartMonitor, setAutoStartMonitor] = useState(true);

  const [dcStatusMap, setDcStatusMap] = useState<Record<string, string>>({});
  const [configChoices, setConfigChoices] = useState<NormalizedConfigChoice[]>([]);
  const [targets, setTargets] = useState<StockTarget[]>([]);

  const planMeta = useMemo(
    () => resolvePlanDisplay(planCode, '', locale, catalogDict),
    [planCode, locale, catalogDict]
  );

  const displayName = serverName || planMeta.name || planCode;
  const generation = inferPlanGeneration(planCode);
  const effectiveListingState: ListingState =
    listingState === 'announced'
      ? 'announced'
      : isPresale || listingState === 'presale'
      ? 'presale'
      : 'listed';

  const normalizeRawConfigOption = (
    raw: any,
    fallbackMeta: ReturnType<typeof resolvePlanDisplay>
  ): NormalizedConfigChoice | null => {
    if (!raw) return null;
    const mem = String(raw.memory || '').trim();
    const sto = String(raw.storage || '').trim();
    const cfgKey =
      String(raw.configKey || '').trim() || buildConfigKey(mem, sto);
    if (!cfgKey) return null;

    const opts: string[] = Array.isArray(raw.options) && raw.options.length > 0
      ? raw.options.filter(Boolean)
      : [mem, sto].filter((x) => x && x !== 'N/A' && x !== 'none');

    const label =
      String(raw.label || '').trim() ||
      formatConfigLabel(mem, sto, locale);

    const priceCalc = resolveConfigPrice(fallbackMeta, {
      memory: mem,
      storage: sto,
      systemStorage: raw.systemStorage,
      gpu: raw.gpu,
    });

    const estPrice =
      typeof raw.estimatedMonthlyPrice === 'number' && raw.estimatedMonthlyPrice > 0
        ? raw.estimatedMonthlyPrice
        : typeof raw.price === 'number' && raw.price > 0
        ? raw.price
        : priceCalc.totalPrice;

    const originalMonthlyPrice =
      typeof raw.originalMonthlyPrice === 'number' && raw.originalMonthlyPrice > 0
        ? raw.originalMonthlyPrice
        : typeof raw.originalPrice === 'number' && raw.originalPrice > 0
        ? raw.originalPrice
        : raw.isDefault
        ? fallbackMeta.originalMonthlyPrice ?? null
        : null;

    const setupFee =
      typeof raw.setupFee === 'number'
        ? raw.setupFee
        : typeof fallbackMeta.setupFee === 'number'
        ? fallbackMeta.setupFee
        : null;

    const originalSetupFee =
      typeof raw.originalSetupFee === 'number'
        ? raw.originalSetupFee
        : typeof fallbackMeta.originalSetupFee === 'number'
        ? fallbackMeta.originalSetupFee
        : null;

    const freeSetupPromo =
      setupFee === 0 && (originalSetupFee ?? 0) > 0;
    const freeSetup = Boolean(
      raw.freeSetup || setupFee === 0 || freeSetupPromo
    );

    const hasPromo = Boolean(
      raw.hasPromo ||
        (Array.isArray(raw.pricingModes) &&
          raw.pricingModes.some((m: any) => m.hasPromo)) ||
        freeSetupPromo ||
        (originalMonthlyPrice &&
          estPrice &&
          originalMonthlyPrice > estPrice + 0.01)
    );

    const promoCode = raw.promoCode || fallbackMeta.promoCode;
    const promoBadge = raw.promoBadge || fallbackMeta.promoBadge;
    const promoDescription =
      raw.promoDescription || fallbackMeta.promoDescription;

    const pricingModes = synthesizePricingModes({
      pricingModes:
        Array.isArray(raw.pricingModes) && raw.pricingModes.length > 0
          ? raw.pricingModes
          : fallbackMeta.pricingModes,
      estimatedMonthlyPrice: estPrice ?? null,
      originalMonthlyPrice,
      setupFee,
      originalSetupFee,
      freeSetup,
      hasPromo,
      promoCode,
      promoBadge,
      promoDescription,
    });

    const currency =
      raw.currency || fallbackMeta.currency || priceCalc.currency || 'EUR';

    const dcStatuses: Record<string, string> = {};
    if (Array.isArray(raw.datacenters)) {
      raw.datacenters.forEach((d: any) => {
        const norm = normalizeDatacenterCode(d?.datacenter || '');
        if (norm) {
          dcStatuses[norm] = d.availability || 'unknown';
        }
      });
    } else if (raw.datacenters && typeof raw.datacenters === 'object') {
      Object.entries(raw.datacenters).forEach(([dc, st]) => {
        const norm = normalizeDatacenterCode(dc);
        if (norm && typeof st === 'string') {
          dcStatuses[norm] = st;
        }
      });
    }

    const inStockCount =
      typeof raw.inStockCount === 'number'
        ? raw.inStockCount
        : Object.values(dcStatuses).filter(isAvailStatus).length;

    return {
      configKey: cfgKey,
      memory: mem,
      storage: sto,
      label,
      options: opts,
      isDefault: Boolean(raw.isDefault),
      estimatedMonthlyPrice: estPrice ?? null,
      originalMonthlyPrice,
      setupFee,
      originalSetupFee,
      freeSetup,
      hasPromo,
      promoCode,
      promoBadge,
      promoDescription,
      pricingModes,
      currency,
      dcStatuses,
      inStockCount,
    };
  };

  useEffect(() => {
    if (!open || !planCode) return;

    const seedMap: Record<string, string> = {};
    initialDatacenters.forEach((d) => {
      const code = normalizeDatacenterCode(d.datacenter);
      if (code) {
        const prev = seedMap[code];
        if (!prev || isAvailStatus(d.availability)) {
          seedMap[code] = d.availability || 'unknown';
        }
      }
    });
    setDcStatusMap(seedMap);

    let cancelled = false;
    const fetchAll = async () => {
      setIsLoading(true);
      try {
        const [planConfigsRes, subsRes, statusRes, availMapRes, rawAvailRes] =
          await Promise.allSettled([
            api.getPlanConfigs(planCode, subsidiary),
            api.get('/monitor/subscriptions'),
            api.get('/monitor/status'),
            api.get(`/availability/${encodeURIComponent(planCode)}`),
            api.get('/availability', { params: { planCode } }),
          ]);

        if (cancelled) return;

        const mergedDcMap: Record<string, string> = { ...seedMap };
        if (
          availMapRes.status === 'fulfilled' &&
          availMapRes.value?.data &&
          typeof availMapRes.value.data === 'object' &&
          !Array.isArray(availMapRes.value.data)
        ) {
          Object.entries(availMapRes.value.data).forEach(([dc, st]) => {
            const norm = normalizeDatacenterCode(dc);
            if (norm && typeof st === 'string') {
              mergedDcMap[norm] = st;
            }
          });
        }

        const choiceMap = new Map<string, NormalizedConfigChoice>();

        // 1. Prefer GET /api/catalog/plans/{planCode}/configs
        if (
          planConfigsRes.status === 'fulfilled' &&
          Array.isArray(planConfigsRes.value?.data?.configs)
        ) {
          planConfigsRes.value.data.configs.forEach((c: any) => {
            const norm = normalizeRawConfigOption(c, planMeta);
            if (norm) {
              choiceMap.set(norm.configKey, norm);
              Object.entries(norm.dcStatuses).forEach(([dc, st]) => {
                if (!mergedDcMap[dc] || isAvailStatus(st)) {
                  mergedDcMap[dc] = st;
                }
              });
            }
          });
        }

        // 2. Fallback / merge with caller-supplied initialConfigs
        if (choiceMap.size === 0 && Array.isArray(initialConfigs)) {
          initialConfigs.forEach((c) => {
            const norm = normalizeRawConfigOption(c, planMeta);
            if (norm && !choiceMap.has(norm.configKey)) {
              choiceMap.set(norm.configKey, norm);
              Object.entries(norm.dcStatuses).forEach(([dc, st]) => {
                if (!mergedDcMap[dc] || isAvailStatus(st)) {
                  mergedDcMap[dc] = st;
                }
              });
            }
          });
        }

        // 3. Fallback / merge with raw availability rows
        if (
          rawAvailRes.status === 'fulfilled' &&
          Array.isArray(rawAvailRes.value?.data)
        ) {
          const matchingRows = rawAvailRes.value.data.filter(
            (it: any) =>
              it &&
              typeof it.planCode === 'string' &&
              it.planCode.toLowerCase() === planCode.toLowerCase()
          );
          matchingRows.forEach((row: any) => {
            const norm = normalizeRawConfigOption(row, planMeta);
            if (norm) {
              const existing = choiceMap.get(norm.configKey);
              if (!existing) {
                choiceMap.set(norm.configKey, norm);
              } else {
                Object.entries(norm.dcStatuses).forEach(([dc, st]) => {
                  if (!existing.dcStatuses[dc] || isAvailStatus(st)) {
                    existing.dcStatuses[dc] = st;
                  }
                });
                existing.inStockCount = Object.values(existing.dcStatuses).filter(
                  isAvailStatus
                ).length;
              }
              Object.entries(norm.dcStatuses).forEach(([dc, st]) => {
                if (!mergedDcMap[dc] || isAvailStatus(st)) {
                  mergedDcMap[dc] = st;
                }
              });
            }
          });
        }

        // Ensure initialConfig passed by caller is present in choices if specified
        if (initialConfig?.configKey && !choiceMap.has(initialConfig.configKey)) {
          const normInit = normalizeRawConfigOption(initialConfig, planMeta);
          if (normInit) {
            choiceMap.set(normInit.configKey, normInit);
          }
        }

        const sortedChoices = Array.from(choiceMap.values()).sort((a, b) => {
          if (a.isDefault && !b.isDefault) return -1;
          if (!a.isDefault && b.isDefault) return 1;
          if (b.inStockCount !== a.inStockCount) {
            return b.inStockCount - a.inStockCount;
          }
          return (a.estimatedMonthlyPrice ?? 999999) - (b.estimatedMonthlyPrice ?? 999999);
        });

        setDcStatusMap(mergedDcMap);
        setConfigChoices(sortedChoices);

        // Determine initial preselected config if caller passed one
        let resolvedInitialChoice: NormalizedConfigChoice | undefined;
        if (initialConfig?.configKey) {
          resolvedInitialChoice = sortedChoices.find(
            (c) => c.configKey === initialConfig.configKey
          );
        } else if (initialMemory || initialStorage) {
          const candKey = buildConfigKey(initialMemory, initialStorage);
          resolvedInitialChoice = sortedChoices.find(
            (c) =>
              c.configKey === candKey ||
              ((!initialMemory || c.memory === initialMemory) &&
                (!initialStorage || c.storage === initialStorage))
          );
        }

        const callerDcs = initialSelectedDcs
          .map((d) => normalizeDatacenterCode(d))
          .filter(Boolean);

        // Check existing subscription
        let foundExisting: StockSubscription | null = existingSubscription || null;
        if (
          !foundExisting &&
          subsRes.status === 'fulfilled' &&
          Array.isArray(subsRes.value?.data)
        ) {
          foundExisting =
            subsRes.value.data.find(
              (s: any) =>
                typeof s.planCode === 'string' &&
                s.planCode.toLowerCase() === planCode.toLowerCase()
            ) || null;
        }

        if (foundExisting) {
          setIsExistingSub(true);
          setNeedsConfigSelection(Boolean(foundExisting.needsConfigSelection));

          if (Array.isArray(foundExisting.targets) && foundExisting.targets.length > 0) {
            const loadedTargets: StockTarget[] = foundExisting.targets.map(
              (tItem, idx) => {
                const matchedCfg = sortedChoices.find(
                  (c) => c.configKey === tItem.configKey
                );
                // If legacy auto-order was paused and user opened dialog with a specific config, pre-fill it
                const useChoice =
                  !tItem.configKey &&
                  foundExisting?.needsConfigSelection &&
                  idx === 0 &&
                  resolvedInitialChoice
                    ? resolvedInitialChoice
                    : matchedCfg;

                  const initPricingMode =
                    tItem.pricingMode ||
                    (idx === 0 && initialConfig?.pricingMode) ||
                    'default';
                  const modeEntry = resolvePricingModeEntry(
                    useChoice?.pricingModes,
                    initPricingMode
                  );
                  const resolvedMonthlyCap =
                    typeof tItem.maxMonthlyPrice === 'number' &&
                    tItem.maxMonthlyPrice > 0
                      ? tItem.maxMonthlyPrice
                      : modeEntry && modeEntry.monthlyEquivalentPrice > 0
                      ? modeEntry.monthlyEquivalentPrice
                      : useChoice?.estimatedMonthlyPrice ?? undefined;

                  return {
                    id: tItem.id || `tgt-${idx + 1}`,
                    configKey: useChoice ? useChoice.configKey : tItem.configKey || '',
                    options:
                      useChoice && useChoice.options.length > 0
                        ? useChoice.options
                        : Array.isArray(tItem.options)
                        ? tItem.options
                        : [],
                    label:
                      useChoice?.label ||
                      tItem.label ||
                      '',
                    datacenters: Array.isArray(tItem.datacenters)
                      ? tItem.datacenters
                          .map((d) => normalizeDatacenterCode(d))
                          .filter(Boolean)
                      : [],
                    notifyAvailable: tItem.notifyAvailable !== false,
                    notifyUnavailable: Boolean(tItem.notifyUnavailable),
                    autoOrder: Boolean(tItem.autoOrder),
                    quantity:
                      typeof tItem.quantity === 'number' && tItem.quantity > 0
                        ? tItem.quantity
                        : 1,
                    maxMonthlyPrice: resolvedMonthlyCap,
                    pricingMode: initPricingMode,
                    duration:
                      tItem.duration ||
                      modeEntry?.duration ||
                      resolvePricingModeDuration(
                        initPricingMode,
                        useChoice?.pricingModes
                      ),
                    coupon:
                      tItem.coupon ??
                      (idx === 0 ? initialConfig?.coupon || '' : ''),
                    currency:
                      tItem.currency ||
                      useChoice?.currency ||
                      planMeta.currency ||
                      'EUR',
                    lastStatus: tItem.lastStatus || {},
                  };
                }
              );
              setTargets(loadedTargets);
            } else {
              // Convert legacy top-level subscription into a single StockTarget
              const subDcs = Array.isArray(foundExisting.datacenters)
                ? foundExisting.datacenters
                    .map((d) => normalizeDatacenterCode(d))
                    .filter(Boolean)
                : [];
              const initPricingMode = initialConfig?.pricingMode || 'default';
              const modeEntry = resolvePricingModeEntry(
                resolvedInitialChoice?.pricingModes,
                initPricingMode
              );
              setTargets([
                {
                  id: 'tgt-1',
                  configKey: resolvedInitialChoice?.configKey || '',
                  options: resolvedInitialChoice?.options || [],
                  label: resolvedInitialChoice?.label || '',
                  datacenters: subDcs,
                  notifyAvailable: foundExisting.notifyAvailable !== false,
                  notifyUnavailable: Boolean(foundExisting.notifyUnavailable),
                  autoOrder: Boolean(foundExisting.autoOrder),
                  quantity:
                    typeof foundExisting.autoOrderQuantity === 'number' &&
                    foundExisting.autoOrderQuantity > 0
                      ? foundExisting.autoOrderQuantity
                      : 1,
                  maxMonthlyPrice:
                    modeEntry && modeEntry.monthlyEquivalentPrice > 0
                      ? modeEntry.monthlyEquivalentPrice
                      : resolvedInitialChoice?.estimatedMonthlyPrice ?? undefined,
                  pricingMode: initPricingMode,
                  duration:
                    initialConfig?.duration ||
                    modeEntry?.duration ||
                    resolvePricingModeDuration(
                      initPricingMode,
                      resolvedInitialChoice?.pricingModes
                    ),
                  coupon: initialConfig?.coupon || '',
                  currency:
                    resolvedInitialChoice?.currency || planMeta.currency || 'EUR',
                  lastStatus: foundExisting.lastStatus || {},
                },
              ]);
            }
          } else {
            setIsExistingSub(false);
            setNeedsConfigSelection(false);
            const initPricingMode = initialConfig?.pricingMode || 'default';
            const modeEntry = resolvePricingModeEntry(
              resolvedInitialChoice?.pricingModes || initialConfig?.pricingModes,
              initPricingMode
            );
            setTargets([
              {
                id: 'tgt-1',
                configKey: resolvedInitialChoice?.configKey || initialConfig?.configKey || '',
                options:
                  resolvedInitialChoice?.options ||
                  initialConfig?.options ||
                  [],
                label:
                  resolvedInitialChoice?.label ||
                  initialConfig?.label ||
                  '',
                datacenters: callerDcs,
                notifyAvailable: true,
                notifyUnavailable: false,
                autoOrder: false,
                quantity: 1,
                maxMonthlyPrice:
                  modeEntry && modeEntry.monthlyEquivalentPrice > 0
                    ? modeEntry.monthlyEquivalentPrice
                    : resolvedInitialChoice?.estimatedMonthlyPrice ??
                      initialConfig?.estimatedMonthlyPrice ??
                      undefined,
                pricingMode: initPricingMode,
                duration:
                  initialConfig?.duration ||
                  modeEntry?.duration ||
                  resolvePricingModeDuration(
                    initPricingMode,
                    resolvedInitialChoice?.pricingModes ||
                      initialConfig?.pricingModes
                  ),
                coupon: initialConfig?.coupon || '',
                currency:
                  resolvedInitialChoice?.currency ||
                  initialConfig?.currency ||
                  planMeta.currency ||
                  'EUR',
                lastStatus: {},
              },
            ]);
          }

        if (statusRes.status === 'fulfilled' && statusRes.value?.data) {
          const running = Boolean(statusRes.value.data.running);
          setMonitorRunning(running);
          setAutoStartMonitor(!running);
        }
      } finally {
        if (!cancelled) {
          setIsLoading(false);
        }
      }
    };

    fetchAll();
    return () => {
      cancelled = true;
    };
  }, [open, planCode, subsidiary]);

  const availableDcCodes = useMemo(() => {
    const keys = Object.keys(dcStatusMap);
    if (keys.length > 0) return keys;
    return OVH_DATACENTERS.map((d) => d.code);
  }, [dcStatusMap]);

  const getTargetDcStatuses = (target: StockTarget): Record<string, string> => {
    if (!target.configKey) return dcStatusMap;
    const found = configChoices.find((c) => c.configKey === target.configKey);
    if (!found || Object.keys(found.dcStatuses).length === 0) return dcStatusMap;
    const merged: Record<string, string> = {};
    availableDcCodes.forEach((dc) => {
      merged[dc] = found.dcStatuses[dc] || 'unavailable';
    });
    return merged;
  };

  const updateTarget = (index: number, patch: Partial<StockTarget>) => {
    setTargets((prev) =>
      prev.map((item, i) => (i === index ? { ...item, ...patch } : item))
    );
  };

  const handleSelectConfigForTarget = (index: number, configKey: string) => {
    if (!configKey) {
      updateTarget(index, {
        configKey: '',
        options: [],
        label: '',
      });
      return;
    }
    const chosen = configChoices.find((c) => c.configKey === configKey);
    if (!chosen) return;
    const currentMode = targets[index]?.pricingMode || 'default';
    const modeEntry = resolvePricingModeEntry(chosen.pricingModes, currentMode);
    const nextMode = modeEntry?.mode || currentMode;
    const nextDuration =
      modeEntry?.duration ||
      resolvePricingModeDuration(nextMode, chosen.pricingModes);
    const nextMonthlyEq =
      modeEntry && modeEntry.monthlyEquivalentPrice > 0
        ? modeEntry.monthlyEquivalentPrice
        : chosen.estimatedMonthlyPrice;

    updateTarget(index, {
      configKey: chosen.configKey,
      options: chosen.options,
      label: chosen.label,
      pricingMode: nextMode,
      duration: nextDuration,
      currency: chosen.currency || planMeta.currency || 'EUR',
      maxMonthlyPrice:
        nextMonthlyEq && nextMonthlyEq > 0
          ? Number(nextMonthlyEq.toFixed(2))
          : targets[index]?.maxMonthlyPrice,
    });
  };

  const handleSelectPricingModeForTarget = (index: number, mode: string) => {
    const tgt = targets[index];
    const chosen =
      configChoices.find((c) => c.configKey === tgt?.configKey) ||
      configChoices.find((c) => c.isDefault) ||
      configChoices[0];
    const modeEntry = resolvePricingModeEntry(chosen?.pricingModes, mode);
    const nextDuration =
      modeEntry?.duration ||
      resolvePricingModeDuration(mode, chosen?.pricingModes);
    const nextMonthlyEq =
      modeEntry && modeEntry.monthlyEquivalentPrice > 0
        ? modeEntry.monthlyEquivalentPrice
        : chosen?.estimatedMonthlyPrice;

    updateTarget(index, {
      pricingMode: mode,
      duration: nextDuration,
      maxMonthlyPrice:
        nextMonthlyEq && nextMonthlyEq > 0
          ? Number(nextMonthlyEq.toFixed(2))
          : tgt?.maxMonthlyPrice,
    });
  };

  const toggleTargetDc = (index: number, dcCode: string) => {
    const norm = normalizeDatacenterCode(dcCode);
    const current = targets[index]?.datacenters || [];
    // When current is empty (all DCs monitored), clicking one toggles from all-selected state
    const effectiveList =
      current.length === 0 ? [...availableDcCodes] : [...current];
    const next = effectiveList.includes(norm)
      ? effectiveList.filter((d) => d !== norm)
      : [...effectiveList, norm];
    updateTarget(index, { datacenters: next });
  };

  const addTarget = () => {
    const defaultChoice =
      configChoices.find((c) => c.isDefault) || configChoices[0];
    const modeEntry = resolvePricingModeEntry(
      defaultChoice?.pricingModes,
      'default'
    );
    setTargets((prev) => [
      ...prev,
      {
        id: `tgt-${Date.now().toString(36)}-${prev.length + 1}`,
        configKey: defaultChoice?.configKey || '',
        options: defaultChoice?.options || [],
        label: defaultChoice?.label || '',
        datacenters: [],
        notifyAvailable: true,
        notifyUnavailable: false,
        autoOrder: false,
        quantity: 1,
        maxMonthlyPrice:
          modeEntry && modeEntry.monthlyEquivalentPrice > 0
            ? modeEntry.monthlyEquivalentPrice
            : defaultChoice?.estimatedMonthlyPrice ?? undefined,
        pricingMode: 'default',
        duration: modeEntry?.duration || 'P1M',
        coupon: '',
        currency: defaultChoice?.currency || planMeta.currency || 'EUR',
        lastStatus: {},
      },
    ]);
  };

  const removeTarget = (index: number) => {
    setTargets((prev) => (prev.length > 1 ? prev.filter((_, i) => i !== index) : prev));
  };

  const getStatusBadge = (status?: string) => {
    if (!status || status === 'unknown') {
      return {
        label:
          locale === 'zh-CN' ? '未知' : locale === 'fr' ? 'Inconnu' : 'Unknown',
        cls: 'bg-slate-500/15 text-slate-400 border-slate-500/30',
      };
    }
    if (status === 'unavailable') {
      return {
        label:
          locale === 'zh-CN'
            ? '无货'
            : locale === 'fr'
            ? 'Rupture'
            : 'Out of Stock',
        cls: 'bg-red-500/15 text-red-400 border-red-500/30',
      };
    }
    return {
      label: status,
      cls: 'bg-emerald-500/15 text-emerald-400 border-emerald-500/30',
    };
  };

  const hasInvalidAutoOrderTarget = useMemo(
    () =>
      targets.some(
        (tgt) =>
          tgt.autoOrder && (!tgt.configKey || !tgt.options || tgt.options.length === 0)
      ),
    [targets]
  );

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!planCode) return;

    if (hasInvalidAutoOrderTarget) {
      toast.error(t('quickMonitor.autoOrderConfigRequiredWarning'));
      return;
    }

    setIsSaving(true);
    try {
      const normalizedTargets: StockTarget[] = targets.map((tgt, idx) => {
        const isAllSelected =
          tgt.datacenters.length === 0 ||
          tgt.datacenters.length >= availableDcCodes.length;
        const chosen = configChoices.find((c) => c.configKey === tgt.configKey);
        const mode = (tgt.pricingMode || 'default').trim();
        const duration =
          tgt.duration ||
          resolvePricingModeDuration(mode, chosen?.pricingModes);
        let trimmedCoupon = (tgt.coupon || '').trim();
        if (trimmedCoupon) {
          const modePool = [
            ...(chosen?.pricingModes || []),
            ...(planMeta.pricingModes || []),
          ];
          const matchedAuto = findAutoAppliedPromoCodeForPlanMode(
            planCode,
            mode,
            trimmedCoupon,
            modePool,
            promotions
          );
          if (matchedAuto) {
            toast.info(t('promoDebug.autoCouponStripped', { code: matchedAuto }));
            trimmedCoupon = '';
          }
        }
        return {
          id: tgt.id || `tgt-${idx + 1}`,
          configKey: tgt.configKey || '',
          options: tgt.configKey ? tgt.options : [],
          label: tgt.configKey ? tgt.label : '',
          datacenters: isAllSelected ? [] : tgt.datacenters,
          notifyAvailable: tgt.notifyAvailable,
          notifyUnavailable: tgt.notifyUnavailable,
          autoOrder: Boolean(tgt.autoOrder),
          quantity: Math.max(1, Number(tgt.quantity) || 1),
          maxMonthlyPrice:
            tgt.maxMonthlyPrice && Number(tgt.maxMonthlyPrice) > 0
              ? Number(tgt.maxMonthlyPrice)
              : undefined,
          pricingMode: mode,
          duration,
          coupon: trimmedCoupon || undefined,
          currency: tgt.currency || planMeta.currency || 'EUR',
          lastStatus: tgt.lastStatus || {},
        };
      });

      const primary = normalizedTargets[0];
      const anyAutoOrder = normalizedTargets.some((tItem) => tItem.autoOrder);
      const anyNotifyAvail = normalizedTargets.some(
        (tItem) => tItem.notifyAvailable
      );
      const anyNotifyUnavail = normalizedTargets.some(
        (tItem) => tItem.notifyUnavailable
      );

      await api.post('/monitor/subscriptions', {
        planCode: planCode.trim(),
        serverName: displayName,
        datacenters: primary?.datacenters || [],
        notifyAvailable: anyNotifyAvail,
        notifyUnavailable: anyNotifyUnavail,
        autoOrder: anyAutoOrder,
        autoOrderQuantity: primary?.quantity || 1,
        targets: normalizedTargets,
        needsConfigSelection: false,
      });

      if (!monitorRunning && autoStartMonitor) {
        try {
          await api.post('/monitor/start');
        } catch {
          // Monitor start is also triggered by backend UpsertStockSub
        }
      }

      try {
        const raw = localStorage.getItem('ovh_subscribed_servers');
        const list: string[] = raw ? JSON.parse(raw) : [];
        if (!list.includes(planCode)) {
          list.push(planCode);
          localStorage.setItem('ovh_subscribed_servers', JSON.stringify(list));
        }
      } catch {
        // ignore storage error
      }

      toast.success(t('quickMonitor.savedToast', { planCode }), {
        action: {
          label: t('quickMonitor.viewInMonitor'),
          onClick: () =>
            navigate(`/monitor?planCode=${encodeURIComponent(planCode)}`),
        },
      });

      onSuccess?.(planCode);
      onOpenChange(false);
    } catch (err: any) {
      const msg =
        err?.response?.data?.message ||
        err?.response?.data?.error ||
        err?.message ||
        'Failed to save subscription';
      toast.error(msg);
    } finally {
      setIsSaving(false);
    }
  };

  if (!open) return null;

  return createPortal(
    <AnimatePresence>
      {open && (
        <div className="fixed inset-0 z-[9999] flex items-center justify-center p-3 sm:p-4">
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            className="fixed inset-0 bg-black/70 backdrop-blur-sm"
            onClick={() => !isSaving && onOpenChange(false)}
          />

          <motion.div
            initial={{ opacity: 0, scale: 0.95, y: 10 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0, scale: 0.95, y: 10 }}
            transition={{ duration: 0.18 }}
            className="relative z-10 w-full max-w-3xl max-h-[92vh] overflow-y-auto rounded-xl bg-cyber-surface border border-cyber-accent/40 shadow-2xl text-cyber-text p-4 sm:p-6"
            onClick={(e) => e.stopPropagation()}
          >
            {/* Header */}
            <div className="flex items-start justify-between gap-3 pb-3 border-b border-cyber-grid">
              <div>
                <div className="flex flex-wrap items-center gap-2">
                  <div className="p-1.5 rounded-lg bg-cyber-accent/15 border border-cyber-accent/30 text-cyber-accent">
                    <Bell className="w-4 h-4" />
                  </div>
                  <h2 className="text-base sm:text-lg font-bold text-cyber-text">
                    {isExistingSub
                      ? t('quickMonitor.titleUpdate')
                      : t('quickMonitor.titleAdd')}
                  </h2>
                  <span className="px-2 py-0.5 text-xs font-bold rounded bg-cyber-accent/15 text-cyber-accent border border-cyber-accent/30">
                    {displayName}
                  </span>
                  <span className="px-2 py-0.5 text-xs font-mono rounded bg-cyber-grid/50 text-cyber-muted">
                    SKU: {planCode}
                  </span>
                  {effectiveListingState === 'announced' && (
                    <span className="px-2 py-0.5 text-[11px] font-semibold rounded bg-amber-500/15 text-amber-400 border border-amber-500/30 flex items-center gap-1">
                      <Sparkles className="w-3 h-3" />
                      {t('announced.badge')}
                    </span>
                  )}
                  {effectiveListingState === 'presale' && (
                    <span className="px-2 py-0.5 text-[11px] font-semibold rounded bg-purple-500/15 text-purple-400 border border-purple-500/30 flex items-center gap-1">
                      <Sparkles className="w-3 h-3" />
                      {t('presale.badge')}
                    </span>
                  )}
                  {generation && (
                    <span className="px-1.5 py-0.5 text-[11px] font-mono rounded bg-indigo-500/15 text-indigo-300 border border-indigo-500/30">
                      {t('presale.generation', { year: generation })}
                    </span>
                  )}
                </div>
                <p className="text-xs text-cyber-muted mt-1">
                  {t('quickMonitor.subtitle', { planCode })}
                </p>
              </div>

              <button
                type="button"
                onClick={() => onOpenChange(false)}
                className="p-1.5 rounded-lg text-cyber-muted hover:text-cyber-text hover:bg-cyber-grid/40 transition-colors"
              >
                <X className="w-4 h-4" />
              </button>
            </div>

            <form onSubmit={handleSave} className="space-y-4 pt-4">
              {needsConfigSelection && (
                <div className="flex items-start gap-2.5 p-3 rounded-lg bg-amber-500/15 border border-amber-500/40 text-amber-300 text-xs">
                  <ShieldAlert className="w-4 h-4 text-amber-400 flex-shrink-0 mt-0.5" />
                  <span>{t('monitor.legacyAutoOrderPausedBanner')}</span>
                </div>
              )}

              {effectiveListingState === 'announced' && (
                <div className="flex items-start gap-2 p-2.5 rounded-md bg-amber-500/10 border border-amber-500/30 text-amber-300 text-xs leading-relaxed">
                  <AlertTriangle className="w-4 h-4 text-amber-400 flex-shrink-0 mt-0.5" />
                  <span>{t('announced.noStockFeedHint')}</span>
                </div>
              )}

              {/* Targets Section Header */}
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex items-center gap-2">
                  <Cpu className="w-4 h-4 text-cyber-accent" />
                  <span className="text-xs sm:text-sm font-bold text-cyber-text">
                    {t('quickMonitor.targetsSectionTitle')}
                  </span>
                  <span className="px-2 py-0.5 text-[11px] font-mono rounded-full bg-cyber-accent/15 text-cyber-accent">
                    {targets.length}
                  </span>
                </div>
                <button
                  type="button"
                  onClick={addTarget}
                  className="px-2.5 py-1.5 rounded-lg text-xs font-semibold bg-cyber-accent/15 text-cyber-accent border border-cyber-accent/40 hover:bg-cyber-accent/25 transition-colors flex items-center gap-1"
                >
                  <Plus className="w-3.5 h-3.5" />
                  <span>{t('quickMonitor.addTargetButton')}</span>
                </button>
              </div>

              {isLoading ? (
                <div className="flex items-center justify-center py-8 text-xs text-cyber-muted gap-2 border border-cyber-grid rounded-lg bg-cyber-bg/40">
                  <RefreshCw className="w-4 h-4 animate-spin text-cyber-accent" />
                  <span>Loading configurations & live datacenter stock...</span>
                </div>
              ) : (
                <div className="space-y-4">
                  {targets.map((target, idx) => {
                    const targetDcStatuses = getTargetDcStatuses(target);
                    const isConfigMissingForAutoOrder =
                      target.autoOrder &&
                      (!target.configKey ||
                        !target.options ||
                        target.options.length === 0);
                    const symbol = getCurrencySymbol(
                      target.currency || planMeta.currency
                    );

                    return (
                      <div
                        key={target.id || idx}
                        className="p-3.5 sm:p-4 rounded-xl border border-cyber-grid bg-cyber-bg/40 space-y-3.5"
                      >
                        {/* Target Card Header */}
                        <div className="flex items-center justify-between gap-2 border-b border-cyber-grid/60 pb-2">
                          <div className="flex items-center gap-2">
                            <span className="px-2 py-0.5 text-xs font-bold rounded bg-cyber-accent/15 text-cyber-accent border border-cyber-accent/30">
                              {t('quickMonitor.targetTitle', { index: idx + 1 })}
                            </span>
                            {target.configKey ? (
                              <span className="text-xs font-medium text-cyber-text truncate max-w-xs sm:max-w-md">
                                {target.label || target.configKey}
                              </span>
                            ) : (
                              <span className="text-xs text-cyber-muted">
                                {t('quickMonitor.anyConfigNotifyOnly')}
                              </span>
                            )}
                          </div>
                          {targets.length > 1 && (
                            <button
                              type="button"
                              onClick={() => removeTarget(idx)}
                              className="px-2 py-1 rounded text-xs text-red-400 hover:bg-red-500/15 border border-transparent hover:border-red-500/30 flex items-center gap-1 transition-colors"
                            >
                              <Trash2 className="w-3.5 h-3.5" />
                              <span>{t('quickMonitor.removeTargetButton')}</span>
                            </button>
                          )}
                        </div>

                        {/* 1. Configuration Selector + Setup Fee / Promo Summary */}
                        {(() => {
                          const selectedChoice =
                            configChoices.find(
                              (c) => c.configKey === target.configKey
                            ) ||
                            configChoices.find((c) => c.isDefault) ||
                            configChoices[0] ||
                            null;
                          const activeMode = target.pricingMode || 'default';
                          const availableModes: PlanPricingMode[] =
                            selectedChoice &&
                            selectedChoice.pricingModes.length > 0
                              ? selectedChoice.pricingModes
                              : [
                                  {
                                    mode: 'default',
                                    duration: 'P1M',
                                    intervalMonths: 1,
                                    commitmentMonths: 0,
                                    periodPrice:
                                      selectedChoice?.estimatedMonthlyPrice ?? 0,
                                    originalPeriodPrice:
                                      selectedChoice?.originalMonthlyPrice ??
                                      selectedChoice?.estimatedMonthlyPrice ??
                                      0,
                                    monthlyEquivalentPrice:
                                      selectedChoice?.estimatedMonthlyPrice ?? 0,
                                    originalMonthlyPrice:
                                      selectedChoice?.originalMonthlyPrice ??
                                      selectedChoice?.estimatedMonthlyPrice ??
                                      0,
                                    setupFee: selectedChoice?.setupFee ?? 0,
                                    originalSetupFee:
                                      selectedChoice?.originalSetupFee ?? 0,
                                    freeSetup: Boolean(
                                      selectedChoice?.freeSetup
                                    ),
                                    hasPromo: Boolean(selectedChoice?.hasPromo),
                                    promoBadge: selectedChoice?.promoBadge,
                                    promoDescription:
                                      selectedChoice?.promoDescription,
                                  },
                                ];
                          // Ensure standard commitment modes are selectable if backend only returned 1 synthesized mode
                          const modeChoices: PlanPricingMode[] =
                            availableModes.length > 1
                              ? availableModes
                              : [
                                  availableModes[0],
                                  {
                                    ...availableModes[0],
                                    mode: 'upfront12',
                                    duration: 'P12M',
                                    intervalMonths: 12,
                                    commitmentMonths: 12,
                                    periodPrice: Number(
                                      (
                                        (availableModes[0]
                                          ?.monthlyEquivalentPrice || 0) * 12
                                      ).toFixed(2)
                                    ),
                                    originalPeriodPrice: Number(
                                      (
                                        (availableModes[0]
                                          ?.originalMonthlyPrice || 0) * 12
                                      ).toFixed(2)
                                    ),
                                    setupFee: 0,
                                    freeSetup: true,
                                  },
                                  {
                                    ...availableModes[0],
                                    mode: 'upfront24',
                                    duration: 'P24M',
                                    intervalMonths: 24,
                                    commitmentMonths: 24,
                                    periodPrice: Number(
                                      (
                                        (availableModes[0]
                                          ?.monthlyEquivalentPrice || 0) * 24
                                      ).toFixed(2)
                                    ),
                                    originalPeriodPrice: Number(
                                      (
                                        (availableModes[0]
                                          ?.originalMonthlyPrice || 0) * 24
                                      ).toFixed(2)
                                    ),
                                    setupFee: 0,
                                    freeSetup: true,
                                  },
                                ];
                          const activeModeEntry =
                            resolvePricingModeEntry(modeChoices, activeMode) ||
                            modeChoices[0];

                          return (
                            <div className="space-y-3">
                              <div>
                                <div className="flex flex-wrap items-center justify-between gap-2 mb-1">
                                  <label className="block text-xs font-semibold text-cyber-text">
                                    {t('quickMonitor.configSelectLabel')}
                                  </label>
                                  {selectedChoice && (
                                    <div className="flex flex-wrap items-center gap-1.5 text-[11px]">
                                      {(activeModeEntry?.hasPromo ||
                                        selectedChoice.hasPromo) && (
                                        <span
                                          title={
                                            activeModeEntry?.promoDescription ||
                                            selectedChoice.promoDescription
                                          }
                                          className="px-1.5 py-0.5 rounded bg-rose-500/15 text-rose-300 border border-rose-500/35 font-bold"
                                        >
                                          🔥{' '}
                                          {activeModeEntry?.promoBadge ||
                                            selectedChoice.promoBadge ||
                                            t('promo.badge')}
                                        </span>
                                      )}
                                      {activeModeEntry &&
                                        activeModeEntry.monthlyEquivalentPrice >
                                          0 && (
                                          <span className="px-2 py-0.5 rounded bg-emerald-500/10 text-emerald-300 border border-emerald-500/30 font-mono font-semibold">
                                            {activeModeEntry.intervalMonths > 1
                                              ? `${t('billing.periodTotal', {
                                                  price: `${symbol}${activeModeEntry.periodPrice.toFixed(
                                                    2
                                                  )}`,
                                                  months:
                                                    activeModeEntry.intervalMonths,
                                                })} (${t(
                                                  'billing.monthlyEquivalent',
                                                  {
                                                    price: `${symbol}${activeModeEntry.monthlyEquivalentPrice.toFixed(
                                                      2
                                                    )}`,
                                                  }
                                                )})`
                                              : `${symbol}${activeModeEntry.monthlyEquivalentPrice.toFixed(
                                                  2
                                                )}${t('availability.perMonth')}`}
                                            {activeModeEntry.originalMonthlyPrice >
                                              activeModeEntry.monthlyEquivalentPrice +
                                                0.01 && (
                                              <span className="line-through text-[10px] opacity-70 ml-1">
                                                {symbol}
                                                {activeModeEntry.originalMonthlyPrice.toFixed(
                                                  2
                                                )}
                                              </span>
                                            )}
                                          </span>
                                        )}
                                      {activeModeEntry &&
                                        (activeModeEntry.setupFee === 0 ||
                                          activeModeEntry.freeSetup) && (
                                          <span className="px-1.5 py-0.5 rounded bg-emerald-500/15 text-emerald-400 border border-emerald-500/30 font-semibold">
                                            {t('promo.freeSetup')}
                                            {activeModeEntry.originalSetupFee >
                                              0 && (
                                              <span className="line-through text-[10px] opacity-70 ml-1 font-mono">
                                                {symbol}
                                                {activeModeEntry.originalSetupFee.toFixed(
                                                  2
                                                )}
                                              </span>
                                            )}
                                          </span>
                                        )}
                                      {activeModeEntry &&
                                        activeModeEntry.setupFee > 0 && (
                                          <span className="px-1.5 py-0.5 rounded bg-cyber-grid/40 text-cyber-muted border border-cyber-grid font-mono">
                                            {t('promo.setupFee', {
                                              price: `${symbol}${activeModeEntry.setupFee.toFixed(
                                                2
                                              )}`,
                                            })}
                                          </span>
                                        )}
                                    </div>
                                  )}
                                </div>

                                <select
                                  value={target.configKey}
                                  onChange={(e) =>
                                    handleSelectConfigForTarget(
                                      idx,
                                      e.target.value
                                    )
                                  }
                                  className={`cyber-input w-full text-xs py-2 ${
                                    isConfigMissingForAutoOrder
                                      ? 'border-amber-500 ring-1 ring-amber-500/50'
                                      : ''
                                  }`}
                                >
                                  <option value="">
                                    {t('quickMonitor.anyConfigNotifyOnly')}
                                  </option>
                                  {configChoices.map((cfg) => {
                                    const cfgSym = getCurrencySymbol(
                                      cfg.currency
                                    );
                                    const cfgMode =
                                      resolvePricingModeEntry(
                                        cfg.pricingModes,
                                        activeMode
                                      ) || null;
                                    const displayMo =
                                      cfgMode &&
                                      cfgMode.monthlyEquivalentPrice > 0
                                        ? cfgMode.monthlyEquivalentPrice
                                        : cfg.estimatedMonthlyPrice;
                                    const defaultTag = cfg.isDefault
                                      ? `[${t('catalog.defaultConfigBadge')}] `
                                      : '';
                                    const promoTag =
                                      cfgMode?.hasPromo || cfg.hasPromo
                                        ? `🔥${
                                            cfgMode?.promoBadge ||
                                            cfg.promoBadge ||
                                            t('promo.badge')
                                          } `
                                        : '';
                                    const priceTag =
                                      displayMo && displayMo > 0
                                        ? ` — ${cfgSym}${displayMo.toFixed(2)}${t(
                                            'availability.perMonth'
                                          )}`
                                        : '';
                                    const setupVal =
                                      cfgMode?.setupFee ?? cfg.setupFee;
                                    const setupTag =
                                      setupVal === 0
                                        ? ` · ${t('promo.freeSetup')}`
                                        : typeof setupVal === 'number' &&
                                          setupVal > 0
                                        ? ` · ${t('promo.setupFee', {
                                            price: `${cfgSym}${setupVal.toFixed(
                                              2
                                            )}`,
                                          })}`
                                        : '';
                                    const stockTag = ` (${t(
                                      'availability.inStockDcCount',
                                      {
                                        count: cfg.inStockCount,
                                      }
                                    )})`;
                                    return (
                                      <option
                                        key={cfg.configKey}
                                        value={cfg.configKey}
                                      >
                                        {`${defaultTag}${promoTag}${cfg.label}${priceTag}${setupTag}${stockTag}`}
                                      </option>
                                    );
                                  })}
                                </select>
                              </div>

                              {/* Billing / Commitment Mode Selector + Coupon Input */}
                              <div className="grid grid-cols-1 md:grid-cols-12 gap-2.5">
                                <div className="md:col-span-8 space-y-1">
                                  <label className="block text-xs font-semibold text-cyber-text">
                                    {t('billing.modeSelectorTitle')}
                                  </label>
                                  <div className="grid grid-cols-1 sm:grid-cols-3 gap-1.5">
                                    {modeChoices.map((m) => {
                                      const isSelectedMode =
                                        activeMode === m.mode;
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
                                            handleSelectPricingModeForTarget(
                                              idx,
                                              m.mode
                                            )
                                          }
                                          className={`px-2.5 py-1.5 rounded-lg border text-left transition-all flex flex-col justify-between gap-0.5 ${
                                            isSelectedMode
                                              ? 'bg-cyber-accent/15 border-cyber-accent text-cyber-text ring-1 ring-cyber-accent/40'
                                              : 'bg-cyber-bg/50 border-cyber-grid text-cyber-muted hover:border-cyber-accent/40 hover:text-cyber-text'
                                          }`}
                                        >
                                          <div className="flex items-center justify-between gap-1">
                                            <span className="text-[11px] font-bold truncate">
                                              {modeTitle}
                                            </span>
                                            {m.hasPromo && (
                                              <span className="text-[10px] text-rose-300 font-semibold flex-shrink-0">
                                                🔥{m.promoBadge || ''}
                                              </span>
                                            )}
                                          </div>
                                          <div className="flex flex-wrap items-center justify-between gap-1 text-[10px] font-mono">
                                            {m.monthlyEquivalentPrice > 0 ? (
                                              <span className="text-emerald-400 font-semibold">
                                                {symbol}
                                                {m.monthlyEquivalentPrice.toFixed(
                                                  2
                                                )}
                                                {t('availability.perMonth')}
                                              </span>
                                            ) : (
                                              <span>{m.duration}</span>
                                            )}
                                            <span
                                              className={
                                                m.setupFee === 0
                                                  ? 'text-emerald-300'
                                                  : 'text-cyber-muted'
                                              }
                                            >
                                              {m.setupFee === 0
                                                ? t('promo.freeSetup')
                                                : t('promo.setupFee', {
                                                    price: `${symbol}${m.setupFee.toFixed(
                                                      2
                                                    )}`,
                                                  })}
                                            </span>
                                          </div>
                                          {m.intervalMonths > 1 &&
                                            m.periodPrice > 0 && (
                                              <div className="text-[10px] text-cyber-muted font-mono truncate">
                                                {t('billing.periodTotal', {
                                                  price: `${symbol}${m.periodPrice.toFixed(
                                                    2
                                                  )}`,
                                                  months: m.intervalMonths,
                                                })}
                                              </div>
                                            )}
                                        </button>
                                      );
                                    })}
                                  </div>
                                </div>

                                <div className="md:col-span-4 space-y-1">
                                  <label className="block text-xs font-semibold text-cyber-text">
                                    {t('coupon.label')}
                                  </label>
                                  <input
                                    type="text"
                                    value={target.coupon || ''}
                                    onChange={(e) =>
                                      updateTarget(idx, {
                                        coupon: e.target.value,
                                      })
                                    }
                                    placeholder={t('coupon.placeholder')}
                                    className="cyber-input w-full text-xs py-1.5 font-mono"
                                  />
                                </div>
                              </div>
                            </div>
                          );
                        })()}

                        {/* 2. Datacenter Multi-Select */}
                        <div className="space-y-2">
                          <div className="flex flex-wrap items-center justify-between gap-2">
                            <div>
                              <label className="text-xs font-semibold text-cyber-text">
                                {t('quickMonitor.datacentersLabel')}
                              </label>
                              <p className="text-[11px] text-cyber-muted">
                                {t('quickMonitor.allDatacentersHint')}
                              </p>
                            </div>
                            <div className="flex items-center gap-1.5">
                              <button
                                type="button"
                                onClick={() =>
                                  updateTarget(idx, {
                                    datacenters: [...availableDcCodes],
                                  })
                                }
                                className="px-2 py-0.5 text-[11px] rounded bg-cyber-accent/15 text-cyber-accent border border-cyber-accent/30 hover:bg-cyber-accent/25 transition-colors"
                              >
                                {t('quickMonitor.selectAll')}
                              </button>
                              <button
                                type="button"
                                onClick={() =>
                                  updateTarget(idx, { datacenters: [] })
                                }
                                className="px-2 py-0.5 text-[11px] rounded bg-cyber-grid/30 text-cyber-muted border border-cyber-grid hover:text-cyber-text transition-colors"
                              >
                                {t('quickMonitor.clearAll')}
                              </button>
                            </div>
                          </div>

                          <div className="grid grid-cols-2 sm:grid-cols-3 gap-1.5 max-h-40 overflow-y-auto p-0.5">
                            {availableDcCodes.map((dcCode) => {
                              const norm = normalizeDatacenterCode(dcCode);
                              const isChecked =
                                target.datacenters.length === 0 ||
                                target.datacenters.includes(norm);
                              const status = targetDcStatuses[norm];
                              const badge = getStatusBadge(status);

                              return (
                                <button
                                  key={norm}
                                  type="button"
                                  onClick={() => toggleTargetDc(idx, norm)}
                                  className={`flex items-center justify-between gap-1.5 px-2.5 py-1.5 rounded-lg border text-left text-xs transition-all ${
                                    isChecked
                                      ? 'bg-cyber-accent/12 border-cyber-accent/50 text-cyber-text'
                                      : 'bg-cyber-bg/40 border-cyber-grid text-cyber-muted hover:border-cyber-accent/30'
                                  }`}
                                >
                                  <div className="flex items-center gap-1.5 min-w-0">
                                    <span
                                      className={`w-3.5 h-3.5 rounded flex items-center justify-center border flex-shrink-0 ${
                                        isChecked
                                          ? 'bg-cyber-accent border-cyber-accent text-white'
                                          : 'border-cyber-grid bg-cyber-bg'
                                      }`}
                                    >
                                      {isChecked && (
                                        <Check className="w-2.5 h-2.5" strokeWidth={3} />
                                      )}
                                    </span>
                                    <DCLabel
                                      code={norm}
                                      mode="compact"
                                      className="truncate font-medium"
                                    />
                                  </div>
                                  <span
                                    className={`px-1.5 py-0.2 text-[10px] font-semibold rounded border flex-shrink-0 ${badge.cls}`}
                                  >
                                    {badge.label}
                                  </span>
                                </button>
                              );
                            })}
                          </div>
                        </div>

                        {/* 3. Notification Toggles */}
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                          <label className="flex items-center gap-2 text-xs cursor-pointer select-none p-2 rounded border border-cyber-grid/60 bg-cyber-surface/50">
                            <input
                              type="checkbox"
                              checked={target.notifyAvailable}
                              onChange={(e) =>
                                updateTarget(idx, {
                                  notifyAvailable: e.target.checked,
                                })
                              }
                              className="h-4 w-4 accent-cyber-accent"
                            />
                            <span>{t('quickMonitor.notifyAvailable')}</span>
                          </label>
                          <label className="flex items-center gap-2 text-xs cursor-pointer select-none p-2 rounded border border-cyber-grid/60 bg-cyber-surface/50">
                            <input
                              type="checkbox"
                              checked={target.notifyUnavailable}
                              onChange={(e) =>
                                updateTarget(idx, {
                                  notifyUnavailable: e.target.checked,
                                })
                              }
                              className="h-4 w-4 accent-cyber-accent"
                            />
                            <span>{t('quickMonitor.notifyUnavailable')}</span>
                          </label>
                        </div>

                        {/* 4. Auto-Order Toggle, Validation, Quantity & Max Monthly Price */}
                        <div className="p-3 rounded-lg border border-cyber-accent/25 bg-cyber-accent/5 space-y-2.5">
                          <div className="flex items-center justify-between gap-2">
                            <label className="flex items-center gap-2 text-xs sm:text-sm font-semibold text-cyber-text cursor-pointer select-none">
                              <input
                                type="checkbox"
                                checked={target.autoOrder}
                                onChange={(e) => {
                                  const enabled = e.target.checked;
                                  const selectedCfg = configChoices.find(
                                    (c) => c.configKey === target.configKey
                                  );
                                  const modeEntry = resolvePricingModeEntry(
                                    selectedCfg?.pricingModes,
                                    target.pricingMode
                                  );
                                  const fallbackMonthly =
                                    modeEntry &&
                                    modeEntry.monthlyEquivalentPrice > 0
                                      ? modeEntry.monthlyEquivalentPrice
                                      : selectedCfg?.estimatedMonthlyPrice;
                                  updateTarget(idx, {
                                    autoOrder: enabled,
                                    maxMonthlyPrice:
                                      enabled &&
                                      (!target.maxMonthlyPrice ||
                                        target.maxMonthlyPrice <= 0) &&
                                      fallbackMonthly
                                        ? Number(fallbackMonthly.toFixed(2))
                                        : target.maxMonthlyPrice,
                                  });
                                }}
                                className="h-4 w-4 accent-cyber-accent"
                              />
                              <ShoppingCart className="w-4 h-4 text-cyber-accent" />
                              <span>{t('quickMonitor.autoOrderToggle')}</span>
                            </label>
                          </div>

                          {isConfigMissingForAutoOrder && (
                            <div className="flex items-start gap-2 p-2.5 rounded-md bg-amber-500/15 border border-amber-500/40 text-amber-300 text-xs leading-relaxed">
                              <AlertTriangle className="w-4 h-4 text-amber-400 flex-shrink-0 mt-0.5" />
                              <span>
                                {t('quickMonitor.autoOrderConfigRequiredWarning')}
                              </span>
                            </div>
                          )}

                          {effectiveListingState === 'presale' && target.autoOrder && (
                            <div className="flex items-start gap-2 p-2.5 rounded-md bg-purple-500/10 border border-purple-500/30 text-purple-300 text-xs leading-relaxed">
                              <AlertTriangle className="w-4 h-4 text-purple-400 flex-shrink-0 mt-0.5" />
                              <span>{t('quickMonitor.presaleCallout')}</span>
                            </div>
                          )}

                          {target.autoOrder && (
                            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 pt-1">
                              <div>
                                <label className="block text-xs text-cyber-muted mb-1">
                                  {t('quickMonitor.quantityLabel')}
                                </label>
                                <input
                                  type="number"
                                  min={1}
                                  max={100}
                                  value={target.quantity}
                                  onChange={(e) =>
                                    updateTarget(idx, {
                                      quantity: Math.max(
                                        1,
                                        Math.min(
                                          100,
                                          parseInt(e.target.value, 10) || 1
                                        )
                                      ),
                                    })
                                  }
                                  className="cyber-input w-full text-xs py-1.5"
                                />
                                <p className="text-[11px] text-cyber-muted mt-1">
                                  {t('quickMonitor.autoOrderQtyHint')}
                                </p>
                              </div>

                              <div>
                                <label className="block text-xs text-cyber-muted mb-1">
                                  {t('quickMonitor.maxMonthlyPriceLabel')} ({symbol})
                                </label>
                                <input
                                  type="number"
                                  step="0.01"
                                  min={0}
                                  placeholder="0.00"
                                  value={
                                    typeof target.maxMonthlyPrice === 'number' &&
                                    target.maxMonthlyPrice > 0
                                      ? target.maxMonthlyPrice
                                      : ''
                                  }
                                  onChange={(e) => {
                                    const rawVal = parseFloat(e.target.value);
                                    updateTarget(idx, {
                                      maxMonthlyPrice:
                                        !isNaN(rawVal) && rawVal > 0
                                          ? rawVal
                                          : undefined,
                                    });
                                  }}
                                  className="cyber-input w-full text-xs py-1.5 font-mono"
                                />
                                <p className="text-[11px] text-cyber-muted mt-1 leading-relaxed">
                                  {t('quickMonitor.maxMonthlyPriceHint')}
                                </p>
                              </div>
                            </div>
                          )}
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}

              {/* Auto-Start Monitor Checkbox when Stopped */}
              {!monitorRunning && (
                <label className="flex items-center gap-2 p-2.5 rounded-lg border border-emerald-500/30 bg-emerald-500/10 text-emerald-300 text-xs cursor-pointer select-none">
                  <input
                    type="checkbox"
                    checked={autoStartMonitor}
                    onChange={(e) => setAutoStartMonitor(e.target.checked)}
                    className="h-4 w-4 accent-emerald-500"
                  />
                  <span className="font-medium">
                    {t('quickMonitor.autoStartMonitor')}
                  </span>
                </label>
              )}

              {/* Footer Actions */}
              <div className="flex items-center justify-between gap-3 pt-2 border-t border-cyber-grid">
                <button
                  type="button"
                  onClick={() => {
                    onOpenChange(false);
                    navigate(`/monitor?planCode=${encodeURIComponent(planCode)}`);
                  }}
                  className="text-xs text-cyber-accent hover:underline flex items-center gap-1"
                >
                  <span>{t('quickMonitor.viewInMonitor')}</span>
                  <ExternalLink className="w-3 h-3" />
                </button>

                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    onClick={() => onOpenChange(false)}
                    disabled={isSaving}
                    className="cyber-button px-4 py-2 text-xs"
                  >
                    {t('quickMonitor.cancel')}
                  </button>
                  <button
                    type="submit"
                    disabled={isSaving || hasInvalidAutoOrderTarget}
                    className="cyber-button px-4 py-2 text-xs font-semibold bg-cyber-accent text-white hover:bg-cyber-accent/90 border-cyber-accent flex items-center gap-1.5 disabled:opacity-50 disabled:cursor-not-allowed"
                  >
                    <Bell className="w-3.5 h-3.5" />
                    <span>
                      {isSaving
                        ? t('quickMonitor.saving')
                        : isExistingSub
                        ? t('quickMonitor.saveUpdate')
                        : t('quickMonitor.saveAdd')}
                    </span>
                  </button>
                </div>
              </div>
            </form>
          </motion.div>
        </div>
      )}
    </AnimatePresence>,
    document.body
  );
};

export default QuickMonitorDialog;
