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
  formatMemoryCode,
  formatStorageCode,
} from '@/utils/hardwareFormat';

export interface QuickMonitorDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  planCode: string;
  serverName?: string;
  isPresale?: boolean;
  initialDatacenters?: { datacenter: string; availability: string }[];
  initialMemory?: string;
  initialStorage?: string;
  onSuccess?: (planCode: string) => void;
}

interface RawConfigItem {
  fqn?: string;
  memory: string;
  storage: string;
  systemStorage?: string;
  gpu?: string;
  datacenters: { datacenter: string; availability: string }[];
}

export const QuickMonitorDialog: React.FC<QuickMonitorDialogProps> = ({
  open,
  onOpenChange,
  planCode,
  serverName,
  isPresale = false,
  initialDatacenters = [],
  initialMemory = '',
  initialStorage = '',
  onSuccess,
}) => {
  const { locale, t } = useLocale();
  const navigate = useNavigate();
  const catalogDict = useCatalogDictionary();

  const [isLoading, setIsLoading] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [isExistingSub, setIsExistingSub] = useState(false);
  const [monitorRunning, setMonitorRunning] = useState(true);
  const [autoStartMonitor, setAutoStartMonitor] = useState(true);

  const [selectedDcs, setSelectedDcs] = useState<string[]>([]);
  const [notifyAvailable, setNotifyAvailable] = useState(true);
  const [notifyUnavailable, setNotifyUnavailable] = useState(false);
  const [autoOrder, setAutoOrder] = useState(false);
  const [autoOrderQuantity, setAutoOrderQuantity] = useState(1);

  const [memoryFilter, setMemoryFilter] = useState<string>(initialMemory || '');
  const [storageFilter, setStorageFilter] = useState<string>(initialStorage || '');

  const [dcStatusMap, setDcStatusMap] = useState<Record<string, string>>({});
  const [rawConfigs, setRawConfigs] = useState<RawConfigItem[]>([]);

  const planMeta = useMemo(
    () => resolvePlanDisplay(planCode, '', locale, catalogDict),
    [planCode, locale, catalogDict]
  );

  const displayName = serverName || planMeta.name || planCode;
  const generation = inferPlanGeneration(planCode);

  // Seed initial datacenters from caller props when dialog opens
  useEffect(() => {
    if (!open || !planCode) return;

    setMemoryFilter(initialMemory || '');
    setStorageFilter(initialStorage || '');

    const seedMap: Record<string, string> = {};
    initialDatacenters.forEach((d) => {
      const code = normalizeDatacenterCode(d.datacenter);
      if (code) {
        const prev = seedMap[code];
        const isAvail =
          d.availability &&
          d.availability !== 'unavailable' &&
          d.availability !== 'unknown';
        if (!prev || isAvail) {
          seedMap[code] = d.availability || 'unknown';
        }
      }
    });
    setDcStatusMap(seedMap);
    setRawConfigs([]);

    let cancelled = false;
    const fetchAll = async () => {
      setIsLoading(true);
      try {
        const [subsRes, statusRes, availMapRes, rawAvailRes] =
          await Promise.allSettled([
            api.get('/monitor/subscriptions'),
            api.get('/monitor/status'),
            api.get(`/availability/${encodeURIComponent(planCode)}`),
            api.get('/availability', { params: { planCode } }),
          ]);

        if (cancelled) return;

        // 1. Parse live availability map & raw config items
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

        let configsList: RawConfigItem[] = [];
        if (
          rawAvailRes.status === 'fulfilled' &&
          Array.isArray(rawAvailRes.value?.data)
        ) {
          configsList = rawAvailRes.value.data.filter(
            (it: any) =>
              it &&
              typeof it.planCode === 'string' &&
              it.planCode.toLowerCase() === planCode.toLowerCase()
          );
          configsList.forEach((cfg) => {
            (cfg.datacenters || []).forEach((d) => {
              const norm = normalizeDatacenterCode(d.datacenter);
              if (!norm) return;
              const cur = mergedDcMap[norm];
              const isAvail =
                d.availability &&
                d.availability !== 'unavailable' &&
                d.availability !== 'unknown';
              if (!cur || isAvail) {
                mergedDcMap[norm] = d.availability || 'unknown';
              }
            });
          });
        }

        setDcStatusMap(mergedDcMap);
        setRawConfigs(configsList);

        const discoveredDcList = Object.keys(mergedDcMap);
        const defaultDcPool =
          discoveredDcList.length > 0
            ? discoveredDcList
            : OVH_DATACENTERS.map((d) => d.code);

        // 2. Check existing subscription
        let foundExisting: any = null;
        if (subsRes.status === 'fulfilled' && Array.isArray(subsRes.value?.data)) {
          foundExisting = subsRes.value.data.find(
            (s: any) =>
              typeof s.planCode === 'string' &&
              s.planCode.toLowerCase() === planCode.toLowerCase()
          );
        }

        if (foundExisting) {
          setIsExistingSub(true);
          const subDcs: string[] = Array.isArray(foundExisting.datacenters)
            ? foundExisting.datacenters
                .map((d: string) => normalizeDatacenterCode(d))
                .filter(Boolean)
            : [];
          // Empty array in backend means monitor all DCs
          setSelectedDcs(subDcs.length > 0 ? subDcs : defaultDcPool);
          setNotifyAvailable(foundExisting.notifyAvailable !== false);
          setNotifyUnavailable(Boolean(foundExisting.notifyUnavailable));
          setAutoOrder(Boolean(foundExisting.autoOrder));
          setAutoOrderQuantity(
            typeof foundExisting.autoOrderQuantity === 'number' &&
              foundExisting.autoOrderQuantity > 0
              ? foundExisting.autoOrderQuantity
              : 1
          );
        } else {
          setIsExistingSub(false);
          setSelectedDcs(defaultDcPool);
          setNotifyAvailable(true);
          setNotifyUnavailable(false);
          setAutoOrder(false);
          setAutoOrderQuantity(1);
        }

        // 3. Check monitor status
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
  }, [open, planCode]);

  // Distinct memory and storage options discovered from raw availability items
  const memoryOptions = useMemo(() => {
    const set = new Set<string>();
    rawConfigs.forEach((c) => {
      if (c.memory && c.memory !== 'N/A') set.add(c.memory);
    });
    return Array.from(set);
  }, [rawConfigs]);

  const storageOptions = useMemo(() => {
    const set = new Set<string>();
    rawConfigs.forEach((c) => {
      if (c.storage && c.storage !== 'N/A') set.add(c.storage);
    });
    return Array.from(set);
  }, [rawConfigs]);

  // Effective per-DC availability considering optional memory/storage filter
  const effectiveDcStatuses = useMemo(() => {
    if ((!memoryFilter && !storageFilter) || rawConfigs.length === 0) {
      return dcStatusMap;
    }
    const filtered = rawConfigs.filter((c) => {
      if (memoryFilter && c.memory !== memoryFilter) return false;
      if (storageFilter && c.storage !== storageFilter) return false;
      return true;
    });
    if (filtered.length === 0) return dcStatusMap;

    const map: Record<string, string> = {};
    Object.keys(dcStatusMap).forEach((k) => {
      map[k] = 'unavailable';
    });
    filtered.forEach((cfg) => {
      (cfg.datacenters || []).forEach((d) => {
        const norm = normalizeDatacenterCode(d.datacenter);
        if (!norm) return;
        const isAvail =
          d.availability &&
          d.availability !== 'unavailable' &&
          d.availability !== 'unknown';
        if (!map[norm] || isAvail) {
          map[norm] = d.availability || 'unavailable';
        }
      });
    });
    return map;
  }, [dcStatusMap, rawConfigs, memoryFilter, storageFilter]);

  const availableDcCodes = useMemo(() => {
    const keys = Object.keys(effectiveDcStatuses);
    if (keys.length > 0) return keys;
    return OVH_DATACENTERS.map((d) => d.code);
  }, [effectiveDcStatuses]);

  const toggleDc = (dcCode: string) => {
    const norm = normalizeDatacenterCode(dcCode);
    setSelectedDcs((prev) =>
      prev.includes(norm) ? prev.filter((d) => d !== norm) : [...prev, norm]
    );
  };

  const selectAllDcs = () => {
    setSelectedDcs([...availableDcCodes]);
  };

  const clearAllDcs = () => {
    setSelectedDcs([]);
  };

  const getStatusBadge = (status?: string) => {
    if (!status || status === 'unknown') {
      return {
        label: locale === 'zh-CN' ? '未知' : locale === 'fr' ? 'Inconnu' : 'Unknown',
        cls: 'bg-slate-500/15 text-slate-400 border-slate-500/30',
      };
    }
    if (status === 'unavailable') {
      return {
        label: locale === 'zh-CN' ? '无货' : locale === 'fr' ? 'Rupture' : 'Out of Stock',
        cls: 'bg-red-500/15 text-red-400 border-red-500/30',
      };
    }
    return {
      label: status,
      cls: 'bg-emerald-500/15 text-emerald-400 border-emerald-500/30',
    };
  };

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!planCode) return;

    setIsSaving(true);
    try {
      // When all DCs or 0 DCs are selected, pass [] to monitor all datacenters
      const isAllSelected =
        selectedDcs.length === 0 ||
        selectedDcs.length >= availableDcCodes.length;
      const payloadDcs = isAllSelected ? [] : selectedDcs;

      await api.post('/monitor/subscriptions', {
        planCode: planCode.trim(),
        serverName: displayName,
        datacenters: payloadDcs,
        notifyAvailable,
        notifyUnavailable,
        autoOrder,
        autoOrderQuantity: autoOrder
          ? Math.max(1, Number(autoOrderQuantity) || 1)
          : 1,
      });

      if (!monitorRunning && autoStartMonitor) {
        try {
          await api.post('/monitor/start');
        } catch {
          // Monitor start is also triggered by backend UpsertStockSub
        }
      }

      // Sync cached subscribed servers in localStorage so ServersPage updates immediately
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
            className="relative z-10 w-full max-w-2xl max-h-[90vh] overflow-y-auto rounded-xl bg-cyber-surface border border-cyber-accent/40 shadow-2xl text-cyber-text p-4 sm:p-6"
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
                  {isPresale && (
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
              {/* 1. Datacenter Multi-Select */}
              <div className="space-y-2">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div>
                    <label className="text-xs sm:text-sm font-semibold text-cyber-text">
                      {t('quickMonitor.datacentersLabel')}
                    </label>
                    <p className="text-[11px] text-cyber-muted">
                      {t('quickMonitor.allDatacentersHint')}
                    </p>
                  </div>
                  <div className="flex items-center gap-2">
                    <button
                      type="button"
                      onClick={selectAllDcs}
                      className="px-2 py-1 text-[11px] rounded bg-cyber-accent/15 text-cyber-accent border border-cyber-accent/30 hover:bg-cyber-accent/25 transition-colors"
                    >
                      {t('quickMonitor.selectAll')}
                    </button>
                    <button
                      type="button"
                      onClick={clearAllDcs}
                      className="px-2 py-1 text-[11px] rounded bg-cyber-grid/30 text-cyber-muted border border-cyber-grid hover:text-cyber-text transition-colors"
                    >
                      {t('quickMonitor.clearAll')}
                    </button>
                  </div>
                </div>

                {isLoading ? (
                  <div className="flex items-center justify-center py-6 text-xs text-cyber-muted gap-2 border border-cyber-grid rounded-lg bg-cyber-bg/40">
                    <RefreshCw className="w-4 h-4 animate-spin text-cyber-accent" />
                    <span>Loading live datacenter stock...</span>
                  </div>
                ) : (
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 max-h-52 overflow-y-auto p-1">
                    {availableDcCodes.map((dcCode) => {
                      const norm = normalizeDatacenterCode(dcCode);
                      const isChecked =
                        selectedDcs.length === 0 || selectedDcs.includes(norm);
                      const status = effectiveDcStatuses[norm];
                      const badge = getStatusBadge(status);

                      return (
                        <button
                          key={norm}
                          type="button"
                          onClick={() => toggleDc(norm)}
                          className={`flex items-center justify-between gap-2 px-3 py-2 rounded-lg border text-left text-xs transition-all ${
                            isChecked
                              ? 'bg-cyber-accent/12 border-cyber-accent/50 text-cyber-text'
                              : 'bg-cyber-bg/40 border-cyber-grid text-cyber-muted hover:border-cyber-accent/30'
                          }`}
                        >
                          <div className="flex items-center gap-2 min-w-0">
                            <span
                              className={`w-4 h-4 rounded flex items-center justify-center border flex-shrink-0 ${
                                isChecked
                                  ? 'bg-cyber-accent border-cyber-accent text-white'
                                  : 'border-cyber-grid bg-cyber-bg'
                              }`}
                            >
                              {isChecked && <Check className="w-3 h-3" strokeWidth={3} />}
                            </span>
                            <DCLabel
                              code={norm}
                              mode="full"
                              className="truncate font-medium"
                            />
                          </div>
                          <span
                            className={`px-1.5 py-0.5 text-[10px] font-semibold rounded border flex-shrink-0 ${badge.cls}`}
                          >
                            {badge.label}
                          </span>
                        </button>
                      );
                    })}
                  </div>
                )}
              </div>

              {/* 2. Optional Hardware Configuration Filter */}
              {(memoryOptions.length > 0 || storageOptions.length > 0) && (
                <div className="p-3 rounded-lg border border-cyber-grid bg-cyber-bg/40 space-y-2.5">
                  <div className="flex items-center gap-1.5 text-xs font-semibold text-cyber-text">
                    <Cpu className="w-3.5 h-3.5 text-cyber-accent" />
                    <span>{t('quickMonitor.configSection')}</span>
                  </div>
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-2.5">
                    <div>
                      <label className="block text-[11px] text-cyber-muted mb-1">
                        {t('quickMonitor.memoryFilter')}
                      </label>
                      <select
                        value={memoryFilter}
                        onChange={(e) => setMemoryFilter(e.target.value)}
                        className="cyber-input w-full text-xs py-1.5"
                      >
                        <option value="">{t('quickMonitor.anyConfig')}</option>
                        {memoryOptions.map((mem) => {
                          const friendly = formatMemoryCode(mem, locale);
                          return (
                            <option key={mem} value={mem}>
                              {friendly ? `${friendly} (${mem})` : mem}
                            </option>
                          );
                        })}
                      </select>
                    </div>
                    <div>
                      <label className="block text-[11px] text-cyber-muted mb-1">
                        {t('quickMonitor.storageFilter')}
                      </label>
                      <select
                        value={storageFilter}
                        onChange={(e) => setStorageFilter(e.target.value)}
                        className="cyber-input w-full text-xs py-1.5"
                      >
                        <option value="">{t('quickMonitor.anyConfig')}</option>
                        {storageOptions.map((sto) => {
                          const friendly = formatStorageCode(sto, locale, false);
                          return (
                            <option key={sto} value={sto}>
                              {friendly ? `${friendly} (${sto})` : sto}
                            </option>
                          );
                        })}
                      </select>
                    </div>
                  </div>
                </div>
              )}

              {/* 3. Alert Notification Toggles */}
              <div className="p-3 rounded-lg border border-cyber-grid bg-cyber-bg/40 space-y-2">
                <div className="text-xs font-semibold text-cyber-text">
                  {t('quickMonitor.notifySection')}
                </div>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                  <label className="flex items-center gap-2 text-xs cursor-pointer select-none p-2 rounded border border-cyber-grid/60 bg-cyber-surface/50">
                    <input
                      type="checkbox"
                      checked={notifyAvailable}
                      onChange={(e) => setNotifyAvailable(e.target.checked)}
                      className="h-4 w-4 accent-cyber-accent"
                    />
                    <span>{t('quickMonitor.notifyAvailable')}</span>
                  </label>
                  <label className="flex items-center gap-2 text-xs cursor-pointer select-none p-2 rounded border border-cyber-grid/60 bg-cyber-surface/50">
                    <input
                      type="checkbox"
                      checked={notifyUnavailable}
                      onChange={(e) => setNotifyUnavailable(e.target.checked)}
                      className="h-4 w-4 accent-cyber-accent"
                    />
                    <span>{t('quickMonitor.notifyUnavailable')}</span>
                  </label>
                </div>
              </div>

              {/* 4. Auto-Order Toggle & Quantity */}
              <div className="p-3 rounded-lg border border-cyber-accent/25 bg-cyber-accent/5 space-y-2.5">
                <div className="flex items-center justify-between gap-2">
                  <label className="flex items-center gap-2 text-xs sm:text-sm font-semibold text-cyber-text cursor-pointer select-none">
                    <input
                      type="checkbox"
                      checked={autoOrder}
                      onChange={(e) => setAutoOrder(e.target.checked)}
                      className="h-4 w-4 accent-cyber-accent"
                    />
                    <ShoppingCart className="w-4 h-4 text-cyber-accent" />
                    <span>{t('quickMonitor.autoOrderToggle')}</span>
                  </label>
                </div>

                {isPresale && (
                  <div className="flex items-start gap-2 p-2.5 rounded-md bg-amber-500/10 border border-amber-500/30 text-amber-300 text-xs leading-relaxed">
                    <AlertTriangle className="w-4 h-4 text-amber-400 flex-shrink-0 mt-0.5" />
                    <span>{t('quickMonitor.presaleCallout')}</span>
                  </div>
                )}

                {autoOrder && (
                  <div className="pt-1 space-y-1">
                    <label className="block text-xs text-cyber-muted">
                      {t('quickMonitor.autoOrderQtyLabel')}
                    </label>
                    <input
                      type="number"
                      min={1}
                      max={100}
                      value={autoOrderQuantity}
                      onChange={(e) =>
                        setAutoOrderQuantity(
                          Math.max(1, Math.min(100, parseInt(e.target.value, 10) || 1))
                        )
                      }
                      className="cyber-input w-40 text-xs py-1.5"
                    />
                    <p className="text-[11px] text-cyber-muted">
                      {t('quickMonitor.autoOrderQtyHint')}
                    </p>
                  </div>
                )}
              </div>

              {/* 5. Auto-Start Monitor Checkbox when Stopped */}
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
                    disabled={isSaving}
                    className="cyber-button px-4 py-2 text-xs font-semibold bg-cyber-accent text-white hover:bg-cyber-accent/90 border-cyber-accent flex items-center gap-1.5"
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
