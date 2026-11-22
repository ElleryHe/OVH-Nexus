import React, { useEffect, useMemo, useState } from 'react';
import {
  ChevronDown,
  ChevronUp,
  Clock,
  Flame,
  Info,
  Sparkles,
  Tag,
  Terminal,
  X,
} from 'lucide-react';
import { useLocale } from '@/context/LocaleContext';
import type { CatalogPromoCampaign, CatalogPromotionsResponse } from '@/types';
import { getCurrencySymbol } from '@/utils/hardwareFormat';

interface CatalogPromoBannerProps {
  promotions: CatalogPromotionsResponse | null;
  selectedPromoCode: string | null;
  onSelectPromoCode: (promoCode: string | null) => void;
}

const STORAGE_COLLAPSED_KEY = 'ovh_nexus_promo_banner_collapsed_v1';

const formatCountdown = (
  endDate: string | undefined,
  remainingHours: number | null | undefined,
  nowMs: number,
  t: (key: string, params?: Record<string, string | number>) => string
): string | null => {
  let hours = remainingHours;
  if (endDate) {
    const parsedEnd = Date.parse(endDate);
    if (Number.isFinite(parsedEnd)) {
      hours = (parsedEnd - nowMs) / 3600000;
    }
  }
  if (typeof hours !== 'number' || !Number.isFinite(hours) || hours <= 0) {
    return null;
  }
  if (hours >= 48) {
    return t('promoBanner.endsInDays', { days: Math.floor(hours / 24) });
  }
  return t('promoBanner.endsInHours', { hours: Math.max(1, Math.round(hours)) });
};

const formatModeShort = (
  mode: string,
  t: (key: string, params?: Record<string, string | number>) => string
): string => {
  const key = `billing.modeShort.${mode}`;
  const translated = t(key);
  return translated === key ? mode : translated;
};

