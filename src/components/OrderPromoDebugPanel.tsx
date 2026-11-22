import React, { useEffect, useRef, useState } from 'react';
import {
  AlertTriangle,
  CheckCircle2,
  FlaskConical,
  Loader2,
  Sparkles,
  Terminal,
  XCircle,
} from 'lucide-react';
import { api } from '@/utils/apiClient';
import { useLocale } from '@/context/LocaleContext';
import type {
  CatalogPromotionsResponse,
  PlanPricingMode,
  ServerPlan,
  ServerPriceProbeResult,
} from '@/types';
import {
  findAutoAppliedPromoCodeForPlanMode,
  getCurrencySymbol,
} from '@/utils/hardwareFormat';

interface OrderPromoDebugPanelProps {
  server: ServerPlan;
  promotions: CatalogPromotionsResponse | null;
  activeModeKey: string;
  activeDuration: string;
  activeConfigMode: PlanPricingMode | null;
  basePlanModes: PlanPricingMode[];
  configModes: PlanPricingMode[];
  selectedDatacenter: string;
  selectedOptions: string[];
  coupon: string;
  isAuthenticated: boolean;
  onClearCoupon: () => void;
  onSwitchMode: (mode: string) => void;
}

const formatModeShort = (
  mode: string,
  t: (key: string, params?: Record<string, string | number>) => string
): string => {
  const key = `billing.modeShort.${mode}`;
  const translated = t(key);
  return translated === key ? mode : translated;
};

