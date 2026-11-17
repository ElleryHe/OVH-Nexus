import { useState, useEffect, useRef, useCallback } from 'react';
import { useSearchParams, useNavigate } from 'react-router-dom';
import { motion } from 'framer-motion';
import { api } from '@/utils/apiClient';
import { toast } from 'sonner';
import {
  Bell,
  BellOff,
  Plus,
  Trash2,
  Settings,
  RefreshCw,
  History,
  ChevronUp,
  ChevronDown,
  ExternalLink,
  Filter,
  AlertTriangle,
  Edit3,
  Cpu,
} from 'lucide-react';
import { useAPI } from '@/context/APIContext';
import { useLocale } from '@/context/LocaleContext';
import { useIsMobile } from '@/hooks/use-mobile';
import { useToast } from '@/components/ToastContainer';
import { DCLabel } from '@/components/DCLabel';
import { QuickMonitorDialog } from '@/components/QuickMonitorDialog';
import {
  useCatalogDictionary,
  resolvePlanDisplay,
  formatMemoryCode,
  formatStorageCode,
  getCurrencySymbol,
} from '@/utils/hardwareFormat';
import type {
  StockSubscription,
  StockTarget,
  SkuChangeRecord,
} from '@/types';

interface MonitorStatus {
  running: boolean;
  subscriptions_count: number;
  known_servers_count: number;
  check_interval: number;
}

interface HistoryEntry {
  timestamp: string;
  datacenter: string;
  status: string;
  changeType: string;
  oldStatus: string | null;
  configKey?: string;
  config?: {
    memory: string;
    storage: string;
    display: string;
  };
}

const isStatusAvailable = (st?: string): boolean =>
  Boolean(st && st !== 'unavailable' && st !== 'unknown');

