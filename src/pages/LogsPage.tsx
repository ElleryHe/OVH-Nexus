import { useState, useEffect, useRef, useMemo, useCallback } from "react";
import { api } from "@/utils/apiClient";
import { API_URL } from "@/config/constants";
import { useLocale } from "@/context/LocaleContext";
import { toast } from "sonner";
import { useIsMobile } from "@/hooks/use-mobile";
import { useToast } from "@/components/ToastContainer";
import {
  Search,
  RefreshCw,
  Trash2,
  Download,
  FileText,
  Radio,
  PauseCircle,
  RotateCcw,
  ArrowDown,
} from "lucide-react";

export interface LogEntry {
  id: string | number;
  timestamp: string;
  level: "INFO" | "WARNING" | "ERROR" | "DEBUG" | string;
  message: string;
  source: string;
  attrs?: Record<string, unknown> | null;
}

const LOG_SOURCES = [
  "server",
  "http",
  "auth",
  "totp",
  "vault",
  "ovh",
  "catalog",
  "queue",
  "stock-monitor",
  "vps-monitor",
  "sku-watcher",
  "fleet",
  "notify",
  "mcp",
] as const;

const LOG_LEVELS = ["INFO", "WARNING", "ERROR", "DEBUG"] as const;

const MAX_CLIENT_LOGS = 2000;

type StreamStatus = "connecting" | "live" | "reconnecting" | "paused";

function pad2(n: number): string {
  return n < 10 ? `0${n}` : String(n);
}

function formatLocalTimestamp(ts: string): { display: string; title: string } {
  if (!ts) return { display: "-", title: "" };
  const date = new Date(ts);
  if (Number.isNaN(date.getTime())) {
    return { display: ts, title: ts };
  }
  const yyyy = date.getFullYear();
  const mm = pad2(date.getMonth() + 1);
  const dd = pad2(date.getDate());
  const hh = pad2(date.getHours());
  const min = pad2(date.getMinutes());
  const ss = pad2(date.getSeconds());
  return {
    display: `${yyyy}-${mm}-${dd} ${hh}:${min}:${ss}`,
    title: `${date.toLocaleString()} (UTC: ${ts})`,
  };
}

function formatAttrValue(val: unknown): string {
  if (val === null || val === undefined) return String(val);
  if (typeof val === "object") {
    try {
      return JSON.stringify(val);
    } catch {
      return String(val);
    }
  }
  return String(val);
}

function isLogEntryRecord(item: unknown): item is LogEntry {
  return Boolean(
    item &&
      typeof item === "object" &&
      "message" in item &&
      (item as Record<string, unknown>).message !== undefined
  );
}

function normalizeLogList(raw: unknown): LogEntry[] {
  let arr: unknown[] = [];
  if (Array.isArray(raw)) {
    arr = raw;
  } else if (
    raw &&
    typeof raw === "object" &&
    "logs" in raw &&
    Array.isArray((raw as { logs?: unknown }).logs)
  ) {
    arr = (raw as { logs: unknown[] }).logs;
  }
  return arr.filter(isLogEntryRecord);
}

