import { useEffect, useState } from "react";
import { motion } from "framer-motion";
import { Link } from "react-router-dom";
import { useAPI } from "@/context/APIContext";
import { useLocale } from "@/context/LocaleContext";
import { api } from "@/utils/apiClient";
import {
  Terminal,
  ShoppingCart,
  CalendarClock,
  HardDrive,
  Globe,
  KeyRound,
  ArrowUpRight,
  ListChecks,
  Server,
  CheckCircle2,
  Activity,
} from "lucide-react";

interface StatsType {
  activeQueues: number;
  totalServers: number;
  availableServers: number;
  purchaseSuccess: number;
  purchaseFailed: number;
  queueProcessorRunning?: boolean;
  monitorRunning?: boolean;
}

interface QueueItem {
  id: string;
  planCode: string;
  datacenter?: string;
  datacenters?: string[];
  status: string;
  retryCount: number;
  retryInterval: number;
  createdAt: string;
  accountId?: string;
}

const Dashboard = () => {
  const { isAuthenticated, accounts } = useAPI();
  const { t } = useLocale();
  const [stats, setStats] = useState<StatsType>({
    activeQueues: 0,
    totalServers: 0,
    availableServers: 0,
    purchaseSuccess: 0,
    purchaseFailed: 0,
    queueProcessorRunning: true,
    monitorRunning: false,
  });
  const [queueItems, setQueueItems] = useState<QueueItem[]>([]);
  const [isLoading, setIsLoading] = useState(true);

  const getAccountLabel = (id?: string) => {
    if (!id) return "Default";
    const acc = accounts.find((a: any) => a?.id === id);
    return acc?.alias || id;
  };

  useEffect(() => {
    const fetchData = async () => {
      try {
        const [statsResponse, queueResponse] = await Promise.all([
          api.get(`/stats`),
          api.get(`/queue/all`),
        ]);
        setStats(statsResponse.data);
        const activeItems = (queueResponse.data || [])
          .filter((item: QueueItem) => ["running", "pending", "paused"].includes(item.status))
          .slice(0, 4);
        setQueueItems(activeItems);
      } catch (error) {
        console.error("Error fetching dashboard data:", error);
      } finally {
        setIsLoading(false);
      }
    };

    fetchData();
    const interval = setInterval(fetchData, 30000);
    return () => clearInterval(interval);
  }, []);

  const shortcuts = [
    {
      title: t("dashboard.shortcut.dedicated"),
      desc: t("dashboard.shortcut.dedicatedDesc"),
      path: "/server-control",
      icon: Terminal,
    },
    {
      title: t("dashboard.shortcut.orders"),
      desc: t("dashboard.shortcut.ordersDesc"),
      path: "/account-management?tab=orders",
      icon: ShoppingCart,
    },
    {
      title: t("dashboard.shortcut.renewals"),
      desc: t("dashboard.shortcut.renewalsDesc"),
      path: "/account-management?tab=services",
      icon: CalendarClock,
    },
    {
      title: t("dashboard.shortcut.vps"),
      desc: t("dashboard.shortcut.vpsDesc"),
      path: "/account-management?tab=vps",
      icon: HardDrive,
    },
    {
      title: t("dashboard.shortcut.availability"),
      desc: t("dashboard.shortcut.availabilityDesc"),
      path: "/availability",
      icon: Globe,
    },
    {
      title: t("dashboard.shortcut.ssh"),
      desc: t("dashboard.shortcut.sshDesc"),
      path: "/account-management?tab=ssh",
      icon: KeyRound,
    },
  ];

  return (
    <div className="space-y-6">
      <motion.div
        initial={{ opacity: 0, y: -12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.25 }}
      >
        <h1 className="text-2xl sm:text-3xl font-bold mb-1 cyber-glow-text">
          {t("dashboard.title")}
        </h1>
        <p className="text-cyber-muted text-sm">{t("dashboard.subtitle")}</p>
      </motion.div>

      {/* Top KPI Cards */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <div className="cyber-card flex flex-col justify-between">
          <div className="flex justify-between items-start">
            <div>
              <h3 className="text-cyber-muted text-xs font-medium uppercase tracking-wider mb-1">
                {t("dashboard.stat.activeQueues")}
              </h3>
              {isLoading ? (
                <div className="h-8 w-16 bg-cyber-grid animate-pulse rounded" />
              ) : (
                <p className="text-3xl font-bold text-cyber-accent">{stats.activeQueues}</p>
              )}
            </div>
            <div className="p-2.5 bg-cyber-accent/10 border border-cyber-accent/20 rounded-lg text-cyber-accent">
              <ListChecks size={18} />
            </div>
          </div>
          <div className="mt-4 text-xs text-cyber-muted">
            <Link to="/queue" className="inline-flex items-center hover:text-cyber-accent transition-colors">
              {t("nav.queue")} <ArrowUpRight size={13} className="ml-1" />
            </Link>
          </div>
        </div>

        <div className="cyber-card flex flex-col justify-between">
          <div className="flex justify-between items-start">
            <div>
              <h3 className="text-cyber-muted text-xs font-medium uppercase tracking-wider mb-1">
                {t("dashboard.stat.servers")}
              </h3>
              {isLoading ? (
                <div className="h-8 w-16 bg-cyber-grid animate-pulse rounded" />
              ) : (
                <p className="text-3xl font-bold text-cyber-text">{stats.totalServers}</p>
              )}
            </div>
            <div className="p-2.5 bg-cyber-accent/10 border border-cyber-accent/20 rounded-lg text-cyber-accent">
              <Server size={18} />
            </div>
          </div>
          <div className="mt-4 flex items-center justify-between text-xs text-cyber-muted">
            <Link to="/servers" className="inline-flex items-center hover:text-cyber-accent transition-colors">
              {t("nav.servers")} <ArrowUpRight size={13} className="ml-1" />
            </Link>
            <span className="px-2 py-0.5 rounded bg-cyber-dark text-cyber-text border border-cyber-grid">
              Avail: {isLoading ? "-" : stats.availableServers}
            </span>
          </div>
        </div>

        <div className="cyber-card flex flex-col justify-between">
          <div className="flex justify-between items-start">
            <div>
              <h3 className="text-cyber-muted text-xs font-medium uppercase tracking-wider mb-1">
                {t("dashboard.stat.successOrders")}
              </h3>
              {isLoading ? (
                <div className="h-8 w-16 bg-cyber-grid animate-pulse rounded" />
              ) : (
                <p className="text-3xl font-bold text-emerald-600 dark:text-emerald-400">
                  {stats.purchaseSuccess}
                </p>
              )}
            </div>
            <div className="p-2.5 bg-emerald-500/10 border border-emerald-500/20 rounded-lg text-emerald-600 dark:text-emerald-400">
              <CheckCircle2 size={18} />
            </div>
          </div>
          <div className="mt-4 text-xs text-cyber-muted">
            <Link to="/history" className="inline-flex items-center hover:text-cyber-accent transition-colors">
              {t("nav.history")} <ArrowUpRight size={13} className="ml-1" />
            </Link>
          </div>
        </div>

        <div className="cyber-card flex flex-col justify-between">
          <div className="flex justify-between items-start">
            <div>
              <h3 className="text-cyber-muted text-xs font-medium uppercase tracking-wider mb-1">
                {t("dashboard.stat.apiNode")}
              </h3>
              <p
                className={`text-xl font-bold mt-1 ${
                  isAuthenticated
                    ? "text-emerald-600 dark:text-emerald-400"
                    : "text-amber-600 dark:text-amber-400"
                }`}
              >
                {isAuthenticated ? t("sidebar.connected") : t("sidebar.notConfigured")}
              </p>
            </div>
            <div className="p-2.5 bg-cyber-accent/10 border border-cyber-accent/20 rounded-lg text-cyber-accent">
              <Activity size={18} />
            </div>
          </div>
          <div className="mt-4 flex items-center justify-between text-xs text-cyber-muted">
            <span>{t("dashboard.stat.failedOrders", { failed: stats.purchaseFailed })}</span>
            <Link to="/settings" className="hover:text-cyber-accent transition-colors">
              {t("nav.settings")}
            </Link>
          </div>
        </div>
      </div>

      {/* Cloud Fleet & Procurement Shortcuts */}
      <div className="space-y-3">
        <h2 className="text-base font-semibold text-cyber-text">
          {t("dashboard.quickAccess")}
        </h2>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {shortcuts.map((sc) => {
            const Icon = sc.icon;
            return (
              <Link
                key={sc.path}
                to={sc.path}
                className="cyber-card group hover:border-cyber-accent/50 transition-all flex items-start gap-3.5"
              >
                <div className="p-2.5 rounded-lg border shrink-0 text-cyber-accent bg-cyber-accent/10 border-cyber-accent/20">
                  <Icon size={18} />
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center justify-between">
                    <h3 className="text-sm font-semibold text-cyber-text group-hover:text-cyber-accent transition-colors">
                      {sc.title}
                    </h3>
                    <ArrowUpRight
                      size={14}
                      className="text-cyber-muted group-hover:text-cyber-accent transition-colors shrink-0"
                    />
                  </div>
                  <p className="text-xs text-cyber-muted mt-1 line-clamp-2">{sc.desc}</p>
                </div>
              </Link>
            );
          })}
        </div>
      </div>

      {/* Active Queue & System Status */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <div className="cyber-card lg:col-span-2">
          <div className="flex justify-between items-center mb-4">
            <h2 className="text-base font-semibold text-cyber-text">
              {t("dashboard.queueSection")}
            </h2>
            <Link
              to="/queue"
              className="text-cyber-muted text-xs hover:text-cyber-accent transition-colors"
            >
              {t("dashboard.viewAll")}
            </Link>
          </div>

          {isLoading ? (
            <div className="space-y-3">
              {[...Array(3)].map((_, i) => (
                <div key={i} className="h-16 bg-cyber-grid/50 animate-pulse rounded-lg" />
              ))}
            </div>
          ) : queueItems.length === 0 ? (
            <div className="bg-cyber-dark/60 p-8 rounded-lg text-center border border-cyber-grid">
              <p className="text-cyber-muted text-sm mb-4">{t("dashboard.noQueues")}</p>
              <Link
                to="/queue"
                className="cyber-button-primary text-xs inline-flex items-center px-4 py-2 gap-1.5"
              >
                {t("dashboard.newQueue")}
              </Link>
            </div>
          ) : (
            <div className="space-y-2.5">
              {queueItems.map((item) => {
                const dcList =
                  Array.isArray(item.datacenters) && item.datacenters.length > 0
                    ? item.datacenters
                    : item.datacenter
                    ? [item.datacenter]
                    : [];
                return (
                  <div
                    key={item.id}
                    className="p-3.5 bg-cyber-dark/50 rounded-lg border border-cyber-grid hover:border-cyber-accent/40 transition-all flex justify-between items-center"
                  >
                    <div className="min-w-0">
                      <p className="font-semibold text-sm text-cyber-accent truncate">
                        {item.planCode}
                      </p>
                      <div className="flex flex-wrap items-center gap-2 text-xs text-cyber-muted mt-1">
                        <span className="px-2 py-0.5 rounded bg-cyber-surface border border-cyber-grid text-cyber-text">
                          #{item.retryCount + 1}
                        </span>
                        {dcList.length > 0 && (
                          <span className="px-2 py-0.5 rounded bg-cyber-accent/10 text-cyber-accent border border-cyber-accent/25">
                            {dcList.map((dc) => dc.toUpperCase()).join(" > ")}
                          </span>
                        )}
                      </div>
                    </div>
                    <div className="flex items-center gap-2 shrink-0 ml-3">
                      <span className="px-2 py-0.5 text-[11px] bg-cyber-surface border border-cyber-grid text-cyber-text rounded">
                        {getAccountLabel(item.accountId)}
                      </span>
                      <span
                        className={`text-xs px-2.5 py-1 rounded-md font-medium ${
                          item.status === "running"
                            ? "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border border-emerald-500/30"
                            : item.status === "pending"
                            ? "bg-amber-500/15 text-amber-600 dark:text-amber-400 border border-amber-500/30"
                            : "bg-cyber-surface border border-cyber-grid text-cyber-muted"
                        }`}
                      >
                        {item.status.toUpperCase()}
                      </span>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>

        <div className="cyber-card">
          <h2 className="text-base font-semibold text-cyber-text mb-4">
            OVH Nexus Status
          </h2>
          <div className="space-y-3 text-sm">
            <div className="flex justify-between items-center p-2.5 rounded-md bg-cyber-dark/50 border border-cyber-grid">
              <span className="text-cyber-muted">{t("sidebar.apiStatus")}</span>
              <span
                className={`font-medium ${
                  isAuthenticated
                    ? "text-emerald-600 dark:text-emerald-400"
                    : "text-amber-600 dark:text-amber-400"
                }`}
              >
                {isAuthenticated ? t("sidebar.connected") : t("sidebar.notConfigured")}
              </span>
            </div>
            <div className="flex justify-between items-center p-2.5 rounded-md bg-cyber-dark/50 border border-cyber-grid">
              <span className="text-cyber-muted">API Accounts</span>
              <span className="font-medium text-cyber-text">{accounts?.length || 0}</span>
            </div>
            <div className="flex justify-between items-center p-2.5 rounded-md bg-cyber-dark/50 border border-cyber-grid">
              <span className="text-cyber-muted">{t("nav.queue")}</span>
              <span className={`font-medium ${stats.activeQueues > 0 ? "text-cyber-accent" : "text-cyber-muted"}`}>
                {stats.activeQueues > 0 ? `${stats.activeQueues} Active` : "Idle"}
              </span>
            </div>
            <div className="flex justify-between items-center p-2.5 rounded-md bg-cyber-dark/50 border border-cyber-grid">
              <span className="text-cyber-muted">{t("nav.monitor")}</span>
              <span
                className={`font-medium ${
                  stats.monitorRunning
                    ? "text-emerald-600 dark:text-emerald-400"
                    : "text-cyber-muted"
                }`}
              >
                {stats.monitorRunning ? "Running" : "Standby"}
              </span>
            </div>
            <div className="flex justify-between items-center pt-3 border-t border-cyber-grid text-xs text-cyber-muted">
              <span>Release</span>
              <span className="font-mono text-cyber-text">
                OVH Nexus v{typeof __APP_VERSION__ !== "undefined" ? __APP_VERSION__ : "1.0.0"} (AGPL-3.0)
              </span>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

export default Dashboard;