export const OrderPromoDebugPanel: React.FC<OrderPromoDebugPanelProps> = ({
  server,
  promotions,
  activeModeKey,
  activeDuration,
  activeConfigMode,
  basePlanModes,
  configModes,
  selectedDatacenter,
  selectedOptions,
  coupon,
  isAuthenticated,
  onClearCoupon,
  onSwitchMode,
}) => {
  const { t } = useLocale();
  const [isProbing, setIsProbing] = useState(false);
  const [probeResult, setProbeResult] = useState<ServerPriceProbeResult | null>(null);
  const probeAbortRef = useRef<AbortController | null>(null);
  const probeSeqRef = useRef(0);

  // Reset stale cart probe and cancel any in-flight request whenever plan/mode/duration/options/coupon/dc changes.
  const sortedOptionsKey = [...selectedOptions].sort().join(',');
  const probeKey = `${server.planCode}|${activeModeKey}|${activeDuration}|${selectedDatacenter}|${sortedOptionsKey}|${coupon.trim()}`;
  useEffect(() => {
    probeSeqRef.current += 1;
    if (probeAbortRef.current) {
      probeAbortRef.current.abort();
      probeAbortRef.current = null;
    }
    setIsProbing(false);
    setProbeResult(null);
    return () => {
      probeSeqRef.current += 1;
      if (probeAbortRef.current) {
        probeAbortRef.current.abort();
        probeAbortRef.current = null;
      }
    };
  }, [probeKey]);

  const symbol = getCurrencySymbol(server.currency || promotions?.currency || 'EUR');
  const trimmedCoupon = coupon.trim();

  // Smart Auto-Promo Guard: detect if user pasted an auto-applied catalog campaign ID
  // that specifically applies to this server planCode and active pricing mode.
  const matchedAutoCode = findAutoAppliedPromoCodeForPlanMode(
    server.planCode,
    activeModeKey,
    trimmedCoupon,
    [
      ...(activeConfigMode ? [activeConfigMode] : []),
      ...configModes,
      ...basePlanModes,
    ],
    promotions
  );

  const baseMode =
    basePlanModes.find((m) => m.mode.toLowerCase() === activeModeKey.toLowerCase()) ||
    basePlanModes[0] ||
    null;

  const eligiblePromoModes = (configModes.length > 0 ? configModes : basePlanModes).filter(
    (m) => m.hasPromo
  );
  const activeHasPromo = Boolean(activeConfigMode?.hasPromo || baseMode?.hasPromo);
  const missedPromoSample =
    !activeHasPromo && eligiblePromoModes.length > 0 ? eligiblePromoModes[0] : null;

  const baseMonthly = baseMode?.monthlyEquivalentPrice ?? server.startingPrice ?? 0;
  const baseOrigMonthly = baseMode?.originalMonthlyPrice ?? server.originalMonthlyPrice ?? baseMonthly;
  const cfgMonthly = activeConfigMode?.monthlyEquivalentPrice ?? baseMonthly;
  const cfgOrigMonthly = activeConfigMode?.originalMonthlyPrice ?? baseOrigMonthly;
  const addonMonthlyDelta = Math.max(0, Number((cfgMonthly - baseMonthly).toFixed(2)));
  const addonOrigDelta = Math.max(0, Number((cfgOrigMonthly - baseOrigMonthly).toFixed(2)));
  const addonDiscounted = addonOrigDelta > addonMonthlyDelta + 0.01;

  const renewPromo = activeConfigMode?.renewPromo || baseMode?.renewPromo || null;
  const setupPromo = activeConfigMode?.setupPromo || baseMode?.setupPromo || null;
  const setupFee = activeConfigMode?.setupFee ?? baseMode?.setupFee ?? server.setupFee ?? 0;
  const origSetupFee =
    activeConfigMode?.originalSetupFee ??
    baseMode?.originalSetupFee ??
    server.originalSetupFee ??
    setupFee;

  const runLiveCartProbe = async () => {
    if (!isAuthenticated || isProbing) return;
    if (probeAbortRef.current) {
      probeAbortRef.current.abort();
    }
    const controller = new AbortController();
    probeAbortRef.current = controller;
    const reqSeq = ++probeSeqRef.current;

    setIsProbing(true);
    setProbeResult(null);
    try {
      const res = await api.post(
        `/servers/${encodeURIComponent(server.planCode)}/price`,
        {
          datacenter: selectedDatacenter || 'gra',
          options: selectedOptions,
          pricingMode: activeModeKey,
          duration: activeDuration,
          coupon: trimmedCoupon || undefined,
        },
        { signal: controller.signal }
      );
      if (reqSeq !== probeSeqRef.current || controller.signal.aborted) {
        return;
      }
      setProbeResult(res.data as ServerPriceProbeResult);
    } catch (err: any) {
      if (
        reqSeq !== probeSeqRef.current ||
        controller.signal.aborted ||
        err?.name === 'CanceledError' ||
        err?.name === 'AbortError' ||
        err?.code === 'ERR_CANCELED'
      ) {
        return;
      }
      const payload = err?.response?.data as ServerPriceProbeResult | undefined;
      if (payload && typeof payload === 'object') {
        setProbeResult({
          ...payload,
          success: false,
          error: payload.autoAppliedPromoHint || payload.error || err?.message || 'Cart probe failed',
        });
      } else {
        setProbeResult({
          success: false,
          error: err?.message || 'Cart probe failed',
        });
      }
    } finally {
      if (reqSeq === probeSeqRef.current) {
        setIsProbing(false);
        if (probeAbortRef.current === controller) {
          probeAbortRef.current = null;
        }
      }
    }
  };

  return (
    <div className="space-y-2 pt-1">
      {/* Smart Auto-Promo Coupon Guard Warning */}
      {matchedAutoCode && (
        <div className="p-2.5 rounded-lg border border-amber-500/45 bg-amber-500/12 text-xs flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-start gap-2 text-amber-700 dark:text-amber-200 min-w-0 flex-1">
            <AlertTriangle size={14} className="text-amber-500 flex-shrink-0 mt-0.5" />
            <span>{t('promoDebug.autoCouponWarn', { code: matchedAutoCode })}</span>
          </div>
          <button
            type="button"
            onClick={onClearCoupon}
            className="px-2 py-1 rounded bg-amber-500/20 hover:bg-amber-500/30 border border-amber-500/40 text-amber-800 dark:text-amber-100 text-[11px] font-semibold flex-shrink-0"
          >
            {t('promoDebug.clearAutoCoupon')}
          </button>
        </div>
      )}

      {/* Missed-Mode Promo Alert (e.g. selected upfront24 when promo only applies to default/upfront12) */}
      {missedPromoSample && (
        <div className="p-2.5 rounded-lg border border-rose-500/35 bg-rose-500/10 text-xs flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-start gap-2 text-rose-700 dark:text-rose-200 min-w-0 flex-1">
            <Sparkles size={14} className="text-rose-500 flex-shrink-0 mt-0.5" />
            <span>
              {t('promoDebug.missedModeWarn', {
                currentMode: formatModeShort(activeModeKey, t),
                eligibleModes: eligiblePromoModes
                  .map((m) => formatModeShort(m.mode, t))
                  .join(' / '),
                code: missedPromoSample.promoCode || missedPromoSample.promoBadge || 'PROMO',
              })}
            </span>
          </div>
          <button
            type="button"
            onClick={() => onSwitchMode(missedPromoSample.mode)}
            className="px-2 py-1 rounded bg-rose-500/20 hover:bg-rose-500/30 border border-rose-500/40 text-rose-800 dark:text-rose-100 text-[11px] font-semibold flex-shrink-0"
          >
            {t('promoDebug.switchToMode', {
              mode: formatModeShort(missedPromoSample.mode, t),
            })}
          </button>
        </div>
      )}

      {/* Live Cart Coupon / Quote Probe Action + Debug Accordion */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <details className="flex-1 min-w-[240px] rounded-lg border border-cyber-grid/80 bg-cyber-bg/40 text-xs">
          <summary className="px-3 py-2 cursor-pointer font-semibold text-cyber-muted hover:text-cyber-text flex items-center justify-between gap-2 select-none">
            <span className="inline-flex items-center gap-1.5">
              <Terminal size={12} className="text-cyber-accent" />
              <span>{t('promoDebug.title')}</span>
              {activeHasPromo && (
                <span className="px-1.5 py-0.2 rounded bg-emerald-500/15 text-emerald-700 dark:text-emerald-300 border border-emerald-500/30 text-[10px] font-mono">
                  {t('promoDebug.autoBadge')}
                </span>
              )}
            </span>
          </summary>

          <div className="px-3 pb-3 pt-2 border-t border-cyber-grid/50 space-y-2 text-[11px]">
            {/* Row 1: Base Plan */}
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="space-y-0.5">
                <div className="font-medium text-cyber-text">
                  {t('promoDebug.basePlanRow', {
                    mode: `${formatModeShort(activeModeKey, t)} (${activeModeKey})`,
                  })}
                </div>
                {renewPromo && (
                  <div className="flex flex-wrap items-center gap-1.5 font-mono text-[10px] text-rose-600 dark:text-rose-300">
                    <span className="px-1.5 py-0.2 rounded bg-rose-500/15 border border-rose-500/30 font-bold">
                      {renewPromo.code}
                    </span>
                    <span>context={renewPromo.context}</span>
                    {renewPromo.duration ? <span>{renewPromo.duration}m</span> : null}
                    {renewPromo.minimumDuration ? (
                      <span>min={renewPromo.minimumDuration}m</span>
                    ) : null}
                    {renewPromo.endDate ? (
                      <span>until {renewPromo.endDate.slice(0, 10)}</span>
                    ) : null}
                  </div>
                )}
              </div>
              <div className="font-mono text-right">
                <span className="font-bold text-emerald-500 dark:text-emerald-400">
                  {symbol}
                  {baseMonthly.toFixed(2)}/m
                </span>
                {baseOrigMonthly > baseMonthly + 0.01 && (
                  <span className="line-through text-cyber-muted ml-1.5">
                    {symbol}
                    {baseOrigMonthly.toFixed(2)}
                  </span>
                )}
              </div>
            </div>

            {/* Row 2: Hardware Addon Delta */}
            {addonMonthlyDelta > 0 && (
              <div className="flex flex-wrap items-center justify-between gap-2 pt-1.5 border-t border-cyber-grid/30">
                <div className="space-y-0.5">
                  <div className="font-medium text-cyber-text">
                    {t('promoDebug.addonDeltaRow')}
                  </div>
                  <div className="text-[10px] text-cyber-muted">
                    {addonDiscounted
                      ? t('promoDebug.addonDiscounted')
                      : t('promoDebug.addonNotDiscounted')}
                  </div>
                </div>
                <div className="font-mono text-right">
                  <span className="font-semibold text-cyber-text">
                    +{symbol}
                    {addonMonthlyDelta.toFixed(2)}/m
                  </span>
                  {addonDiscounted && (
                    <span className="line-through text-cyber-muted ml-1.5">
                      +{symbol}
                      {addonOrigDelta.toFixed(2)}
                    </span>
                  )}
                </div>
              </div>
            )}

            {/* Row 3: Setup Fee & Dual-Promo Code */}
            <div className="flex flex-wrap items-center justify-between gap-2 pt-1.5 border-t border-cyber-grid/30">
              <div className="space-y-0.5">
                <div className="font-medium text-cyber-text">
                  {t('promoDebug.setupFeeRow')}
                </div>
                {setupPromo && (
                  <div className="flex flex-wrap items-center gap-1.5 font-mono text-[10px] text-emerald-700 dark:text-emerald-300">
                    <span className="px-1.5 py-0.2 rounded bg-emerald-500/15 border border-emerald-500/30 font-bold">
                      {setupPromo.code}
                    </span>
                    <span>context={setupPromo.context}</span>
                    {setupPromo.endDate ? (
                      <span>until {setupPromo.endDate.slice(0, 10)}</span>
                    ) : null}
                  </div>
                )}
              </div>
              <div className="font-mono text-right">
                <span
                  className={
                    setupFee <= 0
                      ? 'font-bold text-emerald-500 dark:text-emerald-400'
                      : 'text-cyber-text'
                  }
                >
                  {symbol}
                  {setupFee.toFixed(2)}
                </span>
                {origSetupFee > setupFee + 0.01 && (
                  <span className="line-through text-cyber-muted ml-1.5">
                    {symbol}
                    {origSetupFee.toFixed(2)}
                  </span>
                )}
              </div>
            </div>
          </div>
        </details>

        <button
          type="button"
          onClick={runLiveCartProbe}
          disabled={!isAuthenticated || isProbing || Boolean(matchedAutoCode)}
          title={!isAuthenticated ? t('promoDebug.verifyNeedAuth') : undefined}
          className="px-3 py-2 rounded-lg border border-cyber-accent/45 bg-cyber-accent/12 hover:bg-cyber-accent/22 text-cyber-accent text-xs font-semibold inline-flex items-center gap-1.5 disabled:opacity-50 transition-colors self-start"
        >
          {isProbing ? (
            <Loader2 size={13} className="animate-spin" />
          ) : (
            <FlaskConical size={13} />
          )}
          <span>{isProbing ? t('promoDebug.verifying') : t('promoDebug.verifyBtn')}</span>
        </button>
      </div>

      {/* Live OVH Cart Probe Result Box */}
      {probeResult && (
        <div
          className={`p-3 rounded-lg border text-xs space-y-2 ${
            probeResult.success
              ? 'border-emerald-500/40 bg-emerald-500/10'
              : 'border-red-500/45 bg-red-500/10'
          }`}
        >
          {probeResult.success && probeResult.price ? (
            <>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <span className="inline-flex items-center gap-1.5 font-semibold text-emerald-700 dark:text-emerald-300">
                  <CheckCircle2 size={14} />
                  <span>{t('promoDebug.verifyTitle')}</span>
                </span>
                {trimmedCoupon && (
                  <span className="px-2 py-0.5 rounded bg-emerald-500/20 text-emerald-700 dark:text-emerald-200 border border-emerald-500/35 font-mono text-[10px] font-bold">
                    {t('promoDebug.cartCouponAccepted', { coupon: trimmedCoupon })}
                  </span>
                )}
              </div>

              <div className="grid grid-cols-2 sm:grid-cols-4 gap-2 font-mono text-[11px]">
                {typeof probeResult.price.prices?.withoutTax === 'number' && (
                  <div className="p-2 rounded bg-cyber-surface/70 border border-cyber-grid/60">
                    <div className="text-[10px] font-sans text-cyber-muted">
                      {t('promoDebug.cartExVat')}
                    </div>
                    <div className="font-bold text-cyber-text">
                      {getCurrencySymbol(probeResult.price.prices.currencyCode)}
                      {probeResult.price.prices.withoutTax.toFixed(2)}
                    </div>
                  </div>
                )}
                {typeof probeResult.price.monthlyEquivalentWithoutTax === 'number' && (
                  <div className="p-2 rounded bg-cyber-surface/70 border border-cyber-grid/60">
                    <div className="text-[10px] font-sans text-cyber-muted">
                      {t('promoDebug.cartMonthlyEq')}
                    </div>
                    <div className="font-bold text-emerald-500 dark:text-emerald-400">
                      {getCurrencySymbol(probeResult.price.prices?.currencyCode)}
                      {probeResult.price.monthlyEquivalentWithoutTax.toFixed(2)}/m
                    </div>
                  </div>
                )}
                {typeof probeResult.price.prices?.withTax === 'number' && (
                  <div className="p-2 rounded bg-cyber-surface/70 border border-cyber-grid/60">
                    <div className="text-[10px] font-sans text-cyber-muted">
                      {t('promoDebug.cartWithTax')}
                    </div>
                    <div className="font-bold text-cyber-accent">
                      {getCurrencySymbol(probeResult.price.prices.currencyCode)}
                      {probeResult.price.prices.withTax.toFixed(2)}
                    </div>
                  </div>
                )}
                {typeof probeResult.price.setupFeeWithoutTax === 'number' && (
                  <div className="p-2 rounded bg-cyber-surface/70 border border-cyber-grid/60">
                    <div className="text-[10px] font-sans text-cyber-muted">
                      {t('promoDebug.cartSetup')}
                    </div>
                    <div className="font-bold text-cyber-text">
                      {getCurrencySymbol(probeResult.price.prices?.currencyCode)}
                      {probeResult.price.setupFeeWithoutTax.toFixed(2)}
                    </div>
                  </div>
                )}
              </div>

              {probeResult.price.details && probeResult.price.details.length > 0 && (
                <div className="pt-1.5 border-t border-emerald-500/25 space-y-1 font-mono text-[10px]">
                  {probeResult.price.details.map((line, idx) => (
                    <div
                      key={`${line.detailType || 'line'}-${idx}`}
                      className="flex items-center justify-between gap-2 text-cyber-muted"
                    >
                      <span className="truncate">
                        {line.detailType ? `[${line.detailType}] ` : ''}
                        {line.description || '-'}
                      </span>
                      {typeof line.totalWithoutTax === 'number' && (
                        <span className="flex-shrink-0 text-cyber-text font-semibold">
                          {getCurrencySymbol(line.currencyCode || probeResult.price?.prices?.currencyCode)}
                          {line.totalWithoutTax.toFixed(2)}
                        </span>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </>
          ) : (
            <div className="flex items-start gap-2 text-red-700 dark:text-red-300">
              <XCircle size={15} className="flex-shrink-0 mt-0.5 text-red-500" />
              <div className="space-y-0.5 min-w-0">
                <div className="font-semibold">{t('promoDebug.cartRejected')}</div>
                <div className="font-mono text-[11px] break-words">
                  {probeResult.autoAppliedPromoHint || probeResult.error}
                </div>
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
};
