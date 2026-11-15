import { useState } from "react";
import { Link } from "react-router-dom";
import { AlertTriangle, X } from "lucide-react";
import { useAPI } from "@/context/APIContext";
import { useLocale } from "@/context/LocaleContext";

const DISMISS_STORAGE_KEY = "ovh_nexus_api_notice_dismissed";

const APINotice = () => {
  const { isAuthenticated } = useAPI();
  const { t } = useLocale();
  const [dismissed, setDismissed] = useState<boolean>(() => {
    try {
      return sessionStorage.getItem(DISMISS_STORAGE_KEY) === "1";
    } catch {
      return false;
    }
  });

  if (isAuthenticated || dismissed) return null;

  const handleDismiss = () => {
    setDismissed(true);
    try {
      sessionStorage.setItem(DISMISS_STORAGE_KEY, "1");
    } catch {
      /* ignore */
    }
  };

  return (
    <div className="mb-5 cyber-panel px-4 py-3 border-amber-500/30 bg-amber-500/5">
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div className="flex items-start sm:items-center gap-2.5 min-w-0">
          <AlertTriangle size={17} className="text-amber-500 shrink-0 mt-0.5 sm:mt-0" />
          <div className="text-xs sm:text-sm text-cyber-text leading-relaxed">
            <span className="font-semibold text-amber-600 dark:text-amber-400 mr-1.5">
              {t("dashboard.apiNotConfigured")}:
            </span>
            <span className="text-cyber-muted">
              {t("dashboard.apiNotConfiguredDesc")}
            </span>
          </div>
        </div>
        <div className="flex items-center justify-end gap-2 shrink-0">
          <Link
            to="/api-accounts"
            className="cyber-button-primary text-xs px-3 py-1.5 inline-flex items-center justify-center whitespace-nowrap"
          >
            {t("dashboard.configureApi")}
          </Link>
          <button
            type="button"
            onClick={handleDismiss}
            title={t("dashboard.dismissNotice")}
            aria-label={t("dashboard.dismissNotice")}
            className="p-1.5 rounded-md text-cyber-muted hover:text-cyber-text hover:bg-cyber-grid/40 transition-colors"
          >
            <X size={15} />
          </button>
        </div>
      </div>
    </div>
  );
};

export default APINotice;
