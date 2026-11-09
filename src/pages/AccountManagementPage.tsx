import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { api } from "@/utils/apiClient";
import { useLocale } from "@/context/LocaleContext";
import { toast } from "sonner";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  User,
  CreditCard,
  RefreshCw,
  Wallet,
  FileText,
  CheckCircle,
  Mail,
  Inbox,
  Clock,
  ShoppingCart,
  CalendarClock,
  KeyRound,
  HardDrive,
  ExternalLink,
  Plus,
  Trash2,
  Star,
  Power,
  Activity,
} from "lucide-react";

interface AccountInfo {
  nichandle: string;
  customerCode: string;
  email: string;
  firstname: string;
  name: string;
  state: string;
  kycValidated: boolean;
  city: string;
  country: string;
  phone: string;
  currency: {
    code: string;
    symbol: string;
  };
}

interface Refund {
  refundId: string;
  date: string;
  orderId: number;
  originalBillId: string;
  password: string;
  pdfUrl: string;
  priceWithTax: {
    currencyCode: string;
    text: string;
    value: number;
  };
}

interface EmailHistory {
  id: number;
  subject: string;
  date: string;
  body: string;
}

interface OrderItem {
  orderId: number;
  date: string;
  expirationDate?: string;
  status: string;
  priceWithTax: {
    currencyCode?: string;
    text?: string;
    value?: number;
  };
  url?: string;
  pdfUrl?: string;
}

interface FollowUpStep {
  step: string;
  status: string;
  history?: Array<{ date: string; description: string; label: string }>;
}

interface ServiceOverviewItem {
  serviceName: string;
  displayName: string;
  type: "dedicated" | "vps";
  status: string;
  creation?: string;
  expiration?: string;
  daysRemaining?: number | null;
  renewMode: "auto" | "manual" | "deleteAtExpiration";
  renewPeriod?: number;
}

interface SshKeyItem {
  keyName: string;
  key: string;
  default: boolean;
}

interface VpsItem {
  serviceName: string;
  displayName: string;
  state: string;
  zone: string;
  model: string;
  vcore?: number;
  memoryLimitMb?: number;
  diskGb?: number;
  ips: string[];
  expiration?: string;
  daysRemaining?: number | null;
  renewMode: string;
}

const VALID_TABS = ["orders", "services", "ssh", "vps", "emails", "refunds"];

