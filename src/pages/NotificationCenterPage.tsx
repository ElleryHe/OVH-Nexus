import { useState, useEffect, useCallback } from "react";
import { motion } from "framer-motion";
import { api } from "@/utils/apiClient";
import { toast } from "sonner";
import {
  Bell,
  Plus,
  Trash2,
  Send,
  Save,
  Mail,
  MessageSquare,
  ShieldAlert,
  CheckCircle2,
  Sliders,
  RefreshCw,
  Eye,
  EyeOff,
  Webhook,
  ExternalLink,
} from "lucide-react";

interface ChannelConfig {
  // Telegram
  token?: string;
  chatId?: string;
  // SMTP
  host?: string;
  port?: number;
  security?: "ssl" | "starttls" | "none";
  username?: string;
  password?: string;
  fromName?: string;
  fromAddress?: string;
  recipients?: string[] | string;
}

interface NotificationChannel {
  id: string;
  type: "telegram" | "smtp";
  name: string;
  enabled: boolean;
  events: string[];
  config: ChannelConfig;
  hasSecrets?: Record<string, boolean>;
}

interface NotificationRules {
  skuEnabled: boolean;
  skuInterval: number;
  skuTrackConfigs: boolean;
  skuRanges: string[];
  skuMergeRegional: boolean;
  expiryDays: number;
}

interface SkuWatcherStatus {
  running: boolean;
  initialized: boolean;
  endpoint: string;
  totalSkus: number;
  lastCheckTime?: string | null;
  lastError?: string | null;
}

const EVENT_DEFINITIONS = [
  {
    key: "stock.available",
    label: "库存补货",
    desc: "订阅监控的服务器型号变为有货状态时推送",
    requiresApiKey: false,
  },
  {
    key: "stock.unavailable",
    label: "库存售罄",
    desc: "订阅监控的服务器从有货变为无货状态时推送",
    requiresApiKey: false,
  },
  {
    key: "sku.changed",
    label: "SKU 变动（上新 / 下架）",
    desc: "公开货架新增或移除服务器型号（含未开售预上架型号）时推送摘要",
    requiresApiKey: false,
  },
  {
    key: "order.success",
    label: "抢购成功",
    desc: "自动抢购队列成功创建订单或完成结账时推送",
    requiresApiKey: true,
  },
  {
    key: "order.failed",
    label: "抢购失败",
    desc: "自动抢购下单接口报错或异常失败时推送",
    requiresApiKey: true,
  },
  {
    key: "order.delivered",
    label: "订单交付完成",
    desc: "已抢购成功的服务器完成装机并正式交付可用时推送",
    requiresApiKey: true,
  },
  {
    key: "service.expiring",
    label: "服务即将到期",
    desc: "名下独立服务器或 VPS 距离到期不足指定天数时推送提醒",
    requiresApiKey: true,
  },
];

const PRODUCT_RANGES = [
  { code: "KS", label: "Kimsufi (KS)" },
  { code: "RISE", label: "Rise (RISE)" },
  { code: "SYS", label: "So you Start (SYS)" },
  { code: "ADV", label: "Advance / Game (ADV)" },
  { code: "SCALE", label: "Scale (SCALE)" },
  { code: "HGR", label: "High Grade / HCI (HGR)" },
];

const DEFAULT_EVENTS = [
  "stock.available",
  "sku.changed",
  "order.success",
  "order.failed",
  "order.delivered",
];

