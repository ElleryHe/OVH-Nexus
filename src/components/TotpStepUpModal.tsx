import React, { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate } from 'react-router-dom';
import { ShieldAlert, ShieldCheck, KeyRound, X, ArrowRight } from 'lucide-react';
import {
  TotpStepUpRequestDetail,
  TotpSetupRequiredDetail,
} from '@/utils/apiClient';

export const TotpStepUpModal: React.FC = () => {
  const navigate = useNavigate();
  const [stepUpQueue, setStepUpQueue] = useState<TotpStepUpRequestDetail[]>([]);
  const [setupPrompt, setSetupPrompt] = useState<TotpSetupRequiredDetail | null>(
    null
  );
  const [code, setCode] = useState('');
  const [errorText, setErrorText] = useState('');
  const inputRef = useRef<HTMLInputElement | null>(null);

  const activeRequest = stepUpQueue[0] || null;

  useEffect(() => {
    const onStepUp = (e: Event) => {
      const custom = e as CustomEvent<TotpStepUpRequestDetail>;
      if (custom.detail) {
        setStepUpQueue((prev) => [...prev, custom.detail]);
        setCode('');
        setErrorText('');
      }
    };

    const onSetupRequired = (e: Event) => {
      const custom = e as CustomEvent<TotpSetupRequiredDetail>;
      if (custom.detail) {
        setSetupPrompt(custom.detail);
      }
    };

    window.addEventListener('totp:step-up', onStepUp);
    window.addEventListener('totp:setup-required', onSetupRequired);
    return () => {
      window.removeEventListener('totp:step-up', onStepUp);
      window.removeEventListener('totp:setup-required', onSetupRequired);
    };
  }, []);

  useEffect(() => {
    if (activeRequest) {
      setTimeout(() => {
        inputRef.current?.focus();
      }, 40);
    }
  }, [activeRequest]);

  const handleCancelStepUp = () => {
    if (!activeRequest) return;
    activeRequest.reject(new Error('已取消 2FA 二次验证'));
    setStepUpQueue((prev) => prev.slice(1));
    setCode('');
    setErrorText('');
  };

  const handleConfirmStepUp = (e: React.FormEvent) => {
    e.preventDefault();
    if (!activeRequest) return;
    const trimmed = code.trim();
    if (!trimmed) {
      setErrorText('请输入 6 位验证器动态码或 8 位应急恢复码');
      return;
    }
    activeRequest.resolve(trimmed);
    setStepUpQueue((prev) => prev.slice(1));
    setCode('');
    setErrorText('');
  };

  if (!activeRequest && !setupPrompt) {
    return null;
  }

  return createPortal(
    <div className="fixed inset-0 z-[10000] flex items-center justify-center p-4 bg-black/70 backdrop-blur-sm">
      {/* Case 1: 2FA Not Bound Yet -> Prompt to Go to Settings */}
      {!activeRequest && setupPrompt && (
        <div
          className="cyber-panel w-full max-w-md p-6 space-y-4 border border-amber-500/40 shadow-2xl relative"
          onClick={(e) => e.stopPropagation()}
        >
          <button
            type="button"
            onClick={() => setSetupPrompt(null)}
            className="absolute top-4 right-4 text-cyber-muted hover:text-cyber-text"
          >
            <X size={18} />
          </button>

          <div className="flex items-start gap-3">
            <div className="p-2.5 rounded-lg bg-amber-500/15 border border-amber-500/30 text-amber-400 shrink-0">
              <ShieldAlert size={22} />
            </div>
            <div className="space-y-1">
              <h3 className="text-base font-bold text-cyber-text">
                需先启用双因素认证 (2FA)
              </h3>
              <span className="inline-block text-[11px] font-semibold px-2 py-0.5 rounded bg-amber-500/15 text-amber-400 border border-amber-500/30">
                高危操作安全门禁已拦截
              </span>
            </div>
          </div>

          <p className="text-xs text-cyber-muted leading-relaxed">
            {setupPrompt.message}
          </p>

          <div className="p-3 rounded-lg bg-cyber-bg/60 border border-cyber-grid text-[11px] text-cyber-muted space-y-1">
            <div className="font-semibold text-cyber-text">
              受 2FA 保护的高危操作包括：
            </div>
            <div>• 独服/VPS 重启、重装系统、销毁退订、过户转移、IPMI 控制台</div>
            <div>• 边缘防火墙规则、vRack/IP 漂移、API 密钥修改、MCP 权限变更</div>
            <div>• 抢购队列开启「自动扣款」或「放弃欧盟 14 天撤回权」</div>
          </div>

          <div className="flex items-center justify-end gap-2.5 pt-2">
            <button
              type="button"
              onClick={() => setSetupPrompt(null)}
              className="px-3.5 py-2 rounded text-xs font-medium border border-cyber-grid text-cyber-muted hover:text-cyber-text"
            >
              稍后再说
            </button>
            <button
              type="button"
              onClick={() => {
                setSetupPrompt(null);
                navigate('/settings#totp');
              }}
              className="px-4 py-2 rounded text-xs font-semibold bg-cyber-accent text-white hover:opacity-90 flex items-center gap-1.5"
            >
              <span>前往设置绑定 2FA</span>
              <ArrowRight size={14} />
            </button>
          </div>
        </div>
      )}

      {/* Case 2: Step-Up Verification Modal (`strict` or `sudo`) */}
      {activeRequest && (
        <form
          onSubmit={handleConfirmStepUp}
          className="cyber-panel w-full max-w-md p-6 space-y-4 border border-cyber-accent/40 shadow-2xl relative"
          onClick={(e) => e.stopPropagation()}
        >
          <button
            type="button"
            onClick={handleCancelStepUp}
            className="absolute top-4 right-4 text-cyber-muted hover:text-cyber-text"
          >
            <X size={18} />
          </button>

          <div className="flex items-start gap-3">
            <div
              className={`p-2.5 rounded-lg border shrink-0 ${
                activeRequest.tier === 'strict'
                  ? 'bg-rose-500/15 border-rose-500/35 text-rose-400'
                  : 'bg-cyber-accent/15 border-cyber-accent/35 text-cyber-accent'
              }`}
            >
              {activeRequest.tier === 'strict' ? (
                <ShieldAlert size={22} />
              ) : (
                <ShieldCheck size={22} />
              )}
            </div>
            <div className="space-y-1">
              <h3 className="text-base font-bold text-cyber-text">
                {activeRequest.tier === 'strict'
                  ? '高危操作严格二次确认 (Strict 2FA)'
                  : '高危运维 Sudo 二次认证'}
              </h3>
              <span
                className={`inline-block text-[11px] font-semibold px-2 py-0.5 rounded border ${
                  activeRequest.tier === 'strict'
                    ? 'bg-rose-500/15 text-rose-400 border-rose-500/30'
                    : 'bg-emerald-500/15 text-emerald-400 border-emerald-500/30'
                }`}
              >
                {activeRequest.tier === 'strict'
                  ? '不可逆操作 · 每次执行均需输入最新动态码'
                  : 'Sudo 提权模式 · 验证一次后 5 分钟内免重复输入'}
              </span>
            </div>
          </div>

          <p className="text-xs text-cyber-muted leading-relaxed">
            {activeRequest.message}
          </p>

          <div className="space-y-2">
            <label className="block text-xs font-semibold text-cyber-text flex items-center gap-1.5">
              <KeyRound size={13} className="text-cyber-accent" />
              <span>6 位 TOTP 动态码 或 8 位应急恢复码</span>
            </label>
            <input
              ref={inputRef}
              type="text"
              inputMode="text"
              autoComplete="one-time-code"
              value={code}
              onChange={(e) => {
                setCode(e.target.value);
                if (errorText) setErrorText('');
              }}
              placeholder="例如: 123456 或 A1B2-C3D4"
              className="cyber-input w-full py-2.5 text-center font-mono text-base tracking-widest"
            />
            {errorText && (
              <p className="text-xs text-rose-400">{errorText}</p>
            )}
          </div>

          <div className="flex items-center justify-end gap-2.5 pt-2">
            <button
              type="button"
              onClick={handleCancelStepUp}
              className="px-3.5 py-2 rounded text-xs font-medium border border-cyber-grid text-cyber-muted hover:text-cyber-text"
            >
              取消操作
            </button>
            <button
              type="submit"
              className={`px-4 py-2 rounded text-xs font-semibold text-white transition-opacity hover:opacity-90 ${
                activeRequest.tier === 'strict'
                  ? 'bg-rose-600'
                  : 'bg-cyber-accent'
              }`}
            >
              验证并继续执行
            </button>
          </div>
        </form>
      )}
    </div>,
    document.body
  );
};

export default TotpStepUpModal;
