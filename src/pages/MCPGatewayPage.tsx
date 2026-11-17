import { useState, useEffect, useCallback, useMemo } from "react";
import { motion } from "framer-motion";
import { api } from "@/utils/apiClient";
import { useAPI } from "@/context/APIContext";
import { useLocale } from "@/context/LocaleContext";
import { toast } from "sonner";
import {
  Bot,
  Shield,
  ShieldAlert,
  ShieldCheck,
  KeyRound,
  Copy,
  RefreshCw,
  Trash2,
  Eye,
  EyeOff,
  Save,
  Terminal,
  Lock,
  CheckCircle2,
  XCircle,
  AlertTriangle,
  Activity,
  Users,
  Code2,
  Search,
  Layers,
  Sparkles,
} from "lucide-react";

interface MCPScopes {
  read: boolean;
  procurement: boolean;
  operations: boolean;
}

interface MCPSettings {
  enabled: boolean;
  hasToken: boolean;
  maskedToken: string;
  revealedToken?: string;
  tokenRotatedAt: string | null;
  scopes: MCPScopes;
  totpEnabled?: boolean;
  operationsLockedByTotp?: boolean;
  allowedAccountIds: string[];
  allowAllAccounts: boolean;
  hourlyOrderLimit: number;
  notifyOnMutation: boolean;
  updatedAt: string | null;
}

interface AliasEntry {
  alias: string;
  category: "account" | "baremetal" | "vps" | "ip" | "order" | string;
  label?: string;
  hint?: string;
  accountAlias?: string;
  maskedReal?: string;
  realValue: string;
}

interface AuditEntry {
  id: string;
  timestamp: string;
  tool: string;
  scope: string;
  status: "success" | "allowed" | "blocked" | "error" | string;
  clientIp?: string;
  arguments?: Record<string, any>;
  summary?: string;
}

interface ToolCatalogItem {
  name: string;
  scope: "read" | "procurement" | "operations";
  title: string;
  desc: string;
  cliEquivalent: string;
}

const TOOL_CATALOG: ToolCatalogItem[] = [
  // L1 Read-Only (11 tools)
  {
    name: "profile_list",
    scope: "read",
    title: "列出授权 OVH 账户画像",
    desc: "返回白名单内的账户混淆代号 (acct-xx)、安全备注名、站点区域与默认标识，脱敏 NIC Handle 与邮箱。",
    cliEquivalent: "ovhcloud account info",
  },
  {
    name: "catalog_availability",
    scope: "read",
    title: "查询全球机房实时库存与目录配置",
    desc: "按关键词、系列或 planCode (如 24ska01) 查询独服硬件规格、选配价格及全球数据中心实时库存状态。",
    cliEquivalent: "ovhcloud baremetal availability <planCode>",
  },
  {
    name: "catalog_sku_changes",
    scope: "read",
    title: "查询货架 SKU 上新与下架动态",
    desc: "获取 SKU 监控器捕获的最新公开货架型号上新、隐藏预上架型号及配置变动记录。",
    cliEquivalent: "ovh-nexus sku changes",
  },
  {
    name: "baremetal_list",
    scope: "read",
    title: "列出独服资产列表",
    desc: "返回名下独立服务器混淆代号 (bm-xx)、安全备注名、机房、型号、系统及掩码尾号 IP (*.*.*.42)。",
    cliEquivalent: "ovhcloud baremetal list",
  },
  {
    name: "baremetal_get",
    scope: "read",
    title: "查询单台独服硬件与网络详情",
    desc: "按代号 (bm-xx) 查询独服硬件规格、实时状态、硬件维保介入记录与网络信息。",
    cliEquivalent: "ovhcloud baremetal get <bm-xx>",
  },
  {
    name: "baremetal_firewall_get",
    scope: "read",
    title: "查询独服 IP 边缘防火墙与清洗策略",
    desc: "按独服代号 (bm-xx) 查看 OVH 边缘防火墙状态、Anti-DDoS 清洗模式及当前规则列表（IP 全程以 ip-xx 脱敏）。",
    cliEquivalent: "ovhcloud ip firewall get <bm-xx>",
  },
  {
    name: "vps_list",
    scope: "read",
    title: "列出已购 VPS 实例",
    desc: "返回名下 VPS 混淆代号 (vps-xx)、型号规格、机房区域、vCore/内存、运行状态与掩码尾号 IP。",
    cliEquivalent: "ovhcloud vps list",
  },
  {
    name: "order_list",
    scope: "read",
    title: "查询最近订单列表",
    desc: "列出近期订单代号 (ord-xx)、含税总价与创建时间（自动剥离含订单密码的付款链接与 PDF 账单链接）。",
    cliEquivalent: "ovhcloud billing order list",
  },
  {
    name: "order_follow_up",
    scope: "read",
    title: "追踪单笔订单 4 步装机交付进度",
    desc: "按订单代号 (ord-xx) 查询 OVH 订单的创建、验证、交付与装机实时步骤详情。",
    cliEquivalent: "ovhcloud billing order follow-up <ord-xx>",
  },
  {
    name: "service_renewals",
    scope: "read",
    title: "统览服务到期倒计时与续费策略",
    desc: "汇总名下独服与 VPS 的到期日、剩余天数、自动续费与到期销毁标记。",
    cliEquivalent: "ovhcloud billing service list",
  },
  {
    name: "procurement_status",
    scope: "read",
    title: "查看库存监控与自动抢购队列状态",
    desc: "返回当前独服/VPS 库存监控订阅列表、自动抢购队列任务概览与近期抢购历史。",
    cliEquivalent: "ovh-nexus status",
  },
  // L2 Unpaid Procurement (5 tools)
  {
    name: "order_quote",
    scope: "procurement",
    title: "第一步：试算配置报价并签发报价单",
    desc: "创建临时购物车试算含税价格，生成 5 分钟有效的 quote_id 供用户在对话中确认，绝不产生真实订单。",
    cliEquivalent: "ovhcloud baremetal order quote",
  },
  {
    name: "order_create_unpaid",
    scope: "procurement",
    title: "第二步：凭确认报价单生成【未付款订单】",
    desc: "校验 quote_id 后生成待支付订单 (ord-xx)。底层硬编码锁死 auto_pay=False 且单次限购 1 台，需人工前往控制台或官网付款。",
    cliEquivalent: "ovhcloud baremetal order create --no-autopay",
  },
  {
    name: "queue_add",
    scope: "procurement",
    title: "新增免自动扣款抢购队列任务",
    desc: "添加后台库存轮询抢购任务（底层强制锁定 auto_pay=False 且数量固定为 1，抢到后仅生成待支付订单）。",
    cliEquivalent: "ovh-nexus queue add --no-autopay",
  },
  {
    name: "queue_manage",
    scope: "procurement",
    title: "暂停 / 恢复 / 移除抢购队列任务",
    desc: "对指定抢购队列任务执行暂停 (pause)、恢复 (resume) 或删除 (delete) 操作。",
    cliEquivalent: "ovh-nexus queue pause|resume|delete <id>",
  },
  {
    name: "stock_subscription_manage",
    scope: "procurement",
    title: "管理独服 / VPS 到货提醒订阅",
    desc: "新增或移除指定 planCode 的独服或 VPS 实时补货通知订阅。",
    cliEquivalent: "ovh-nexus monitor add|remove <planCode>",
  },
  // L3 Reversible Operations (4 tools)
  {
    name: "baremetal_reboot",
    scope: "operations",
    title: "独服硬件重启 (Hard Reboot)",
    desc: "对指定独服代号 (bm-xx) 下发硬件重启任务。",
    cliEquivalent: "ovhcloud baremetal reboot <bm-xx>",
  },
  {
    name: "vps_reboot",
    scope: "operations",
    title: "VPS 实例重启",
    desc: "对指定 VPS 代号 (vps-xx) 下发重启指令。",
    cliEquivalent: "ovhcloud vps reboot <vps-xx>",
  },
  {
    name: "baremetal_ipmi_reset",
    scope: "operations",
    title: "重置独服 IPMI/KVM 卡死会话",
    desc: "当带外控制台连接异常时重置 IPMI 接口与会话（禁止通过 MCP 获取带一次性令牌的 KVM 远程控制链接）。",
    cliEquivalent: "ovhcloud baremetal ipmi reset <bm-xx>",
  },
  {
    name: "baremetal_firewall_update",
    scope: "operations",
    title: "配置 IP 边缘防火墙规则与 Anti-DDoS 清洗",
    desc: "按独服代号 (bm-xx) 切换永久防洪清洗模式，或增删序列号 0–19 的边缘防火墙规则。",
    cliEquivalent: "ovhcloud ip firewall update <bm-xx>",
  },
];