const AccountManagementPage = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const { t } = useLocale();
  const initialTab = VALID_TABS.includes(searchParams.get("tab") || "")
    ? (searchParams.get("tab") as string)
    : "orders";
  const [activeTab, setActiveTab] = useState<string>(initialTab);

  useEffect(() => {
    const tabParam = searchParams.get("tab");
    if (tabParam && VALID_TABS.includes(tabParam) && tabParam !== activeTab) {
      setActiveTab(tabParam);
    }
  }, [searchParams]);

  const handleTabChange = (val: string) => {
    setActiveTab(val);
    setSearchParams({ tab: val });
  };

  const [accountInfo, setAccountInfo] = useState<AccountInfo | null>(null);
  const [orders, setOrders] = useState<OrderItem[]>([]);
  const [followUpMap, setFollowUpMap] = useState<
    Record<number, { status: string; followUp: FollowUpStep[]; loading?: boolean }>
  >({});
  const [services, setServices] = useState<ServiceOverviewItem[]>([]);
  const [sshKeys, setSshKeys] = useState<SshKeyItem[]>([]);
  const [newKeyName, setNewKeyName] = useState("");
  const [newKeyContent, setNewKeyContent] = useState("");
  const [vpsList, setVpsList] = useState<VpsItem[]>([]);
  const [rebootingVps, setRebootingVps] = useState<Record<string, boolean>>({});
  const [refunds, setRefunds] = useState<Refund[]>([]);
  const [emails, setEmails] = useState<EmailHistory[]>([]);
  const [selectedEmail, setSelectedEmail] = useState<EmailHistory | null>(null);

  const [loading, setLoading] = useState({
    account: false,
    orders: false,
    services: false,
    ssh: false,
    vps: false,
    refunds: false,
    emails: false,
  });

  const fetchAccountInfo = async () => {
    setLoading((prev) => ({ ...prev, account: true }));
    try {
      const response = await api.get("/ovh/account/info");
      if (response.data.status === "success") {
        setAccountInfo(response.data.data);
      }
    } catch (error: any) {
      toast.error("Failed to fetch account info: " + (error.response?.data?.message || error.message));
    } finally {
      setLoading((prev) => ({ ...prev, account: false }));
    }
  };

  const fetchOrders = async () => {
    setLoading((prev) => ({ ...prev, orders: true }));
    try {
      const response = await api.get("/ovh/account/orders?limit=20");
      if (response.data.status === "success") {
        setOrders(response.data.data || []);
      }
    } catch (error: any) {
      toast.error("Failed to fetch orders: " + (error.response?.data?.message || error.message));
    } finally {
      setLoading((prev) => ({ ...prev, orders: false }));
    }
  };

  const fetchOrderFollowUp = async (orderId: number) => {
    setFollowUpMap((prev) => ({
      ...prev,
      [orderId]: { ...(prev[orderId] || { status: "", followUp: [] }), loading: true },
    }));
    try {
      const response = await api.get(`/ovh/account/orders/${orderId}/follow-up`);
      if (response.data.status === "success") {
        setFollowUpMap((prev) => ({
          ...prev,
          [orderId]: {
            status: response.data.data.status,
            followUp: response.data.data.followUp || [],
            loading: false,
          },
        }));
      }
    } catch (error: any) {
      toast.error("Failed to fetch delivery follow-up: " + (error.response?.data?.message || error.message));
      setFollowUpMap((prev) => ({
        ...prev,
        [orderId]: { ...(prev[orderId] || { status: "", followUp: [] }), loading: false },
      }));
    }
  };

  const fetchServicesOverview = async () => {
    setLoading((prev) => ({ ...prev, services: true }));
    try {
      const response = await api.get("/ovh/account/services-overview");
      if (response.data.status === "success") {
        setServices(response.data.data || []);
      }
    } catch (error: any) {
      toast.error("Failed to fetch services overview: " + (error.response?.data?.message || error.message));
    } finally {
      setLoading((prev) => ({ ...prev, services: false }));
    }
  };

  const fetchSshKeys = async () => {
    setLoading((prev) => ({ ...prev, ssh: true }));
    try {
      const response = await api.get("/ovh/account/ssh-keys");
      if (response.data.status === "success") {
        setSshKeys(response.data.data || []);
      }
    } catch (error: any) {
      toast.error("Failed to fetch SSH keys: " + (error.response?.data?.message || error.message));
    } finally {
      setLoading((prev) => ({ ...prev, ssh: false }));
    }
  };

  const handleAddSshKey = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newKeyName.trim() || !newKeyContent.trim()) {
      toast.error("Both keyName and SSH public key are required");
      return;
    }
    try {
      const response = await api.post("/ovh/account/ssh-keys", {
        keyName: newKeyName.trim(),
        key: newKeyContent.trim(),
      });
      if (response.data.status === "success") {
        toast.success("SSH key added");
        setNewKeyName("");
        setNewKeyContent("");
        fetchSshKeys();
      }
    } catch (error: any) {
      toast.error("Failed to add SSH key: " + (error.response?.data?.message || error.message));
    }
  };

  const handleToggleDefaultSshKey = async (keyName: string, currentDefault: boolean) => {
    try {
      await api.put(`/ovh/account/ssh-keys/${encodeURIComponent(keyName)}`, {
        default: !currentDefault,
      });
      toast.success(`Updated SSH key '${keyName}'`);
      fetchSshKeys();
    } catch (error: any) {
      toast.error("Failed to update SSH key: " + (error.response?.data?.message || error.message));
    }
  };

  const handleDeleteSshKey = async (keyName: string) => {
    try {
      await api.delete(`/ovh/account/ssh-keys/${encodeURIComponent(keyName)}`);
      toast.success(`Deleted SSH key '${keyName}'`);
      fetchSshKeys();
    } catch (error: any) {
      toast.error("Failed to delete SSH key: " + (error.response?.data?.message || error.message));
    }
  };

  const fetchVpsList = async () => {
    setLoading((prev) => ({ ...prev, vps: true }));
    try {
      const response = await api.get("/ovh/vps/list");
      if (response.data.status === "success") {
        setVpsList(response.data.data || []);
      }
    } catch (error: any) {
      toast.error("Failed to fetch VPS fleet: " + (error.response?.data?.message || error.message));
    } finally {
      setLoading((prev) => ({ ...prev, vps: false }));
    }
  };

  const handleRebootVps = async (serviceName: string) => {
    setRebootingVps((prev) => ({ ...prev, [serviceName]: true }));
    try {
      const response = await api.post(`/ovh/vps/${encodeURIComponent(serviceName)}/reboot`);
      if (response.data.status === "success") {
        toast.success(response.data.message || `Reboot task triggered for ${serviceName}`);
      }
    } catch (error: any) {
      toast.error("Failed to reboot VPS: " + (error.response?.data?.message || error.message));
    } finally {
      setRebootingVps((prev) => ({ ...prev, [serviceName]: false }));
    }
  };

  const fetchRefunds = async () => {
    setLoading((prev) => ({ ...prev, refunds: true }));
    try {
      const response = await api.get("/ovh/account/refunds");
      if (response.data.status === "success") {
        const sortedRefunds = (response.data.data || []).sort((a: Refund, b: Refund) => {
          return new Date(b.date).getTime() - new Date(a.date).getTime();
        });
        setRefunds(sortedRefunds);
      }
    } catch (error: any) {
      toast.error("Failed to fetch refunds: " + (error.response?.data?.message || error.message));
    } finally {
      setLoading((prev) => ({ ...prev, refunds: false }));
    }
  };

  const fetchEmailHistory = async () => {
    setLoading((prev) => ({ ...prev, emails: true }));
    try {
      const response = await api.get("/ovh/account/email-history");
      if (response.data.status === "success") {
        const sortedEmails = (response.data.data || []).sort((a: EmailHistory, b: EmailHistory) => {
          return new Date(b.date).getTime() - new Date(a.date).getTime();
        });
        setEmails(sortedEmails);
      }
    } catch (error: any) {
      toast.error("Failed to fetch emails: " + (error.response?.data?.message || error.message));
    } finally {
      setLoading((prev) => ({ ...prev, emails: false }));
    }
  };

  useEffect(() => {
    fetchAccountInfo();
    fetchOrders();
    fetchServicesOverview();
    fetchSshKeys();
    fetchVpsList();
    fetchEmailHistory();
    fetchRefunds();
  }, []);

  const formatDateTime = (dateString?: string) => {
    if (!dateString) return "-";
    const date = new Date(dateString);
    return date.toLocaleString("zh-CN", {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
    });
  };

  const formatEmailBody = (body: string) => {
    if (!body) return "";
    const lines = body.split("\n");
    const urlRegex = /(https?:\/\/[^\s]+)/g;
    return lines.map((line, index) => {
      const parts = line.split(urlRegex);
      return (
        <div key={index} className="mb-1.5">
          {parts.map((part, i) =>
            part.match(urlRegex) ? (
              <a
                key={i}
                href={part}
                target="_blank"
                rel="noopener noreferrer"
                className="text-cyber-accent hover:underline break-all"
              >
                {part}
              </a>
            ) : (
              <span key={i}>{part}</span>
            )
          )}
        </div>
      );
    });
  };

  const getOrderStatusBadge = (status: string) => {
    const s = (status || "").toLowerCase();
    if (s.includes("delivered") || s === "done") {
      return (
        <Badge className="bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border-emerald-500/30">
          {status}
        </Badge>
      );
    }
    if (s.includes("delivering") || s.includes("checking")) {
      return (
        <Badge className="bg-cyber-accent/15 text-cyber-accent border-cyber-accent/30">
          {status}
        </Badge>
      );
    }
    if (s.includes("notpaid") || s.includes("unpaid")) {
      return (
        <Badge className="bg-amber-500/15 text-amber-600 dark:text-amber-400 border-amber-500/30">
          {status}
        </Badge>
      );
    }
    if (s.includes("expired") || s.includes("cancelled")) {
      return (
        <Badge className="bg-rose-500/15 text-rose-600 dark:text-rose-400 border-rose-500/30">
          {status}
        </Badge>
      );
    }
    return <Badge variant="outline">{status || "unknown"}</Badge>;
  };

  const getDaysRemainingBadge = (days?: number | null) => {
    if (days === null || days === undefined) return <span className="text-cyber-muted">-</span>;
    if (days <= 7) {
      return (
        <span className="px-2 py-0.5 text-xs font-semibold rounded bg-rose-500/15 text-rose-600 dark:text-rose-400 border border-rose-500/30">
          {days}d left
        </span>
      );
    }
    if (days <= 30) {
      return (
        <span className="px-2 py-0.5 text-xs font-semibold rounded bg-amber-500/15 text-amber-600 dark:text-amber-400 border border-amber-500/30">
          {days}d left
        </span>
      );
    }
    return (
      <span className="px-2 py-0.5 text-xs font-semibold rounded bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border border-emerald-500/30">
        {days}d left
      </span>
    );
  };

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <h1 className="text-2xl sm:text-3xl font-bold mb-1 cyber-glow-text">
          {t("account.title")}
        </h1>
        <p className="text-cyber-muted text-sm">{t("account.subtitle")}</p>
      </div>

      {/* Top Account Summary Card */}
      <div className="cyber-card p-5">
        <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
          <div className="flex items-center gap-3">
            <div className="p-2.5 rounded-lg bg-cyber-accent/10 border border-cyber-accent/20">
              <User className="w-5 h-5 text-cyber-accent" />
            </div>
            <div className="min-w-0">
              <p className="text-xs text-cyber-muted">Customer Code / NicHandle</p>
              <p className="text-lg font-bold text-cyber-text truncate">
                {accountInfo ? `${accountInfo.customerCode} (${accountInfo.nichandle})` : "-"}
              </p>
            </div>
          </div>

          <div className="flex items-center gap-3">
            <div className="p-2.5 rounded-lg bg-cyber-accent/10 border border-cyber-accent/20">
              <Mail className="w-5 h-5 text-cyber-accent" />
            </div>
            <div className="min-w-0">
              <p className="text-xs text-cyber-muted">Email</p>
              <p className="text-sm font-semibold text-cyber-text truncate">
                {accountInfo?.email || "-"}
              </p>
            </div>
          </div>

          <div className="flex items-center gap-3">
            <div className="p-2.5 rounded-lg bg-emerald-500/10 border border-emerald-500/20">
              <CheckCircle className="w-5 h-5 text-emerald-600 dark:text-emerald-400" />
            </div>
            <div className="min-w-0">
              <p className="text-xs text-cyber-muted">Account & KYC Status</p>
              <p className="text-sm font-semibold text-cyber-text">
                {accountInfo
                  ? `${accountInfo.state || "active"} • ${
                      accountInfo.kycValidated ? "KYC Verified" : "KYC Pending"
                    }`
                  : "-"}
              </p>
            </div>
          </div>

          <div className="flex items-center gap-3">
            <div className="p-2.5 rounded-lg bg-cyber-accent/10 border border-cyber-accent/20">
              <Wallet className="w-5 h-5 text-cyber-accent" />
            </div>
            <div className="min-w-0">
              <p className="text-xs text-cyber-muted">Holder & Currency</p>
              <p className="text-sm font-semibold text-cyber-text truncate">
                {accountInfo
                  ? `${accountInfo.firstname || ""} ${accountInfo.name || ""} (${
                      accountInfo.currency?.code || ""
                    })`
                  : "-"}
              </p>
            </div>
          </div>
        </div>
      </div>

      {/* 6 Unified Tabs */}
      <Tabs value={activeTab} onValueChange={handleTabChange} className="w-full space-y-4">
        <TabsList className="grid w-full grid-cols-2 md:grid-cols-6 h-auto gap-1 cyber-card p-1.5">
          <TabsTrigger value="orders" className="data-[state=active]:bg-cyber-accent data-[state=active]:text-white py-2 text-xs">
            <ShoppingCart className="w-3.5 h-3.5 mr-1.5" />
            {t("account.tab.orders")}
          </TabsTrigger>
          <TabsTrigger value="services" className="data-[state=active]:bg-cyber-accent data-[state=active]:text-white py-2 text-xs">
            <CalendarClock className="w-3.5 h-3.5 mr-1.5" />
            {t("account.tab.services")}
          </TabsTrigger>
          <TabsTrigger value="ssh" className="data-[state=active]:bg-cyber-accent data-[state=active]:text-white py-2 text-xs">
            <KeyRound className="w-3.5 h-3.5 mr-1.5" />
            {t("account.tab.ssh")}
          </TabsTrigger>
          <TabsTrigger value="vps" className="data-[state=active]:bg-cyber-accent data-[state=active]:text-white py-2 text-xs">
            <HardDrive className="w-3.5 h-3.5 mr-1.5" />
            {t("account.tab.vps")}
          </TabsTrigger>
          <TabsTrigger value="emails" className="data-[state=active]:bg-cyber-accent data-[state=active]:text-white py-2 text-xs">
            <Mail className="w-3.5 h-3.5 mr-1.5" />
            {t("account.tab.emails")}
          </TabsTrigger>
          <TabsTrigger value="refunds" className="data-[state=active]:bg-cyber-accent data-[state=active]:text-white py-2 text-xs">
            <CreditCard className="w-3.5 h-3.5 mr-1.5" />
            {t("account.tab.refunds")}
          </TabsTrigger>
        </TabsList>

        {/* Tab 1: Orders & Live Delivery Follow-up */}
        <TabsContent value="orders">
          <Card className="cyber-card">
            <CardHeader className="flex flex-row items-center justify-between">
              <div>
                <CardTitle className="text-lg">{t("account.tab.orders")}</CardTitle>
                <CardDescription>
                  Official OVHcloud `/me/order` & `/followUp` live delivery pipeline
                </CardDescription>
              </div>
              <button
                onClick={fetchOrders}
                disabled={loading.orders}
                className="cyber-button text-xs flex items-center gap-1.5"
              >
                <RefreshCw className={`w-3.5 h-3.5 ${loading.orders ? "animate-spin" : ""}`} />
                <span>Refresh</span>
              </button>
            </CardHeader>
            <CardContent>
              {orders.length === 0 ? (
                <div className="text-center py-10 text-cyber-muted">
                  <ShoppingCart className="w-10 h-10 mx-auto mb-2 opacity-40" />
                  <p className="text-sm">No recent OVHcloud orders found</p>
                </div>
              ) : (
                <div className="space-y-3">
                  {orders.map((order) => {
                    const fup = followUpMap[order.orderId];
                    return (
                      <div
                        key={order.orderId}
                        className="p-4 rounded-lg bg-cyber-dark/50 border border-cyber-grid space-y-3"
                      >
                        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                          <div className="space-y-1">
                            <div className="flex items-center gap-2.5">
                              <span className="font-mono font-bold text-cyber-text">
                                Order #{order.orderId}
                              </span>
                              {getOrderStatusBadge(fup?.status || order.status)}
                              <span className="text-sm font-semibold text-cyber-accent">
                                {order.priceWithTax?.text || ""}
                              </span>
                            </div>
                            <p className="text-xs text-cyber-muted">
                              Created: {formatDateTime(order.date)}
                              {order.expirationDate
                                ? ` • Expires: ${formatDateTime(order.expirationDate)}`
                                : ""}
                            </p>
                          </div>

                          <div className="flex flex-wrap items-center gap-2">
                            {order.url && (
                              <a
                                href={order.url}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="cyber-button text-xs py-1.5 px-3 inline-flex items-center gap-1.5 border-cyber-accent/40 text-cyber-accent"
                              >
                                <ExternalLink size={13} />
                                <span>Payment / Order Link</span>
                              </a>
                            )}
                            {order.pdfUrl && (
                              <a
                                href={order.pdfUrl}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="cyber-button text-xs py-1.5 px-3 inline-flex items-center gap-1.5"
                              >
                                <FileText size={13} />
                                <span>PDF</span>
                              </a>
                            )}
                            <button
                              type="button"
                              onClick={() => fetchOrderFollowUp(order.orderId)}
                              disabled={fup?.loading}
                              className="cyber-button text-xs py-1.5 px-3 inline-flex items-center gap-1.5"
                            >
                              <Activity size={13} />
                              <span>{fup?.loading ? "Loading..." : "Track Delivery"}</span>
                            </button>
                          </div>
                        </div>

                        {fup && fup.followUp && fup.followUp.length > 0 && (
                          <div className="pt-3 border-t border-cyber-grid grid grid-cols-1 sm:grid-cols-4 gap-2.5">
                            {fup.followUp.map((stepObj, idx) => {
                              const st = (stepObj.status || "").toUpperCase();
                              const isDone = st === "DONE";
                              const isDoing = st === "DOING";
                              const isError = st === "ERROR";
                              return (
                                <div
                                  key={idx}
                                  className={`p-2.5 rounded-md border text-xs ${
                                    isDone
                                      ? "bg-emerald-500/10 border-emerald-500/30 text-emerald-600 dark:text-emerald-400"
                                      : isDoing
                                      ? "bg-cyber-accent/10 border-cyber-accent/30 text-cyber-accent"
                                      : isError
                                      ? "bg-rose-500/10 border-rose-500/30 text-rose-600 dark:text-rose-400"
                                      : "bg-cyber-surface border-cyber-grid text-cyber-muted"
                                  }`}
                                >
                                  <div className="flex items-center justify-between font-semibold mb-1">
                                    <span>{stepObj.step}</span>
                                    <span>{stepObj.status}</span>
                                  </div>
                                  {stepObj.history && stepObj.history.length > 0 && (
                                    <p className="text-[11px] opacity-80 truncate">
                                      {stepObj.history[stepObj.history.length - 1]?.description ||
                                        stepObj.history[stepObj.history.length - 1]?.label}
                                    </p>
                                  )}
                                </div>
                              );
                            })}
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        {/* Tab 2: Services Expiration & Auto-Renewal Matrix */}
        <TabsContent value="services">
          <Card className="cyber-card">
            <CardHeader className="flex flex-row items-center justify-between">
              <div>
                <CardTitle className="text-lg">{t("account.tab.services")}</CardTitle>
                <CardDescription>
                  Unified expiration countdown & renewal status for Dedicated Servers and VPS
                </CardDescription>
              </div>
              <button
                onClick={fetchServicesOverview}
                disabled={loading.services}
                className="cyber-button text-xs flex items-center gap-1.5"
              >
                <RefreshCw className={`w-3.5 h-3.5 ${loading.services ? "animate-spin" : ""}`} />
                <span>Refresh</span>
              </button>
            </CardHeader>
            <CardContent>
              {services.length === 0 ? (
                <div className="text-center py-10 text-cyber-muted">
                  <CalendarClock className="w-10 h-10 mx-auto mb-2 opacity-40" />
                  <p className="text-sm">No active Dedicated Server or VPS services found</p>
                </div>
              ) : (
                <div className="overflow-x-auto">
                  <table className="w-full cyber-table text-left">
                    <thead>
                      <tr>
                        <th>Type</th>
                        <th>Service / Display Name</th>
                        <th>Status</th>
                        <th>Expiration</th>
                        <th>Countdown</th>
                        <th>Renewal Mode</th>
                      </tr>
                    </thead>
                    <tbody>
                      {services.map((svc) => (
                        <tr key={`${svc.type}-${svc.serviceName}`}>
                          <td>
                            <Badge
                              variant="outline"
                              className="border-cyber-accent/40 text-cyber-accent bg-cyber-accent/10"
                            >
                              {svc.type.toUpperCase()}
                            </Badge>
                          </td>
                          <td>
                            <div className="font-semibold text-cyber-text">{svc.displayName}</div>
                            <div className="text-xs font-mono text-cyber-muted">{svc.serviceName}</div>
                          </td>
                          <td>
                            <span
                              className={`text-xs font-medium ${
                                svc.status === "ok"
                                  ? "text-emerald-600 dark:text-emerald-400"
                                  : "text-amber-600 dark:text-amber-400"
                              }`}
                            >
                              {svc.status}
                            </span>
                          </td>
                          <td className="text-xs font-mono text-cyber-text">
                            {svc.expiration || "-"}
                          </td>
                          <td>{getDaysRemainingBadge(svc.daysRemaining)}</td>
                          <td>
                            <Badge
                              variant="outline"
                              className={
                                svc.renewMode === "auto"
                                  ? "border-emerald-500/30 text-emerald-600 dark:text-emerald-400"
                                  : svc.renewMode === "deleteAtExpiration"
                                  ? "border-rose-500/30 text-rose-600 dark:text-rose-400"
                                  : "border-amber-500/30 text-amber-600 dark:text-amber-400"
                              }
                            >
                              {svc.renewMode === "auto"
                                ? "Auto Renew"
                                : svc.renewMode === "deleteAtExpiration"
                                ? "Terminate at Expiry"
                                : "Manual"}
                            </Badge>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        {/* Tab 3: SSH Keys Registry */}
        <TabsContent value="ssh">
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
            <Card className="cyber-card lg:col-span-2">
              <CardHeader className="flex flex-row items-center justify-between">
                <div>
                  <CardTitle className="text-lg">{t("account.tab.ssh")}</CardTitle>
                  <CardDescription>
                    OVHcloud `/me/sshKey` public keys available for automated OS reinstalls
                  </CardDescription>
                </div>
                <button
                  onClick={fetchSshKeys}
                  disabled={loading.ssh}
                  className="cyber-button text-xs flex items-center gap-1.5"
                >
                  <RefreshCw className={`w-3.5 h-3.5 ${loading.ssh ? "animate-spin" : ""}`} />
                  <span>Refresh</span>
                </button>
              </CardHeader>
              <CardContent>
                {sshKeys.length === 0 ? (
                  <div className="text-center py-10 text-cyber-muted">
                    <KeyRound className="w-10 h-10 mx-auto mb-2 opacity-40" />
                    <p className="text-sm">No SSH keys registered on this OVHcloud account</p>
                  </div>
                ) : (
                  <div className="space-y-3">
                    {sshKeys.map((item) => (
                      <div
                        key={item.keyName}
                        className="p-3.5 rounded-lg bg-cyber-dark/50 border border-cyber-grid flex items-start justify-between gap-3"
                      >
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center gap-2">
                            <span className="font-semibold text-sm text-cyber-text">
                              {item.keyName}
                            </span>
                            {item.default && (
                              <Badge className="bg-cyber-accent/15 text-cyber-accent border-cyber-accent/30 text-[10px]">
                                Default
                              </Badge>
                            )}
                          </div>
                          <p className="text-xs font-mono text-cyber-muted mt-1 break-all line-clamp-2">
                            {item.key}
                          </p>
                        </div>
                        <div className="flex items-center gap-1.5 shrink-0">
                          <button
                            type="button"
                            onClick={() => handleToggleDefaultSshKey(item.keyName, item.default)}
                            className={`cyber-button p-1.5 ${
                              item.default
                                ? "text-amber-600 dark:text-amber-400 border-amber-500/40"
                                : "text-cyber-muted"
                            }`}
                            title="Toggle default SSH key"
                          >
                            <Star size={14} />
                          </button>
                          <button
                            type="button"
                            onClick={() => handleDeleteSshKey(item.keyName)}
                            className="cyber-button p-1.5 text-rose-600 dark:text-rose-400 hover:border-rose-500/40"
                            title="Delete SSH key"
                          >
                            <Trash2 size={14} />
                          </button>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </CardContent>
            </Card>

            <Card className="cyber-card">
              <CardHeader>
                <CardTitle className="text-lg">Add SSH Public Key</CardTitle>
                <CardDescription>Register a new key in `/me/sshKey`</CardDescription>
              </CardHeader>
              <CardContent>
                <form onSubmit={handleAddSshKey} className="space-y-4">
                  <div>
                    <label className="block text-xs text-cyber-muted mb-1">Key Name</label>
                    <input
                      type="text"
                      value={newKeyName}
                      onChange={(e) => setNewKeyName(e.target.value)}
                      placeholder="e.g. prod-ed25519"
                      className="cyber-input w-full text-sm"
                    />
                  </div>
                  <div>
                    <label className="block text-xs text-cyber-muted mb-1">Public Key</label>
                    <textarea
                      rows={4}
                      value={newKeyContent}
                      onChange={(e) => setNewKeyContent(e.target.value)}
                      placeholder="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI..."
                      className="cyber-input w-full text-xs font-mono"
                    />
                  </div>
                  <button
                    type="submit"
                    className="cyber-button-primary w-full inline-flex items-center justify-center gap-1.5"
                  >
                    <Plus size={15} />
                    <span>Register SSH Key</span>
                  </button>
                </form>
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        {/* Tab 4: Purchased VPS Fleet Control */}
        <TabsContent value="vps">
          <Card className="cyber-card">
            <CardHeader className="flex flex-row items-center justify-between">
              <div>
                <CardTitle className="text-lg">{t("account.tab.vps")}</CardTitle>
                <CardDescription>
                  Manage purchased OVHcloud VPS instances (`/vps`), inspect IPs & trigger reboots
                </CardDescription>
              </div>
              <button
                onClick={fetchVpsList}
                disabled={loading.vps}
                className="cyber-button text-xs flex items-center gap-1.5"
              >
                <RefreshCw className={`w-3.5 h-3.5 ${loading.vps ? "animate-spin" : ""}`} />
                <span>Refresh</span>
              </button>
            </CardHeader>
            <CardContent>
              {vpsList.length === 0 ? (
                <div className="text-center py-10 text-cyber-muted">
                  <HardDrive className="w-10 h-10 mx-auto mb-2 opacity-40" />
                  <p className="text-sm">No active VPS instances on this account</p>
                </div>
              ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  {vpsList.map((vps) => (
                    <div
                      key={vps.serviceName}
                      className="p-4 rounded-lg bg-cyber-dark/50 border border-cyber-grid space-y-3"
                    >
                      <div className="flex items-start justify-between gap-2">
                        <div>
                          <div className="flex items-center gap-2">
                            <h3 className="font-bold text-cyber-text">{vps.displayName}</h3>
                            <Badge
                              className={
                                vps.state === "running"
                                  ? "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border-emerald-500/30"
                                  : "bg-amber-500/15 text-amber-600 dark:text-amber-400 border-amber-500/30"
                              }
                            >
                              {vps.state}
                            </Badge>
                          </div>
                          <p className="text-xs font-mono text-cyber-muted mt-0.5">
                            {vps.serviceName}
                          </p>
                        </div>

                        <button
                          type="button"
                          onClick={() => handleRebootVps(vps.serviceName)}
                          disabled={rebootingVps[vps.serviceName]}
                          className="cyber-button text-xs py-1.5 px-3 inline-flex items-center gap-1.5"
                        >
                          <Power size={13} />
                          <span>{rebootingVps[vps.serviceName] ? "Rebooting..." : "Reboot"}</span>
                        </button>
                      </div>

                      <div className="grid grid-cols-2 gap-2 text-xs bg-cyber-surface p-2.5 rounded border border-cyber-grid">
                        <div>
                          <span className="text-cyber-muted block">Model / Specs</span>
                          <span className="text-cyber-text font-medium">
                            {vps.model}{" "}
                            {vps.vcore ? `(${vps.vcore} vCore / ${vps.memoryLimitMb} MB)` : ""}
                          </span>
                        </div>
                        <div>
                          <span className="text-cyber-muted block">Zone / Expiry</span>
                          <span className="text-cyber-text font-medium">
                            {vps.zone || "-"} • {vps.expiration || "-"}
                          </span>
                        </div>
                      </div>

                      {vps.ips && vps.ips.length > 0 && (
                        <div className="flex flex-wrap gap-1.5">
                          {vps.ips.map((ip) => (
                            <span
                              key={ip}
                              className="px-2 py-0.5 text-[11px] font-mono rounded bg-cyber-surface border border-cyber-grid text-cyber-text"
                            >
                              {ip}
                            </span>
                          ))}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        {/* Tab 5: Email History */}
        <TabsContent value="emails">
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
            <Card className="cyber-card">
              <CardHeader>
                <CardTitle className="flex items-center justify-between text-lg">
                  <span>{t("account.tab.emails")}</span>
                  <button
                    onClick={fetchEmailHistory}
                    className="cyber-button text-xs flex items-center gap-1.5"
                  >
                    <RefreshCw className="w-3.5 h-3.5" />
                    <span>Refresh</span>
                  </button>
                </CardTitle>
                <CardDescription>Recent OVHcloud notification emails</CardDescription>
              </CardHeader>
              <CardContent>
                {emails.length > 0 ? (
                  <div className="space-y-2 max-h-[550px] overflow-y-auto pr-1">
                    {emails.map((email) => (
                      <div
                        key={email.id}
                        className={`p-3.5 rounded-lg border cursor-pointer transition-all ${
                          selectedEmail?.id === email.id
                            ? "bg-cyber-accent/10 border-cyber-accent"
                            : "bg-cyber-dark/40 border-cyber-grid hover:border-cyber-accent/40"
                        }`}
                        onClick={() => setSelectedEmail(email)}
                      >
                        <div className="flex items-start justify-between gap-2">
                          <p className="font-medium text-sm text-cyber-text truncate">
                            {email.subject}
                          </p>
                          <Badge variant="outline" className="text-[11px] shrink-0">
                            #{email.id}
                          </Badge>
                        </div>
                        <div className="flex items-center gap-1.5 mt-1 text-xs text-cyber-muted">
                          <Clock className="w-3 h-3" />
                          <span>{formatDateTime(email.date)}</span>
                        </div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="text-center py-8 text-cyber-muted">
                    <Inbox className="w-10 h-10 mx-auto mb-2 opacity-40" />
                    <p className="text-sm">No email records</p>
                  </div>
                )}
              </CardContent>
            </Card>

            <Card className="cyber-card">
              <CardHeader>
                <CardTitle className="text-lg">Email Content</CardTitle>
                <CardDescription>
                  {selectedEmail ? `Message #${selectedEmail.id}` : "Select an email on the left"}
                </CardDescription>
              </CardHeader>
              <CardContent>
                {selectedEmail ? (
                  <div className="space-y-3">
                    <div className="p-3 rounded-lg bg-cyber-dark/50 border border-cyber-grid">
                      <p className="font-semibold text-sm text-cyber-text">
                        {selectedEmail.subject}
                      </p>
                      <p className="text-xs text-cyber-muted mt-1">
                        {formatDateTime(selectedEmail.date)}
                      </p>
                    </div>
                    <div className="p-3.5 rounded-lg bg-cyber-dark/50 border border-cyber-grid max-h-[450px] overflow-y-auto text-xs font-mono text-cyber-text leading-relaxed">
                      {formatEmailBody(selectedEmail.body)}
                    </div>
                  </div>
                ) : (
                  <div className="text-center py-12 text-cyber-muted">
                    <Mail className="w-12 h-12 mx-auto mb-3 opacity-40" />
                    <p className="text-sm">Select an email from the list to view its content</p>
                  </div>
                )}
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        {/* Tab 6: Refunds */}
        <TabsContent value="refunds">
          <Card className="cyber-card">
            <CardHeader className="flex flex-row items-center justify-between">
              <div>
                <CardTitle className="text-lg">{t("account.tab.refunds")}</CardTitle>
                <CardDescription>OVHcloud `/me/refund` credit notes and refunds</CardDescription>
              </div>
              <button
                onClick={fetchRefunds}
                className="cyber-button text-xs flex items-center gap-1.5"
              >
                <RefreshCw className="w-3.5 h-3.5" />
                <span>Refresh</span>
              </button>
            </CardHeader>
            <CardContent>
              {refunds.length > 0 ? (
                <div className="space-y-3">
                  {refunds.map((refund) => (
                    <div
                      key={refund.refundId}
                      className="p-4 rounded-lg bg-cyber-dark/50 border border-cyber-grid flex justify-between items-center"
                    >
                      <div>
                        <div className="flex items-center gap-2">
                          <span className="font-semibold text-cyber-text">
                            Refund #{refund.refundId}
                          </span>
                          <Badge variant="outline" className="text-xs">
                            Order {refund.orderId}
                          </Badge>
                        </div>
                        <p className="text-xs text-cyber-muted mt-1">
                          {formatDateTime(refund.date)}
                        </p>
                      </div>
                      <div className="text-right">
                        <p className="text-lg font-bold text-emerald-600 dark:text-emerald-400">
                          {refund.priceWithTax?.text}
                        </p>
                        {refund.pdfUrl && (
                          <a
                            href={refund.pdfUrl}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-xs text-cyber-accent hover:underline inline-flex items-center gap-1 mt-1"
                          >
                            <FileText className="w-3 h-3" />
                            PDF
                          </a>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="text-center py-8 text-cyber-muted">
                  <CreditCard className="w-10 h-10 mx-auto mb-2 opacity-40" />
                  <p className="text-sm">No refund records found</p>
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
};

export default AccountManagementPage;