const NotificationCenterPage = () => {
  const [activeTab, setActiveTab] = useState<"channels" | "rules">("channels");
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [testingChannelId, setTestingChannelId] = useState<string | null>(null);
  const [hasApiKey, setHasApiKey] = useState(false);

  const [channels, setChannels] = useState<NotificationChannel[]>([]);
  const [panelUrl, setPanelUrl] = useState("");
  const [rules, setRules] = useState<NotificationRules>({
    skuEnabled: true,
    skuInterval: 600,
    skuTrackConfigs: false,
    skuRanges: ["KS", "RISE", "SYS", "ADV", "SCALE", "HGR"],
    skuMergeRegional: true,
    expiryDays: 7,
  });

  const [showSecrets, setShowSecrets] = useState<Record<string, boolean>>({});
  const [skuStatus, setSkuStatus] = useState<SkuWatcherStatus | null>(null);
  const [isCheckingSku, setIsCheckingSku] = useState(false);

  // Telegram Webhook state
  const [webhookUrl, setWebhookUrl] = useState("");
  const [webhookInfo, setWebhookInfo] = useState<any>(null);
  const [isSettingWebhook, setIsSettingWebhook] = useState(false);
  const [isLoadingWebhook, setIsLoadingWebhook] = useState(false);
  const [expandedWebhookChannelId, setExpandedWebhookChannelId] = useState<
    string | null
  >(null);

  const fetchState = useCallback(async () => {
    setIsLoading(true);
    try {
      const [notifRes, skuRes] = await Promise.all([
        api.get("/notifications"),
        api.get("/sku/status").catch(() => ({ data: null })),
      ]);
      const data = notifRes.data || {};
      const loadedChannels: NotificationChannel[] = (data.channels || []).map(
        (ch: NotificationChannel) => ({
          ...ch,
          config: {
            ...ch.config,
            // Clear masked placeholders in input value so user can leave empty to keep existing secret
            token: "",
            password: "",
          },
        })
      );
      setChannels(loadedChannels);
      setPanelUrl(data.panelUrl || "");
      if (data.rules) {
        setRules((prev) => ({ ...prev, ...data.rules }));
      }
      setHasApiKey(Boolean(data.hasApiKey));
      if (skuRes.data) {
        setSkuStatus(skuRes.data);
      }
    } catch (err: any) {
      toast.error(
        "加载通知中心配置失败: " + (err.response?.data?.message || err.message)
      );
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchState();
  }, [fetchState]);

  const handleSaveAll = async () => {
    setIsSaving(true);
    try {
      const payloadChannels = channels.map((ch) => {
        const cfg = { ...ch.config };
        if (ch.type === "smtp" && typeof cfg.recipients === "string") {
          cfg.recipients = cfg.recipients
            .replace(/;/g, ",")
            .split(",")
            .map((s) => s.trim())
            .filter(Boolean);
        }
        return {
          ...ch,
          config: cfg,
        };
      });

      const res = await api.put("/notifications", {
        channels: payloadChannels,
        panelUrl,
        rules,
      });

      if (res.data?.data) {
        const updated = res.data.data;
        setChannels(
          (updated.channels || []).map((ch: NotificationChannel) => ({
            ...ch,
            config: {
              ...ch.config,
              token: "",
              password: "",
            },
          }))
        );
        setHasApiKey(Boolean(updated.hasApiKey));
      }
      toast.success("通知渠道与规则已保存");
    } catch (err: any) {
      toast.error(
        "保存通知配置失败: " + (err.response?.data?.message || err.message)
      );
    } finally {
      setIsSaving(false);
    }
  };

  const handleAddChannel = (type: "telegram" | "smtp") => {
    const newId = `${type}-${Date.now().toString(36)}`;
    const newChannel: NotificationChannel =
      type === "telegram"
        ? {
            id: newId,
            type: "telegram",
            name: `Telegram 机器人 #${
              channels.filter((c) => c.type === "telegram").length + 1
            }`,
            enabled: true,
            events: [...DEFAULT_EVENTS],
            config: {
              token: "",
              chatId: "",
            },
            hasSecrets: { token: false },
          }
        : {
            id: newId,
            type: "smtp",
            name: `SMTP 邮件通知 #${
              channels.filter((c) => c.type === "smtp").length + 1
            }`,
            enabled: true,
            events: [...DEFAULT_EVENTS],
            config: {
              host: "",
              port: 465,
              security: "ssl",
              username: "",
              password: "",
              fromName: "OVH Nexus",
              fromAddress: "",
              recipients: [],
            },
            hasSecrets: { password: false },
          };

    setChannels((prev) => [...prev, newChannel]);
  };

  const handleRemoveChannel = (id: string) => {
    setChannels((prev) => prev.filter((c) => c.id !== id));
  };

  const updateChannel = (
    id: string,
    updater: (ch: NotificationChannel) => NotificationChannel
  ) => {
    setChannels((prev) => prev.map((c) => (c.id === id ? updater(c) : c)));
  };

  const toggleChannelEvent = (channelId: string, eventKey: string) => {
    updateChannel(channelId, (ch) => {
      const exists = ch.events.includes(eventKey);
      return {
        ...ch,
        events: exists
          ? ch.events.filter((e) => e !== eventKey)
          : [...ch.events, eventKey],
      };
    });
  };

  const handleTestChannel = async (ch: NotificationChannel) => {
    setTestingChannelId(ch.id);
    try {
      const cfg = { ...ch.config };
      if (ch.type === "smtp" && typeof cfg.recipients === "string") {
        cfg.recipients = cfg.recipients
          .replace(/;/g, ",")
          .split(",")
          .map((s) => s.trim())
          .filter(Boolean);
      }
      const res = await api.post(`/notifications/channels/${ch.id}/test`, {
        channel: {
          ...ch,
          config: cfg,
        },
      });
      toast.success(res.data?.message || `渠道「${ch.name}」测试发送成功`);
    } catch (err: any) {
      toast.error(
        `测试发送失败: ` + (err.response?.data?.message || err.message)
      );
    } finally {
      setTestingChannelId(null);
    }
  };

  const handleTriggerSkuCheck = async () => {
    setIsCheckingSku(true);
    try {
      const res = await api.post("/sku/check");
      if (res.data?.status) {
        setSkuStatus(res.data.status);
      }
      const r = res.data?.result;
      if (r?.status === "baseline_initialized") {
        toast.success(`SKU 基线初始化完成，共索引 ${r.totalSkus} 款型号`);
      } else if (r?.status === "ok") {
        toast.success(
          `SKU 检测完成：当前共 ${r.totalSkus} 款型号（新增 ${
            r.added?.length || 0
          }，下架 ${r.removed?.length || 0}）`
        );
      } else {
        toast.info("SKU 检测已执行");
      }
    } catch (err: any) {
      toast.error(
        "触发 SKU 检测失败: " + (err.response?.data?.message || err.message)
      );
    } finally {
      setIsCheckingSku(false);
    }
  };

  const fetchWebhookInfo = async () => {
    setIsLoadingWebhook(true);
    try {
      const res = await api.get("/telegram/get-webhook-info");
      if (res.data?.success) {
        setWebhookInfo(res.data.webhook_info);
        if (res.data.webhook_info?.url) {
          setWebhookUrl(res.data.webhook_info.url);
        }
      } else {
        toast.error(res.data?.error || "获取 Webhook 状态失败");
      }
    } catch (err: any) {
      toast.error(
        "获取 Webhook 状态失败: " + (err.response?.data?.error || err.message)
      );
    } finally {
      setIsLoadingWebhook(false);
    }
  };

  const handleSetWebhook = async () => {
    if (!webhookUrl.trim()) {
      toast.error("请输入公网可访问的 Webhook URL");
      return;
    }
    setIsSettingWebhook(true);
    try {
      const res = await api.post("/telegram/set-webhook", {
        webhook_url: webhookUrl.trim(),
      });
      if (res.data?.success) {
        setWebhookInfo(res.data.webhook_info);
        toast.success("Telegram Webhook 设置成功");
      } else {
        toast.error(res.data?.error || "设置 Webhook 失败");
      }
    } catch (err: any) {
      toast.error(
        "设置 Webhook 失败: " + (err.response?.data?.error || err.message)
      );
    } finally {
      setIsSettingWebhook(false);
    }
  };

  const toggleRange = (code: string) => {
    setRules((prev) => {
      const exists = prev.skuRanges.includes(code);
      const next = exists
        ? prev.skuRanges.filter((r) => r !== code)
        : [...prev.skuRanges, code];
      return { ...prev, skuRanges: next.length > 0 ? next : [code] };
    });
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <motion.div
        initial={{ opacity: 0, y: -16 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.25 }}
        className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4"
      >
        <div>
          <h1 className="text-2xl sm:text-3xl font-bold tracking-tight text-cyber-text flex items-center gap-2.5">
            <Bell className="w-7 h-7 text-cyber-accent" />
            通知中心
          </h1>
          <p className="text-cyber-muted text-sm mt-1">
            管理多实例 Telegram 与 SMTP 邮件通知渠道，按需订阅库存补货、SKU 上下架、抢购结果与服务到期提醒
          </p>
        </div>

        <button
          onClick={handleSaveAll}
          disabled={isSaving || isLoading}
          className="cyber-button-primary px-5 py-2.5 rounded-lg font-medium flex items-center justify-center gap-2 text-sm shadow-sm"
        >
          <Save className="w-4 h-4" />
          {isSaving ? "保存中..." : "保存全部配置"}
        </button>
      </motion.div>

      {/* Tabs */}
      <div className="flex items-center gap-2 border-b border-cyber-grid pb-3">
        <button
          type="button"
          onClick={() => setActiveTab("channels")}
          className={`px-4 py-2 rounded-lg text-sm font-medium transition-all flex items-center gap-2 ${
            activeTab === "channels"
              ? "bg-cyber-accent text-white shadow-sm"
              : "bg-cyber-surface text-cyber-muted hover:text-cyber-text border border-cyber-grid"
          }`}
        >
          <MessageSquare className="w-4 h-4" />
          通知渠道 ({channels.length})
        </button>
        <button
          type="button"
          onClick={() => setActiveTab("rules")}
          className={`px-4 py-2 rounded-lg text-sm font-medium transition-all flex items-center gap-2 ${
            activeTab === "rules"
              ? "bg-cyber-accent text-white shadow-sm"
              : "bg-cyber-surface text-cyber-muted hover:text-cyber-text border border-cyber-grid"
          }`}
        >
          <Sliders className="w-4 h-4" />
          事件与全局规则
        </button>
      </div>

      {/* API Key Notice for lifecycle events */}
      {!hasApiKey && (
        <div className="cyber-panel p-3.5 border-amber-500/30 bg-amber-500/5 flex items-start gap-3 text-xs sm:text-sm">
          <ShieldAlert className="w-5 h-5 text-amber-500 flex-shrink-0 mt-0.5" />
          <div className="text-cyber-text">
            <span className="font-semibold text-amber-600 dark:text-amber-400">
              当前未配置 OVH API Key：
            </span>{" "}
            「库存补货」「库存售罄」「SKU 变动」使用公开免登录 API，可正常推送；标有{" "}
            <span className="px-1.5 py-0.5 rounded bg-amber-500/15 text-amber-600 dark:text-amber-400 text-xs font-mono">
              需 API Key
            </span>{" "}
            的事件（抢购结果、服务到期、订单交付）将在配置 OVH API 密钥后自动生效。
          </div>
        </div>
      )}

      {isLoading ? (
        <div className="cyber-panel p-8 text-center text-cyber-muted">
          正在加载通知渠道与规则配置...
        </div>
      ) : activeTab === "channels" ? (
        <div className="space-y-5">
          {/* Add channel buttons */}
          <div className="flex flex-wrap items-center justify-between gap-3 cyber-panel p-4">
            <div className="text-sm text-cyber-muted">
              支持同时添加多个通知渠道实例（例如多个邮箱分组或不同 Telegram 群组），每个渠道可独立开关并勾选事件。
            </div>
            <div className="flex flex-wrap items-center gap-2.5">
              <button
                type="button"
                onClick={() => handleAddChannel("telegram")}
                className="cyber-button text-xs sm:text-sm px-3.5 py-2 flex items-center gap-1.5"
              >
                <Plus className="w-4 h-4 text-cyber-accent" />
                添加 Telegram 渠道
              </button>
              <button
                type="button"
                onClick={() => handleAddChannel("smtp")}
                className="cyber-button text-xs sm:text-sm px-3.5 py-2 flex items-center gap-1.5"
              >
                <Plus className="w-4 h-4 text-cyber-accent" />
                添加 SMTP 邮件渠道
              </button>
            </div>
          </div>

          {channels.length === 0 ? (
            <div className="cyber-panel p-10 text-center space-y-3">
              <Bell className="w-10 h-10 text-cyber-muted mx-auto opacity-60" />
              <div className="text-base font-medium text-cyber-text">
                尚未添加任何通知渠道
              </div>
              <p className="text-xs sm:text-sm text-cyber-muted max-w-md mx-auto">
                点击上方「添加 Telegram 渠道」或「添加 SMTP 邮件渠道」即可创建推送目标。
              </p>
            </div>
          ) : (
            <div className="space-y-5">
              {channels.map((ch) => {
                const isTg = ch.type === "telegram";
                const recipientsValue = Array.isArray(ch.config.recipients)
                  ? ch.config.recipients.join(", ")
                  : ch.config.recipients || "";
                const secretVisible = Boolean(showSecrets[ch.id]);

                return (
                  <div
                    key={ch.id}
                    className={`cyber-panel p-5 space-y-5 transition-all ${
                      ch.enabled
                        ? "border-cyber-accent/40"
                        : "opacity-75 border-cyber-grid"
                    }`}
                  >
                    {/* Card Top Bar */}
                    <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pb-4 border-b border-cyber-grid">
                      <div className="flex items-center gap-3 flex-1">
                        <span
                          className={`px-2.5 py-1 rounded text-xs font-mono font-bold flex items-center gap-1.5 ${
                            isTg
                              ? "bg-sky-500/15 text-sky-600 dark:text-sky-400 border border-sky-500/30"
                              : "bg-cyber-accent/15 text-cyber-accent border border-cyber-accent/30"
                          }`}
                        >
                          {isTg ? (
                            <MessageSquare className="w-3.5 h-3.5" />
                          ) : (
                            <Mail className="w-3.5 h-3.5" />
                          )}
                          {isTg ? "TELEGRAM" : "SMTP 邮件"}
                        </span>

                        <input
                          type="text"
                          value={ch.name}
                          onChange={(e) =>
                            updateChannel(ch.id, (c) => ({
                              ...c,
                              name: e.target.value,
                            }))
                          }
                          placeholder="渠道名称"
                          className="cyber-input py-1.5 px-3 text-sm font-semibold max-w-xs"
                        />
                      </div>

                      <div className="flex items-center gap-2.5">
                        <label className="flex items-center gap-2 text-xs sm:text-sm cursor-pointer select-none px-2.5 py-1.5 rounded border border-cyber-grid bg-cyber-bg">
                          <input
                            type="checkbox"
                            checked={ch.enabled}
                            onChange={(e) =>
                              updateChannel(ch.id, (c) => ({
                                ...c,
                                enabled: e.target.checked,
                              }))
                            }
                            className="rounded border-cyber-grid text-cyber-accent focus:ring-cyber-accent"
                          />
                          <span
                            className={
                              ch.enabled
                                ? "text-emerald-600 dark:text-emerald-400 font-medium"
                                : "text-cyber-muted"
                            }
                          >
                            {ch.enabled ? "已启用" : "已停用"}
                          </span>
                        </label>

                        <button
                          type="button"
                          onClick={() => handleTestChannel(ch)}
                          disabled={testingChannelId === ch.id}
                          className="cyber-button text-xs py-1.5 px-3 flex items-center gap-1.5"
                        >
                          <Send className="w-3.5 h-3.5" />
                          {testingChannelId === ch.id
                            ? "发送中..."
                            : "发送测试"}
                        </button>

                        <button
                          type="button"
                          onClick={() => handleRemoveChannel(ch.id)}
                          className="cyber-button text-xs py-1.5 px-2.5 text-red-500 border-red-500/30 hover:bg-red-500/10"
                          title="删除此渠道"
                        >
                          <Trash2 className="w-4 h-4" />
                        </button>
                      </div>
                    </div>

                    {/* Credentials Form */}
                    {isTg ? (
                      <div className="space-y-4">
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                          <div>
                            <label className="block text-xs font-medium text-cyber-muted mb-1">
                              Telegram Bot Token
                            </label>
                            <div className="relative">
                              <input
                                type={secretVisible ? "text" : "password"}
                                value={ch.config.token || ""}
                                onChange={(e) =>
                                  updateChannel(ch.id, (c) => ({
                                    ...c,
                                    config: {
                                      ...c.config,
                                      token: e.target.value,
                                    },
                                  }))
                                }
                                placeholder={
                                  ch.hasSecrets?.token
                                    ? "已配置密钥，留空保持不变"
                                    : "123456789:ABCdefGHIjklMNO..."
                                }
                                className="cyber-input w-full pr-10 text-sm"
                              />
                              <button
                                type="button"
                                onClick={() =>
                                  setShowSecrets((p) => ({
                                    ...p,
                                    [ch.id]: !p[ch.id],
                                  }))
                                }
                                className="absolute inset-y-0 right-0 px-3 text-cyber-muted hover:text-cyber-text"
                              >
                                {secretVisible ? (
                                  <EyeOff className="w-4 h-4" />
                                ) : (
                                  <Eye className="w-4 h-4" />
                                )}
                              </button>
                            </div>
                          </div>

                          <div>
                            <label className="block text-xs font-medium text-cyber-muted mb-1">
                              Chat ID（个人 ID 或群组/频道 ID）
                            </label>
                            <input
                              type="text"
                              value={ch.config.chatId || ""}
                              onChange={(e) =>
                                updateChannel(ch.id, (c) => ({
                                  ...c,
                                  config: {
                                    ...c.config,
                                    chatId: e.target.value,
                                  },
                                }))
                              }
                              placeholder="例如: 123456789 或 -100123456789"
                              className="cyber-input w-full text-sm"
                            />
                          </div>
                        </div>

                        {/* Collapsible Telegram Webhook Config */}
                        <div className="pt-2">
                          <button
                            type="button"
                            onClick={() =>
                              setExpandedWebhookChannelId((prev) =>
                                prev === ch.id ? null : ch.id
                              )
                            }
                            className="text-xs text-cyber-accent hover:underline flex items-center gap-1.5 font-medium"
                          >
                            <Webhook className="w-3.5 h-3.5" />
                            {expandedWebhookChannelId === ch.id
                              ? "收起 Telegram 按钮回调 Webhook 设置"
                              : "展开 Telegram 按钮回调 Webhook 设置（用于消息内一键加入抢购队列）"}
                          </button>

                          {expandedWebhookChannelId === ch.id && (
                            <div className="mt-3 p-4 rounded-lg border border-cyber-grid bg-cyber-bg space-y-3">
                              <p className="text-xs text-cyber-muted">
                                配置公网 HTTPS Webhook 后，当收到补货通知时可直接点击 Telegram 消息下方的按钮将机型加入抢购队列（先保存 Bot Token 后再操作）。
                              </p>
                              <div className="flex flex-col sm:flex-row gap-2">
                                <input
                                  type="text"
                                  value={webhookUrl}
                                  onChange={(e) =>
                                    setWebhookUrl(e.target.value)
                                  }
                                  placeholder="https://your-domain.com/api/telegram/webhook"
                                  className="cyber-input flex-1 text-xs sm:text-sm"
                                />
                                <button
                                  type="button"
                                  onClick={() =>
                                    setWebhookUrl(
                                      `${window.location.origin}/api/telegram/webhook`
                                    )
                                  }
                                  className="cyber-button text-xs px-3 py-2 whitespace-nowrap"
                                >
                                  填入当前域名
                                </button>
                                <button
                                  type="button"
                                  onClick={handleSetWebhook}
                                  disabled={isSettingWebhook}
                                  className="cyber-button-primary text-xs px-3.5 py-2 rounded whitespace-nowrap"
                                >
                                  {isSettingWebhook
                                    ? "设置中..."
                                    : "设置 Webhook"}
                                </button>
                                <button
                                  type="button"
                                  onClick={fetchWebhookInfo}
                                  disabled={isLoadingWebhook}
                                  className="cyber-button text-xs px-3 py-2 whitespace-nowrap"
                                >
                                  {isLoadingWebhook
                                    ? "查询中..."
                                    : "查询当前状态"}
                                </button>
                              </div>
                              {webhookInfo && (
                                <div className="text-xs text-cyber-muted font-mono bg-cyber-surface p-2.5 rounded border border-cyber-grid">
                                  <div>
                                    当前 URL: {webhookInfo.url || "未设置"}
                                  </div>
                                  <div>
                                    待处理更新数:{" "}
                                    {webhookInfo.pending_update_count ?? 0}
                                  </div>
                                  {webhookInfo.last_error_message && (
                                    <div className="text-red-500">
                                      最近错误: {webhookInfo.last_error_message}
                                    </div>
                                  )}
                                </div>
                              )}
                            </div>
                          )}
                        </div>
                      </div>
                    ) : (
                      /* SMTP Configuration Form */
                      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                        <div>
                          <label className="block text-xs font-medium text-cyber-muted mb-1">
                            SMTP 服务器地址 (Host)
                          </label>
                          <input
                            type="text"
                            value={ch.config.host || ""}
                            onChange={(e) =>
                              updateChannel(ch.id, (c) => ({
                                ...c,
                                config: { ...c.config, host: e.target.value },
                              }))
                            }
                            placeholder="smtp.example.com"
                            className="cyber-input w-full text-sm"
                          />
                        </div>

                        <div>
                          <label className="block text-xs font-medium text-cyber-muted mb-1">
                            加密方式 (Security)
                          </label>
                          <select
                            value={ch.config.security || "ssl"}
                            onChange={(e) => {
                              const sec = e.target.value as
                                | "ssl"
                                | "starttls"
                                | "none";
                              updateChannel(ch.id, (c) => {
                                let nextPort = c.config.port || 465;
                                if (
                                  sec === "ssl" &&
                                  (nextPort === 587 || nextPort === 25)
                                )
                                  nextPort = 465;
                                if (
                                  sec === "starttls" &&
                                  (nextPort === 465 || nextPort === 25)
                                )
                                  nextPort = 587;
                                if (
                                  sec === "none" &&
                                  (nextPort === 465 || nextPort === 587)
                                )
                                  nextPort = 25;
                                return {
                                  ...c,
                                  config: {
                                    ...c.config,
                                    security: sec,
                                    port: nextPort,
                                  },
                                };
                              });
                            }}
                            className="cyber-input w-full text-sm"
                          >
                            <option value="ssl">SSL / TLS (默认端口 465)</option>
                            <option value="starttls">
                              STARTTLS (默认端口 587)
                            </option>
                            <option value="none">不加密 Plain (端口 25)</option>
                          </select>
                        </div>

                        <div>
                          <label className="block text-xs font-medium text-cyber-muted mb-1">
                            端口 (Port)
                          </label>
                          <input
                            type="number"
                            value={ch.config.port ?? 465}
                            onChange={(e) =>
                              updateChannel(ch.id, (c) => ({
                                ...c,
                                config: {
                                  ...c.config,
                                  port: parseInt(e.target.value, 10) || 465,
                                },
                              }))
                            }
                            placeholder="465"
                            className="cyber-input w-full text-sm"
                          />
                        </div>

                        <div>
                          <label className="block text-xs font-medium text-cyber-muted mb-1">
                            认证用户名 (Username)
                          </label>
                          <input
                            type="text"
                            value={ch.config.username || ""}
                            onChange={(e) =>
                              updateChannel(ch.id, (c) => ({
                                ...c,
                                config: {
                                  ...c.config,
                                  username: e.target.value,
                                },
                              }))
                            }
                            placeholder="notify@example.com"
                            className="cyber-input w-full text-sm"
                          />
                        </div>

                        <div>
                          <label className="block text-xs font-medium text-cyber-muted mb-1">
                            认证密码 / 应用授权码
                          </label>
                          <div className="relative">
                            <input
                              type={secretVisible ? "text" : "password"}
                              value={ch.config.password || ""}
                              onChange={(e) =>
                                updateChannel(ch.id, (c) => ({
                                  ...c,
                                  config: {
                                    ...c.config,
                                    password: e.target.value,
                                  },
                                }))
                              }
                              placeholder={
                                ch.hasSecrets?.password
                                  ? "已配置密码，留空保持不变"
                                  : "请输入 SMTP 密码或授权码"
                              }
                              className="cyber-input w-full pr-10 text-sm"
                            />
                            <button
                              type="button"
                              onClick={() =>
                                setShowSecrets((p) => ({
                                  ...p,
                                  [ch.id]: !p[ch.id],
                                }))
                              }
                              className="absolute inset-y-0 right-0 px-3 text-cyber-muted hover:text-cyber-text"
                            >
                              {secretVisible ? (
                                <EyeOff className="w-4 h-4" />
                              ) : (
                                <Eye className="w-4 h-4" />
                              )}
                            </button>
                          </div>
                        </div>

                        <div>
                          <label className="block text-xs font-medium text-cyber-muted mb-1">
                            发件人显示名称 (From Name)
                          </label>
                          <input
                            type="text"
                            value={ch.config.fromName || ""}
                            onChange={(e) =>
                              updateChannel(ch.id, (c) => ({
                                ...c,
                                config: {
                                  ...c.config,
                                  fromName: e.target.value,
                                },
                              }))
                            }
                            placeholder="OVH Nexus"
                            className="cyber-input w-full text-sm"
                          />
                        </div>

                        <div>
                          <label className="block text-xs font-medium text-cyber-muted mb-1">
                            发件人邮箱地址 (From Address)
                          </label>
                          <input
                            type="email"
                            value={ch.config.fromAddress || ""}
                            onChange={(e) =>
                              updateChannel(ch.id, (c) => ({
                                ...c,
                                config: {
                                  ...c.config,
                                  fromAddress: e.target.value,
                                },
                              }))
                            }
                            placeholder="留空则默认使用认证用户名"
                            className="cyber-input w-full text-sm"
                          />
                        </div>

                        <div className="sm:col-span-2">
                          <label className="block text-xs font-medium text-cyber-muted mb-1">
                            收件人邮箱列表（支持多个收件人，用英文逗号分隔）
                          </label>
                          <input
                            type="text"
                            value={recipientsValue}
                            onChange={(e) =>
                              updateChannel(ch.id, (c) => ({
                                ...c,
                                config: {
                                  ...c.config,
                                  recipients: e.target.value,
                                },
                              }))
                            }
                            placeholder="ops@example.com, admin@example.com"
                            className="cyber-input w-full text-sm"
                          />
                        </div>
                      </div>
                    )}

                    {/* Event Subscriptions Matrix */}
                    <div className="pt-3 border-t border-cyber-grid">
                      <div className="text-xs font-semibold text-cyber-muted uppercase tracking-wider mb-3">
                        订阅推送事件（勾选后当事件发生时将通过此渠道推送）
                      </div>
                      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2.5">
                        {EVENT_DEFINITIONS.map((ev) => {
                          const checked = ch.events.includes(ev.key);
                          const disabledByApi = ev.requiresApiKey && !hasApiKey;
                          return (
                            <label
                              key={ev.key}
                              className={`p-2.5 rounded-lg border text-xs flex items-start gap-2.5 cursor-pointer transition-all ${
                                checked
                                  ? "border-cyber-accent/50 bg-cyber-accent/10"
                                  : "border-cyber-grid bg-cyber-bg hover:border-cyber-accent/30"
                              } ${disabledByApi ? "opacity-60" : ""}`}
                            >
                              <input
                                type="checkbox"
                                checked={checked}
                                onChange={() =>
                                  toggleChannelEvent(ch.id, ev.key)
                                }
                                className="mt-0.5 rounded border-cyber-grid text-cyber-accent focus:ring-cyber-accent"
                              />
                              <div className="space-y-0.5 flex-1">
                                <div className="font-semibold text-cyber-text flex items-center justify-between gap-1">
                                  <span>{ev.label}</span>
                                  {ev.requiresApiKey && (
                                    <span className="px-1.5 py-0.2 rounded text-[10px] font-mono bg-amber-500/15 text-amber-600 dark:text-amber-400 border border-amber-500/30">
                                      需 API Key
                                    </span>
                                  )}
                                </div>
                                <p className="text-cyber-muted text-[11px] leading-snug">
                                  {ev.desc}
                                </p>
                              </div>
                            </label>
                          );
                        })}
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      ) : (
        /* Tab 2: Global Rules & SKU Watcher Settings */
        <div className="space-y-6">
          {/* SKU Change Watcher Rules */}
          <div className="cyber-panel p-5 space-y-5">
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pb-4 border-b border-cyber-grid">
              <div>
                <h2 className="text-lg font-bold text-cyber-text flex items-center gap-2">
                  📦 货架 SKU 变动监控规则（免登录公开 API）
                </h2>
                <p className="text-xs sm:text-sm text-cyber-muted mt-0.5">
                  定时拉取 OVHcloud 实时可用接口，监控所有在架型号（包括已上架但未开售型号）的新增与下架变动
                </p>
              </div>

              <div className="flex items-center gap-2.5">
                <button
                  type="button"
                  onClick={handleTriggerSkuCheck}
                  disabled={isCheckingSku}
                  className="cyber-button text-xs sm:text-sm py-2 px-3.5 flex items-center gap-1.5"
                >
                  <RefreshCw
                    className={`w-4 h-4 ${isCheckingSku ? "animate-spin" : ""}`}
                  />
                  {isCheckingSku ? "正在检测..." : "立即检测一次"}
                </button>
              </div>
            </div>

            {skuStatus && (
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 bg-cyber-bg p-3.5 rounded-lg border border-cyber-grid text-xs">
                <div>
                  <span className="text-cyber-muted">监控状态：</span>
                  <span className="font-semibold text-emerald-600 dark:text-emerald-400 ml-1">
                    {skuStatus.initialized ? "已建立基线" : "待建立基线"}
                  </span>
                  <span className="text-cyber-muted ml-1">
                    ({skuStatus.endpoint.toUpperCase()})
                  </span>
                </div>
                <div>
                  <span className="text-cyber-muted">已索引货架型号：</span>
                  <span className="font-mono font-bold text-cyber-accent ml-1">
                    {skuStatus.totalSkus} 款
                  </span>
                </div>
                <div>
                  <span className="text-cyber-muted">最近检测时间：</span>
                  <span className="font-mono text-cyber-text ml-1">
                    {skuStatus.lastCheckTime
                      ? new Date(skuStatus.lastCheckTime).toLocaleString(
                          "zh-CN"
                        )
                      : "尚未执行"}
                  </span>
                </div>
              </div>
            )}

            <div className="grid grid-cols-1 md:grid-cols-2 gap-5">
              <div className="space-y-3">
                <label className="flex items-center gap-2.5 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={rules.skuEnabled}
                    onChange={(e) =>
                      setRules((r) => ({ ...r, skuEnabled: e.target.checked }))
                    }
                    className="rounded border-cyber-grid text-cyber-accent focus:ring-cyber-accent"
                  />
                  <span className="text-sm font-medium text-cyber-text">
                    启用后台 SKU 变动定时监控
                  </span>
                </label>

                <label className="flex items-center gap-2.5 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={rules.skuMergeRegional}
                    onChange={(e) =>
                      setRules((r) => ({
                        ...r,
                        skuMergeRegional: e.target.checked,
                      }))
                    }
                    className="rounded border-cyber-grid text-cyber-accent focus:ring-cyber-accent"
                  />
                  <span className="text-sm text-cyber-text">
                    合并亚太区域后缀变体（将{" "}
                    <code className="font-mono text-xs">-sgp / -syd / -mum</code>{" "}
                    合并至主型号统计）
                  </span>
                </label>

                <label className="flex items-center gap-2.5 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={rules.skuTrackConfigs}
                    onChange={(e) =>
                      setRules((r) => ({
                        ...r,
                        skuTrackConfigs: e.target.checked,
                      }))
                    }
                    className="rounded border-cyber-grid text-cyber-accent focus:ring-cyber-accent"
                  />
                  <span className="text-sm text-cyber-text">
                    同时追踪具体配置组合变动（如同一型号新增 64 GB 内存组合）
                  </span>
                </label>
              </div>

              <div className="space-y-3">
                <div>
                  <label className="block text-xs font-medium text-cyber-muted mb-1">
                    轮询检测间隔（秒，建议 300 ~ 1800 秒）
                  </label>
                  <input
                    type="number"
                    min={60}
                    max={86400}
                    value={rules.skuInterval}
                    onChange={(e) =>
                      setRules((r) => ({
                        ...r,
                        skuInterval: Math.max(
                          60,
                          parseInt(e.target.value, 10) || 600
                        ),
                      }))
                    }
                    className="cyber-input w-full max-w-xs text-sm"
                  />
                </div>

                <div className="text-xs text-cyber-muted space-y-1 bg-cyber-bg p-3 rounded border border-cyber-grid">
                  <div className="font-semibold text-cyber-text flex items-center gap-1">
                    <CheckCircle2 className="w-3.5 h-3.5 text-cyber-accent" />
                    内置防误报保护机制已开启：
                  </div>
                  <div>• 首次启动仅建立型号基线，不触发轰炸推送</div>
                  <div>• 型号下架需连续 2 轮检测均缺失才确认推送</div>
                  <div>
                    • 单轮型号总数骤降超过 30% 时自动判定为 API 抖动并跳过
                  </div>
                </div>
              </div>
            </div>

            <div>
              <div className="text-xs font-semibold text-cyber-muted uppercase tracking-wider mb-2.5">
                监控产品线范围（勾选需要追踪的系列）
              </div>
              <div className="flex flex-wrap gap-2">
                {PRODUCT_RANGES.map((pr) => {
                  const active = rules.skuRanges.includes(pr.code);
                  return (
                    <button
                      key={pr.code}
                      type="button"
                      onClick={() => toggleRange(pr.code)}
                      className={`px-3 py-1.5 rounded-lg text-xs font-medium border transition-all ${
                        active
                          ? "bg-cyber-accent/15 border-cyber-accent text-cyber-accent"
                          : "bg-cyber-bg border-cyber-grid text-cyber-muted"
                      }`}
                    >
                      {active ? "✓ " : ""}
                      {pr.label}
                    </button>
                  );
                })}
              </div>
            </div>
          </div>

          {/* Service Expiration & Email Panel Link */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <div className="cyber-panel p-5 space-y-3">
              <h3 className="text-base font-bold text-cyber-text">
                ⏳ 服务到期提醒阈值（需 API Key）
              </h3>
              <p className="text-xs text-cyber-muted">
                当名下独立服务器或 VPS 距离到期日小于等于该天数时，推送一次到期提醒（同一服务同一到期周期仅提醒一次）。
              </p>
              <div>
                <label className="block text-xs font-medium text-cyber-muted mb-1">
                  提前提醒天数（1 ~ 90 天）
                </label>
                <input
                  type="number"
                  min={1}
                  max={90}
                  value={rules.expiryDays}
                  onChange={(e) =>
                    setRules((r) => ({
                      ...r,
                      expiryDays: Math.max(
                        1,
                        Math.min(90, parseInt(e.target.value, 10) || 7)
                      ),
                    }))
                  }
                  className="cyber-input w-40 text-sm"
                />
              </div>
            </div>

            <div className="cyber-panel p-5 space-y-3">
              <h3 className="text-base font-bold text-cyber-text flex items-center gap-1.5">
                <ExternalLink className="w-4 h-4 text-cyber-accent" />
                邮件底部控制台跳转链接（可选）
              </h3>
              <p className="text-xs text-cyber-muted">
                填写后，SMTP 邮件通知底部将附带「打开 OVH Nexus 控制台」按钮（打开后仍需输入访问密钥，不附带免密下单令牌以保障安全）。
              </p>
              <div>
                <label className="block text-xs font-medium text-cyber-muted mb-1">
                  面板访问 URL
                </label>
                <input
                  type="url"
                  value={panelUrl}
                  onChange={(e) => setPanelUrl(e.target.value)}
                  placeholder="https://nexus.example.com"
                  className="cyber-input w-full text-sm"
                />
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

export default NotificationCenterPage;
