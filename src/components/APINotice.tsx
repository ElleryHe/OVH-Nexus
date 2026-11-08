import { Link } from "react-router-dom";
import { motion } from "framer-motion";
import { useAPI } from "@/context/APIContext";
import { useLocale } from "@/context/LocaleContext";

const APINotice = () => {
  const { isAuthenticated } = useAPI();
  const { t } = useLocale();
  if (isAuthenticated) return null;

  return (
    <motion.div
      initial={{ opacity: 0, y: -10 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.25 }}
      className="mb-6 cyber-panel p-4 border-amber-500/30 bg-amber-500/5"
    >
      <div className="flex flex-col md:flex-row items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <svg
            xmlns="http://www.w3.org/2000/svg"
            width="20"
            height="20"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            className="text-amber-500 shrink-0"
          >
            <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"></path>
            <line x1="12" y1="9" x2="12" y2="13"></line>
            <line x1="12" y1="17" x2="12.01" y2="17"></line>
          </svg>
          <div className="text-xs sm:text-sm text-cyber-text">
            <span className="font-semibold text-amber-600 dark:text-amber-400 mr-2">
              {t('dashboard.apiNotConfigured')}:
            </span>
            <span className="text-cyber-muted">{t('dashboard.apiNotConfiguredDesc')}</span>
          </div>
        </div>
        <Link
          to="/settings"
          className="cyber-button-primary text-xs px-3 py-1.5 shrink-0 flex items-center justify-center"
        >
          {t('dashboard.configureApi')}
        </Link>
      </div>
    </motion.div>
  );
};

export default APINotice;
