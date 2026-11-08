import { Link, useLocation } from "react-router-dom";
import { motion } from "framer-motion";
import { useAPI } from "@/context/APIContext";
import { useLocale, Locale } from "@/context/LocaleContext";
import { useTheme, ThemeMode } from "@/context/ThemeContext";
import { NexusLogo } from "./NexusLogo";
import { useIsMobile } from "@/hooks/use-mobile";
import { logout } from "@/utils/apiClient";
import {
  LayoutDashboard,
  Terminal,
  HardDrive,
  Server,
  Globe,
  ListChecks,
  Bell,
  Cloud,
  History,
  UserCheck,
  FileText,
  Settings,
  Lock,
  Sun,
  Moon,
  Monitor,
  BellRing,
} from "lucide-react";

interface SidebarProps {
  onToggle: () => void;
  isOpen: boolean;
}

const LOCALE_OPTIONS: { code: Locale; label: string }[] = [
  { code: 'en', label: 'EN' },
  { code: 'fr', label: 'FR' },
  { code: 'zh-CN', label: '中文' },
];

const THEME_OPTIONS: { mode: ThemeMode; icon: any; key: string }[] = [
  { mode: 'light', icon: Sun, key: 'theme.light' },
  { mode: 'dark', icon: Moon, key: 'theme.dark' },
  { mode: 'system', icon: Monitor, key: 'theme.system' },
];

