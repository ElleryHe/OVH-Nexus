import { useState, useEffect, useRef } from "react";
import { Link, Outlet, useLocation } from "react-router-dom";
import { AnimatePresence, motion } from "framer-motion";
import { Menu, X } from "lucide-react";
import Sidebar from "./Sidebar";
import { NexusLogo } from "./NexusLogo";
import { useAPI } from "@/context/APIContext";
import { useLocale } from "@/context/LocaleContext";
import APINotice from "./APINotice";

const COMPACT_BREAKPOINT = 1024; // Aligns strictly with Tailwind `lg:`

const Layout = () => {
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [isCompact, setIsCompact] = useState<boolean>(() =>
    typeof window !== "undefined" ? window.innerWidth < COMPACT_BREAKPOINT : false
  );
  const { isAuthenticated, isLoading } = useAPI();
  const { t } = useLocale();
  const location = useLocation();
  const touchStartX = useRef(0);

  useEffect(() => {
    const mql = window.matchMedia(`(max-width: ${COMPACT_BREAKPOINT - 1}px)`);
    const onChange = () => {
      const compact = window.innerWidth < COMPACT_BREAKPOINT;
      setIsCompact(compact);
      if (!compact) {
        setDrawerOpen(false);
      }
    };
    mql.addEventListener("change", onChange);
    onChange();
    return () => mql.removeEventListener("change", onChange);
  }, []);

  useEffect(() => {
    setDrawerOpen(false);
  }, [location.pathname, location.search]);

  useEffect(() => {
    if (!isCompact) return;
    const handleTouchStart = (e: TouchEvent) => {
      touchStartX.current = e.touches[0].clientX;
    };
    const handleTouchEnd = (e: TouchEvent) => {
      const distance = e.changedTouches[0].clientX - touchStartX.current;
      if (touchStartX.current < 24 && distance > 40 && !drawerOpen) {
        setDrawerOpen(true);
      } else if (distance < -50 && drawerOpen) {
        setDrawerOpen(false);
      }
    };
    document.addEventListener("touchstart", handleTouchStart);
    document.addEventListener("touchend", handleTouchEnd);
    return () => {
      document.removeEventListener("touchstart", handleTouchStart);
      document.removeEventListener("touchend", handleTouchEnd);
    };
  }, [isCompact, drawerOpen]);

  const hideApiNoticePaths = ["/server-control", "/api-accounts", "/settings"];
  const shouldShowApiNotice =
    !isLoading &&
    !isAuthenticated &&
    !hideApiNoticePaths.includes(location.pathname);

  return (
    <div className="min-h-screen flex flex-col bg-cyber-bg cyber-grid-bg text-cyber-text">
      {/* Subtle enterprise top accent border */}
      <div className="fixed top-0 left-0 right-0 h-[1px] bg-gradient-to-r from-blue-600/60 via-indigo-500/40 to-transparent z-50" />

      {/* Compact Top Navigation Bar (< 1024px) */}
      <header className="lg:hidden sticky top-0 z-30 h-14 px-4 bg-cyber-surface/95 backdrop-blur border-b border-cyber-grid flex items-center justify-between">
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={() => setDrawerOpen(true)}
            className="p-2 -ml-1 rounded-md border border-cyber-grid bg-cyber-bg/60 text-cyber-text hover:text-cyber-accent hover:border-cyber-accent/40 transition-colors"
            aria-label="打开导航菜单"
          >
            <Menu size={18} />
          </button>
          <Link to="/" className="flex items-center gap-2">
            <NexusLogo size={28} />
            <span className="font-bold text-sm tracking-tight text-cyber-text">
              {t("brand.name")}
            </span>
          </Link>
        </div>
        <Link
          to="/api-accounts"
          className="flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-md border border-cyber-grid bg-cyber-bg/60 text-cyber-muted hover:text-cyber-text transition-colors"
        >
          <span
            className={`w-2 h-2 rounded-full ${
              isAuthenticated ? "bg-emerald-500" : "bg-amber-500"
            }`}
          />
          <span>
            {isAuthenticated ? t("sidebar.connected") : t("sidebar.notConfigured")}
          </span>
        </Link>
      </header>

      <div className="flex-1 flex relative">
        {/* Desktop Permanent Sidebar (>= 1024px) */}
        <div className="hidden lg:block fixed inset-y-0 left-0 z-40">
          <Sidebar onToggle={() => {}} isOpen={true} />
        </div>

        {/* Mobile / Tablet Slide-out Sidebar (< 1024px) */}
        <AnimatePresence mode="wait">
          {isCompact && drawerOpen && (
            <>
              <motion.div
                initial={{ opacity: 0 }}
                animate={{ opacity: 0.55 }}
                exit={{ opacity: 0 }}
                className="fixed inset-0 bg-black z-40"
                onClick={() => setDrawerOpen(false)}
              />
              <motion.div
                initial={{ x: -290 }}
                animate={{ x: 0 }}
                exit={{ x: -290 }}
                transition={{ duration: 0.22, ease: "easeOut" }}
                className="fixed inset-y-0 left-0 z-50"
              >
                <Sidebar
                  onToggle={() => setDrawerOpen(false)}
                  isOpen={drawerOpen}
                />
                <button
                  type="button"
                  onClick={() => setDrawerOpen(false)}
                  className="absolute top-3.5 right-3.5 w-8 h-8 bg-cyber-bg border border-cyber-grid rounded-md flex items-center justify-center text-cyber-muted hover:text-cyber-text transition-colors"
                  aria-label="关闭导航菜单"
                >
                  <X size={16} />
                </button>
              </motion.div>
            </>
          )}
        </AnimatePresence>

        <main className="flex-1 py-6 px-4 sm:px-6 lg:ml-72 min-w-0 relative">
          <div className="container mx-auto max-w-7xl">
            {shouldShowApiNotice && <APINotice />}
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  );
};

export default Layout;