const LogsPage = () => {
  const isMobile = useIsMobile();
  const { t, locale } = useLocale();
  const { showConfirm } = useToast();

  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [streamStatus, setStreamStatus] = useState<StreamStatus>("connecting");
  const [filterLevel, setFilterLevel] = useState<string>("all");
  const [filterSource, setFilterSource] = useState<string>("all");
  const [searchTerm, setSearchTerm] = useState("");
  const [exportingFormat, setExportingFormat] = useState<"jsonl" | "log" | null>(
    null
  );

  const logEndRef = useRef<HTMLDivElement>(null);
  const hasLoadedOnceRef = useRef(false);

  const fetchLogs = useCallback(
    async (options: { isRefresh?: boolean; replace?: boolean } = {}) => {
      const { isRefresh = false, replace = false } = options;
      if (isRefresh) {
        setIsRefreshing(true);
      } else {
        setIsLoading(true);
      }

      try {
        const response = await api.get("/logs");
        const fetched = normalizeLogList(response.data);

        setLogs((prev) => {
          if (replace || prev.length === 0) {
            return fetched.slice(-MAX_CLIENT_LOGS);
          }
          // Preserve any newer SSE entries that arrived while GET /api/logs was in flight
          const seenIds = new Set(fetched.map((entry) => String(entry.id)));
          const lastFetchedMs =
            fetched.length > 0
              ? Date.parse(fetched[fetched.length - 1].timestamp) || 0
              : 0;
          const newerLive = prev.filter((entry) => {
            if (seenIds.has(String(entry.id))) return false;
            const entryMs = Date.parse(entry.timestamp) || 0;
            return entryMs >= lastFetchedMs;
          });
          return [...fetched, ...newerLive].slice(-MAX_CLIENT_LOGS);
        });
        hasLoadedOnceRef.current = true;
      } catch (error) {
        console.error("Error fetching logs:", error);
        if (hasLoadedOnceRef.current && !isRefresh) {
          toast.error(t("logs.fetchFailed"));
        }
      } finally {
        setIsLoading(false);
        setIsRefreshing(false);
      }
    },
    [t]
  );

  // Initial load
  useEffect(() => {
    void fetchLogs({ isRefresh: false, replace: true });
  }, [fetchLogs]);

  // Real-time SSE log stream subscription
  useEffect(() => {
    if (!autoRefresh) {
      setStreamStatus("paused");
      return;
    }

    setStreamStatus("connecting");
    const streamUrl = `${API_URL.replace(/\/$/, "")}/logs/stream`;
    const eventSource = new EventSource(streamUrl, { withCredentials: true });

    const handleIncomingEvent = (event: MessageEvent) => {
      if (!event.data) return;
      try {
        const parsed = JSON.parse(event.data);
        if (
          !parsed ||
          typeof parsed !== "object" ||
          parsed.message === undefined
        ) {
          return;
        }
        const entry = parsed as LogEntry;
        const entryId = String(entry.id);

        setLogs((prev) => {
          if (prev.some((existing) => String(existing.id) === entryId)) {
            return prev;
          }
          const next = [...prev, entry];
          return next.length > MAX_CLIENT_LOGS
            ? next.slice(-MAX_CLIENT_LOGS)
            : next;
        });
      } catch {
        // Ignore non-JSON heartbeat or keepalive payloads
      }
    };

    eventSource.onopen = () => {
      setStreamStatus("live");
    };

    eventSource.onmessage = handleIncomingEvent;
    eventSource.addEventListener("log", handleIncomingEvent as EventListener);

    eventSource.onerror = () => {
      if (eventSource.readyState === EventSource.CLOSED) {
        setStreamStatus("paused");
      } else {
        setStreamStatus("reconnecting");
      }
    };

    return () => {
      eventSource.removeEventListener(
        "log",
        handleIncomingEvent as EventListener
      );
      eventSource.close();
    };
  }, [autoRefresh]);

  const handleToggleLiveStream = () => {
    const next = !autoRefresh;
    setAutoRefresh(next);
    if (next && hasLoadedOnceRef.current) {
      // Catch up on any entries emitted while paused before SSE resumes
      void fetchLogs({ isRefresh: true });
    }
  };

  // Clear logs
  const clearLogs = async () => {
    const confirmed = await showConfirm({
      title: t("logs.clearConfirmTitle"),
      message: t("logs.clearConfirmMessage"),
      confirmText: t("logs.clearConfirmButton"),
      cancelText: t("logs.cancelButton"),
    });

    if (!confirmed) return;

    try {
      await api.delete("/logs");
      setLogs([]);
      toast.success(t("logs.clearSuccess"));
      await fetchLogs({ isRefresh: true, replace: true });
    } catch (error) {
      console.error("Error clearing logs:", error);
      toast.error(t("logs.clearFailed"));
    }
  };

  // Export logs (.jsonl / .log)
  const handleExportLogs = async (format: "jsonl" | "log") => {
    setExportingFormat(format);
    try {
      const response = await api.get("/logs/export", {
        params: { format },
        responseType: "blob",
      });
      const blob = new Blob([response.data], {
        type:
          format === "jsonl"
            ? "application/x-ndjson;charset=utf-8"
            : "text/plain;charset=utf-8",
      });
      const disposition = response.headers?.["content-disposition"] as
        | string
        | undefined;
      const match = disposition?.match(/filename="?([^"]+)"?/i);
      const stamp = new Date().toISOString().replace(/[:.]/g, "-").slice(0, 19);
      const filename = match?.[1] || `ovh-nexus-logs-${stamp}.${format}`;
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = filename;
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(url);
      toast.success(t("logs.exportSuccess", { format: `.${format}` }));
    } catch (error) {
      console.error("Error exporting logs:", error);
      toast.error(t("logs.exportFailed"));
    } finally {
      setExportingFormat(null);
    }
  };

  const hasActiveFilters =
    filterLevel !== "all" ||
    filterSource !== "all" ||
    searchTerm.trim().length > 0;

  const resetFilters = () => {
    setFilterLevel("all");
    setFilterSource("all");
    setSearchTerm("");
  };

  // Apply level, module source, and keyword search (message, source, attrs)
  const filteredLogs = useMemo(() => {
    const term = searchTerm.trim().toLowerCase();

    return logs.filter((log) => {
      if (filterLevel !== "all" && log.level !== filterLevel) {
        return false;
      }
      if (filterSource !== "all" && log.source !== filterSource) {
        return false;
      }
      if (term) {
        const msgMatch = (log.message || "").toLowerCase().includes(term);
        const srcMatch = (log.source || "").toLowerCase().includes(term);
        let attrsMatch = false;
        if (log.attrs && typeof log.attrs === "object") {
          attrsMatch = Object.entries(log.attrs).some(
            ([k, v]) =>
              k.toLowerCase().includes(term) ||
              formatAttrValue(v).toLowerCase().includes(term)
          );
          if (!attrsMatch) {
            try {
              attrsMatch = JSON.stringify(log.attrs)
                .toLowerCase()
                .includes(term);
            } catch {
              attrsMatch = false;
            }
          }
        }
        if (!msgMatch && !srcMatch && !attrsMatch) {
          return false;
        }
      }
      return true;
    });
  }, [logs, filterLevel, filterSource, searchTerm]);

  const scrollToBottom = () => {
    logEndRef.current?.scrollIntoView({ behavior: "smooth" });
  };

  // Auto-scroll when streaming live without active filters
  useEffect(() => {
    if (autoRefresh && !hasActiveFilters) {
      scrollToBottom();
    }
  }, [filteredLogs.length, autoRefresh, hasActiveFilters]);

  const getLogLevelStyle = (level: string) => {
    switch (level) {
      case "ERROR":
        return "bg-red-500/15 text-red-600 dark:text-red-400 border border-red-500/30";
      case "WARNING":
        return "bg-amber-500/15 text-amber-600 dark:text-amber-400 border border-amber-500/30";
      case "INFO":
        return "bg-blue-500/15 text-blue-600 dark:text-blue-400 border border-blue-500/30";
      case "DEBUG":
        return "bg-purple-500/15 text-purple-600 dark:text-purple-400 border border-purple-500/30";
      default:
        return "bg-cyber-grid/30 text-cyber-muted border border-cyber-grid";
    }
  };

  const renderStatusBadge = () => {
    switch (streamStatus) {
      case "live":
        return (
          <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[11px] font-medium bg-emerald-500/15 text-emerald-600 dark:text-emerald-400 border border-emerald-500/30">
            <span className="w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
            {t("logs.status.live")}
          </span>
        );
      case "connecting":
        return (
          <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[11px] font-medium bg-sky-500/15 text-sky-600 dark:text-sky-400 border border-sky-500/30">
            <span className="w-2 h-2 rounded-full bg-sky-500 animate-pulse" />
            {t("logs.status.connecting")}
          </span>
        );
      case "reconnecting":
        return (
          <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[11px] font-medium bg-amber-500/15 text-amber-600 dark:text-amber-400 border border-amber-500/30">
            <span className="w-2 h-2 rounded-full bg-amber-500 animate-pulse" />
            {t("logs.status.reconnecting")}
          </span>
        );
      default:
        return (
          <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[11px] font-medium bg-cyber-grid/30 text-cyber-muted border border-cyber-grid">
            <PauseCircle className="w-3 h-3" />
            {t("logs.status.paused")}
          </span>
        );
    }
  };

  const latestTimeLabel = useMemo(() => {
    const last = logs[logs.length - 1];
    if (!last?.timestamp) return "";
    const d = new Date(last.timestamp);
    if (Number.isNaN(d.getTime())) return last.timestamp;
    const intlLocale =
      locale === "zh-CN" ? "zh-CN" : locale === "fr" ? "fr-FR" : "en-US";
    return d.toLocaleTimeString(intlLocale, { hour12: false });
  }, [logs, locale]);

  return (
    <div className="space-y-4 sm:space-y-5 min-w-0 max-w-full overflow-x-hidden">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
        <div>
          <h1
            className={`${
              isMobile ? "text-2xl" : "text-3xl"
            } font-bold mb-1 cyber-glow-text`}
          >
            {t("logs.title")}
          </h1>
          <p className="text-cyber-muted text-sm">{t("logs.subtitle")}</p>
        </div>

        <div className="flex items-center gap-2 self-start sm:self-auto">
          {renderStatusBadge()}
        </div>
      </div>

      {/* Filters and Controls */}
      <div className="cyber-panel p-3 sm:p-4 space-y-3 overflow-visible">
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-12 gap-2.5 sm:gap-3">
          {/* Keyword Search */}
          <div className="relative sm:col-span-2 lg:col-span-5">
            <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
              <Search className="w-4 h-4 text-cyber-muted" />
            </div>
            <input
              type="text"
              placeholder={
                isMobile
                  ? t("logs.searchPlaceholderMobile")
                  : t("logs.searchPlaceholder")
              }
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              className="cyber-input pl-9 w-full text-sm"
            />
          </div>

          {/* Source Module Filter */}
          <div className="lg:col-span-4">
            <select
              value={filterSource}
              onChange={(e) => setFilterSource(e.target.value)}
              className="cyber-input w-full text-sm"
              aria-label={t("logs.filter.allSources")}
            >
              <option value="all">{t("logs.filter.allSources")}</option>
              {LOG_SOURCES.map((mod) => (
                <option key={mod} value={mod}>
                  {t(`logs.source.${mod}`)}
                </option>
              ))}
            </select>
          </div>

          {/* Level Filter */}
          <div className="lg:col-span-3">
            <select
              value={filterLevel}
              onChange={(e) => setFilterLevel(e.target.value)}
              className="cyber-input w-full text-sm"
              aria-label={t("logs.filter.allLevels")}
            >
              <option value="all">{t("logs.filter.allLevels")}</option>
              {LOG_LEVELS.map((lvl) => (
                <option key={lvl} value={lvl}>
                  {lvl}
                </option>
              ))}
            </select>
          </div>
        </div>

        {/* Action Toolbar */}
        <div className="flex flex-wrap items-center justify-between gap-2 pt-2 border-t border-cyber-grid/60">
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={handleToggleLiveStream}
              className={`cyber-button text-xs flex items-center gap-1.5 px-3 py-1.5 ${
                autoRefresh
                  ? "bg-emerald-500/15 border-emerald-500/40 text-emerald-600 dark:text-emerald-400"
                  : ""
              }`}
            >
              {autoRefresh ? (
                <Radio className="w-3.5 h-3.5 animate-pulse" />
              ) : (
                <PauseCircle className="w-3.5 h-3.5" />
              )}
              <span>
                {isMobile ? t("logs.liveStreamShort") : t("logs.liveStream")}
              </span>
            </button>

            {hasActiveFilters && (
              <button
                type="button"
                onClick={resetFilters}
                className="text-xs text-cyber-accent hover:underline flex items-center gap-1"
              >
                <RotateCcw className="w-3 h-3" />
                <span>{t("logs.resetFilters")}</span>
              </button>
            )}
          </div>

          <div className="flex flex-wrap items-center gap-1.5 sm:gap-2">
            <button
              type="button"
              onClick={() => void fetchLogs({ isRefresh: true })}
              className="cyber-button text-xs flex items-center gap-1.5 px-2.5 sm:px-3 py-1.5"
              title={t("logs.refresh")}
              disabled={isLoading || isRefreshing}
            >
              <RefreshCw
                className={`w-3.5 h-3.5 flex-shrink-0 ${
                  isRefreshing ? "animate-spin" : ""
                }`}
              />
              <span>{t("logs.refresh")}</span>
            </button>

            <button
              type="button"
              onClick={() => void handleExportLogs("jsonl")}
              className="cyber-button text-xs flex items-center gap-1.5 px-2.5 sm:px-3 py-1.5"
              disabled={exportingFormat !== null || logs.length === 0}
              title={t("logs.exportJsonl")}
            >
              <Download className="w-3.5 h-3.5 flex-shrink-0" />
              <span>
                {exportingFormat === "jsonl" ? "..." : t("logs.exportJsonl")}
              </span>
            </button>

            <button
              type="button"
              onClick={() => void handleExportLogs("log")}
              className="cyber-button text-xs flex items-center gap-1.5 px-2.5 sm:px-3 py-1.5"
              disabled={exportingFormat !== null || logs.length === 0}
              title={t("logs.exportLog")}
            >
              <Download className="w-3.5 h-3.5 flex-shrink-0" />
              <span>
                {exportingFormat === "log" ? "..." : t("logs.exportLog")}
              </span>
            </button>

            <button
              type="button"
              onClick={() => void clearLogs()}
              className="cyber-button text-xs flex items-center gap-1.5 bg-red-500/10 border-red-500/35 text-red-600 dark:text-red-400 hover:bg-red-500/20 px-2.5 sm:px-3 py-1.5"
              disabled={isLoading || logs.length === 0}
              title={t("logs.clear")}
            >
              <Trash2 className="w-3.5 h-3.5 flex-shrink-0" />
              <span>{t("logs.clear")}</span>
            </button>
          </div>
        </div>
      </div>

      {/* Logs Display */}
      <div className="cyber-panel overflow-hidden max-w-full">
        <div className="flex justify-between items-center px-3 sm:px-4 py-2.5 border-b border-cyber-grid bg-cyber-dark/60">
          <h2 className="font-semibold text-xs sm:text-sm text-cyber-text flex items-center gap-2">
            <FileText className="w-4 h-4 text-cyber-accent" />
            <span>{t("logs.panelTitle")}</span>
          </h2>
          <div className="flex items-center gap-3">
            <div className="text-cyber-muted text-[11px] sm:text-xs font-mono">
              {hasActiveFilters
                ? t("logs.filteredCount", {
                    shown: filteredLogs.length,
                    total: logs.length,
                  })
                : t("logs.count", { count: logs.length })}
            </div>
            {latestTimeLabel && !isMobile && (
              <div className="text-[11px] text-cyber-accent/80 font-mono">
                {t("logs.latest", { time: latestTimeLabel })}
              </div>
            )}
            {filteredLogs.length > 0 && (
              <button
                type="button"
                onClick={scrollToBottom}
                className="text-cyber-muted hover:text-cyber-accent transition-colors"
                title="Scroll to latest"
              >
                <ArrowDown className="w-3.5 h-3.5" />
              </button>
            )}
          </div>
        </div>

        {isLoading && logs.length === 0 ? (
          <div className="p-4 animate-pulse space-y-3">
            {[...Array(10)].map((_, i) => (
              <div key={i} className="h-6 bg-cyber-grid/40 rounded" />
            ))}
          </div>
        ) : filteredLogs.length === 0 ? (
          <div className="p-10 text-center space-y-3">
            <FileText className="w-10 h-10 text-cyber-muted mx-auto opacity-60" />
            <p className="text-sm text-cyber-muted">{t("logs.empty")}</p>
            {hasActiveFilters && (
              <button
                type="button"
                onClick={resetFilters}
                className="cyber-button text-xs px-3 py-1.5 inline-flex items-center gap-1.5"
              >
                <RotateCcw className="w-3.5 h-3.5" />
                <span>{t("logs.resetFilters")}</span>
              </button>
            )}
          </div>
        ) : (
          <div className="max-h-[calc(100vh-300px)] sm:max-h-[calc(100vh-275px)] overflow-y-auto overflow-x-hidden overscroll-contain">
            {!isMobile && (
              <div className="sticky top-0 z-10 bg-cyber-surface/95 backdrop-blur-sm border-b border-cyber-grid px-3 sm:px-4 py-2">
                <div className="flex items-center gap-3 text-[11px] font-semibold uppercase tracking-wider text-cyber-muted">
                  <div className="flex-shrink-0 w-10 text-right">
                    {t("logs.col.index")}
                  </div>
                  <div className="flex-shrink-0 w-36">
                    {t("logs.col.time")}
                  </div>
                  <div className="flex-shrink-0 w-20">
                    {t("logs.col.level")}
                  </div>
                  <div className="flex-shrink-0 w-28">
                    {t("logs.col.source")}
                  </div>
                  <div className="flex-1 min-w-0">
                    {t("logs.col.message")}
                  </div>
                </div>
              </div>
            )}

            <div className="divide-y divide-cyber-grid/40">
              {filteredLogs.map((log, index) => {
                const timeFmt = formatLocalTimestamp(log.timestamp);
                const attrEntries =
                  log.attrs && typeof log.attrs === "object"
                    ? Object.entries(log.attrs)
                    : [];

                return (
                  <div
                    key={String(log.id)}
                    className="px-3 sm:px-4 py-2.5 hover:bg-cyber-dark/50 transition-colors overflow-hidden"
                  >
                    {isMobile ? (
                      <div className="space-y-1.5">
                        <div className="flex items-center justify-between gap-2">
                          <div className="flex items-center gap-2">
                            <span className="text-cyber-muted/70 font-mono text-[10px]">
                              #{index + 1}
                            </span>
                            <span
                              className={`inline-flex items-center justify-center px-1.5 py-0.5 rounded text-[10px] font-semibold ${getLogLevelStyle(
                                log.level
                              )}`}
                            >
                              {log.level}
                            </span>
                            <span className="px-1.5 py-0.5 rounded bg-cyber-accent/10 text-cyber-accent border border-cyber-accent/25 font-mono text-[10px]">
                              {log.source}
                            </span>
                          </div>
                          <div
                            className="text-cyber-muted font-mono text-[10px]"
                            title={timeFmt.title}
                          >
                            {timeFmt.display}
                          </div>
                        </div>
                        <div className="text-cyber-text text-xs leading-relaxed break-words">
                          {log.message}
                        </div>
                        {attrEntries.length > 0 && (
                          <div className="flex flex-wrap gap-1.5 pt-0.5">
                            {attrEntries.map(([k, v]) => (
                              <span
                                key={k}
                                className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-mono bg-cyber-dark border border-cyber-grid text-cyber-muted max-w-full"
                              >
                                <span className="text-cyber-accent font-medium">
                                  {k}=
                                </span>
                                <span className="text-cyber-text break-all">
                                  {formatAttrValue(v)}
                                </span>
                              </span>
                            ))}
                          </div>
                        )}
                      </div>
                    ) : (
                      <div className="flex items-start gap-3 text-xs min-w-0">
                        <div
                          className="flex-shrink-0 w-10 text-cyber-muted/70 text-right font-mono pt-0.5"
                          title={`ID: ${log.id}`}
                        >
                          #{index + 1}
                        </div>

                        <div
                          className="flex-shrink-0 w-36 text-cyber-muted font-mono pt-0.5"
                          title={timeFmt.title}
                        >
                          {timeFmt.display}
                        </div>

                        <div className="flex-shrink-0 w-20">
                          <span
                            className={`inline-flex items-center justify-center w-full px-1.5 py-0.5 rounded text-[10px] font-semibold ${getLogLevelStyle(
                              log.level
                            )}`}
                          >
                            {log.level}
                          </span>
                        </div>

                        <div
                          className="flex-shrink-0 w-28 truncate pt-0.5"
                          title={log.source}
                        >
                          <span className="inline-block px-1.5 py-0.5 rounded bg-cyber-accent/10 text-cyber-accent border border-cyber-accent/25 font-mono text-[11px] max-w-full truncate">
                            {log.source}
                          </span>
                        </div>

                        <div className="flex-1 min-w-0">
                          <div className="text-cyber-text leading-relaxed break-words">
                            {log.message}
                          </div>
                          {attrEntries.length > 0 && (
                            <div className="flex flex-wrap gap-1.5 mt-1.5">
                              {attrEntries.map(([k, v]) => (
                                <span
                                  key={k}
                                  className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-[11px] font-mono bg-cyber-dark border border-cyber-grid text-cyber-muted max-w-full"
                                >
                                  <span className="text-cyber-accent font-medium">
                                    {k}=
                                  </span>
                                  <span className="text-cyber-text break-all">
                                    {formatAttrValue(v)}
                                  </span>
                                </span>
                              ))}
                            </div>
                          )}
                        </div>
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
            <div ref={logEndRef} />
          </div>
        )}
      </div>
    </div>
  );
};

export default LogsPage;