const CATEGORY_LABELS: Record<string, { label: string; color: string }> = {
  account: {
    label: "OVH 账户",
    color: "bg-indigo-500/10 text-indigo-500 border-indigo-500/25",
  },
  baremetal: {
    label: "独立服务器",
    color: "bg-cyber-accent/10 text-cyber-accent border-cyber-accent/25",
  },
  vps: {
    label: "VPS 实例",
    color: "bg-emerald-500/10 text-emerald-500 border-emerald-500/25",
  },
  ip: {
    label: "公网 IP",
    color: "bg-amber-500/10 text-amber-500 border-amber-500/25",
  },
  order: {
    label: "采购订单",
    color: "bg-purple-500/10 text-purple-500 border-purple-500/25",
  },
};

const MCPGatewayPage = () => {
  const { accounts } = useAPI();
  const { t } = useLocale();

  const [activeTab, setActiveTab] = useState<
    "config" | "aliases" | "audit" | "tools"
  >("config");
  const [clientSnippetTab, setClientSnippetTab] = useState<
    "claude-cli" | "claude-desktop" | "antigravity" | "curl"
  >("claude-cli");
  const [jsonSnippetMode, setJsonSnippetMode] = useState<"entry-only" | "full">(
    "entry-only"
  );

  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [isRotating, setIsRotating] = useState(false);
  const [showToken, setShowToken] = useState(false);
  const [freshToken, setFreshToken] = useState<string>("");

  const [settings, setSettings] = useState<MCPSettings>({
    enabled: false,
    hasToken: false,
    maskedToken: "",
    tokenRotatedAt: null,
    scopes: {
      read: true,
      procurement: false,
      operations: false,
    },
    allowedAccountIds: [],
    allowAllAccounts: true,
    hourlyOrderLimit: 5,
    notifyOnMutation: true,
    updatedAt: null,
  });

  // Aliases state
  const [aliases, setAliases] = useState<AliasEntry[]>([]);
  const [aliasCategoryFilter, setAliasCategoryFilter] = useState<string>("all");
  const [aliasSearch, setAliasSearch] = useState<string>("");
  const [revealedAliases, setRevealedAliases] = useState<
    Record<string, boolean>
  >({});
  const [isLoadingAliases, setIsLoadingAliases] = useState(false);

  // Audit state
  const [auditLogs, setAuditLogs] = useState<AuditEntry[]>([]);
  const [isLoadingAudit, setIsLoadingAudit] = useState(false);

  const mcpEndpointUrl = useMemo(() => {
    if (typeof window === "undefined") return "http://localhost:5000/api/mcp";
    return `${window.location.origin}/api/mcp`;
  }, []);

  const fetchSettings = useCallback(async () => {
    setIsLoading(true);
    try {
      const res = await api.get("/mcp/settings");
      const data = res.data?.data || res.data;
      if (data) {
        setSettings(data);
        if (data.revealedToken) {
          setFreshToken(data.revealedToken);
          setShowToken(true);
        }
      }
    } catch (err: any) {
      toast.error(
        err?.response?.data?.error || "加载 MCP 网关设置失败，请检查后端连接"
      );
    } finally {
      setIsLoading(false);
    }
  }, []);

  const fetchAliases = useCallback(async () => {
    setIsLoadingAliases(true);
    try {
      const res = await api.get("/mcp/aliases");
      const list = res.data?.data ?? res.data?.items ?? [];
      setAliases(Array.isArray(list) ? list : []);
    } catch {
      toast.error("加载隐私混淆映射表失败");
    } finally {
      setIsLoadingAliases(false);
    }
  }, []);

  const fetchAuditLogs = useCallback(async () => {
    setIsLoadingAudit(true);
    try {
      const res = await api.get("/mcp/audit?limit=100");
      const list = res.data?.data ?? res.data?.items ?? [];
      setAuditLogs(Array.isArray(list) ? list : []);
    } catch {
      toast.error("加载 MCP 审计日志失败");
    } finally {
      setIsLoadingAudit(false);
    }
  }, []);

  useEffect(() => {
    fetchSettings();
    fetchAliases();
    fetchAuditLogs();
  }, [fetchSettings, fetchAliases, fetchAuditLogs]);

  const handleSaveSettings = async (override?: Partial<MCPSettings>) => {
    setIsSaving(true);
    try {
      const payload = {
        enabled: override?.enabled ?? settings.enabled,
        scopes: override?.scopes ?? settings.scopes,
        allowAllAccounts:
          override?.allowAllAccounts ?? settings.allowAllAccounts,
        allowedAccountIds:
          override?.allowedAccountIds ?? settings.allowedAccountIds,
        hourlyOrderLimit:
          override?.hourlyOrderLimit ?? settings.hourlyOrderLimit,
        notifyOnMutation:
          override?.notifyOnMutation ?? settings.notifyOnMutation,
      };
      const res = await api.put("/mcp/settings", payload);
      const data = res.data?.data || res.data;
      if (data) {
        setSettings(data);
        if (data.revealedToken) {
          setFreshToken(data.revealedToken);
          setShowToken(true);
          toast.success("MCP 网关已开启并自动签发初始 Bearer Token，请立即复制保存！");
          return;
        }
      }
      toast.success("MCP 网关配置已保存");
    } catch (err: any) {
      toast.error(err?.response?.data?.error || "保存 MCP 配置失败");
    } finally {
      setIsSaving(false);
    }
  };

  const handleRotateToken = async () => {
    setIsRotating(true);
    try {
      const res = await api.post("/mcp/token/rotate");
      const data = res.data?.data || res.data;
      if (data) {
        setSettings(data);
        if (data.revealedToken) {
          setFreshToken(data.revealedToken);
          setShowToken(true);
        }
      }
      toast.success("已生成新的 MCP 专属 Bearer Token（仅明文展示一次，请及时复制）");
    } catch (err: any) {
      toast.error(err?.response?.data?.error || "轮换 Token 失败");
    } finally {
      setIsRotating(false);
    }
  };

  const handleRevokeToken = async () => {
    if (
      !window.confirm(
        "确定要吊销当前 MCP Token 并关闭网关吗？所有已连接的 AI 客户端将立即断开。"
      )
    ) {
      return;
    }
    try {
      const res = await api.post("/mcp/token/revoke");
      const data = res.data?.data || res.data;
      if (data) {
        setSettings(data);
        setFreshToken("");
        setShowToken(false);
      }
      toast.success("MCP Token 已吊销，网关已自动关闭");
    } catch {
      toast.error("吊销 Token 失败");
    }
  };

  const handleClearAliases = async () => {
    if (
      !window.confirm(
        "确定要重置全部隐私混淆代号映射吗？重置后下次 AI 查询将重新分配代号（如 bm-01）。"
      )
    ) {
      return;
    }
    try {
      await api.delete("/mcp/aliases");
      setAliases([]);
      toast.success("隐私混淆映射表已清空");
    } catch {
      toast.error("清空混淆映射表失败");
    }
  };

  const handleClearAudit = async () => {
    if (!window.confirm("确定要清空全部 MCP AI 操作审计日志吗？")) {
      return;
    }
    try {
      await api.delete("/mcp/audit");
      setAuditLogs([]);
      toast.success("MCP 审计日志已清空");
    } catch {
      toast.error("清空审计日志失败");
    }
  };

  const copyToClipboard = (text: string, label: string) => {
    if (!text) {
      toast.error("当前无可复制的明文内容，请先点击「生成 / 轮换 Token」");
      return;
    }
    navigator.clipboard.writeText(text);
    toast.success(`${label} 已复制到剪贴板`);
  };

  const toggleAccountWhitelist = (accountId: string) => {
    const exists = settings.allowedAccountIds.includes(accountId);
    const updated = exists
      ? settings.allowedAccountIds.filter((id) => id !== accountId)
      : [...settings.allowedAccountIds, accountId];
    setSettings((prev) => ({
      ...prev,
      allowedAccountIds: updated,
    }));
  };

  const filteredAliases = useMemo(() => {
    return aliases.filter((item) => {
      if (
        aliasCategoryFilter !== "all" &&
        item.category !== aliasCategoryFilter
      ) {
        return false;
      }
      if (!aliasSearch.trim()) return true;
      const q = aliasSearch.trim().toLowerCase();
      return (
        item.alias.toLowerCase().includes(q) ||
        (item.label || "").toLowerCase().includes(q) ||
        (item.hint || "").toLowerCase().includes(q) ||
        item.realValue.toLowerCase().includes(q)
      );
    });
  }, [aliases, aliasCategoryFilter, aliasSearch]);

  const tokenDisplay = freshToken || "<YOUR_MCP_TOKEN>";

  const snippetContent = useMemo(() => {
    if (clientSnippetTab === "claude-cli") {
      return `claude mcp add --transport http ovh-nexus "${mcpEndpointUrl}" \\
  --header "Authorization: Bearer ${tokenDisplay}"`;
    }
    if (clientSnippetTab === "claude-desktop") {
      const serverEntry = {
        url: mcpEndpointUrl,
        headers: {
          Authorization: `Bearer ${tokenDisplay}`,
        },
      };
      if (jsonSnippetMode === "entry-only") {
        return `"ovh-nexus": ${JSON.stringify(serverEntry, null, 2)}`;
      }
      return JSON.stringify(
        {
          mcpServers: {
            "ovh-nexus": serverEntry,
          },
        },
        null,
        2
      );
    }
    if (clientSnippetTab === "antigravity") {
      const serverEntry = {
        serverUrl: mcpEndpointUrl,
        headers: {
          Authorization: `Bearer ${tokenDisplay}`,
        },
      };
      if (jsonSnippetMode === "entry-only") {
        return `"ovh-nexus": ${JSON.stringify(serverEntry, null, 2)}`;
      }
      return JSON.stringify(
        {
          mcpServers: {
            "ovh-nexus": serverEntry,
          },
        },
        null,
        2
      );
    }
    return `curl -sS -X POST "${mcpEndpointUrl}" \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer ${tokenDisplay}" \\
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'`;
  }, [clientSnippetTab, jsonSnippetMode, mcpEndpointUrl, tokenDisplay]);

  return (
    <div className="space-y-6 pb-10">
      {/* Page Header */}
      <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4">
        <div>
          <div className="flex items-center gap-2.5">
            <div className="p-2 rounded-lg bg-cyber-accent/10 border border-cyber-accent/30 text-cyber-accent">
              <Bot size={22} />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h1 className="text-2xl font-bold tracking-tight text-cyber-text">
                  MCP AI 网关与隐私沙盒
                </h1>
                <span className="text-[11px] font-semibold px-2 py-0.5 rounded-full bg-cyber-accent/10 text-cyber-accent border border-cyber-accent/25">
                  Streamable HTTP · JSON-RPC 2.0
                </span>
              </div>
              <p className="text-xs text-cyber-muted mt-0.5">
                参考 OVHcloud CLI 交互范式，内置双向资产脱敏混淆引擎 (`bm-xx` / `vps-xx` / `ord-xx`) 与“只允许下单、硬性禁止自动付款”财务安全红线
              </p>
            </div>
          </div>
        </div>

        {/* Master Switch & Save */}
        <div className="flex items-center gap-3">
          <div
            className={`flex items-center gap-2.5 px-3.5 py-2 rounded-lg border transition-colors ${
              settings.enabled
                ? "bg-emerald-500/10 border-emerald-500/30 text-emerald-500"
                : "bg-cyber-surface border-cyber-grid text-cyber-muted"
            }`}
          >
            <span
              className={`w-2.5 h-2.5 rounded-full ${
                settings.enabled ? "bg-emerald-500 animate-pulse" : "bg-cyber-muted"
              }`}
            />
            <span className="text-xs font-semibold">
              {settings.enabled ? "MCP 网关已开启" : "MCP 网关已关闭（默认安全态）"}
            </span>
            <button
              type="button"
              onClick={() => {
                const nextEnabled = !settings.enabled;
                setSettings((prev) => ({ ...prev, enabled: nextEnabled }));
                handleSaveSettings({ enabled: nextEnabled });
              }}
              disabled={isSaving || isLoading}
              className={`ml-1 px-2.5 py-1 rounded text-xs font-semibold transition-colors ${
                settings.enabled
                  ? "bg-red-500/15 text-red-400 hover:bg-red-500/25 border border-red-500/30"
                  : "bg-cyber-accent text-white hover:bg-cyber-accent/90"
              }`}
            >
              {settings.enabled ? "立即停用" : "启用网关"}
            </button>
          </div>

          <button
            type="button"
            onClick={() => handleSaveSettings()}
            disabled={isSaving || isLoading}
            className="cyber-button flex items-center gap-1.5 px-4 py-2 text-xs font-semibold"
          >
            <Save size={14} />
            <span>{isSaving ? "保存中..." : "保存配置"}</span>
          </button>
        </div>
      </div>

      {/* Navigation Tabs */}
      <div className="flex flex-wrap items-center gap-2 border-b border-cyber-grid pb-2">
        {[
          {
            id: "config" as const,
            label: "网关鉴权与权限沙盒",
            icon: Shield,
          },
          {
            id: "aliases" as const,
            label: `隐私混淆映射表 (${aliases.length})`,
            icon: Layers,
          },
          {
            id: "audit" as const,
            label: `AI 操作审计日志 (${auditLogs.length})`,
            icon: Activity,
          },
          {
            id: "tools" as const,
            label: `MCP 工具目录 (${TOOL_CATALOG.length})`,
            icon: Terminal,
          },
        ].map((tab) => {
          const Icon = tab.icon;
          const active = activeTab === tab.id;
          return (
            <button
              key={tab.id}
              type="button"
              onClick={() => setActiveTab(tab.id)}
              className={`flex items-center gap-2 px-3.5 py-2 rounded-md text-xs font-semibold transition-colors ${
                active
                  ? "bg-cyber-accent text-white shadow-sm"
                  : "bg-cyber-surface text-cyber-muted hover:text-cyber-text border border-cyber-grid"
              }`}
            >
              <Icon size={14} />
              <span>{tab.label}</span>
            </button>
          );
        })}
      </div>

      {/* TAB 1: CONFIG & SANDBOX */}
      {activeTab === "config" && (
        <motion.div
          initial={{ opacity: 0, y: 6 }}
          animate={{ opacity: 1, y: 0 }}
          className="grid grid-cols-1 lg:grid-cols-12 gap-6"
        >
          {/* Left Column: Token + Permission Tiers */}
          <div className="lg:col-span-7 space-y-6">
            {/* Endpoint & Dedicated Token Card */}
            <div className="cyber-panel p-5 space-y-4">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <KeyRound size={18} className="text-cyber-accent" />
                  <h2 className="text-base font-bold text-cyber-text">
                    独立 MCP Bearer Token 与接入端点
                  </h2>
                </div>
                <span className="text-[11px] px-2 py-0.5 rounded bg-cyber-grid/60 text-cyber-muted">
                  与 WebUI 登录密钥完全隔离
                </span>
              </div>

              <p className="text-xs text-cyber-muted leading-relaxed">
                MCP 网关采用独立的随机高熵令牌（前缀{" "}
                <code className="text-cyber-accent">nxs_mcp_</code>
                ）进行身份验证。即使将该令牌配置给外部 AI 客户端，对方也无法登录 WebUI 控制台或读取原始 OVH API 密钥。
              </p>

              {/* Endpoint URL */}
              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-cyber-muted">
                  Streamable HTTP 端点地址 (Endpoint URL)
                </label>
                <div className="flex items-center gap-2">
                  <input
                    type="text"
                    readOnly
                    value={mcpEndpointUrl}
                    className="cyber-input flex-1 font-mono text-xs bg-cyber-bg/70"
                  />
                  <button
                    type="button"
                    onClick={() => copyToClipboard(mcpEndpointUrl, "MCP 端点地址")}
                    className="px-3 py-2 rounded border border-cyber-grid bg-cyber-surface hover:bg-cyber-grid/50 text-xs font-medium flex items-center gap-1.5"
                  >
                    <Copy size={13} />
                    <span>复制地址</span>
                  </button>
                </div>
              </div>

              {/* Bearer Token */}
              <div className="space-y-1.5">
                <div className="flex items-center justify-between">
                  <label className="text-xs font-semibold text-cyber-muted">
                    MCP 专属访问令牌 (Bearer Token)
                  </label>
                  {settings.tokenRotatedAt && (
                    <span className="text-[11px] text-cyber-muted">
                      最近签发: {new Date(settings.tokenRotatedAt).toLocaleString()}
                    </span>
                  )}
                </div>

                <div className="flex flex-wrap items-center gap-2">
                  <div className="relative flex-1 min-w-[240px]">
                    <input
                      type={showToken && freshToken ? "text" : "password"}
                      readOnly
                      value={
                        freshToken
                          ? freshToken
                          : settings.hasToken
                          ? settings.maskedToken
                          : "尚未生成 Token — 请点击右侧「生成 / 轮换 Token」"
                      }
                      className="cyber-input w-full pr-9 font-mono text-xs bg-cyber-bg/70"
                    />
                    {freshToken && (
                      <button
                        type="button"
                        onClick={() => setShowToken((v) => !v)}
                        className="absolute right-2.5 top-1/2 -translate-y-1/2 text-cyber-muted hover:text-cyber-text"
                        title={showToken ? "隐藏 Token" : "显示 Token"}
                      >
                        {showToken ? <EyeOff size={14} /> : <Eye size={14} />}
                      </button>
                    )}
                  </div>

                  {freshToken && (
                    <button
                      type="button"
                      onClick={() =>
                        copyToClipboard(freshToken, "MCP Bearer Token")
                      }
                      className="px-3 py-2 rounded border border-cyber-grid bg-cyber-surface hover:bg-cyber-grid/50 text-xs font-medium flex items-center gap-1.5"
                    >
                      <Copy size={13} />
                      <span>复制令牌</span>
                    </button>
                  )}

                  <button
                    type="button"
                    disabled={isRotating}
                    onClick={handleRotateToken}
                    className="px-3 py-2 rounded bg-cyber-accent/15 text-cyber-accent border border-cyber-accent/30 hover:bg-cyber-accent/25 text-xs font-semibold flex items-center gap-1.5"
                  >
                    <RefreshCw
                      size={13}
                      className={isRotating ? "animate-spin" : ""}
                    />
                    <span>
                      {settings.hasToken ? "轮换 Token" : "生成 Token"}
                    </span>
                  </button>

                  {settings.hasToken && (
                    <button
                      type="button"
                      onClick={handleRevokeToken}
                      className="px-3 py-2 rounded bg-red-500/10 text-red-400 border border-red-500/25 hover:bg-red-500/20 text-xs font-medium flex items-center gap-1.5"
                    >
                      <Trash2 size={13} />
                      <span>吊销</span>
                    </button>
                  )}
                </div>
                {settings.hasToken && !freshToken && (
                  <p className="text-[11px] text-cyber-muted">
                    出于安全考虑，已有 Token 仅展示掩码摘要（{settings.maskedToken}）。如需配置新客户端，请点击「轮换 Token」获取新明文令牌。
                  </p>
                )}
              </div>
            </div>

            {/* L1 - L4 Permission Tiers Card */}
            <div className="cyber-panel p-5 space-y-4">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <ShieldCheck size={18} className="text-cyber-accent" />
                  <h2 className="text-base font-bold text-cyber-text">
                    四级权限沙盒与财务安全红线 (L1 – L4)
                  </h2>
                </div>
                <span className="text-[11px] font-semibold px-2 py-0.5 rounded bg-emerald-500/10 text-emerald-500 border border-emerald-500/25">
                  均衡隐私模式 (Balanced Shield)
                </span>
              </div>

              <div className="space-y-3">
                {/* L1 Read-Only */}
                <div className="p-3.5 rounded-lg border border-cyber-grid bg-cyber-bg/40 flex items-start justify-between gap-4">
                  <div className="space-y-1">
                    <div className="flex items-center gap-2">
                      <span className="text-xs font-bold px-2 py-0.5 rounded bg-sky-500/15 text-sky-400 border border-sky-500/30">
                        L1 只读查询
                      </span>
                      <span className="text-sm font-semibold text-cyber-text">
                        资产清点、机房库存、SKU 动态与订单进度 (11 个工具)
                      </span>
                    </div>
                    <p className="text-xs text-cyber-muted leading-relaxed">
                      允许 AI 查看脱敏后的独服 (`bm-xx`)、VPS (`vps-xx`)、服务到期日、公开机房实时库存以及订单交付进度。所有真实主机名、完整 IP 与付款链接均被自动剥离。
                    </p>
                  </div>
                  <input
                    type="checkbox"
                    checked={settings.scopes.read}
                    onChange={(e) =>
                      setSettings((prev) => ({
                        ...prev,
                        scopes: { ...prev.scopes, read: e.target.checked },
                      }))
                    }
                    className="mt-1 h-4 w-4 accent-cyber-accent cursor-pointer"
                  />
                </div>

                {/* L2 Procurement (Unpaid Only) */}
                <div className="p-3.5 rounded-lg border border-amber-500/30 bg-amber-500/5 space-y-3">
                  <div className="flex items-start justify-between gap-4">
                    <div className="space-y-1">
                      <div className="flex items-center gap-2">
                        <span className="text-xs font-bold px-2 py-0.5 rounded bg-amber-500/20 text-amber-400 border border-amber-500/30">
                          L2 免付采购
                        </span>
                        <span className="text-sm font-semibold text-cyber-text">
                          两步确认生成【未付款订单】与管理抢购队列 (5 个工具)
                        </span>
                      </div>
                      <p className="text-xs text-cyber-muted leading-relaxed">
                        AI 必须先调用 <code className="text-amber-400">order_quote</code>{" "}
                        获取含税报价与 5 分钟有效期的{" "}
                        <code className="text-amber-400">quote_id</code>，经您确认后才能调用{" "}
                        <code className="text-amber-400">order_create_unpaid</code>{" "}
                        生成待支付订单。底层代码硬性锁死{" "}
                        <code className="text-emerald-400 font-semibold">
                          auto_pay = False
                        </code>{" "}
                        且单次数量固定为 <code className="text-emerald-400">1</code>。
                      </p>
                    </div>
                    <input
                      type="checkbox"
                      checked={settings.scopes.procurement}
                      onChange={(e) =>
                        setSettings((prev) => ({
                          ...prev,
                          scopes: {
                            ...prev.scopes,
                            procurement: e.target.checked,
                          },
                        }))
                      }
                      className="mt-1 h-4 w-4 accent-cyber-accent cursor-pointer"
                    />
                  </div>

                  {/* L2 Rate Limit & Notification */}
                  <div className="pt-2 border-t border-amber-500/20 flex flex-wrap items-center justify-between gap-4 text-xs">
                    <div className="flex items-center gap-2">
                      <span className="text-cyber-muted font-medium">
                        每小时最多允许生成未付款订单数:
                      </span>
                      <input
                        type="number"
                        min={1}
                        max={20}
                        value={settings.hourlyOrderLimit}
                        onChange={(e) =>
                          setSettings((prev) => ({
                            ...prev,
                            hourlyOrderLimit: Math.max(
                              1,
                              Math.min(20, Number(e.target.value) || 5)
                            ),
                          }))
                        }
                        className="cyber-input w-20 py-1 px-2 text-center font-mono text-xs"
                      />
                      <span className="text-cyber-muted">单 / 小时</span>
                    </div>

                    <label className="flex items-center gap-2 cursor-pointer select-none">
                      <input
                        type="checkbox"
                        checked={settings.notifyOnMutation}
                        onChange={(e) =>
                          setSettings((prev) => ({
                            ...prev,
                            notifyOnMutation: e.target.checked,
                          }))
                        }
                        className="h-3.5 w-3.5 accent-cyber-accent"
                      />
                      <span className="text-cyber-text font-medium">
                        L2/L3 写操作实时推送通知告警 (`mcp.action`)
                      </span>
                    </label>
                  </div>
                </div>

                {/* L3 Reversible Operations */}
                <div className="p-3.5 rounded-lg border border-cyber-grid bg-cyber-bg/40 flex items-start justify-between gap-4">
                  <div className="space-y-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-xs font-bold px-2 py-0.5 rounded bg-purple-500/15 text-purple-400 border border-purple-500/30">
                        L3 可逆运维
                      </span>
                      <span className="text-sm font-semibold text-cyber-text">
                        独服/VPS 重启、IPMI 会话重置与边缘防火墙管理 (4 个工具)
                      </span>
                      {settings.operationsLockedByTotp && (
                        <span className="text-[11px] font-semibold px-2 py-0.5 rounded bg-amber-500/15 text-amber-400 border border-amber-500/30 flex items-center gap-1">
                          <Lock size={11} />
                          需先在系统设置启用 2FA
                        </span>
                      )}
                    </div>
                    <p className="text-xs text-cyber-muted leading-relaxed">
                      允许 AI 通过混淆代号 (`bm-xx` / `vps-xx`) 执行不破坏磁盘数据的可逆运维操作。
                      {settings.operationsLockedByTotp && (
                        <span className="text-amber-400 ml-1">
                          （当前面板尚未绑定 2FA 双因素认证，L3 运维层已被底层强制锁定关闭）
                        </span>
                      )}
                    </p>
                  </div>
                  <input
                    type="checkbox"
                    disabled={!!settings.operationsLockedByTotp}
                    checked={
                      settings.operationsLockedByTotp
                        ? false
                        : settings.scopes.operations
                    }
                    onChange={(e) =>
                      setSettings((prev) => ({
                        ...prev,
                        scopes: {
                          ...prev.scopes,
                          operations: e.target.checked,
                        },
                      }))
                    }
                    className="mt-1 h-4 w-4 accent-cyber-accent cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed"
                  />
                </div>

                {/* L4 Permanently Blocked Redlines */}
                <div className="p-3.5 rounded-lg border border-red-500/35 bg-red-500/5 space-y-2">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <span className="text-xs font-bold px-2 py-0.5 rounded bg-red-500/20 text-red-400 border border-red-500/35 flex items-center gap-1">
                        <Lock size={11} />
                        L4 永久熔断红线
                      </span>
                      <span className="text-sm font-semibold text-red-400">
                        代码级硬性拦截（不提供任何开启开关）
                      </span>
                    </div>
                    <span className="text-[11px] font-mono text-red-400/90">
                      HARDCODED BLOCK
                    </span>
                  </div>
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-1.5 text-xs text-cyber-muted pt-1">
                    <div className="flex items-center gap-1.5">
                      <XCircle size={13} className="text-red-400 shrink-0" />
                      <span>
                        禁止自动扣款结账 (<code>autoPay=False</code> 锁死)
                      </span>
                    </div>
                    <div className="flex items-center gap-1.5">
                      <XCircle size={13} className="text-red-400 shrink-0" />
                      <span>禁止重装操作系统 / 擦除磁盘分区</span>
                    </div>
                    <div className="flex items-center gap-1.5">
                      <XCircle size={13} className="text-red-400 shrink-0" />
                      <span>禁止退订销毁服务或修改续费策略</span>
                    </div>
                    <div className="flex items-center gap-1.5">
                      <XCircle size={13} className="text-red-400 shrink-0" />
                      <span>禁止读取 KVM 远程控制链接与 SSH/API 密钥</span>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>

          {/* Right Column: Multi-Account Whitelist & Client Snippets */}
          <div className="lg:col-span-5 space-y-6">
            {/* Multi-Account Visibility Whitelist */}
            <div className="cyber-panel p-5 space-y-4">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Users size={18} className="text-cyber-accent" />
                  <h2 className="text-base font-bold text-cyber-text">
                    多账户可见性白名单
                  </h2>
                </div>
                <label className="flex items-center gap-2 text-xs cursor-pointer select-none">
                  <input
                    type="checkbox"
                    checked={settings.allowAllAccounts}
                    onChange={(e) =>
                      setSettings((prev) => ({
                        ...prev,
                        allowAllAccounts: e.target.checked,
                      }))
                    }
                    className="h-3.5 w-3.5 accent-cyber-accent"
                  />
                  <span className="text-cyber-text font-medium">
                    允许访问全部已配置账户
                  </span>
                </label>
              </div>

              <p className="text-xs text-cyber-muted leading-relaxed">
                指定允许暴露给 MCP 网关的 OVH 账户。未勾选的账户对 AI 完全隐身，其资产不会出现在任何查询结果中。
              </p>

              {accounts.length === 0 ? (
                <div className="p-3 rounded border border-cyber-grid bg-cyber-bg/40 text-xs text-cyber-muted">
                  当前仅使用默认环境变量/基础设置账户，无需单独配置多账户白名单。
                </div>
              ) : (
                <div className="space-y-2 max-h-52 overflow-y-auto pr-1">
                  {accounts.map((acc: any, idx: number) => {
                    const checked =
                      settings.allowAllAccounts ||
                      settings.allowedAccountIds.includes(acc.id);
                    return (
                      <label
                        key={acc.id}
                        className={`flex items-center justify-between p-2.5 rounded-md border text-xs transition-colors cursor-pointer ${
                          checked
                            ? "border-cyber-accent/40 bg-cyber-accent/5"
                            : "border-cyber-grid bg-cyber-bg/30 opacity-60"
                        }`}
                      >
                        <div className="flex items-center gap-2.5">
                          <input
                            type="checkbox"
                            disabled={settings.allowAllAccounts}
                            checked={checked}
                            onChange={() => toggleAccountWhitelist(acc.id)}
                            className="h-3.5 w-3.5 accent-cyber-accent"
                          />
                          <div>
                            <div className="font-semibold text-cyber-text flex items-center gap-1.5">
                              <span>{acc.alias || acc.name || acc.id}</span>
                              <span className="text-[10px] font-mono px-1.5 py-0.2 rounded bg-cyber-grid/60 text-cyber-muted">
                                acct-{String(idx + 1).padStart(2, "0")}
                              </span>
                            </div>
                            <div className="text-[11px] text-cyber-muted">
                              节点: {acc.endpoint || "ovh-eu"} · 区域:{" "}
                              {acc.zone || "IE"}
                            </div>
                          </div>
                        </div>
                        <span className="text-[11px] font-medium text-cyber-accent">
                          {checked ? "AI 可见" : "已隔离"}
                        </span>
                      </label>
                    );
                  })}
                </div>
              )}
            </div>

            {/* Client Configuration Generator */}
            <div className="cyber-panel p-5 space-y-4">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Code2 size={18} className="text-cyber-accent" />
                  <h2 className="text-base font-bold text-cyber-text">
                    一键生成 AI 客户端接入配置
                  </h2>
                </div>
                <Sparkles size={15} className="text-cyber-accent" />
              </div>

              <div className="flex flex-wrap gap-1.5">
                {[
                  { id: "claude-cli" as const, label: "Claude Code CLI" },
                  {
                    id: "claude-desktop" as const,
                    label: "Claude Desktop / Cursor",
                  },
                  {
                    id: "antigravity" as const,
                    label: "Antigravity / Windsurf",
                  },
                  { id: "curl" as const, label: "cURL 连通性测试" },
                ].map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => setClientSnippetTab(item.id)}
                    className={`px-2.5 py-1 rounded text-xs font-medium transition-colors ${
                      clientSnippetTab === item.id
                        ? "bg-cyber-accent text-white"
                        : "bg-cyber-bg/60 text-cyber-muted hover:text-cyber-text border border-cyber-grid"
                    }`}
                  >
                    {item.label}
                  </button>
                ))}
              </div>

              {(clientSnippetTab === "claude-desktop" ||
                clientSnippetTab === "antigravity") && (
                <div className="flex flex-wrap items-center gap-1.5 p-1 rounded-md bg-cyber-bg/60 border border-cyber-grid">
                  <button
                    type="button"
                    onClick={() => setJsonSnippetMode("entry-only")}
                    className={`flex-1 px-2.5 py-1 rounded text-[11px] font-medium transition-colors ${
                      jsonSnippetMode === "entry-only"
                        ? "bg-cyber-accent/20 text-cyber-accent border border-cyber-accent/40"
                        : "text-cyber-muted hover:text-cyber-text"
                    }`}
                  >
                    {t("mcp.snippet.modeEntryOnly")}
                  </button>
                  <button
                    type="button"
                    onClick={() => setJsonSnippetMode("full")}
                    className={`px-2.5 py-1 rounded text-[11px] font-medium transition-colors ${
                      jsonSnippetMode === "full"
                        ? "bg-cyber-accent/20 text-cyber-accent border border-cyber-accent/40"
                        : "text-cyber-muted hover:text-cyber-text"
                    }`}
                  >
                    {t("mcp.snippet.modeFullFile")}
                  </button>
                </div>
              )}

              <div className="relative">
                <pre className="p-3.5 rounded-lg bg-cyber-bg border border-cyber-grid font-mono text-[11px] text-cyber-text overflow-x-auto leading-relaxed">
                  {snippetContent}
                </pre>
                <button
                  type="button"
                  onClick={() =>
                    copyToClipboard(snippetContent, "客户端接入配置")
                  }
                  className="absolute top-2.5 right-2.5 px-2.5 py-1 rounded bg-cyber-surface/90 hover:bg-cyber-grid border border-cyber-grid text-[11px] font-medium flex items-center gap-1"
                >
                  <Copy size={12} />
                  <span>复制代码</span>
                </button>
              </div>

              {(clientSnippetTab === "claude-desktop" ||
                clientSnippetTab === "antigravity") && (
                <div className="p-2.5 rounded-lg bg-amber-500/10 border border-amber-500/30 text-[11px] text-amber-300 leading-relaxed flex items-start gap-2">
                  <AlertTriangle
                    size={14}
                    className="text-amber-400 shrink-0 mt-0.5"
                  />
                  <span>{t("mcp.snippet.mergeHint")}</span>
                </div>
              )}

              <div className="p-3 rounded-lg bg-cyber-accent/5 border border-cyber-accent/20 text-[11px] text-cyber-muted space-y-1">
                <div className="font-semibold text-cyber-text">
                  推荐 AI 提问示例（对齐 OVHcloud CLI 体验）：
                </div>
                <div>
                  • “帮我列出名下所有独服和 VPS 的到期时间与运行状态”
                </div>
                <div>
                  • “查询 <code>24ska01</code> 在全球机房的实时库存，如果有货帮我先试算一份报价单”
                </div>
                <div>
                  • “凭刚才的报价单帮我生成未付款订单，我去面板手动付款”
                </div>
              </div>
            </div>
          </div>
        </motion.div>
      )}

      {/* TAB 2: PRIVACY ALIAS REFERENCE TABLE */}
      {activeTab === "aliases" && (
        <motion.div
          initial={{ opacity: 0, y: 6 }}
          animate={{ opacity: 1, y: 0 }}
          className="cyber-panel p-5 space-y-4"
        >
          <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-3">
            <div>
              <h2 className="text-base font-bold text-cyber-text">
                双向隐私混淆代号映射表 (`mcp_aliases.json`)
              </h2>
              <p className="text-xs text-cyber-muted mt-0.5">
                AI 仅能看到左侧的混淆代号（如 <code>bm-01</code>、<code>ord-01</code>）与脱敏提示（如掩码 IP 尾号 <code>*.*.*.42</code>）。当 AI 传入代号调用工具时，网关在后端自动还原为真实 OVH 标识符。
              </p>
            </div>

            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={fetchAliases}
                disabled={isLoadingAliases}
                className="px-3 py-1.5 rounded border border-cyber-grid bg-cyber-surface hover:bg-cyber-grid/50 text-xs font-medium flex items-center gap-1.5"
              >
                <RefreshCw
                  size={13}
                  className={isLoadingAliases ? "animate-spin" : ""}
                />
                <span>刷新映射</span>
              </button>
              {aliases.length > 0 && (
                <button
                  type="button"
                  onClick={handleClearAliases}
                  className="px-3 py-1.5 rounded border border-red-500/30 bg-red-500/10 text-red-400 hover:bg-red-500/20 text-xs font-medium flex items-center gap-1.5"
                >
                  <Trash2 size={13} />
                  <span>重置代号</span>
                </button>
              )}
            </div>
          </div>

          {/* Filter & Search Bar */}
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex flex-wrap items-center gap-1.5">
              {[
                { id: "all", label: "全部类型" },
                { id: "account", label: "账户 (acct-xx)" },
                { id: "baremetal", label: "独服 (bm-xx)" },
                { id: "vps", label: "VPS (vps-xx)" },
                { id: "ip", label: "公网 IP (ip-xx)" },
                { id: "order", label: "订单 (ord-xx)" },
              ].map((cat) => (
                <button
                  key={cat.id}
                  type="button"
                  onClick={() => setAliasCategoryFilter(cat.id)}
                  className={`px-2.5 py-1 rounded text-xs font-medium transition-colors ${
                    aliasCategoryFilter === cat.id
                      ? "bg-cyber-accent text-white"
                      : "bg-cyber-bg/60 text-cyber-muted hover:text-cyber-text border border-cyber-grid"
                  }`}
                >
                  {cat.label}
                </button>
              ))}
            </div>

            <div className="relative min-w-[240px]">
              <Search
                size={14}
                className="absolute left-3 top-1/2 -translate-y-1/2 text-cyber-muted"
              />
              <input
                type="text"
                value={aliasSearch}
                onChange={(e) => setAliasSearch(e.target.value)}
                placeholder="搜索混淆代号、备注或真实标识..."
                className="cyber-input w-full pl-8 py-1.5 text-xs"
              />
            </div>
          </div>

          {/* Table */}
          {filteredAliases.length === 0 ? (
            <div className="py-12 text-center text-xs text-cyber-muted border border-dashed border-cyber-grid rounded-lg">
              暂无隐私混淆映射记录。当 AI 客户端首次调用查询工具时，系统会自动分配并持久化代号映射。
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-left border-collapse text-xs">
                <thead>
                  <tr className="border-b border-cyber-grid text-cyber-muted">
                    <th className="py-2.5 px-3 font-semibold">AI 混淆代号</th>
                    <th className="py-2.5 px-3 font-semibold">资产类型</th>
                    <th className="py-2.5 px-3 font-semibold">
                      AI 可见安全摘要 (Balanced Hint)
                    </th>
                    <th className="py-2.5 px-3 font-semibold">
                      真实 OVH 标识 (仅本地可见)
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-cyber-grid/60">
                  {filteredAliases.map((item) => {
                    const catMeta = CATEGORY_LABELS[item.category] || {
                      label: item.category,
                      color: "bg-cyber-grid text-cyber-text",
                    };
                    const isRevealed = Boolean(revealedAliases[item.alias]);
                    const safeDisplay = [item.label, item.hint]
                      .filter(Boolean)
                      .join(" · ");
                    return (
                      <tr
                        key={item.alias}
                        className="hover:bg-cyber-grid/20 transition-colors"
                      >
                        <td className="py-2.5 px-3 font-mono font-bold text-cyber-accent">
                          {item.alias}
                        </td>
                        <td className="py-2.5 px-3">
                          <span
                            className={`inline-block text-[11px] font-semibold px-2 py-0.5 rounded border ${catMeta.color}`}
                          >
                            {catMeta.label}
                          </span>
                        </td>
                        <td className="py-2.5 px-3 font-mono text-cyber-text">
                          {safeDisplay || "—"}
                        </td>
                        <td className="py-2.5 px-3">
                          <div className="flex items-center gap-2 font-mono">
                            <span
                              className={
                                isRevealed
                                  ? "text-cyber-text"
                                  : "text-cyber-muted select-none"
                              }
                            >
                              {isRevealed
                                ? item.realValue
                                : item.maskedReal || "••••••••••••••••"}
                            </span>
                            <button
                              type="button"
                              onClick={() =>
                                setRevealedAliases((prev) => ({
                                  ...prev,
                                  [item.alias]: !prev[item.alias],
                                }))
                              }
                              className="text-cyber-muted hover:text-cyber-text"
                              title={isRevealed ? "隐藏真实值" : "显示真实值"}
                            >
                              {isRevealed ? (
                                <EyeOff size={13} />
                              ) : (
                                <Eye size={13} />
                              )}
                            </button>
                            <button
                              type="button"
                              onClick={() =>
                                copyToClipboard(
                                  item.realValue,
                                  `${item.alias} 真实标识`
                                )
                              }
                              className="text-cyber-muted hover:text-cyber-text"
                              title="复制真实标识"
                            >
                              <Copy size={13} />
                            </button>
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </motion.div>
      )}

      {/* TAB 3: MCP AUDIT LOGS */}
      {activeTab === "audit" && (
        <motion.div
          initial={{ opacity: 0, y: 6 }}
          animate={{ opacity: 1, y: 0 }}
          className="cyber-panel p-5 space-y-4"
        >
          <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-3">
            <div>
              <h2 className="text-base font-bold text-cyber-text">
                MCP AI 操作审计日志 (`mcp_audit.json`)
              </h2>
              <p className="text-xs text-cyber-muted mt-0.5">
                记录每一次外部 AI 工具调用、权限拦截与未付款订单生成记录。L2 采购与 L3 运维操作同时会向「通知中心」广播 <code>mcp.action</code> 事件。
              </p>
            </div>

            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={fetchAuditLogs}
                disabled={isLoadingAudit}
                className="px-3 py-1.5 rounded border border-cyber-grid bg-cyber-surface hover:bg-cyber-grid/50 text-xs font-medium flex items-center gap-1.5"
              >
                <RefreshCw
                  size={13}
                  className={isLoadingAudit ? "animate-spin" : ""}
                />
                <span>刷新日志</span>
              </button>
              {auditLogs.length > 0 && (
                <button
                  type="button"
                  onClick={handleClearAudit}
                  className="px-3 py-1.5 rounded border border-red-500/30 bg-red-500/10 text-red-400 hover:bg-red-500/20 text-xs font-medium flex items-center gap-1.5"
                >
                  <Trash2 size={13} />
                  <span>清空记录</span>
                </button>
              )}
            </div>
          </div>

          {auditLogs.length === 0 ? (
            <div className="py-12 text-center text-xs text-cyber-muted border border-dashed border-cyber-grid rounded-lg">
              暂无 MCP 调用审计记录。
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-left border-collapse text-xs">
                <thead>
                  <tr className="border-b border-cyber-grid text-cyber-muted">
                    <th className="py-2.5 px-3 font-semibold">时间</th>
                    <th className="py-2.5 px-3 font-semibold">工具名称</th>
                    <th className="py-2.5 px-3 font-semibold">权限级别</th>
                    <th className="py-2.5 px-3 font-semibold">状态</th>
                    <th className="py-2.5 px-3 font-semibold">入参摘要 / 结果</th>
                    <th className="py-2.5 px-3 font-semibold text-right">
                      来源 IP
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-cyber-grid/60">
                  {auditLogs.map((log) => {
                    const isAllowed =
                      log.status === "success" || log.status === "allowed";
                    const isBlocked = log.status === "blocked";
                    return (
                      <tr
                        key={log.id}
                        className="hover:bg-cyber-grid/20 transition-colors"
                      >
                        <td className="py-2.5 px-3 whitespace-nowrap text-cyber-muted font-mono">
                          {new Date(log.timestamp).toLocaleString()}
                        </td>
                        <td className="py-2.5 px-3 font-mono font-semibold text-cyber-text">
                          {log.tool}
                        </td>
                        <td className="py-2.5 px-3">
                          <span className="px-2 py-0.5 rounded text-[11px] font-mono bg-cyber-grid/60 text-cyber-text">
                            {log.scope}
                          </span>
                        </td>
                        <td className="py-2.5 px-3">
                          {isAllowed ? (
                            <span className="inline-flex items-center gap-1 text-emerald-500 font-semibold">
                              <CheckCircle2 size={13} />
                              成功
                            </span>
                          ) : isBlocked ? (
                            <span className="inline-flex items-center gap-1 text-amber-400 font-semibold">
                              <ShieldAlert size={13} />
                              拦截
                            </span>
                          ) : (
                            <span className="inline-flex items-center gap-1 text-red-400 font-semibold">
                              <AlertTriangle size={13} />
                              异常
                            </span>
                          )}
                        </td>
                        <td className="py-2.5 px-3 max-w-md">
                          <div className="text-cyber-text truncate">
                            {log.summary || "—"}
                          </div>
                          {log.arguments &&
                            Object.keys(log.arguments).length > 0 && (
                              <div className="text-[11px] font-mono text-cyber-muted truncate">
                                args: {JSON.stringify(log.arguments)}
                              </div>
                            )}
                        </td>
                        <td className="py-2.5 px-3 text-right whitespace-nowrap font-mono text-cyber-muted">
                          {log.clientIp || "local"}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </motion.div>
      )}

      {/* TAB 4: TOOL CATALOG */}
      {activeTab === "tools" && (
        <motion.div
          initial={{ opacity: 0, y: 6 }}
          animate={{ opacity: 1, y: 0 }}
          className="space-y-4"
        >
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {TOOL_CATALOG.map((tool) => {
              const scopeEnabled = settings.scopes[tool.scope];
              const scopeBadge =
                tool.scope === "read"
                  ? {
                      label: "L1 只读",
                      cls: "bg-sky-500/15 text-sky-400 border-sky-500/30",
                    }
                  : tool.scope === "procurement"
                  ? {
                      label: "L2 免付采购",
                      cls: "bg-amber-500/20 text-amber-400 border-amber-500/30",
                    }
                  : {
                      label: "L3 可逆运维",
                      cls: "bg-purple-500/15 text-purple-400 border-purple-500/30",
                    };

              return (
                <div
                  key={tool.name}
                  className={`cyber-panel p-4 space-y-2 border transition-opacity ${
                    scopeEnabled ? "opacity-100" : "opacity-55"
                  }`}
                >
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                      <code className="text-xs font-bold text-cyber-accent bg-cyber-accent/10 px-2 py-0.5 rounded border border-cyber-accent/25">
                        {tool.name}
                      </code>
                      <span
                        className={`text-[10px] font-bold px-2 py-0.5 rounded border ${scopeBadge.cls}`}
                      >
                        {scopeBadge.label}
                      </span>
                    </div>
                    <span
                      className={`text-[11px] font-semibold ${
                        scopeEnabled ? "text-emerald-500" : "text-cyber-muted"
                      }`}
                    >
                      {scopeEnabled ? "● 已向 AI 开放" : "○ 当前权限已关闭"}
                    </span>
                  </div>

                  <div className="text-sm font-semibold text-cyber-text">
                    {tool.title}
                  </div>
                  <p className="text-xs text-cyber-muted leading-relaxed">
                    {tool.desc}
                  </p>
                  <div className="pt-1 flex items-center gap-1.5 text-[11px] font-mono text-cyber-muted">
                    <Terminal size={12} className="text-cyber-accent" />
                    <span>CLI 对标: {tool.cliEquivalent}</span>
                  </div>
                </div>
              );
            })}
          </div>
        </motion.div>
      )}
    </div>
  );
};

export default MCPGatewayPage;