const Sidebar = ({ onToggle }: SidebarProps) => {
  const location = useLocation();
  const { isAuthenticated, accounts, currentAccountId, setCurrentAccount } = useAPI();
  const { locale, setLocale, t } = useLocale();
  const { theme, setTheme } = useTheme();
  const isMobile = useIsMobile();
  const currentZone = (accounts.find((acc: any) => acc?.id === currentAccountId)?.zone) || '';

  const navGroups = [
    {
      title: t('nav.group.overview'),
      items: [
        { path: "/", icon: LayoutDashboard, label: t('nav.dashboard') },
      ],
    },
    {
      title: t('nav.group.fleet'),
      items: [
        { path: "/server-control", icon: Terminal, label: t('nav.dedicated') },
        { path: "/account-management?tab=vps", matchPath: "/account-management", matchTab: "vps", icon: HardDrive, label: t('nav.vps') },
        { path: "/account-management", matchPath: "/account-management", excludeTab: "vps", icon: UserCheck, label: t('nav.account') },
      ],
    },
    {
      title: t('nav.group.procurement'),
      items: [
        { path: "/availability", icon: Globe, label: t('nav.availability') },
        { path: "/servers", icon: Server, label: t('nav.servers') },
        { path: "/queue", icon: ListChecks, label: t('nav.queue') },
        { path: "/monitor", icon: Bell, label: t('nav.monitor') },
        { path: "/vps-monitor", icon: Cloud, label: t('nav.vpsMonitor') },
        { path: "/history", icon: History, label: t('nav.history') },
      ],
    },
    {
      title: t('nav.group.system'),
      items: [
        { path: "/notifications", icon: BellRing, label: t('nav.notifications') },
        { path: "/logs", icon: FileText, label: t('nav.logs') },
        { path: "/settings", icon: Settings, label: t('nav.settings') },
      ],
    },
  ];

  const isItemActive = (item: any) => {
    const searchParams = new URLSearchParams(location.search);
    const currentTab = searchParams.get('tab');

    if (item.matchPath) {
      if (!location.pathname.startsWith(item.matchPath)) return false;
      if (item.matchTab) return currentTab === item.matchTab;
      if (item.excludeTab && currentTab === item.excludeTab) return false;
      return true;
    }

    if (item.path === "/" && location.pathname === "/") return true;
    if (item.path !== "/" && location.pathname.startsWith(item.path)) return true;
    return false;
  };

  return (
    <div className="w-72 h-full bg-cyber-surface flex flex-col border-r border-cyber-grid select-none">
      {/* Brand Header */}
      <div className="p-4 border-b border-cyber-grid flex items-center justify-between">
        <Link to="/" className="flex items-center space-x-3 group">
          <NexusLogo size={38} className="shadow-sm transition-transform duration-200 group-hover:scale-105" />
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-lg font-bold tracking-tight text-cyber-text">{t('brand.name')}</h1>
              <span className="text-[10px] font-semibold px-1.5 py-0.5 rounded bg-cyber-accent/10 text-cyber-accent border border-cyber-accent/25">
                v{typeof __APP_VERSION__ !== "undefined" ? __APP_VERSION__ : "1.0.0"}
              </span>
            </div>
            <div className="text-[11px] text-cyber-muted">{t('brand.shortSubtitle')}</div>
          </div>
        </Link>
      </div>

      {/* Grouped Navigation */}
      <div className="flex-1 overflow-y-auto py-3 px-3 space-y-5">
        {navGroups.map((group) => (
          <div key={group.title} className="space-y-1">
            <div className="px-3 pb-1 text-[11px] font-semibold uppercase tracking-wider text-cyber-muted">
              {group.title}
            </div>
            {group.items.map((item) => {
              const active = isItemActive(item);
              const Icon = item.icon;
              return (
                <Link
                  key={item.path}
                  to={item.path}
                  className={`flex items-center px-3 py-2 rounded-md text-sm transition-colors relative group ${
                    active
                      ? "text-cyber-accent bg-cyber-accent/10 border border-cyber-accent/25 font-semibold"
                      : "text-cyber-muted hover:text-cyber-text hover:bg-cyber-grid/40 border border-transparent"
                  }`}
                  onClick={isMobile ? onToggle : undefined}
                >
                  {active && (
                    <motion.div
                      layoutId="active-pill"
                      className="absolute left-0 w-1 h-5 bg-cyber-accent rounded-r-full"
                      transition={{ type: "spring", stiffness: 350, damping: 30 }}
                    />
                  )}
                  <Icon
                    size={16}
                    className={`mr-2.5 shrink-0 ${
                      active ? "text-cyber-accent" : "text-cyber-muted group-hover:text-cyber-text"
                    }`}
                  />
                  <span className="truncate">{item.label}</span>
                </Link>
              );
            })}
          </div>
        ))}
      </div>

      {/* Footer Status & Controls */}
      <div className="border-t border-cyber-grid p-3.5 bg-cyber-bg/60 space-y-2.5">
        {/* API Status + Theme Switcher */}
        <div className="flex items-center justify-between">
          <div className="flex items-center text-xs">
            <span
              className={`w-2 h-2 rounded-full mr-2 ${
                isAuthenticated ? "bg-emerald-500" : "bg-amber-500"
              }`}
            />
            <span className={isAuthenticated ? "text-emerald-600 dark:text-emerald-400 font-medium" : "text-cyber-muted"}>
              {isAuthenticated ? t('sidebar.connected') : t('sidebar.notConfigured')}
            </span>
          </div>

          <div className="flex items-center gap-0.5 bg-cyber-surface border border-cyber-grid rounded-md p-0.5">
            {THEME_OPTIONS.map((opt) => {
              const Icon = opt.icon;
              return (
                <button
                  key={opt.mode}
                  type="button"
                  onClick={() => setTheme(opt.mode)}
                  title={t(opt.key)}
                  className={`px-1.5 py-1 text-[11px] font-medium rounded inline-flex items-center gap-1 transition-colors ${
                    theme === opt.mode
                      ? "bg-cyber-accent text-white"
                      : "text-cyber-muted hover:text-cyber-text"
                  }`}
                >
                  <Icon size={12} />
                </button>
              );
            })}
          </div>
        </div>

        {/* Language Switcher */}
        <div className="flex items-center justify-between">
          <span className="text-[11px] text-cyber-muted">Language</span>
          <div className="flex items-center gap-0.5 bg-cyber-surface border border-cyber-grid rounded-md p-0.5">
            {LOCALE_OPTIONS.map((opt) => (
              <button
                key={opt.code}
                type="button"
                onClick={() => setLocale(opt.code)}
                className={`px-2 py-0.5 text-[11px] font-medium rounded transition-colors ${
                  locale === opt.code
                    ? "bg-cyber-accent text-white"
                    : "text-cyber-muted hover:text-cyber-text"
                }`}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>

        {/* Account Selector */}
        {accounts.length > 0 && (
          <div className="flex items-center justify-between gap-2">
            <select
              className="flex-1 text-xs bg-cyber-surface border border-cyber-grid rounded-md px-2.5 py-1.5 text-cyber-text focus:border-cyber-accent"
              value={currentAccountId || ""}
              onChange={(e) => setCurrentAccount(e.target.value)}
            >
              {accounts.map((acc: any) => (
                <option key={acc.id} value={acc.id}>
                  {acc.alias || acc.id}
                </option>
              ))}
            </select>
            {currentZone && (
              <span className="px-2 py-1 text-[10px] font-semibold uppercase rounded-md bg-cyber-accent/10 text-cyber-accent border border-cyber-accent/25">
                {currentZone}
              </span>
            )}
          </div>
        )}

        <button
          type="button"
          onClick={() => {
            void logout();
          }}
          className="w-full flex items-center justify-center gap-1.5 text-xs py-1.5 rounded-md border border-cyber-grid bg-cyber-surface text-cyber-muted hover:text-rose-600 dark:hover:text-rose-400 hover:border-rose-500/40 hover:bg-rose-500/10 transition-colors"
        >
          <Lock size={13} />
          <span>{t('sidebar.logout')}</span>
        </button>
      </div>
    </div>
  );
};

export default Sidebar;
