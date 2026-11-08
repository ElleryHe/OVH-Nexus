import { useState, useEffect } from "react";
import { createPortal } from "react-dom";
import { motion, AnimatePresence } from "framer-motion";
import { useAPI } from "@/context/APIContext";
import { toast } from "sonner";
import { useNavigate } from "react-router-dom";
import { CacheManager } from "@/components/CacheManager";
import { useIsMobile } from "@/hooks/use-mobile";
import { api } from "@/utils/apiClient";
import { X, AlertCircle, FileText, CheckCircle, AlertTriangle } from "lucide-react";

const SettingsPage = () => {
  const isMobile = useIsMobile();
  const navigate = useNavigate();
  const { 
    appKey, 
    appSecret, 
    consumerKey, 
    endpoint,
    tgToken,
    hasTgToken,
    tgChatId,
    iam,
    zone,
    isLoading,
    isAuthenticated,
    setAPIKeys,
    checkAuthentication,
    accounts,
    currentAccountId,
    setCurrentAccount,
    refreshAccounts
  } = useAPI();

  const [formValues, setFormValues] = useState({
    appKey: "",
    appSecret: "",
    consumerKey: "",
    endpoint: "ovh-eu",
    tgToken: "",
    tgChatId: "",
    iam: "go-ovh-ie",
    zone: "IE"
  });
  const [isSaving, setIsSaving] = useState(false);
  const [showValues, setShowValues] = useState({
    appKey: false,
    appSecret: false,
    consumerKey: false,
    tgToken: false
  });
  const [accountForm, setAccountForm] = useState({
    id: "",
    alias: "",
    appKey: "",
    appSecret: "",
    consumerKey: "",
    endpoint: "ovh-eu",
    zone: "IE"
  });
  const [isSubmittingAccount, setIsSubmittingAccount] = useState(false);
  
  // Telegram Webhook 相关状态
  const [webhookUrl, setWebhookUrl] = useState("");
  const [webhookInfo, setWebhookInfo] = useState<any>(null);
  const [isSettingWebhook, setIsSettingWebhook] = useState(false);
  const [isLoadingWebhookInfo, setIsLoadingWebhookInfo] = useState(false);
  const [showErrorHistoryDialog, setShowErrorHistoryDialog] = useState(false);
  const [apiKeyValid, setApiKeyValid] = useState<boolean | null>(null);
  const [ovhAuthValid, setOvhAuthValid] = useState<boolean | null>(null);
  const accountFieldsHidden = true;

  // Load current values when component mounts
  useEffect(() => {
    setFormValues({
      appKey: appKey || "",
      appSecret: appSecret || "",
      consumerKey: consumerKey || "",
      endpoint: endpoint || "ovh-eu",
      tgToken: "",
      tgChatId: tgChatId || "",
      iam: iam || "go-ovh-ie",
      zone: zone || "IE"
    });
  }, [appKey, appSecret, consumerKey, endpoint, tgToken, tgChatId, iam, zone]);

  // Auto-update IAM when zone changes
  useEffect(() => {
    if (formValues.zone) {
      setFormValues(prev => ({
        ...prev,
        iam: `go-ovh-${formValues.zone.toLowerCase()}`
      }));
    }
  }, [formValues.zone]);

  // 加载 Webhook 信息（可选功能，失败不显示错误）
  const loadWebhookInfo = async () => {
    if (!hasTgToken && !tgToken) {
      // 没有token时，不尝试加载
      setIsLoadingWebhookInfo(false);
      return;
    }
    
    setIsLoadingWebhookInfo(true);
    try {
      const response = await api.get('/telegram/get-webhook-info');
      const data = response.data;
      if (data.success && data.webhook_info) {
        setWebhookInfo(data.webhook_info);
        if (data.webhook_info.url) {
          setWebhookUrl(data.webhook_info.url.replace('/api/telegram/webhook', ''));
        }
      }
    } catch (error: any) {
      // 静默失败，webhook是可选的
      console.log('Webhook 功能未配置或不可用（这是可选的）');
      setWebhookInfo(null);
    } finally {
      setIsLoadingWebhookInfo(false);
    }
  };

  // 自动检测 Webhook URL（使用当前页面的域名）
  const autoDetectWebhookUrl = () => {
    const currentUrl = window.location.origin;
    setWebhookUrl(currentUrl);
  };

  // 设置 Webhook
  const handleSetWebhook = async () => {
    if (!hasTgToken && !tgToken) {
      toast.error('请先配置 Telegram Bot Token');
      return;
    }
    
    if (!webhookUrl.trim()) {
      toast.error('请输入 Webhook URL');
      return;
    }

    setIsSettingWebhook(true);
    try {
      const response = await api.post('/telegram/set-webhook', {
        webhook_url: webhookUrl
      });
      
      const data = response.data;
      
      if (data.success) {
        toast.success('Webhook 设置成功！');
        setWebhookInfo(data.webhook_info);
        // 重新加载信息
        await loadWebhookInfo();
      } else {
        toast.error(data.error || '设置失败');
      }
    } catch (error: any) {
      const errorMsg = error.response?.data?.error || error.message || '未知错误';
      toast.error('设置失败：' + errorMsg);
    } finally {
      setIsSettingWebhook(false);
    }
  };

  // 组件加载时获取 Webhook 信息
  useEffect(() => {
    if (hasTgToken || tgToken) {
      loadWebhookInfo();
      autoDetectWebhookUrl();
    }
  }, [hasTgToken, tgToken]);

  // 计算错误信息辅助函数
  const getErrorInfo = () => {
    if (!webhookInfo?.last_error_date) return null;
    
    const errorDate = new Date(webhookInfo.last_error_date * 1000);
    const now = new Date();
    const msSinceError = now.getTime() - errorDate.getTime();
    const hoursSinceError = msSinceError / (1000 * 60 * 60);
    const daysSinceError = msSinceError / (1000 * 60 * 60 * 24);
    const isRecentError = hoursSinceError < 24;
    
    const formatRelativeTime = () => {
      if (hoursSinceError < 1) {
        const minutes = Math.floor(msSinceError / (1000 * 60));
        return `${minutes}分钟前`;
      } else if (hoursSinceError < 24) {
        return `${Math.floor(hoursSinceError)}小时前`;
      } else if (daysSinceError < 7) {
        return `${Math.floor(daysSinceError)}天前`;
      } else {
        return errorDate.toLocaleDateString('zh-CN');
      }
    };
    
    return {
      errorDate,
      isRecentError,
      formatRelativeTime,
      hoursSinceError,
      daysSinceError
    };
  };

  useEffect(() => {
    (async () => {
      try {
        await api.get('/settings');
        setApiKeyValid(true);
      } catch {
        setApiKeyValid(false);
      }
      try {
        const valid = await checkAuthentication();
        setOvhAuthValid(!!valid);
      } catch {
        setOvhAuthValid(false);
      }
    })();
  }, []);

  // Handle input changes
  const handleChange = (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => {
    const { name, value } = e.target;
    setFormValues({
      ...formValues,
      [name]: value
    });
  };

  // Toggle password visibility
  const toggleShowValue = (field: keyof typeof showValues) => {
    setShowValues({
      ...showValues,
      [field]: !showValues[field]
    });
  };

  // Save settings
  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    
    setIsSaving(true);
    try {
      const isMasked = (value: string) => !value || value.includes("****");
      const hasOVHConfig = !isMasked(formValues.appKey) && !isMasked(formValues.appSecret) && !isMasked(formValues.consumerKey);
      
      if (hasOVHConfig) {
        // 如果填写了 OVH API，则保存并验证
        await setAPIKeys(formValues);
        const isValid = await checkAuthentication();
        
        if (isValid) {
          toast.success("所有设置已保存并验证通过");
          // 刷新页面加载新配置
          setTimeout(() => {
            window.location.reload();
          }, 500);
        } else {
          toast.warning("OVH API 配置已保存，但验证失败，请检查密钥是否正确");
          setIsSaving(false);
          setOvhAuthValid(false);
        }
      } else {
        // 未填写 OVH API：也要保存 Telegram 配置到后端（部分更新）
        try {
          const payload: Record<string, any> = {
            tgChatId: formValues.tgChatId.trim(),
          };
          if (formValues.tgToken.trim() && !formValues.tgToken.includes("****")) {
            payload.tgToken = formValues.tgToken.trim();
          }
          await api.post('/settings', payload);
          toast.success("Telegram 配置已保存，页面将刷新");
        } catch (err) {
          toast.error("保存Telegram配置失败");
        }
        setTimeout(() => { window.location.reload(); }, 800);
      }
    } catch (error) {
      console.error("Error saving settings:", error);
      toast.error("保存设置失败");
      setIsSaving(false);
    }
  };

  const handleClearTelegram = async () => {
    if (!window.confirm("确定要清空 Telegram Bot Token 与 Chat ID 并移除 Webhook 吗？")) {
      return;
    }
    setIsSaving(true);
    try {
      await api.post('/settings', { clearTelegram: true });
      toast.success("Telegram 配置已清空，页面将刷新");
      setTimeout(() => { window.location.reload(); }, 600);
    } catch (err) {
      toast.error("清空 Telegram 配置失败");
      setIsSaving(false);
    }
  };

  const handleAccountFormChange = (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => {
    const { name, value } = e.target;
    setAccountForm(prev => ({ ...prev, [name]: value }));
  };

  const handleAddOrUpdateAccount = async () => {
    if (!accountForm.id) {
      toast.error('请输入账户ID');
      return;
    }
    setIsSubmittingAccount(true);
    try {
      const res = await api.post('/accounts', accountForm);
      if (res.data?.success) {
        toast.success('账户已保存');
        await refreshAccounts();
      } else {
        toast.error(res.data?.error || '保存失败');
      }
    } catch (error: any) {
      toast.error(error.response?.data?.error || error.message || '保存失败');
    } finally {
      setIsSubmittingAccount(false);
    }
  };

  const handleDeleteAccount = async (id: string) => {
    try {
      const res = await api.delete(`/accounts/${id}`);
      if (res.data?.success) {
        toast.success('账户已删除');
        await refreshAccounts();
      } else {
        toast.error(res.data?.error || '删除失败');
      }
    } catch (error: any) {
      toast.error(error.response?.data?.error || error.message || '删除失败');
    }
  };

  const handleSetDefaultAccount = async (id: string) => {
    try {
      const res = await api.put('/accounts/default', { id });
      if (res.data?.success) {
        toast.success('默认账户已设置');
        await refreshAccounts();
      } else {
        toast.error(res.data?.error || '设置失败');
      }
    } catch (error: any) {
      toast.error(error.response?.data?.error || error.message || '设置失败');
    }
  };

  return (
    <div className="space-y-4 sm:space-y-6">
      <motion.div
        initial={{ opacity: 0, y: -20 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3 }}
      >
        <h1 className={`${isMobile ? 'text-2xl' : 'text-3xl'} font-bold mb-1 cyber-glow-text`}>设置</h1>
        <p className="text-cyber-muted text-sm mb-4 sm:mb-6">配置通知与集成设置</p>
      </motion.div>

      {isLoading ? (
        <div className="flex justify-center items-center h-64">
          <div className="animate-spin w-10 h-10 border-4 border-cyber-accent border-t-transparent rounded-full"></div>
          <span className="ml-3 text-cyber-muted">加载中...</span>
        </div>
      ) : (
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-4 sm:gap-6">
          <div className="lg:col-span-2">
            <form onSubmit={handleSubmit} className="cyber-panel p-4 sm:p-6 space-y-4 sm:space-y-6">
              <div className="cyber-panel p-4 bg-cyan-500/5 border border-cyan-500/20 text-xs text-cyan-300">
                登录密码由后端 <code>backend/.env</code> 的 <code>API_SECRET_KEY</code> 管理；修改后需重启后端并重新登录。
              </div>

              <div className={`cyber-grid-line pt-4 ${accountFieldsHidden ? 'hidden' : ''}`}>
                <h2 className={`${isMobile ? 'text-lg' : 'text-xl'} font-bold mb-3 sm:mb-4`}>OVH API 凭据</h2>
                
                <div className="space-y-3 sm:space-y-4">
                  <div>
                    <label className="block text-cyber-muted mb-1 text-xs sm:text-sm">
                      应用密钥 (APP KEY)
                    </label>
                    <div className="relative">
                      <input
                        type={showValues.appKey ? "text" : "password"}
                        name="appKey"
                        value={formValues.appKey}
                        onChange={handleChange}
                        className="cyber-input w-full pr-10 text-sm"
                        placeholder="xxxxxxxxxxxxxxxx"
                      />
                      <button
                        type="button"
                        onClick={() => toggleShowValue("appKey")}
                        className="absolute inset-y-0 right-0 px-3 text-cyber-muted hover:text-cyber-accent"
                      >
                        {showValues.appKey ? (
                          <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                            <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"></path>
                            <line x1="1" y1="1" x2="23" y2="23"></line>
                          </svg>
                        ) : (
                          <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                            <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                            <circle cx="12" cy="12" r="3"></circle>
                          </svg>
                        )}
                      </button>
                    </div>
                  </div>
                  
                  <div>
                    <label className="block text-cyber-muted mb-1">
                      应用密钥 (APP SECRET)
                    </label>
                    <div className="relative">
                      <input
                        type={showValues.appSecret ? "text" : "password"}
                        name="appSecret"
                        value={formValues.appSecret}
                        onChange={handleChange}
                        className="cyber-input w-full pr-10"
                        placeholder="xxxxxxxxxxxxxxxx"
                      />
                      <button
                        type="button"
                        onClick={() => toggleShowValue("appSecret")}
                        className="absolute inset-y-0 right-0 px-3 text-cyber-muted hover:text-cyber-accent"
                      >
                        {showValues.appSecret ? (
                          <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                            <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"></path>
                            <line x1="1" y1="1" x2="23" y2="23"></line>
                          </svg>
                        ) : (
                          <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                            <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                            <circle cx="12" cy="12" r="3"></circle>
                          </svg>
                        )}
                      </button>
                    </div>
                  </div>
                  
                  <div>
                    <label className="block text-cyber-muted mb-1">
                      消费者密钥 (CONSUMER KEY)
                    </label>
                    <div className="relative">
                      <input
                        type={showValues.consumerKey ? "text" : "password"}
                        name="consumerKey"
                        value={formValues.consumerKey}
                        onChange={handleChange}
                        className="cyber-input w-full pr-10"
                        placeholder="xxxxxxxxxxxxxxxx"
                      />
                      <button
                        type="button"
                        onClick={() => toggleShowValue("consumerKey")}
                        className="absolute inset-y-0 right-0 px-3 text-cyber-muted hover:text-cyber-accent"
                      >
                        {showValues.consumerKey ? (
                          <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                            <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"></path>
                            <line x1="1" y1="1" x2="23" y2="23"></line>
                          </svg>
                        ) : (
                          <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                            <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                            <circle cx="12" cy="12" r="3"></circle>
                          </svg>
                        )}
                      </button>
                    </div>
                  </div>
                  
                  <div>
                    <label className="block text-cyber-muted mb-1">
                      API 节点 (ENDPOINT)
                    </label>
                    <select
                      name="endpoint"
                      value={formValues.endpoint}
                      onChange={handleChange}
                      className="cyber-input w-full"
                    >
                      <option value="ovh-eu">🇪🇺 欧洲 (ovh-eu) - eu.api.ovh.com</option>
                      <option value="ovh-us">🇺🇸 美国 (ovh-us) - api.us.ovhcloud.com</option>
                      <option value="ovh-ca">🇨🇦 加拿大 (ovh-ca) - ca.api.ovh.com</option>
                    </select>
                    <p className="text-xs text-cyan-400 mt-1">
                      ⚠️ 请选择与您OVH账户所在区域匹配的endpoint
                    </p>
                  </div>
                </div>
              </div>
              
              <div className={`cyber-grid-line pt-4 ${accountFieldsHidden ? 'hidden' : ''}`}>
                <h2 className="text-xl font-bold mb-4">区域设置</h2>
                
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  <div>
                    <label className="block text-cyber-muted mb-1">
                      OVH 子公司 (ZONE)
                    </label>
                    <select
                      name="zone"
                      value={formValues.zone}
                      onChange={handleChange}
                      className="cyber-input w-full"
                    >
                      <option value="IE">爱尔兰 (IE)</option>
                      <option value="FR">法国 (FR)</option>
                      <option value="GB">英国 (GB)</option>
                      <option value="DE">德国 (DE)</option>
                      <option value="ES">西班牙 (ES)</option>
                      <option value="PT">葡萄牙 (PT)</option>
                      <option value="IT">意大利 (IT)</option>
                      <option value="PL">波兰 (PL)</option>
                      <option value="FI">芬兰 (FI)</option>
                      <option value="LT">立陶宛 (LT)</option>
                      <option value="CZ">捷克 (CZ)</option>
                      <option value="NL">荷兰 (NL)</option>
                      <option value="CA">加拿大 (CA)</option>
                      <option value="US">美国 (US)</option>
                    </select>
                    <p className="text-xs text-cyber-muted mt-1">默认: IE (欧洲区), CA (加拿大), US (美国)</p>
                  </div>
                  
                  <div>
                    <label className="block text-cyber-muted mb-1">
                      标识 (IAM)
                    </label>
                    <input
                      type="text"
                      name="iam"
                      value={formValues.iam}
                      onChange={handleChange}
                      className="cyber-input w-full"
                      placeholder="go-ovh-ie"
                    />
                    <p className="text-xs text-cyber-muted mt-1">默认会根据 ZONE 设置自动生成，例如: go-ovh-ie</p>
                  </div>
                </div>
              </div>
              
              <div className="cyber-grid-line pt-4">
                <div className="p-4 rounded-lg border border-cyber-grid bg-cyber-bg flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                  <div>
                    <h2 className="text-base font-bold text-cyber-text">通知渠道与事件规则</h2>
                    <p className="text-xs text-cyber-muted mt-1">
                      Telegram 机器人、Webhook 按钮回调以及 SMTP 邮件通知已统一迁入独立的「通知中心」，您已有的 Telegram 配置已自动迁移。
                    </p>
                  </div>
                  <button
                    type="button"
                    onClick={() => navigate("/notifications")}
                    className="cyber-button text-xs sm:text-sm px-4 py-2 whitespace-nowrap flex-shrink-0"
                  >
                    前往通知中心 →
                  </button>
                </div>
              </div>
              
              <div className="flex justify-end pt-4">
                <button
                  type="submit"
                  className="cyber-button px-6"
                  disabled={isSaving}
                >
                  {isSaving ? (
                    <span className="flex items-center">
                      <svg className="animate-spin -ml-1 mr-2 h-4 w-4 text-cyber-text" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                        <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                        <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                      </svg>
                      保存中...
                    </span>
                  ) : "保存设置"}
                </button>
              </div>
            </form>
          </div>
          
          <div>
            <div className="cyber-panel p-6">
              <h2 className="text-lg font-bold mb-4">连接状态</h2>
              
              <div className="space-y-4">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <div className={`w-3 h-3 rounded-full ${apiKeyValid ? 'bg-green-400 animate-pulse' : 'bg-red-400'}`}></div>
                    <span className={`${apiKeyValid ? 'text-green-400' : 'text-red-400'} text-sm`}>登录会话</span>
                  </div>
                  <span className="text-xs text-cyber-muted">{apiKeyValid === null ? '检测中' : apiKeyValid ? '有效' : '已失效'}</span>
                </div>
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <div className={`w-3 h-3 rounded-full ${ovhAuthValid ? 'bg-green-400 animate-pulse' : 'bg-red-400'}`}></div>
                    <span className={`${ovhAuthValid ? 'text-green-400' : 'text-red-400'} text-sm`}>OVH API</span>
                  </div>
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-cyber-muted">{ovhAuthValid === null ? '检测中' : ovhAuthValid ? '已连接' : '未连接'}</span>
                    <button type="button" className="cyber-button h-7 px-2 text-xs" onClick={async () => { const valid = await checkAuthentication(); setOvhAuthValid(!!valid); }}>
                      重新验证
                    </button>
                  </div>
                </div>
                
              </div>
            </div>
            
            {/* 缓存管理器 */}
            <div className="mt-6">
              <CacheManager />
            </div>

          </div>
        </div>
      )}

      {/* 错误历史模态框 */}
      {createPortal(
        <AnimatePresence>
          {showErrorHistoryDialog && webhookInfo?.last_error_date && (
            <div className="fixed inset-0 z-[9999] flex items-center justify-center p-4 pointer-events-none">
              <motion.div
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                onClick={() => setShowErrorHistoryDialog(false)}
                className="absolute inset-0 bg-black/70 backdrop-blur-sm pointer-events-auto"
              />
              <motion.div
                initial={{ opacity: 0, scale: 0.9 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.9 }}
                onClick={(e) => e.stopPropagation()}
                className="cyber-card max-w-2xl w-full max-h-[90vh] overflow-y-auto pointer-events-auto relative"
              >
                <div className="flex items-center justify-between mb-4 pb-4 border-b border-cyber-accent/20">
                  <div className="flex items-center gap-2">
                    <AlertCircle className="w-5 h-5 text-yellow-400" />
                    <h3 className="text-xl font-semibold text-cyber-text">
                      {(() => {
                        const errorInfo = getErrorInfo();
                        return errorInfo?.isRecentError ? '最后错误' : '历史错误';
                      })()}
                    </h3>
                  </div>
                  <button
                    onClick={() => setShowErrorHistoryDialog(false)}
                    className="p-2 hover:bg-cyber-grid/50 rounded-lg transition-colors text-cyber-muted hover:text-cyber-text"
                  >
                    <X className="w-5 h-5" />
                  </button>
                </div>

                <div className="space-y-4">
                  {(() => {
                    const errorInfo = getErrorInfo();
                    if (!errorInfo) return null;
                    
                    const hasNoPendingUpdates = webhookInfo.pending_update_count === 0;
                    
                    return (
                      <div className={`border rounded-lg p-4 transition-all ${
                        errorInfo.isRecentError 
                          ? 'bg-red-500/10 border-red-500/30' 
                          : 'bg-yellow-500/10 border-yellow-500/30'
                      }`}>
                        <div className="flex items-start justify-between gap-2 mb-3">
                          <div className="flex items-center gap-1.5">
                            <span className={`text-sm font-semibold ${
                              errorInfo.isRecentError ? 'text-red-400' : 'text-yellow-400'
                            }`}>
                              {errorInfo.isRecentError ? '⚠️' : '📋'} {errorInfo.isRecentError ? '最后错误' : '历史错误'}
                            </span>
                          </div>
                          <span className={`text-xs px-2 py-1 rounded ${
                            errorInfo.isRecentError 
                              ? 'bg-red-500/20 text-red-300' 
                              : 'bg-yellow-500/20 text-yellow-300'
                          }`}>
                            {errorInfo.formatRelativeTime()}
                          </span>
                        </div>
                        
                        <div className={`text-sm font-mono mb-3 ${
                          errorInfo.isRecentError ? 'text-red-300/80' : 'text-yellow-300/80'
                        }`}>
                          {errorInfo.errorDate.toLocaleString('zh-CN', {
                            year: 'numeric',
                            month: '2-digit',
                            day: '2-digit',
                            hour: '2-digit',
                            minute: '2-digit',
                            second: '2-digit',
                            hour12: false
                          })}
                        </div>
                        
                        {webhookInfo.last_error_message && (
                          <div className={`text-sm leading-relaxed break-words p-3 rounded bg-black/20 mb-3 ${
                            errorInfo.isRecentError ? 'text-red-200' : 'text-yellow-200'
                          }`}>
                            {webhookInfo.last_error_message}
                          </div>
                        )}
                        
                        {!errorInfo.isRecentError && hasNoPendingUpdates && (
                          <div className="text-sm text-green-300/90 pt-3 border-t border-yellow-500/20 flex items-start gap-2">
                            <span className="text-base">💡</span>
                            <span>待处理更新为 0，Webhook 可能已恢复正常。如需清除此错误记录，请重新设置 Webhook。</span>
                          </div>
                        )}
                        
                        {errorInfo.isRecentError && (
                          <div className="text-sm text-red-300/80 pt-3 border-t border-red-500/20 flex items-start gap-2">
                            <span className="text-base">🔍</span>
                            <span>这是最近的错误，请检查 Webhook 配置和服务器状态。</span>
                          </div>
                        )}
                      </div>
                    );
                  })()}
                </div>
              </motion.div>
            </div>
          )}
        </AnimatePresence>,
        document.body
      )}
    </div>
  );
};

export default SettingsPage;