export const CatalogPromoBanner: React.FC<CatalogPromoBannerProps> = ({
  promotions,
  selectedPromoCode,
  onSelectPromoCode,
}) => {
  const { t } = useLocale();
  const [showInspector, setShowInspector] = useState(false);
  const [inspectorTab, setInspectorTab] = useState<'campaigns' | 'guide'>('campaigns');
  const [nowMs, setNowMs] = useState<number>(() => Date.now());

  useEffect(() => {
    const timer = window.setInterval(() => setNowMs(Date.now()), 60_000);
    return () => window.clearInterval(timer);
  }, []);

  const campaignFingerprint = useMemo(() => {
    if (!promotions || !promotions.campaigns || promotions.campaigns.length === 0) {
      return '';
    }
    return promotions.campaigns
      .map((c) => c.promoCode)
      .sort()
      .join('|');
  }, [promotions]);

  const [collapsedForFp, setCollapsedForFp] = useState<string>(() => {
    try {
      return localStorage.getItem(STORAGE_COLLAPSED_KEY) || '';
    } catch {
      return '';
    }
  });

  if (!promotions || !promotions.campaigns || promotions.campaigns.length === 0) {
    return null;
  }

  const isCollapsed = collapsedForFp !== '' && collapsedForFp === campaignFingerprint;

  const setCollapsed = (collapse: boolean) => {
    const val = collapse ? campaignFingerprint : '';
    setCollapsedForFp(val);
    try {
      if (val) {
        localStorage.setItem(STORAGE_COLLAPSED_KEY, val);
      } else {
        localStorage.removeItem(STORAGE_COLLAPSED_KEY);
      }
    } catch {
      // ignore storage errors
    }
  };

  const handleToggleCampaign = (camp: CatalogPromoCampaign) => {
    if (selectedPromoCode?.toLowerCase() === camp.promoCode.toLowerCase()) {
      onSelectPromoCode(null);
    } else {
      onSelectPromoCode(camp.promoCode);
    }
  };

  const symbol = getCurrencySymbol(promotions.currency || 'EUR');

  return (
    <>
      {isCollapsed ? (
        <div className="rounded-lg border border-rose-500/35 bg-gradient-to-r from-rose-500/10 via-amber-500/8 to-cyber-surface px-3 py-2 flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-wrap items-center gap-2 text-xs min-w-0">
            <span className="inline-flex items-center gap-1 font-semibold text-rose-600 dark:text-rose-300">
              <Flame size={14} className="text-rose-500 flex-shrink-0" />
              <span>
                {t('promoBanner.collapsedSummary', {
                  campaigns: promotions.totalCampaigns,
                  inStock: promotions.inStockPromotedPlans,
                })}
              </span>
            </span>
            {selectedPromoCode && (
              <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-rose-500/20 text-rose-700 dark:text-rose-200 border border-rose-500/40 font-mono text-[11px]">
                <span>{t('promoBanner.filteringBy', { code: selectedPromoCode })}</span>
                <button
                  type="button"
                  onClick={() => onSelectPromoCode(null)}
                  className="hover:opacity-75"
                  aria-label={t('promoBanner.clearFilter')}
                >
                  <X size={11} />
                </button>
              </span>
            )}
          </div>

          <div className="flex items-center gap-1.5 flex-shrink-0">
            <button
              type="button"
              onClick={() => setShowInspector(true)}
              className="px-2 py-1 rounded-md border border-cyber-grid bg-cyber-surface hover:border-cyber-accent/50 text-[11px] font-medium text-cyber-text inline-flex items-center gap-1 transition-colors"
            >
              <Terminal size={11} className="text-cyber-accent" />
              <span>{t('promoBanner.openInspector')}</span>
            </button>
            <button
              type="button"
              onClick={() => setCollapsed(false)}
              className="px-2 py-1 rounded-md border border-rose-500/35 bg-rose-500/15 hover:bg-rose-500/25 text-[11px] font-semibold text-rose-700 dark:text-rose-200 inline-flex items-center gap-1 transition-colors"
            >
              <ChevronDown size={12} />
              <span>{t('promoBanner.expand')}</span>
            </button>
          </div>
        </div>
      ) : (
        <div className="rounded-xl border border-rose-500/35 bg-gradient-to-br from-rose-500/12 via-amber-500/6 to-cyber-surface/95 p-3.5 shadow-sm space-y-2.5">
          {/* Header Row */}
          <div className="flex flex-wrap items-start justify-between gap-2">
            <div className="flex items-start gap-2.5 min-w-0">
              <div className="w-8 h-8 rounded-lg bg-rose-500/20 border border-rose-500/40 flex items-center justify-center flex-shrink-0 mt-0.5">
                <Sparkles size={16} className="text-rose-500 dark:text-rose-300" />
              </div>
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <h2 className="text-sm font-bold text-cyber-text">
                    {t('promoBanner.title')}
                  </h2>
                  <span className="px-1.5 py-0.5 rounded bg-rose-500/20 text-rose-700 dark:text-rose-200 border border-rose-500/35 text-[10px] font-mono font-bold">
                    {promotions.zone} · {promotions.currency}
                  </span>
                  {selectedPromoCode && (
                    <button
                      type="button"
                      onClick={() => onSelectPromoCode(null)}
                      className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full bg-rose-500 text-white text-[10px] font-semibold hover:bg-rose-600 transition-colors"
                    >
                      <span>{t('promoBanner.clearFilter')}</span>
                      <X size={10} />
                    </button>
                  )}
                </div>
                <p className="text-xs text-cyber-muted mt-0.5">
                  {t('promoBanner.subtitle', {
                    zone: promotions.zone,
                    campaigns: promotions.totalCampaigns,
                    plans: promotions.totalPromotedPlans,
                    inStock: promotions.inStockPromotedPlans,
                  })}
                </p>
              </div>
            </div>

            <div className="flex items-center gap-1.5 flex-shrink-0">
              <button
                type="button"
                onClick={() => {
                  setInspectorTab('campaigns');
                  setShowInspector(true);
                }}
                className="px-2.5 py-1 rounded-md border border-cyber-accent/40 bg-cyber-accent/10 hover:bg-cyber-accent/20 text-xs font-semibold text-cyber-accent inline-flex items-center gap-1.5 transition-colors"
              >
                <Terminal size={12} />
                <span>{t('promoBanner.openInspector')}</span>
              </button>
              <button
                type="button"
                onClick={() => setCollapsed(true)}
                className="px-2 py-1 rounded-md border border-cyber-grid bg-cyber-surface hover:text-cyber-text text-xs text-cyber-muted inline-flex items-center gap-1 transition-colors"
                title={t('promoBanner.collapse')}
              >
                <ChevronUp size={13} />
                <span className="hidden sm:inline">{t('promoBanner.collapse')}</span>
              </button>
            </div>
          </div>

          {/* Interactive Campaign Chips */}
          <div className="flex flex-wrap items-center gap-2">
            {promotions.campaigns.map((camp) => {
              const isSelected =
                selectedPromoCode?.toLowerCase() === camp.promoCode.toLowerCase();
              const countdown = formatCountdown(camp.endDate, camp.remainingHours, nowMs, t);
              return (
                <button
                  key={camp.promoCode}
                  type="button"
                  onClick={() => handleToggleCampaign(camp)}
                  className={`group text-left px-2.5 py-1.5 rounded-lg border transition-all flex flex-wrap items-center gap-1.5 text-xs ${
                    isSelected
                      ? 'bg-rose-500/20 border-rose-500 text-rose-800 dark:text-rose-100 ring-1 ring-rose-500/40 shadow-sm'
                      : 'bg-cyber-surface/85 border-cyber-grid hover:border-rose-500/45 text-cyber-text'
                  }`}
                  title={camp.description || camp.promoCode}
                >
                  <span className="px-1.5 py-0.5 rounded bg-rose-500/20 text-rose-700 dark:text-rose-200 font-bold font-mono text-[10px]">
                    {camp.badge}
                  </span>
                  <span className="font-mono font-semibold text-[11px]">
                    {camp.promoCode}
                  </span>
                  <span
                    className={`px-1.5 py-0.2 rounded text-[10px] font-medium border ${
                      camp.autoApplied
                        ? 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-300 border-emerald-500/30'
                        : 'bg-amber-500/15 text-amber-700 dark:text-amber-300 border-amber-500/30'
                    }`}
                  >
                    {camp.autoApplied
                      ? t('promoBanner.autoAppliedTag')
                      : t('promoBanner.voucherTag')}
                  </span>
                  <span className="text-[10px] font-mono text-cyber-muted">
                    {t('promoBanner.inStockCount', {
                      inStock: camp.inStockPlanCount,
                      total: camp.planCount,
                    })}
                  </span>
                  {countdown && (
                    <span className="inline-flex items-center gap-0.5 text-[10px] font-mono text-amber-600 dark:text-amber-300">
                      <Clock size={10} />
                      {countdown}
                    </span>
                  )}
                </button>
              );
            })}
          </div>
        </div>
      )}

      {/* Global Promo Rules & Debug Inspector Modal */}
      {showInspector && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center p-3 sm:p-5 bg-black/70 backdrop-blur-sm"
          onClick={() => setShowInspector(false)}
        >
          <div
            className="w-full max-w-4xl max-h-[88vh] flex flex-col rounded-xl border border-cyber-accent/40 bg-cyber-surface shadow-2xl overflow-hidden"
            onClick={(e) => e.stopPropagation()}
          >
            {/* Modal Header */}
            <div className="px-5 py-3.5 border-b border-cyber-grid/60 bg-cyber-bg/60 flex items-start justify-between gap-3">
              <div>
                <div className="flex items-center gap-2">
                  <Terminal size={16} className="text-cyber-accent" />
                  <h3 className="text-base font-bold text-cyber-text">
                    {t('promoBanner.inspectorTitle')}
                  </h3>
                  <span className="px-1.5 py-0.5 rounded bg-cyber-accent/15 text-cyber-accent font-mono text-[10px] font-bold">
                    {promotions.endpoint} / {promotions.zone}
                  </span>
                </div>
                <p className="text-xs text-cyber-muted mt-1">
                  {t('promoBanner.inspectorSubtitle')}
                </p>
              </div>
              <button
                type="button"
                onClick={() => setShowInspector(false)}
                className="p-1.5 rounded-lg border border-cyber-grid text-cyber-muted hover:text-cyber-text hover:border-cyber-accent/50"
              >
                <X size={16} />
              </button>
            </div>

            {/* Tabs */}
            <div className="px-5 pt-2.5 border-b border-cyber-grid/50 flex items-center gap-2 bg-cyber-bg/30">
              <button
                type="button"
                onClick={() => setInspectorTab('campaigns')}
                className={`px-3 py-2 text-xs font-semibold border-b-2 transition-colors flex items-center gap-1.5 ${
                  inspectorTab === 'campaigns'
                    ? 'border-cyber-accent text-cyber-accent'
                    : 'border-transparent text-cyber-muted hover:text-cyber-text'
                }`}
              >
                <Tag size={12} />
                <span>
                  {t('promoBanner.tabCampaigns')} ({promotions.campaigns.length})
                </span>
              </button>
              <button
                type="button"
                onClick={() => setInspectorTab('guide')}
                className={`px-3 py-2 text-xs font-semibold border-b-2 transition-colors flex items-center gap-1.5 ${
                  inspectorTab === 'guide'
                    ? 'border-cyber-accent text-cyber-accent'
                    : 'border-transparent text-cyber-muted hover:text-cyber-text'
                }`}
              >
                <Info size={12} />
                <span>{t('promoBanner.tabGuide')}</span>
              </button>
            </div>

            {/* Modal Body */}
            <div className="p-5 overflow-y-auto space-y-3 flex-1">
              {inspectorTab === 'campaigns' ? (
                promotions.campaigns.map((camp) => {
                  const countdown = formatCountdown(camp.endDate, camp.remainingHours, nowMs, t);
                  const isSelected =
                    selectedPromoCode?.toLowerCase() === camp.promoCode.toLowerCase();
                  return (
                    <div
                      key={camp.promoCode}
                      className={`p-3.5 rounded-lg border space-y-2 ${
                        isSelected
                          ? 'bg-rose-500/10 border-rose-500/50'
                          : 'bg-cyber-bg/40 border-cyber-grid'
                      }`}
                    >
                      <div className="flex flex-wrap items-start justify-between gap-2">
                        <div className="space-y-1 min-w-0">
                          <div className="flex flex-wrap items-center gap-1.5">
                            <span className="px-2 py-0.5 rounded bg-rose-500/20 text-rose-700 dark:text-rose-200 border border-rose-500/35 font-mono font-bold text-xs">
                              {camp.badge}
                            </span>
                            <span className="font-mono font-bold text-sm text-cyber-text">
                              {camp.promoCode}
                            </span>
                            <span
                              className={`px-1.5 py-0.5 rounded text-[10px] font-semibold border ${
                                camp.autoApplied
                                  ? 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-300 border-emerald-500/30'
                                  : 'bg-amber-500/15 text-amber-700 dark:text-amber-300 border-amber-500/30'
                              }`}
                            >
                              {camp.autoApplied
                                ? `${t('promoBanner.autoAppliedTag')} (context=${camp.context})`
                                : `${t('promoBanner.voucherTag')} (context=${camp.context})`}
                            </span>
                            {camp.maxSavingsAmount > 0 && (
                              <span className="px-1.5 py-0.5 rounded bg-cyber-accent/15 text-cyber-accent font-mono text-[10px] font-semibold">
                                {t('promoBanner.maxSavings', {
                                  amount: `${symbol}${camp.maxSavingsAmount.toFixed(2)}`,
                                })}
                              </span>
                            )}
                          </div>
                          {camp.description && (
                            <p className="text-xs text-cyber-muted">{camp.description}</p>
                          )}
                        </div>

                        <button
                          type="button"
                          onClick={() => {
                            onSelectPromoCode(isSelected ? null : camp.promoCode);
                            setShowInspector(false);
                          }}
                          className={`px-2.5 py-1 rounded-md text-xs font-semibold border flex-shrink-0 transition-colors ${
                            isSelected
                              ? 'bg-rose-500 text-white border-rose-500'
                              : 'bg-cyber-accent/15 text-cyber-accent border-cyber-accent/40 hover:bg-cyber-accent/25'
                          }`}
                        >
                          {isSelected
                            ? t('promoBanner.clearFilter')
                            : t('promoBanner.filterThisCampaign')}
                        </button>
                      </div>

                      <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-[11px] font-mono text-cyber-muted pt-1 border-t border-cyber-grid/40">
                        <span>
                          {t('promoBanner.supportedModes')}:{' '}
                          <strong className="text-cyber-text">
                            {camp.supportedModes
                              .map((m) => `${formatModeShort(m, t)} (${m})`)
                              .join(', ')}
                          </strong>
                        </span>
                        <span>
                          capacities:{' '}
                          <strong className="text-cyber-text">
                            {camp.capacities.join(', ')}
                          </strong>
                        </span>
                        {camp.durationMonths ? (
                          <span>{t('promoBanner.durationRule', { months: camp.durationMonths })}</span>
                        ) : null}
                        {camp.minimumDurationMonths ? (
                          <span>
                            {t('promoBanner.minDurationRule', {
                              months: camp.minimumDurationMonths,
                            })}
                          </span>
                        ) : null}
                        <span>
                          {t('promoBanner.inStockCount', {
                            inStock: camp.inStockPlanCount,
                            total: camp.planCount,
                          })}
                        </span>
                        {countdown && (
                          <span className="text-amber-600 dark:text-amber-300 font-semibold">
                            ⏱ {countdown}
                            {camp.endDate ? ` (${camp.endDate.slice(0, 10)})` : ''}
                          </span>
                        )}
                      </div>

                      {camp.samplePlans && camp.samplePlans.length > 0 && (
                        <div className="flex flex-wrap items-center gap-1 pt-0.5">
                          {camp.samplePlans.map((pc) => (
                            <span
                              key={pc}
                              className="px-1.5 py-0.5 rounded bg-cyber-surface border border-cyber-grid text-[10px] font-mono text-cyber-muted"
                            >
                              {pc}
                            </span>
                          ))}
                          {camp.planCount > camp.samplePlans.length && (
                            <span className="text-[10px] font-mono text-cyber-muted">
                              +{camp.planCount - camp.samplePlans.length}
                            </span>
                          )}
                        </div>
                      )}
                    </div>
                  );
                })
              ) : (
                <div className="space-y-4 text-xs">
                  <div className="p-4 rounded-lg border border-amber-500/35 bg-amber-500/10 space-y-2">
                    <h4 className="font-bold text-sm text-amber-700 dark:text-amber-300">
                      {t('promoBanner.autoGuideTitle')}
                    </h4>
                    <p className="text-cyber-text leading-relaxed">
                      {t('promoBanner.autoGuideBody')}
                    </p>
                    {promotions.autoAppliedCodes.length > 0 && (
                      <div className="flex flex-wrap gap-1.5 pt-1">
                        {promotions.autoAppliedCodes.map((code) => (
                          <span
                            key={code}
                            className="px-2 py-0.5 rounded bg-cyber-surface border border-amber-500/40 font-mono text-[11px] font-semibold text-amber-700 dark:text-amber-200"
                          >
                            {code}
                          </span>
                        ))}
                      </div>
                    )}
                  </div>

                  <div className="p-4 rounded-lg border border-cyber-accent/35 bg-cyber-accent/10 space-y-2">
                    <h4 className="font-bold text-sm text-cyber-accent">
                      {t('promoBanner.addonGuideTitle')}
                    </h4>
                    <p className="text-cyber-text leading-relaxed">
                      {t('promoBanner.addonGuideBody')}
                    </p>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </>
  );
};
