import { useState, useEffect } from "react";
import { createPortal } from "react-dom";
import { motion, AnimatePresence } from "framer-motion";
import { useAPI } from "@/context/APIContext";
import { toast } from "sonner";
import { useNavigate } from "react-router-dom";
import { CacheManager } from "@/components/CacheManager";
import { useIsMobile } from "@/hooks/use-mobile";
import { api } from "@/utils/apiClient";
import QRCode from "qrcode";
import {
  X,
  AlertCircle,
  ShieldCheck,
  ShieldAlert,
  KeyRound,
  Copy,
  Download,
  RefreshCw,
  Lock,
} from "lucide-react";

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

  // TOTP 2FA 双因素认证状态
  const [totpStatus, setTotpStatus] = useState<{
    totpEnabled: boolean;
    recoveryCodesRemaining: number;
    enabledAt: string | null;
    sudoActive: boolean;
    sudoExpiresAt: number;
  }>({
    totpEnabled: false,
    recoveryCodesRemaining: 0,
    enabledAt: null,
    sudoActive: false,
    sudoExpiresAt: 0,
  });
  const [totpSetupData, setTotpSetupData] = useState<{
    secret: string;
    otpauthUri: string;
    qrDataUrl: string;
  } | null>(null);
  const [totpVerifyCode, setTotpVerifyCode] = useState("");
  const [totpBusy, setTotpBusy] = useState(false);
  const [freshRecoveryCodes, setFreshRecoveryCodes] = useState<string[] | null>(
    null
  );

  const loadTotpStatus = async () => {
    try {
      const res = await api.get("/auth/totp/status");
      if (res.data) {
        setTotpStatus({
          totpEnabled: !!res.data.totpEnabled,
          recoveryCodesRemaining: Number(res.data.recoveryCodesRemaining || 0),
          enabledAt: res.data.enabledAt || null,
          sudoActive: !!res.data.sudoActive,
          sudoExpiresAt: Number(res.data.sudoExpiresAt || 0),
        });
      }
    } catch {
      /* ignore */
    }
  };

  useEffect(() => {
    loadTotpStatus();
    const onSudoUpdated = () => loadTotpStatus();
    window.addEventListener("totp:sudo-updated", onSudoUpdated);
    return () => window.removeEventListener("totp:sudo-updated", onSudoUpdated);
  }, []);

  const handleStartTotpSetup = async () => {
    setTotpBusy(true);
    try {
      const res = await api.post("/auth/totp/setup", {
        accountName: "OVH-Nexus-Admin",
      });
      const secret = res.data?.secret || "";
      const otpauthUri = res.data?.otpauthUri || "";
      const qrDataUrl = await QRCode.toDataURL(otpauthUri, {
        width: 200,
        margin: 1,
      });
      setTotpSetupData({ secret, otpauthUri, qrDataUrl });
      setTotpVerifyCode("");
      setFreshRecoveryCodes(null);
    } catch (err: any) {
      toast.error(err?.response?.data?.message || "生成 2FA 绑定密钥失败");
    } finally {
      setTotpBusy(false);
    }
  };

  const handleActivateTotp = async () => {
    if (!totpVerifyCode.trim()) {
      toast.error("请输入验证器 App 显示的 6 位数字动态码");
      return;
    }
    setTotpBusy(true);
    try {
      const res = await api.post("/auth/totp/activate", {
        code: totpVerifyCode.trim(),
      });
      if (res.data?.enabled) {
        setTotpSetupData(null);
        setTotpVerifyCode("");
        setFreshRecoveryCodes(res.data?.recoveryCodes || []);
        toast.success(
          "双因素认证 (2FA) 已成功启用！请立即备份下方的一次性应急恢复码。"
        );
        await loadTotpStatus();
      }
    } catch (err: any) {
      toast.error(
        err?.response?.data?.message || "验证码校验失败，请检查验证器时间同步"
      );
    } finally {
      setTotpBusy(false);
    }
  };

  const handleRegenerateRecoveryCodes = async () => {
    if (
      !window.confirm(
        "确定要重新生成 8 个应急恢复码吗？原有的旧恢复码将立即全部作废。"
      )
    ) {
      return;
    }
    setTotpBusy(true);
    try {
      const res = await api.post("/auth/totp/recovery-codes/regenerate", {});
      if (res.data?.recoveryCodes) {
        setFreshRecoveryCodes(res.data.recoveryCodes);
        toast.success("已生成全新的 8 个应急恢复码，请立即妥善保存");
        await loadTotpStatus();
      }
    } catch {
      /* apiClient interceptor handles step-up or error toast */
    } finally {
      setTotpBusy(false);
    }
  };

  const handleDisableTotp = async () => {
    if (
      !window.confirm(
        "警告：关闭 2FA 后，所有高危操作（独服重装/重启/销毁、防火墙变更、API 密钥修改、MCP L3 运维工具等）将被安全门禁自动锁定拦截。确定要关闭 2FA 吗？"
      )
    ) {
      return;
    }
    setTotpBusy(true);
    try {
      await api.post("/auth/totp/disable", {});
      setFreshRecoveryCodes(null);
      setTotpSetupData(null);
      toast.success("已关闭 2FA 双因素认证");
      await loadTotpStatus();
    } catch {
      /* apiClient interceptor handles step-up or error toast */
    } finally {
      setTotpBusy(false);
    }
  };

  const copyRecoveryCodes = (codes: string[]) => {
    navigator.clipboard.writeText(codes.join("\n"));
    toast.success("应急恢复码已复制到剪贴板");
  };

  const downloadRecoveryCodes = (codes: string[]) => {
    const content = [
      "OVH Nexus - Two-Factor Authentication (2FA) Recovery Codes",
      `Generated At: ${new Date().toISOString()}`,
      "Each code can only be used once. Store this file in a secure password manager.",
      "------------------------------------------------------------",
      ...codes,
      "",
    ].join("\n");
    const blob = new Blob([content], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "ovh-nexus-2fa-recovery-codes.txt";
    a.click();
    URL.revokeObjectURL(url);
  };

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
          <div className="lg:col-span-2 space-y-6">
            {/* TOTP 2FA 双因素认证与落盘加密安全卡片 */}
            <div
              id="totp"
              className="cyber-panel p-4 sm:p-6 space-y-4 border border-cyber-accent/30"
            >
              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                <div className="flex items-start gap-3">
                  <div
                    className={`p-2.5 rounded-lg border shrink-0 ${
                      totpStatus.totpEnabled
                        ? "bg-emerald-500/15 border-emerald-500/35 text-emerald-400"
                        : "bg-amber-500/15 border-amber-500/35 text-amber-400"
                    }`}
                  >
                    {totpStatus.totpEnabled ? (
                      <ShieldCheck size={22} />
                    ) : (
                      <ShieldAlert size={22} />
                    )}
                  </div>
                  <div>
                    <div className="flex flex-wrap items-center gap-2">
                      <h2 className="text-base sm:text-lg font-bold text-cyber-text">
                        双因素认证 (2FA / TOTP) 与高危操作门禁
                      </h2>
                      <span
                        className={`text-[11px] font-semibold px-2 py-0.5 rounded border ${
                          totpStatus.totpEnabled
                            ? "bg-emerald-500/15 text-emerald-400 border-emerald-500/30"
                            : "bg-amber-500/15 text-amber-400 border-amber-500/30"
                        }`}
                      >
                        {totpStatus.totpEnabled
                          ? "已启用 · 高危操作门禁就绪"
                          : "未启用 · 高危操作已锁定"}
                      </span>
                    </div>
                    <p className="text-xs text-cyber-muted mt-1 leading-relaxed">
                      遵循 RFC 6238 标准（支持 Google Authenticator / 1Password / Bitwarden / Authy）。
                      <strong>未绑定 2FA 时，系统将强制锁定全部高危操作</strong>（含服务器重启/重装/退订/过户、IPMI 控制台、边缘防火墙、API 账户密钥变更、抢购自动扣款以及 MCP L3 运维工具）。
                    </p>
                  </div>
                </div>

                {!totpStatus.totpEnabled && !totpSetupData && (
                  <button
                    type="button"
                    disabled={totpBusy}
                    onClick={handleStartTotpSetup}
                    className="px-4 py-2 rounded-md text-xs font-semibold bg-cyber-accent text-white hover:opacity-90 whitespace-nowrap shrink-0"
                  >
                    {totpBusy ? "生成中..." : "立即绑定 2FA 验证器"}
                  </button>
                )}
              </div>

              {/* 尚未绑定时的二维码向导 */}
              {!totpStatus.totpEnabled && totpSetupData && (
                <div className="p-4 rounded-lg border border-cyber-accent/30 bg-cyber-bg/60 space-y-4">
                  <div className="grid grid-cols-1 md:grid-cols-12 gap-4 items-center">
                    <div className="md:col-span-4 flex flex-col items-center justify-center p-3 rounded-lg bg-white">
                      <img
                        src={totpSetupData.qrDataUrl}
                        alt="TOTP 2FA QR Code"
                        className="w-40 h-40 object-contain"
                      />
                    </div>
                    <div className="md:col-span-8 space-y-3">
                      <div className="text-xs font-semibold text-cyber-text">
                        第一步：使用手机验证器 App 扫描左侧二维码，或手动输入下方 Base32 密钥：
                      </div>
                      <div className="flex items-center gap-2">
                        <input
                          type="text"
                          readOnly
                          value={totpSetupData.secret}
                          className="cyber-input flex-1 font-mono text-xs bg-cyber-bg"
                        />
                        <button
                          type="button"
                          onClick={() => {
                            navigator.clipboard.writeText(totpSetupData.secret);
                            toast.success("2FA Base32 密钥已复制");
                          }}
                          className="px-3 py-2 rounded border border-cyber-grid bg-cyber-surface hover:bg-cyber-grid/50 text-xs font-medium flex items-center gap-1"
                        >
                          <Copy size={13} />
                          <span>复制密钥</span>
                        </button>
                      </div>

                      <div className="text-xs font-semibold text-cyber-text pt-1">
                        第二步：输入验证器显示的 6 位数字动态码以完成激活：
                      </div>
                      <div className="flex flex-wrap items-center gap-2">
                        <input
                          type="text"
                          inputMode="numeric"
                          maxLength={6}
                          value={totpVerifyCode}
                          onChange={(e) => setTotpVerifyCode(e.target.value)}
                          placeholder="输入 6 位动态码 (如 123456)"
                          className="cyber-input w-48 font-mono text-sm tracking-widest text-center"
                        />
                        <button
                          type="button"
                          disabled={totpBusy}
                          onClick={handleActivateTotp}
                          className="px-4 py-2 rounded bg-cyber-accent text-white text-xs font-semibold hover:opacity-90"
                        >
                          {totpBusy ? "验证激活中..." : "验证并启用 2FA"}
                        </button>
                        <button
                          type="button"
                          onClick={() => setTotpSetupData(null)}
                          className="px-3 py-2 rounded border border-cyber-grid text-xs text-cyber-muted hover:text-cyber-text"
                        >
                          取消
                        </button>
                      </div>
                    </div>
                  </div>
                </div>
              )}

              {/* 已启用状态管理栏 */}
              {totpStatus.totpEnabled && (
                <div className="p-3.5 rounded-lg border border-cyber-grid bg-cyber-bg/50 flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                  <div className="space-y-1 text-xs">
                    <div className="flex flex-wrap items-center gap-3 text-cyber-text font-medium">
                      <span>
                        剩余可用应急恢复码：
                        <strong
                          className={`ml-1 font-mono ${
                            totpStatus.recoveryCodesRemaining <= 2
                              ? "text-rose-400"
                              : "text-emerald-400"
                          }`}
                        >
                          {totpStatus.recoveryCodesRemaining} / 8
                        </strong>
                      </span>
                      {totpStatus.enabledAt && (
                        <span className="text-cyber-muted">
                          绑定时间：{new Date(totpStatus.enabledAt).toLocaleString()}
                        </span>
                      )}
                      {totpStatus.sudoActive && (
                        <span className="px-2 py-0.5 rounded bg-cyber-accent/15 text-cyber-accent border border-cyber-accent/30 text-[11px]">
                          Sudo 免重复验证窗口生效中
                        </span>
                      )}
                    </div>
                    <p className="text-[11px] text-cyber-muted">
                      分级门禁策略：<strong>Strict 级</strong>（重装系统、服务销毁、过户转移、修改 API 密钥、关闭 2FA）每次必验；
                      <strong>Sudo 级</strong>（重启、救援模式、IPMI 控制台、防火墙、开启自动扣款）验证一次后 5 分钟内免重复输入。
                    </p>
                  </div>

                  <div className="flex flex-wrap items-center gap-2 shrink-0">
                    <button
                      type="button"
                      disabled={totpBusy}
                      onClick={handleRegenerateRecoveryCodes}
                      className="px-3 py-1.5 rounded border border-cyber-accent/35 bg-cyber-accent/10 text-cyber-accent hover:bg-cyber-accent/20 text-xs font-medium flex items-center gap-1.5"
                    >
                      <RefreshCw size={13} />
                      <span>重置恢复码</span>
                    </button>
                    <button
                      type="button"
                      disabled={totpBusy}
                      onClick={handleDisableTotp}
                      className="px-3 py-1.5 rounded border border-rose-500/35 bg-rose-500/10 text-rose-400 hover:bg-rose-500/20 text-xs font-medium"
                    >
                      关闭 2FA
                    </button>
                  </div>
                </div>
              )}

              {/* 一次性应急恢复码明文展示框 */}
              {freshRecoveryCodes && freshRecoveryCodes.length > 0 && (
                <div className="p-4 rounded-lg border border-amber-500/40 bg-amber-500/10 space-y-3">
                  <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
                    <div className="flex items-center gap-2 text-amber-300 font-bold text-xs sm:text-sm">
                      <KeyRound size={16} />
                      <span>
                        请立即保存您的 8 个一次性应急恢复码（关闭后不再显示明文）
                      </span>
                    </div>
                    <div className="flex items-center gap-2">
                      <button
                        type="button"
                        onClick={() => copyRecoveryCodes(freshRecoveryCodes)}
                        className="px-2.5 py-1 rounded bg-cyber-surface border border-cyber-grid text-xs font-medium flex items-center gap-1 hover:text-cyber-accent"
                      >
                        <Copy size={12} />
                        <span>复制全部</span>
                      </button>
                      <button
                        type="button"
                        onClick={() => downloadRecoveryCodes(freshRecoveryCodes)}
                        className="px-2.5 py-1 rounded bg-cyber-surface border border-cyber-grid text-xs font-medium flex items-center gap-1 hover:text-cyber-accent"
                      >
                        <Download size={12} />
                        <span>下载 (.txt)</span>
                      </button>
                      <button
                        type="button"
                        onClick={() => setFreshRecoveryCodes(null)}
                        className="px-2 py-1 rounded text-xs text-cyber-muted hover:text-cyber-text"
                      >
                        我已妥善保存
                      </button>
                    </div>
                  </div>
                  <div className="grid grid-cols-2 sm:grid-cols-4 gap-2 pt-1">
                    {freshRecoveryCodes.map((rc) => (
                      <div
                        key={rc}
                        className="px-3 py-1.5 rounded bg-cyber-bg/80 border border-cyber-grid font-mono text-xs text-center text-cyber-text tracking-wider"
                      >
                        {rc}
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>

            <form onSubmit={handleSubmit} className="cyber-panel p-4 sm:p-6 space-y-4 sm:space-y-6">
              <div className="cyber-panel p-4 bg-cyan-500/5 border border-cyan-500/20 text-xs text-cyan-300 space-y-1">
                <div>
                  • 登录主密码由后端环境变量 <code>API_SECRET_KEY</code> 管理；修改后需重启后端并重新登录。
                </div>
                <div>
                  • 静态数据加密由独立环境变量 <code>DATA_ENCRYPTION_KEY</code>（HKDF-SHA256 + AES-256-GCM）驱动，所有 OVH API 密钥、Telegram/SMTP 凭证与 2FA 种子在落盘时均自动加密为 <code>enc:v1:...</code> 密文。
                </div>
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
                <div className="flex items-center justify-between pt-2 border-t border-cyber-grid">
                  <div className="flex items-center gap-2">
                    <Lock className="w-3.5 h-3.5 text-emerald-400" />
                    <span className="text-emerald-400 text-sm">磁盘静态加密</span>
                  </div>
                  <span className="text-xs font-mono text-emerald-400">AES-256-GCM</span>
                </div>
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <div className={`w-3 h-3 rounded-full ${totpStatus.totpEnabled ? 'bg-emerald-400' : 'bg-amber-400'}`}></div>
                    <span className={`${totpStatus.totpEnabled ? 'text-emerald-400' : 'text-amber-400'} text-sm`}>2FA 双因素门禁</span>
                  </div>
                  <span className="text-xs text-cyber-muted">{totpStatus.totpEnabled ? '已启用' : '未绑定 (高危已锁)'}</span>
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