const MonitorPage = () => {
  const isMobile = useIsMobile();
  const { locale, t } = useLocale();
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const filterPlanCode = (searchParams.get('planCode') || '').trim();
  const catalogDict = useCatalogDictionary();
  const { isAuthenticated } = useAPI();
  const { showConfirm } = useToast();

  const [subscriptions, setSubscriptions] = useState<StockSubscription[]>([]);
  const [monitorStatus, setMonitorStatus] = useState<MonitorStatus>({
    running: false,
    subscriptions_count: 0,
    known_servers_count: 0,
    check_interval: 5,
  });
  const [skuChanges, setSkuChanges] = useState<SkuChangeRecord[]>([]);
  const [skuTotalIndexed, setSkuTotalIndexed] = useState<number>(0);
  const [showSkuHistory, setShowSkuHistory] = useState<boolean>(false);
  const [isCheckingSku, setIsCheckingSku] = useState<boolean>(false);

  const [isLoading, setIsLoading] = useState(false);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [showAddForm, setShowAddForm] = useState(false);
  const [newPlanCodeInput, setNewPlanCodeInput] = useState('');
  const [editingSub, setEditingSub] = useState<{
    planCode: string;
    serverName?: string;
    subscription?: StockSubscription | null;
  } | null>(null);

  const [expandedHistory, setExpandedHistory] = useState<string | null>(null);
  const [historyData, setHistoryData] = useState<
    Record<string, HistoryEntry[]>
  >({});
  const prevSubscriptionsRef = useRef<StockSubscription[]>([]);
  const currentSubscriptionsRef = useRef<StockSubscription[]>([]);
  const loadingTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const loadSubscriptions = async (isRefresh = false) => {
    if (isRefresh) {
      setIsRefreshing(true);
    } else {
      loadingTimeoutRef.current = setTimeout(() => {
        setIsLoading(true);
      }, 150);
    }
    try {
      const response = await api.get('/monitor/subscriptions');
      const newData = (
        Array.isArray(response.data) ? response.data : []
      ) as StockSubscription[];

      currentSubscriptionsRef.current = newData;
      setSubscriptions(newData);
      prevSubscriptionsRef.current = newData;
      if (loadingTimeoutRef.current) {
        clearTimeout(loadingTimeoutRef.current);
        loadingTimeoutRef.current = null;
      }
      setIsLoading(false);
    } catch {
      if (loadingTimeoutRef.current) {
        clearTimeout(loadingTimeoutRef.current);
        loadingTimeoutRef.current = null;
      }
      if (!isRefresh) {
        toast.error('加载订阅失败');
      }
      setIsLoading(false);
    } finally {
      setIsRefreshing(false);
    }
  };

  const loadMonitorStatus = async () => {
    try {
      const response = await api.get('/monitor/status');
      setMonitorStatus(response.data);
    } catch {
      // ignore
    }
  };

  const loadSkuChanges = useCallback(async () => {
    try {
      const res = await api.get('/sku/changes?limit=100');
      if (res.data) {
        setSkuChanges(res.data.changes || []);
        setSkuTotalIndexed(res.data.status?.totalSkus || 0);
      }
    } catch {
      // ignore
    }
  }, []);

  const handleManualSkuCheck = async () => {
    setIsCheckingSku(true);
    try {
      const res = await api.post('/sku/check');
      if (res.data) {
        setSkuChanges(res.data.changes || []);
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

  const handleOpenAddModal = (e: React.FormEvent) => {
    e.preventDefault();
    const code = newPlanCodeInput.trim();
    if (!code) {
      toast.error('请输入服务器型号 (planCode)');
      return;
    }
    const existing = subscriptions.find(
      (s) => s.planCode.toLowerCase() === code.toLowerCase()
    );
    setShowAddForm(false);
    setNewPlanCodeInput('');
    setEditingSub({
      planCode: code,
      serverName: existing?.serverName,
      subscription: existing || null,
    });
  };

  const handleRemoveSubscription = async (planCode: string) => {
    const confirmed = await showConfirm({
      title: '取消订阅',
      message: `确定要取消订阅 ${planCode} 吗？`,
      confirmText: '确定',
      cancelText: '取消',
    });

    if (!confirmed) return;

    try {
      await api.delete(`/monitor/subscriptions/${encodeURIComponent(planCode)}`);
      toast.success(`已取消订阅 ${planCode}`);
      const current = prevSubscriptionsRef.current || [];
      prevSubscriptionsRef.current = current.filter(
        (s) => s.planCode !== planCode
      );
      currentSubscriptionsRef.current =
        currentSubscriptionsRef.current.filter((s) => s.planCode !== planCode);
      loadSubscriptions(true);
      loadMonitorStatus();
    } catch {
      toast.error('取消订阅失败');
    }
  };

  const handleClearAll = async () => {
    const confirmed = await showConfirm({
      title: '清空所有订阅',
      message: '确定要清空所有订阅吗？此操作不可撤销。',
      confirmText: '确定清空',
      cancelText: '取消',
    });

    if (!confirmed) return;

    try {
      const response = await api.delete('/monitor/subscriptions/clear');
      toast.success(`已清空 ${response.data.count} 个订阅`);
      prevSubscriptionsRef.current = [];
      currentSubscriptionsRef.current = [];
      loadSubscriptions(true);
      loadMonitorStatus();
    } catch {
      toast.error('清空订阅失败');
    }
  };

  const loadHistory = async (planCode: string) => {
    try {
      const response = await api.get(
        `/monitor/subscriptions/${encodeURIComponent(planCode)}/history`
      );
      setHistoryData((prev) => ({
        ...prev,
        [planCode]: response.data.history || [],
      }));
    } catch {
      toast.error('加载历史记录失败');
    }
  };

  const toggleHistory = async (planCode: string) => {
    if (expandedHistory === planCode) {
      setExpandedHistory(null);
    } else {
      setExpandedHistory(planCode);
      if (!historyData[planCode]) {
        await loadHistory(planCode);
      }
    }
  };

  useEffect(() => {
    loadSkuChanges();
    if (isAuthenticated) {
      loadSubscriptions();
      loadMonitorStatus();

      const interval = setInterval(() => {
        loadMonitorStatus();
        loadSubscriptions(true);
      }, 30000);

      return () => {
        clearInterval(interval);
        if (loadingTimeoutRef.current) {
          clearTimeout(loadingTimeoutRef.current);
          loadingTimeoutRef.current = null;
        }
      };
    }
  }, [isAuthenticated, loadSkuChanges]);

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
    <div className="space-y-4 sm:space-y-6">
      <motion.div
        initial={{ opacity: 0, y: -20 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3 }}
      >
        <h1
          className={`${
            isMobile ? 'text-2xl' : 'text-3xl'
          } font-bold mb-1 cyber-glow-text`}
        >
          独服库存与货架监控
        </h1>
        <p className="text-cyber-muted text-sm mb-4 sm:mb-6">
          支持按配置（内存 + 硬盘）精确监控、最高月价保护上限自动下单与货架 SKU 变动追踪
        </p>
      </motion.div>

      {/* 监控状态卡片 */}
      <div className="cyber-panel p-3 sm:p-4">
        <div className="flex flex-col sm:flex-row justify-between items-start gap-3 sm:gap-0 mb-4">
          <div className="flex items-center gap-2 sm:gap-3">
            {monitorStatus.running ? (
              <div className="p-1.5 sm:p-2 bg-green-500/20 rounded">
                <Bell className="text-green-400" size={isMobile ? 20 : 24} />
              </div>
            ) : (
              <div className="p-1.5 sm:p-2 bg-gray-500/20 rounded">
                <BellOff className="text-gray-400" size={isMobile ? 20 : 24} />
              </div>
            )}
            <div>
              <h3
                className={`${isMobile ? 'text-base' : 'text-lg'} font-semibold`}
              >
                监控状态
              </h3>
              <p className="text-xs sm:text-sm text-cyber-muted">
                {monitorStatus.running ? (
                  <span className="text-green-400">● 运行中</span>
                ) : (
                  <span className="text-gray-400">● 已停止</span>
                )}
              </p>
            </div>
          </div>

          <button
            onClick={() => {
              loadSubscriptions(true);
              loadMonitorStatus();
              loadSkuChanges();
            }}
            disabled={isRefreshing}
            className="cyber-button text-xs sm:text-sm flex items-center gap-2"
          >
            <RefreshCw
              size={isMobile ? 14 : 16}
              className={`flex-shrink-0 ${isRefreshing ? 'animate-spin' : ''}`}
            />
            <span className="min-w-[2.5rem]">刷新</span>
          </button>
        </div>

        <div className="grid grid-cols-3 gap-2 sm:gap-4">
          <div className="bg-cyber-grid/10 p-2 sm:p-3 rounded border border-cyber-accent/20">
            <p className="text-[10px] sm:text-xs text-cyber-muted mb-1">订阅数</p>
            <p className="text-lg sm:text-2xl font-bold text-cyber-accent">
              {monitorStatus.subscriptions_count}
            </p>
          </div>
          <div className="bg-cyber-grid/10 p-2 sm:p-3 rounded border border-cyber-accent/20">
            <p className="text-[10px] sm:text-xs text-cyber-muted mb-1">
              检查间隔
            </p>
            <p className="text-lg sm:text-2xl font-bold text-cyber-accent">
              {monitorStatus.check_interval}s
            </p>
          </div>
          <div className="bg-cyber-grid/10 p-2 sm:p-3 rounded border border-cyber-accent/20">
            <p className="text-[10px] sm:text-xs text-cyber-muted mb-1 truncate">
              已知服务器
            </p>
            <p className="text-lg sm:text-2xl font-bold text-cyber-accent">
              {monitorStatus.known_servers_count}
            </p>
          </div>
        </div>
      </div>

      {/* SKU 货架变动监控记录面板 */}
      <div className="cyber-panel p-3.5 sm:p-4 border-cyber-accent/30">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <button
            type="button"
            onClick={() => setShowSkuHistory((prev) => !prev)}
            className="flex items-center gap-2 text-left font-semibold text-sm sm:text-base text-cyber-text hover:text-cyber-accent transition-colors"
          >
            <History className="w-4 h-4 text-cyber-accent" />
            <span>货架 SKU 变动历史（上新 / 开售 / 换代 / 下架）</span>
            <span className="px-2 py-0.5 text-xs rounded-full bg-cyber-accent/15 text-cyber-accent font-mono">
              {skuChanges.length}
            </span>
            {skuTotalIndexed > 0 && (
              <span className="text-xs text-cyber-muted font-normal hidden sm:inline">
                （已收录 {skuTotalIndexed} 款）
              </span>
            )}
            {showSkuHistory ? (
              <ChevronUp className="w-4 h-4 text-cyber-muted" />
            ) : (
              <ChevronDown className="w-4 h-4 text-cyber-muted" />
            )}
          </button>

          <button
            type="button"
            onClick={handleManualSkuCheck}
            disabled={isCheckingSku}
            className="cyber-button text-xs py-1.5 px-3 flex items-center gap-1.5"
          >
            <RefreshCw
              className={`w-3.5 h-3.5 ${isCheckingSku ? 'animate-spin' : ''}`}
            />
            <span>{isCheckingSku ? '检测中...' : '立即检测 SKU'}</span>
          </button>
        </div>

        {showSkuHistory && (
          <div className="mt-3 pt-3 border-t border-cyber-grid space-y-2 max-h-72 overflow-y-auto">
            {skuChanges.length === 0 ? (
              <p className="text-xs text-cyber-muted text-center py-4">
                暂无 SKU 货架变动记录
              </p>
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

      {/* 订阅列表 */}
      <div className="cyber-panel p-4">
        <div className="flex justify-between items-center mb-4">
          <h4 className="font-semibold flex items-center gap-2">
            <Settings size={18} />
            配置级监控订阅列表
          </h4>
          <div className="flex gap-2">
            {subscriptions.length > 0 && (
              <button
                onClick={handleClearAll}
                className="cyber-button text-sm flex items-center gap-1.5 bg-red-900/30 border-red-700/40 text-red-300 hover:bg-red-800/40"
              >
                <Trash2 size={14} />
                清空全部
              </button>
            )}
            <button
              onClick={() => setShowAddForm(!showAddForm)}
              className="cyber-button text-sm flex items-center gap-1.5 bg-cyber-accent/20 border-cyber-accent/40 text-cyber-accent hover:bg-cyber-accent/30"
            >
              <Plus size={14} />
              添加订阅
            </button>
          </div>
        </div>

        {/* 快速输入型号并打开多目标配置弹窗 */}
        {showAddForm && (
          <motion.div
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: 'auto' }}
            className="mb-4 p-4 bg-cyber-grid/10 rounded border border-cyber-accent/20"
          >
            <form
              onSubmit={handleOpenAddModal}
              className="flex flex-col sm:flex-row items-stretch sm:items-end gap-3"
            >
              <div className="flex-1">
                <label className="block text-xs text-cyber-muted mb-1">
                  输入服务器型号 (planCode)，下一步选择具体配置与最高月价上限 *
                </label>
                <input
                  type="text"
                  value={newPlanCodeInput}
                  onChange={(e) => setNewPlanCodeInput(e.target.value)}
                  placeholder="例如: 24ska01 或 25rise01-v1"
                  className="cyber-input w-full text-sm"
                  required
                />
              </div>
              <div className="flex gap-2">
                <button
                  type="submit"
                  className="cyber-button px-4 py-2 text-xs font-semibold bg-cyber-accent text-white border-cyber-accent"
                >
                  配置监控目标 →
                </button>
                <button
                  type="button"
                  onClick={() => setShowAddForm(false)}
                  className="cyber-button px-4 py-2 text-xs"
                >
                  取消
                </button>
              </div>
            </form>
          </motion.div>
        )}

        {filterPlanCode && (
          <div className="mb-3 px-3 py-2 rounded-md border border-cyber-accent/40 bg-cyber-accent/10 flex items-center justify-between gap-2 text-xs">
            <div className="flex items-center gap-2 text-cyber-accent font-medium">
              <Filter size={14} />
              <span>
                {t('monitor.filterPlanCodeBanner', { planCode: filterPlanCode })}
              </span>
            </div>
            <button
              type="button"
              onClick={() => {
                searchParams.delete('planCode');
                setSearchParams(searchParams);
              }}
              className="px-2 py-0.5 rounded bg-cyber-surface border border-cyber-grid text-cyber-text hover:border-cyber-accent/50 transition-colors"
            >
              {t('monitor.clearFilter')}
            </button>
          </div>
        )}

        {(() => {
          const baseSubscriptions =
            isLoading && prevSubscriptionsRef.current.length > 0
              ? prevSubscriptionsRef.current
              : subscriptions;
          const displaySubscriptions = filterPlanCode
            ? baseSubscriptions.filter(
                (s) => s.planCode.toLowerCase() === filterPlanCode.toLowerCase()
              )
            : baseSubscriptions;

          if (displaySubscriptions.length === 0) {
            return (
              <div className="text-center text-cyber-muted py-12">
                <Bell size={48} className="mx-auto mb-4 opacity-30" />
                <p>暂无监控订阅</p>
                <p className="text-sm mt-2">
                  点击右上角「添加订阅」或在「实时库存矩阵 / 服务器目录」中点击「快速监控」
                </p>
              </div>
            );
          }

          return (
            <div className="space-y-3">
              {displaySubscriptions.map((sub) => {
                const planMeta = resolvePlanDisplay(
                  sub.planCode,
                  '',
                  locale,
                  catalogDict
                );
                const isHighlighted =
                  filterPlanCode &&
                  sub.planCode.toLowerCase() === filterPlanCode.toLowerCase();

                // Normalize targets (fallback to 1 synthesized target if legacy subscription has no targets array)
                const effectiveTargets: StockTarget[] =
                  Array.isArray(sub.targets) && sub.targets.length > 0
                    ? sub.targets
                    : [
                        {
                          id: 'legacy-1',
                          configKey: '',
                          options: [],
                          label: '',
                          datacenters: sub.datacenters || [],
                          notifyAvailable: sub.notifyAvailable,
                          notifyUnavailable: sub.notifyUnavailable,
                          autoOrder: Boolean(sub.autoOrder),
                          quantity: sub.autoOrderQuantity || 1,
                          currency: planMeta.currency || 'EUR',
                          lastStatus: sub.lastStatus || {},
                        },
                      ];

                const anyTargetAutoOrder =
                  Boolean(sub.autoOrder) ||
                  effectiveTargets.some((tItem) => tItem.autoOrder);

                return (
                  <motion.div
                    key={sub.planCode}
                    initial={{ opacity: 0, y: 10 }}
                    animate={{ opacity: 1, y: 0 }}
                    className={`bg-cyber-grid/10 rounded-lg border transition-colors overflow-hidden ${
                      isHighlighted
                        ? 'border-cyber-accent/60 ring-2 ring-cyber-accent/30 bg-cyber-accent/5'
                        : 'border-cyber-accent/20 hover:border-cyber-accent/40'
                    }`}
                  >
                    {/* Legacy Auto-Order Paused Warning Banner */}
                    {sub.needsConfigSelection && (
                      <div className="px-3.5 py-2.5 bg-amber-500/15 border-b border-amber-500/40 flex flex-col sm:flex-row sm:items-center justify-between gap-2 text-xs text-amber-200">
                        <div className="flex items-start sm:items-center gap-2">
                          <AlertTriangle
                            size={15}
                            className="text-amber-400 flex-shrink-0 mt-0.5 sm:mt-0"
                          />
                          <span className="font-medium">
                            {t('monitor.legacyAutoOrderPausedBanner')}
                          </span>
                        </div>
                        <button
                          type="button"
                          onClick={() =>
                            setEditingSub({
                              planCode: sub.planCode,
                              serverName: sub.serverName || planMeta.name,
                              subscription: sub,
                            })
                          }
                          className="px-2.5 py-1 rounded bg-amber-500/25 hover:bg-amber-500/35 text-amber-200 border border-amber-400/50 font-semibold flex items-center gap-1 self-start sm:self-auto whitespace-nowrap"
                        >
                          <Edit3 size={12} />
                          <span>{t('monitor.selectConfigAction')}</span>
                        </button>
                      </div>
                    )}

                    <div className="p-3.5 space-y-3">
                      {/* Card Header */}
                      <div className="flex justify-between items-start gap-2">
                        <div className="flex-1 min-w-0">
                          <div className="flex flex-wrap items-center gap-2">
                            <p className="font-bold text-cyber-accent text-base">
                              {sub.serverName || planMeta.name}
                            </p>
                            {planMeta.regionLabel && (
                              <span className="px-1.5 py-0.5 text-[11px] rounded bg-cyber-accent/15 text-cyber-accent border border-cyber-accent/30">
                                {planMeta.regionLabel}
                              </span>
                            )}
                            <span className="px-2 py-0.5 text-xs font-mono rounded bg-cyber-grid/40 text-cyber-muted">
                              SKU: {sub.planCode}
                            </span>
                            <span className="px-2 py-0.5 text-[11px] rounded-full bg-cyber-accent/15 text-cyber-accent font-medium">
                              {t('monitor.targetCountBadge', {
                                count: effectiveTargets.length,
                              })}
                            </span>
                            {planMeta.cpuText &&
                              !sub.serverName?.includes(planMeta.cpuText) && (
                                <span className="text-xs text-cyber-muted">
                                  · {planMeta.cpuText}
                                </span>
                              )}
                          </div>
                        </div>

                        <div className="flex items-center gap-1.5 flex-shrink-0">
                          {anyTargetAutoOrder && (
                            <button
                              type="button"
                              onClick={() =>
                                navigate(
                                  `/queue?planCode=${encodeURIComponent(
                                    sub.planCode
                                  )}`
                                )
                              }
                              className="text-xs px-2 py-1 rounded bg-indigo-500/15 text-indigo-300 border border-indigo-500/30 hover:bg-indigo-500/25 transition-colors inline-flex items-center gap-1"
                            >
                              <span>{t('monitor.viewQueueTasks')}</span>
                              <ExternalLink size={11} />
                            </button>
                          )}
                          <button
                            type="button"
                            onClick={() =>
                              setEditingSub({
                                planCode: sub.planCode,
                                serverName: sub.serverName || planMeta.name,
                                subscription: sub,
                              })
                            }
                            className="px-2.5 py-1 text-xs font-medium text-cyber-accent bg-cyber-accent/10 hover:bg-cyber-accent/20 border border-cyber-accent/30 rounded transition-colors flex items-center gap-1"
                            title={t('monitor.editSubscription')}
                          >
                            <Edit3 size={13} />
                            <span className="hidden sm:inline">
                              {t('monitor.editSubscription')}
                            </span>
                          </button>
                          <button
                            onClick={() => toggleHistory(sub.planCode)}
                            className="p-1.5 text-cyber-accent hover:bg-cyber-accent/10 rounded transition-colors"
                            title="查看历史记录"
                          >
                            {expandedHistory === sub.planCode ? (
                              <ChevronUp size={16} />
                            ) : (
                              <History size={16} />
                            )}
                          </button>
                          <button
                            onClick={() =>
                              handleRemoveSubscription(sub.planCode)
                            }
                            className="p-1.5 text-red-400 hover:bg-red-500/10 rounded transition-colors"
                            title="删除订阅"
                          >
                            <Trash2 size={16} />
                          </button>
                        </div>
                      </div>

                      {/* Per-Target List */}
                      <div className="space-y-2">
                        {effectiveTargets.map((target, idx) => {
                          const symbol = getCurrencySymbol(
                            target.currency || planMeta.currency
                          );
                          const statusSource =
                            target.lastStatus &&
                            Object.keys(target.lastStatus).length > 0
                              ? target.lastStatus
                              : sub.lastStatus || {};
                          const dcList =
                            target.datacenters && target.datacenters.length > 0
                              ? target.datacenters
                              : Object.keys(statusSource);

                          return (
                            <div
                              key={target.id || idx}
                              className="p-2.5 rounded-md border border-cyber-grid/70 bg-cyber-bg/40 space-y-2 text-xs"
                            >
                              <div className="flex flex-wrap items-center justify-between gap-2">
                                <div className="flex flex-wrap items-center gap-1.5">
                                  <span className="px-1.5 py-0.5 rounded bg-cyber-grid/50 text-cyber-muted font-mono text-[10px]">
                                    #{idx + 1}
                                  </span>
                                  <Cpu
                                    size={13}
                                    className="text-cyber-accent flex-shrink-0"
                                  />
                                  <span className="font-semibold text-cyber-text">
                                    {target.configKey
                                      ? target.label || target.configKey
                                      : t('quickMonitor.anyConfigNotifyOnly')}
                                  </span>
                                  {target.configKey && (
                                    <span className="text-[11px] font-mono text-cyber-muted">
                                      ({target.configKey})
                                    </span>
                                  )}
                                </div>

                                <div className="flex flex-wrap items-center gap-1.5">
                                  {target.notifyAvailable && (
                                    <span className="px-2 py-0.5 bg-green-500/15 text-green-400 border border-green-500/30 rounded text-[11px]">
                                      有货提醒
                                    </span>
                                  )}
                                  {target.notifyUnavailable && (
                                    <span className="px-2 py-0.5 bg-orange-500/15 text-orange-400 border border-orange-500/30 rounded text-[11px]">
                                      无货提醒
                                    </span>
                                  )}
                                  {target.autoOrder && (
                                    <span className="px-2 py-0.5 bg-cyber-accent/20 text-cyber-accent border border-cyber-accent/40 rounded text-[11px] font-semibold">
                                      {t('monitor.autoOrderQtyBadge', {
                                        qty: target.quantity || 1,
                                      })}
                                    </span>
                                  )}
                                  {typeof target.maxMonthlyPrice === 'number' &&
                                    target.maxMonthlyPrice > 0 && (
                                      <span className="px-2 py-0.5 bg-emerald-500/15 text-emerald-300 border border-emerald-500/30 rounded text-[11px] font-mono">
                                        {t('monitor.priceCapBadge', {
                                          price: `${symbol}${target.maxMonthlyPrice.toFixed(
                                            2
                                          )}`,
                                        })}
                                      </span>
                                    )}
                                </div>
                              </div>

                              {/* Per-DC Status Badges */}
                              <div className="flex flex-wrap items-center gap-1.5">
                                <span className="text-[11px] text-cyber-muted">
                                  {target.datacenters &&
                                  target.datacenters.length > 0
                                    ? '监控机房:'
                                    : '全机房监控:'}
                                </span>
                                {dcList.length > 0 ? (
                                  dcList.map((dc) => {
                                    const st = statusSource[dc.toLowerCase()] || statusSource[dc];
                                    const inStock = isStatusAvailable(st);
                                    return (
                                      <span
                                        key={dc}
                                        className={`px-1.5 py-0.5 rounded border text-[11px] font-medium inline-flex items-center gap-1 ${
                                          inStock
                                            ? 'bg-emerald-500/15 border-emerald-500/40 text-emerald-300'
                                            : st === 'unavailable'
                                            ? 'bg-red-500/10 border-red-500/25 text-red-300/80'
                                            : 'bg-cyber-surface border-cyber-grid text-cyber-text'
                                        }`}
                                      >
                                        <DCLabel code={dc} mode="compact" />
                                        {st && (
                                          <span className="text-[10px] font-mono opacity-90">
                                            ({inStock ? st : '无'})
                                          </span>
                                        )}
                                      </span>
                                    );
                                  })
                                ) : (
                                  <span className="text-[11px] text-cyber-muted">
                                    全部数据中心（等待首轮检测）
                                  </span>
                                )}
                              </div>
                            </div>
                          );
                        })}
                      </div>
                    </div>

                    {/* 历史记录展开区域 */}
                    {expandedHistory === sub.planCode && (
                      <motion.div
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: 'auto', opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        className="border-t border-cyber-accent/20 bg-cyber-grid/5"
                      >
                        <div className="p-3">
                          <div className="flex items-center gap-2 mb-3">
                            <History size={14} className="text-cyber-accent" />
                            <span className="text-sm font-medium text-cyber-accent">
                              变化历史
                            </span>
                          </div>

                          {historyData[sub.planCode]?.length > 0 ? (
                            <div className="space-y-2 max-h-64 overflow-y-auto">
                              {historyData[sub.planCode].map((entry, index) => (
                                <div
                                  key={index}
                                  className="flex items-start gap-3 p-2 bg-cyber-grid/10 rounded text-xs"
                                >
                                  <div className="flex-shrink-0 mt-1">
                                    {entry.changeType === 'available' ? (
                                      <div className="w-2 h-2 bg-green-400 rounded-full"></div>
                                    ) : (
                                      <div className="w-2 h-2 bg-red-400 rounded-full"></div>
                                    )}
                                  </div>
                                  <div className="flex-1 min-w-0">
                                    <div className="flex items-center gap-2 flex-wrap">
                                      <span className="font-medium text-cyber-accent">
                                        <DCLabel
                                          code={entry.datacenter}
                                          mode="full"
                                        />
                                      </span>
                                      <span
                                        className={`px-1.5 py-0.5 rounded ${
                                          entry.changeType === 'available'
                                            ? 'bg-green-500/20 text-green-400'
                                            : 'bg-red-500/20 text-red-400'
                                        }`}
                                      >
                                        {entry.changeType === 'available'
                                          ? '有货'
                                          : '无货'}
                                      </span>
                                    </div>
                                    {(entry.config || entry.configKey) && (
                                      <div className="text-xs text-cyber-muted mt-1">
                                        <span className="inline-block px-2 py-0.5 bg-cyber-accent/10 rounded mr-1">
                                          {entry.config &&
                                          (entry.config.memory ||
                                            entry.config.storage)
                                            ? `${formatMemoryCode(
                                                entry.config.memory,
                                                locale
                                              )} + ${formatStorageCode(
                                                entry.config.storage,
                                                locale,
                                                false
                                              )}`
                                            : entry.config?.display ||
                                              entry.configKey}
                                        </span>
                                      </div>
                                    )}
                                    <p className="text-cyber-muted mt-1 text-xs">
                                      {new Date(entry.timestamp).toLocaleString(
                                        'zh-CN',
                                        {
                                          year: 'numeric',
                                          month: '2-digit',
                                          day: '2-digit',
                                          hour: '2-digit',
                                          minute: '2-digit',
                                          second: '2-digit',
                                        }
                                      )}
                                    </p>
                                  </div>
                                </div>
                              ))}
                            </div>
                          ) : (
                            <p className="text-xs text-cyber-muted text-center py-4">
                              暂无历史记录
                            </p>
                          )}
                        </div>
                      </motion.div>
                    )}
                  </motion.div>
                );
              })}
            </div>
          );
        })()}
      </div>

      <QuickMonitorDialog
        open={Boolean(editingSub)}
        onOpenChange={(open) => {
          if (!open) setEditingSub(null);
        }}
        planCode={editingSub?.planCode || ''}
        serverName={editingSub?.serverName}
        existingSubscription={editingSub?.subscription}
        onSuccess={() => {
          loadSubscriptions(true);
          loadMonitorStatus();
        }}
      />
    </div>
  );
};

export default MonitorPage;
