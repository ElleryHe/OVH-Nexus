import { useState, useEffect, useRef } from 'react';
import { motion } from 'framer-motion';
import { api } from '@/utils/apiClient';
import { toast } from 'sonner';
import { Bell, BellOff, Plus, Trash2, Settings, RefreshCw, History, ChevronUp, Server } from 'lucide-react';
import { useAPI } from '@/context/APIContext';
import { useIsMobile } from '@/hooks/use-mobile';
import { useToast } from '@/components/ToastContainer';

interface VPSSubscription {
  id: string;
  planCode: string;
  ovhSubsidiary: string;
  datacenters: string[];
  monitorLinux: boolean;
  monitorWindows: boolean;
  notifyAvailable: boolean;
  notifyUnavailable: boolean;
  lastStatus: Record<string, any>;
  history?: HistoryEntry[];
  createdAt: string;
}

interface MonitorStatus {
  running: boolean;
  subscriptions_count: number;
  check_interval: number;
}

interface HistoryEntry {
  timestamp: string;
  datacenter: string;
  datacenterCode: string;
  status: string;
  changeType: string;
  oldStatus: string | null;
}

const VPSMonitorPage = () => {
  const isMobile = useIsMobile();
  const { isAuthenticated } = useAPI();
  const { showConfirm } = useToast();
  const [subscriptions, setSubscriptions] = useState<VPSSubscription[]>([]);
  const [monitorStatus, setMonitorStatus] = useState<MonitorStatus>({
    running: false,
    subscriptions_count: 0,
    check_interval: 60
  });
  const [isLoading, setIsLoading] = useState(false);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [showAddForm, setShowAddForm] = useState(false);
  const [expandedHistory, setExpandedHistory] = useState<string | null>(null);
  const [historyData, setHistoryData] = useState<Record<string, HistoryEntry[]>>({});
  const prevSubscriptionsRef = useRef<VPSSubscription[]>([]);
  const loadingTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  
  // VPS型号选项
  const vpsModels = [
    { value: 'vps-2025-model1', label: 'VPS-1' },
    { value: 'vps-2025-model2', label: 'VPS-2' },
    { value: 'vps-2025-model3', label: 'VPS-3' },
    { value: 'vps-2025-model4', label: 'VPS-4' },
    { value: 'vps-2025-model5', label: 'VPS-5' },
    { value: 'vps-2025-model6', label: 'VPS-6' },
  ];

  // 添加订阅表单
  const [formData, setFormData] = useState({
    vpsModel: 'vps-2025-model1',
    ovhSubsidiary: 'IE',
    datacenters: '',
    notifyAvailable: true,
    notifyUnavailable: false
  });

  // 加载订阅列表
  const loadSubscriptions = async (isRefresh = false) => {
    if (isRefresh) {
      setIsRefreshing(true);
    } else {
      // 延迟显示加载状态，避免快速加载时的闪烁
      loadingTimeoutRef.current = setTimeout(() => {
        setIsLoading(true);
      }, 150);
    }
    try {
      const response = await api.get('/vps-monitor/subscriptions');
      const newData = response.data;
      setSubscriptions(newData);
      prevSubscriptionsRef.current = newData;
      // 如果数据加载完成，清除延迟显示的加载状态
      if (loadingTimeoutRef.current) {
        clearTimeout(loadingTimeoutRef.current);
        loadingTimeoutRef.current = null;
      }
      setIsLoading(false);
    } catch (error) {
      console.error('加载VPS订阅失败:', error);
      if (loadingTimeoutRef.current) {
        clearTimeout(loadingTimeoutRef.current);
        loadingTimeoutRef.current = null;
      }
      if (!isRefresh) {
        toast.error('加载VPS订阅失败');
      }
      setIsLoading(false);
    } finally {
      setIsRefreshing(false);
    }
  };

  // 加载监控状态
  const loadMonitorStatus = async () => {
    try {
      const response = await api.get('/vps-monitor/status');
      setMonitorStatus(response.data);
    } catch (error) {
      console.error('加载VPS监控状态失败:', error);
    }
  };

  // 添加订阅
  const handleAddSubscription = async (e: React.FormEvent) => {
    e.preventDefault();
    
    try {
      const datacenters = formData.datacenters
        .split(',')
        .map(dc => dc.trim())
        .filter(dc => dc);
      
      // 获取选中的VPS型号的显示名称
      const selectedModel = vpsModels.find(m => m.value === formData.vpsModel);
      const modelLabel = selectedModel?.label || formData.vpsModel;
      
      await api.post('/vps-monitor/subscriptions', {
        planCode: formData.vpsModel,
        ovhSubsidiary: formData.ovhSubsidiary,
        datacenters: datacenters.length > 0 ? datacenters : [],
        monitorLinux: true,  // 自动监控Linux
        monitorWindows: true,  // 自动监控Windows
        notifyAvailable: formData.notifyAvailable,
        notifyUnavailable: formData.notifyUnavailable
      });
      
      toast.success(`已订阅 ${modelLabel}`);
      setFormData({
        vpsModel: 'vps-2025-model1',
        ovhSubsidiary: 'IE',
        datacenters: '',
        notifyAvailable: true,
        notifyUnavailable: false
      });
      setShowAddForm(false);
      loadSubscriptions(true);
      loadMonitorStatus();
    } catch (error: any) {
      const errorMsg = error.response?.data?.message || '订阅失败';
      toast.error(errorMsg);
    }
  };

  // 删除订阅
  const handleRemoveSubscription = async (id: string, planCode: string) => {
    const confirmed = await showConfirm({
      title: '取消订阅',
      message: `确定要取消订阅 ${planCode} 吗？`,
      confirmText: '确定',
      cancelText: '取消'
    });
    
    if (!confirmed) {
      return;
    }
    
    try {
      await api.delete(`/vps-monitor/subscriptions/${id}`);
      toast.success(`已取消订阅 ${planCode}`);
      loadSubscriptions(true);
      loadMonitorStatus();
    } catch (error) {
      toast.error('取消订阅失败');
    }
  };

  // 清空所有订阅
  const handleClearAll = async () => {
    const confirmed = await showConfirm({
      title: '清空所有订阅',
      message: '确定要清空所有VPS订阅吗？此操作不可撤销。',
      confirmText: '确定清空',
      cancelText: '取消'
    });
    
    if (!confirmed) {
      return;
    }
    
    try {
      const response = await api.delete('/vps-monitor/subscriptions/clear');
      toast.success(`已清空 ${response.data.count} 个VPS订阅`);
      loadSubscriptions(true);
      loadMonitorStatus();
    } catch (error) {
      toast.error('清空订阅失败');
    }
  };

  // 启动/停止监控
  const toggleMonitor = async () => {
    setIsLoading(true);
    try {
      if (monitorStatus.running) {
        await api.post('/vps-monitor/stop');
        toast.success('VPS监控已停止');
      } else {
        await api.post('/vps-monitor/start');
        toast.success('VPS监控已启动');
      }
      loadMonitorStatus();
    } catch (error: any) {
      const errorMsg = error.response?.data?.message || '操作失败';
      toast.error(errorMsg);
    } finally {
      setIsLoading(false);
    }
  };

  // 获取订阅历史记录
  const loadHistory = async (subscriptionId: string) => {
    try {
      const response = await api.get(`/vps-monitor/subscriptions/${subscriptionId}/history`);
      setHistoryData(prev => ({
        ...prev,
        [subscriptionId]: response.data.history
      }));
    } catch (error) {
      toast.error('加载历史记录失败');
    }
  };

  // 切换历史记录展开/收起
  const toggleHistory = async (subscriptionId: string) => {
    if (expandedHistory === subscriptionId) {
      setExpandedHistory(null);
    } else {
      setExpandedHistory(subscriptionId);
      if (!historyData[subscriptionId]) {
        await loadHistory(subscriptionId);
      }
    }
  };

  useEffect(() => {
    if (isAuthenticated) {
      loadSubscriptions();
      loadMonitorStatus();
      
      // 定时刷新状态
      const interval = setInterval(() => {
        loadMonitorStatus();
      }, 30000); // 30秒刷新一次
      
      return () => {
        clearInterval(interval);
        // 清理延迟加载的定时器
        if (loadingTimeoutRef.current) {
          clearTimeout(loadingTimeoutRef.current);
          loadingTimeoutRef.current = null;
        }
      };
    }
  }, [isAuthenticated]);

  return (
    <div className="space-y-4 sm:space-y-6">
      <motion.div
        initial={{ opacity: 0, y: -20 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3 }}
      >
        <h1 className={`${isMobile ? 'text-2xl' : 'text-3xl'} font-bold mb-1 cyber-glow-text`}>VPS补货通知</h1>
        <p className="text-cyber-muted text-sm mb-4 sm:mb-6">选择VPS型号，自动监控所有数据中心的库存变化</p>
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
              <h3 className={`${isMobile ? 'text-base' : 'text-lg'} font-semibold`}>VPS监控状态</h3>
              <p className="text-xs sm:text-sm text-cyber-muted">
                {monitorStatus.running ? (
                  <span className="text-green-400">● 运行中</span>
                ) : (
                  <span className="text-gray-400">● 已停止</span>
                )}
              </p>
            </div>
          </div>
          
          <div className="flex gap-2">
            <button
              onClick={() => {
                loadSubscriptions(true);
                loadMonitorStatus();
              }}
              disabled={isRefreshing}
              className="cyber-button text-sm flex items-center gap-2"
            >
              <RefreshCw size={16} className={`flex-shrink-0 ${isRefreshing ? 'animate-spin' : ''}`} />
              <span className="min-w-[2.5rem]">刷新</span>
            </button>
            <button
              onClick={toggleMonitor}
              disabled={isLoading}
              className={`cyber-button text-sm flex items-center gap-2 ${
                monitorStatus.running 
                  ? 'bg-red-900/30 border-red-700/40 text-red-300 hover:bg-red-800/40 hover:border-red-600/50 hover:text-red-200' 
                  : 'bg-green-900/30 border-green-700/40 text-green-300 hover:bg-green-800/40 hover:border-green-600/50 hover:text-green-200'
              }`}
            >
              {monitorStatus.running ? <BellOff size={16} /> : <Bell size={16} />}
              {monitorStatus.running ? '停止监控' : '启动监控'}
            </button>
          </div>
        </div>

        {/* 统计信息 */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <div className="bg-cyber-grid/10 p-3 rounded border border-cyber-accent/20">
            <p className="text-xs text-cyber-muted mb-1">VPS订阅数</p>
            <p className="text-2xl font-bold text-cyber-accent">{monitorStatus.subscriptions_count}</p>
          </div>
          <div className="bg-cyber-grid/10 p-3 rounded border border-cyber-accent/20">
            <p className="text-xs text-cyber-muted mb-1">检查间隔</p>
            <p className="text-2xl font-bold text-cyber-accent">{monitorStatus.check_interval}s</p>
          </div>
        </div>
      </div>

      {/* 订阅列表 */}
      <div className="cyber-panel p-4">
        <div className="flex justify-between items-center mb-4">
          <h4 className="font-semibold flex items-center gap-2">
            <Server size={18} />
            VPS订阅列表
          </h4>
          <div className="flex gap-2">
            {subscriptions.length > 0 && (
              <button
                onClick={handleClearAll}
                className="px-3 py-1.5 bg-red-500/20 hover:bg-red-500/30 text-red-400 border border-red-500/40 hover:border-red-500/60 rounded-md transition-all flex items-center gap-1.5 text-sm font-medium shadow-sm hover:shadow-md"
              >
                <Trash2 size={14} />
                清空全部
              </button>
            )}
            <button
              onClick={() => setShowAddForm(!showAddForm)}
              className="px-3 py-1.5 bg-cyber-primary hover:bg-cyber-primary-dark text-white border border-cyber-primary/40 hover:border-cyber-primary rounded-md transition-all flex items-center gap-1.5 text-sm font-medium shadow-sm hover:shadow-md"
            >
              <Plus size={14} />
              添加订阅
            </button>
          </div>
        </div>

        {/* 添加订阅表单 */}
        {showAddForm && (
          <motion.div
            initial={{ opacity: 0, height: 0 }}
            animate={{ opacity: 1, height: 'auto' }}
            className="mb-4 p-4 bg-cyber-grid/10 rounded border border-cyber-accent/20"
          >
            <form onSubmit={handleAddSubscription} className="space-y-3">
              <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                <div>
                  <label className="block text-sm text-cyber-muted mb-1">VPS型号 *</label>
                  <select
                    value={formData.vpsModel}
                    onChange={(e) => setFormData({...formData, vpsModel: e.target.value})}
                    className="cyber-input w-full"
                    required
                  >
                    {vpsModels.map(model => (
                      <option key={model.value} value={model.value}>
                        {model.label} ({model.value})
                      </option>
                    ))}
                  </select>
                </div>
                <div>
                  <label className="block text-sm text-cyber-muted mb-1">OVH子公司</label>
                  <select
                    value={formData.ovhSubsidiary}
                    onChange={(e) => setFormData({...formData, ovhSubsidiary: e.target.value})}
                    className="cyber-input w-full"
                  >
                    <option value="IE">IE (爱尔兰)</option>
                    <option value="FR">FR (法国)</option>
                    <option value="GB">GB (英国)</option>
                    <option value="DE">DE (德国)</option>
                    <option value="ES">ES (西班牙)</option>
                    <option value="IT">IT (意大利)</option>
                    <option value="PL">PL (波兰)</option>
                    <option value="CA">CA (加拿大)</option>
                    <option value="US">US (美国)</option>
                  </select>
                </div>
              </div>
              <div>
                <label className="block text-sm text-cyber-muted mb-1">
                  数据中心代码（可选，多个用逗号分隔）
                </label>
                <input
                  type="text"
                  value={formData.datacenters}
                  onChange={(e) => setFormData({...formData, datacenters: e.target.value})}
                  placeholder="例如: eu-west-gra,ca-east-bhs 或留空监控所有"
                  className="cyber-input w-full"
                />
                <p className="text-xs text-cyber-muted mt-1">💡 监控该型号在数据中心的整体库存状态</p>
              </div>
              <div className="space-y-2">
                <p className="text-sm font-medium text-cyber-accent">通知设置</p>
                <div className="flex gap-4">
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={formData.notifyAvailable}
                      onChange={(e) => setFormData({...formData, notifyAvailable: e.target.checked})}
                      className="cyber-checkbox"
                    />
                    <span className="text-sm">有货时提醒</span>
                  </label>
                  <label className="flex items-center gap-2 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={formData.notifyUnavailable}
                      onChange={(e) => setFormData({...formData, notifyUnavailable: e.target.checked})}
                      className="cyber-checkbox"
                    />
                    <span className="text-sm">无货时提醒</span>
                  </label>
                </div>
              </div>
              <div className="flex gap-3">
                <button 
                  type="submit" 
                  className="cyber-button flex-1 px-4 py-2.5 bg-cyber-accent/20 border-cyber-accent/40 text-cyber-accent hover:bg-cyber-accent/30 hover:border-cyber-accent/60 hover:text-cyber-accent"
                >
                  确认添加
                </button>
                <button
                  type="button"
                  onClick={() => setShowAddForm(false)}
                  className="cyber-button flex-1 px-4 py-2.5"
                >
                  取消
                </button>
              </div>
            </form>
          </motion.div>
        )}

        {/* 订阅列表 */}
        {(() => {
          // 在加载期间，如果有之前的数据，显示之前的数据；否则显示当前数据
          const displaySubscriptions = (isLoading && prevSubscriptionsRef.current.length > 0) 
            ? prevSubscriptionsRef.current 
            : subscriptions;
          
          if (displaySubscriptions.length === 0) {
            return (
              <div className="text-center text-cyber-muted py-12">
                <Server size={48} className="mx-auto mb-4 opacity-30" />
                <p>暂无VPS订阅</p>
                <p className="text-sm mt-2">点击"添加订阅"按钮，选择VPS型号开始监控</p>
              </div>
            );
          }
          
          return (
            <div className="space-y-3">
              {displaySubscriptions.map((sub) => (
              <motion.div
                key={sub.id}
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                className="bg-cyber-grid/10 rounded border border-cyber-accent/20 hover:border-cyber-accent/40 transition-colors overflow-hidden"
              >
                <div className="flex justify-between items-start p-3">
                  <div className="flex-1">
                    <div className="flex items-center gap-2 mb-1">
                      <p className="font-medium text-cyber-accent">
                        {vpsModels.find(m => m.value === sub.planCode)?.label || sub.planCode}
                      </p>
                      <span className="text-xs px-2 py-0.5 bg-blue-500/20 text-blue-400 rounded">
                        {sub.ovhSubsidiary}
                      </span>
                    </div>
                    <p className="text-xs text-cyber-muted mt-1">
                      {sub.datacenters.length > 0 
                        ? `监控数据中心: ${sub.datacenters.join(', ')}`
                        : '监控所有数据中心'}
                    </p>
                    <div className="flex gap-2 mt-2 flex-wrap">
                      {sub.notifyAvailable && (
                        <span className="text-xs px-2 py-0.5 bg-green-500/20 text-green-400 rounded">
                          有货提醒
                        </span>
                      )}
                      {sub.notifyUnavailable && (
                        <span className="text-xs px-2 py-0.5 bg-orange-500/20 text-orange-400 rounded">
                          无货提醒
                        </span>
                      )}
                    </div>
                  </div>
                  
                  <div className="flex gap-2">
                    <button
                      onClick={() => toggleHistory(sub.id)}
                      className="p-2 text-cyber-accent hover:bg-cyber-accent/10 rounded transition-colors"
                      title="查看历史记录"
                    >
                      {expandedHistory === sub.id ? <ChevronUp size={16} /> : <History size={16} />}
                    </button>
                    <button
                      onClick={() => handleRemoveSubscription(sub.id, sub.planCode)}
                      className="p-2 text-red-400 hover:bg-red-500/10 rounded transition-colors"
                      title="删除订阅"
                    >
                      <Trash2 size={16} />
                    </button>
                  </div>
                </div>

                {/* 历史记录展开区域 */}
                {expandedHistory === sub.id && (
                  <motion.div
                    initial={{ height: 0, opacity: 0 }}
                    animate={{ height: 'auto', opacity: 1 }}
                    exit={{ height: 0, opacity: 0 }}
                    className="border-t border-cyber-accent/20 bg-cyber-grid/5"
                  >
                    <div className="p-3">
                      <div className="flex items-center gap-2 mb-3">
                        <History size={14} className="text-cyber-accent" />
                        <span className="text-sm font-medium text-cyber-accent">变化历史</span>
                      </div>
                      
                      {historyData[sub.id]?.length > 0 ? (
                        <div className="space-y-2 max-h-64 overflow-y-auto">
                          {historyData[sub.id].map((entry, index) => (
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
                                  <span className="font-medium text-cyber-accent">{entry.datacenter}</span>
                                  <span className={`px-1.5 py-0.5 rounded ${
                                    entry.changeType === 'available' 
                                      ? 'bg-green-500/20 text-green-400' 
                                      : 'bg-red-500/20 text-red-400'
                                  }`}>
                                    {entry.changeType === 'available' ? '有货' : '无货'}
                                  </span>
                                </div>
                                <p className="text-cyber-muted mt-1">
                                  {new Date(entry.timestamp).toLocaleString('zh-CN', {
                                    year: 'numeric',
                                    month: '2-digit',
                                    day: '2-digit',
                                    hour: '2-digit',
                                    minute: '2-digit',
                                    second: '2-digit'
                                  })}
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
              ))}
            </div>
          );
        })()}
      </div>
    </div>
  );
};

export default VPSMonitorPage;
