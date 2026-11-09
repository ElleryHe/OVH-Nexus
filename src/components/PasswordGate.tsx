import { ReactNode, useEffect, useState } from 'react';
import { authApi } from '@/utils/apiClient';
import { useLocale, Locale } from '@/context/LocaleContext';
import { useTheme, ThemeMode } from '@/context/ThemeContext';
import { Sun, Moon, Monitor } from 'lucide-react';
import { NexusLogo } from './NexusLogo';

interface PasswordGateProps {
  children: ReactNode;
}

type GateState = 'checking' | 'out' | 'in';

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

const PasswordGate: React.FC<PasswordGateProps> = ({ children }) => {
  const [state, setState] = useState<GateState>('checking');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const { locale, setLocale, t } = useLocale();
  const { theme, setTheme } = useTheme();

  useEffect(() => {
    let alive = true;
    authApi
      .get('/auth/status')
      .then((res) => {
        if (alive) setState(res.data?.authenticated ? 'in' : 'out');
      })
      .catch(() => {
        if (alive) setState('out');
      });
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    const reset = () => setState('out');
    window.addEventListener('auth:expired', reset);
    window.addEventListener('auth:logout', reset);
    return () => {
      window.removeEventListener('auth:expired', reset);
      window.removeEventListener('auth:logout', reset);
    };
  }, []);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!password) {
      setError(t('auth.error.empty'));
      return;
    }
    setBusy(true);
    setError('');
    try {
      await authApi.post('/auth/login', { password });
      setPassword('');
      setState('in');
    } catch (err: any) {
      if (err?.response?.status === 429) {
        setError(err.response.data?.message || t('auth.error.invalid'));
      } else {
        setError(t('auth.error.invalid'));
      }
    } finally {
      setBusy(false);
    }
  };

  if (state === 'checking') {
    return (
      <div className="min-h-screen flex items-center justify-center bg-cyber-bg text-cyber-muted">
        <div className="animate-spin w-8 h-8 border-2 border-cyber-accent border-t-transparent rounded-full" />
      </div>
    );
  }

  if (state === 'out') {
    return (
      <div className="min-h-screen flex flex-col items-center justify-center bg-cyber-bg px-4 cyber-grid-bg relative">
        <div className="absolute top-6 right-6 flex items-center gap-2">
          {/* Theme Switcher */}
          <div className="flex items-center gap-0.5 bg-cyber-surface border border-cyber-grid rounded-lg p-1 shadow-sm">
            {THEME_OPTIONS.map((opt) => {
              const Icon = opt.icon;
              return (
                <button
                  key={opt.mode}
                  type="button"
                  onClick={() => setTheme(opt.mode)}
                  title={t(opt.key)}
                  className={`px-2 py-1 text-xs font-medium rounded-md inline-flex items-center gap-1 transition-colors ${
                    theme === opt.mode
                      ? 'bg-cyber-accent text-white'
                      : 'text-cyber-muted hover:text-cyber-text'
                  }`}
                >
                  <Icon size={13} />
                  <span className="hidden sm:inline">{t(opt.key)}</span>
                </button>
              );
            })}
          </div>

          {/* Language Switcher */}
          <div className="flex items-center gap-0.5 bg-cyber-surface border border-cyber-grid rounded-lg p-1 shadow-sm">
            {LOCALE_OPTIONS.map((opt) => (
              <button
                key={opt.code}
                type="button"
                onClick={() => setLocale(opt.code)}
                className={`px-2.5 py-1 text-xs font-medium rounded-md transition-colors ${
                  locale === opt.code
                    ? 'bg-cyber-accent text-white'
                    : 'text-cyber-muted hover:text-cyber-text'
                }`}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>

        <form
          onSubmit={submit}
          className="w-full max-w-md cyber-panel p-8 space-y-6 shadow-lg"
          autoComplete="off"
        >
          <div className="flex flex-col items-center text-center space-y-2">
            <div className="mb-1">
              <NexusLogo size={52} className="shadow-md" />
            </div>
            <h1 className="text-2xl font-bold tracking-tight text-cyber-text">
              {t('brand.name')}
            </h1>
            <p className="text-xs font-semibold text-cyber-accent">
              {t('brand.subtitle')}
            </p>
            <p className="text-xs text-cyber-muted pt-1">
              {t('auth.subtitle')}
            </p>
          </div>

          <div className="space-y-2">
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="cyber-input w-full py-2.5"
              placeholder={t('auth.placeholder')}
              autoFocus
              autoComplete="current-password"
            />
            {error && <p className="text-xs text-rose-500">{error}</p>}
          </div>

          <button
            type="submit"
            disabled={busy}
            className="w-full py-2.5 px-4 rounded-md text-sm font-medium bg-cyber-accent hover:opacity-90 text-white transition-all disabled:opacity-50"
          >
            {busy ? t('auth.verifying') : t('auth.submit')}
          </button>

          <p className="text-[11px] text-cyber-muted text-center pt-1">
            {t('brand.disclaimer')}
          </p>
        </form>
      </div>
    );
  }

  return <>{children}</>;
};

export default PasswordGate;
