/**
 * API 客户端。
 *
 * 浏览器身份由 HttpOnly 会话 Cookie 承载；
 * 这里附加当前 OVH 账户 ID，并内置 2FA (TOTP) 高危操作二次验证拦截器：
 * - 当后端返回 403 + `TOTP_SETUP_REQUIRED` 时，弹出引导绑定 2FA 模态框；
 * - 当后端返回 403 + `TOTP_STEP_UP_REQUIRED` (`strict` / `sudo`) 时，
 *   自动唤起全局 2FA 验证弹窗，用户输入 6 位动态码或 8 位恢复码后自动附加 `X-TOTP-Code` 重放原请求。
 */

import axios, { AxiosInstance, AxiosRequestConfig, AxiosError } from 'axios';
import { API_URL, API_TIMEOUT } from '@/config/constants';
import type { PlanConfigsResponse } from '@/types';
import { toast } from 'sonner';

const LEGACY_KEY_STORAGE = 'api_secret_key';
const CURRENT_ACCOUNT_ID_KEY = 'current_account_id';

export interface TotpStepUpRequestDetail {
  tier: 'strict' | 'sudo';
  message: string;
  resolve: (code: string) => void;
  reject: (reason?: any) => void;
}

export interface TotpSetupRequiredDetail {
  tier?: 'strict' | 'sudo';
  message: string;
}

export const requestTotpStepUp = (
  tier: 'strict' | 'sudo',
  message: string
): Promise<string> => {
  return new Promise<string>((resolve, reject) => {
    const detail: TotpStepUpRequestDetail = {
      tier,
      message,
      resolve,
      reject,
    };
    window.dispatchEvent(new CustomEvent('totp:step-up', { detail }));
  });
};

// 清除旧版本遗留在 localStorage 的明文密钥
try {
  localStorage.removeItem(LEGACY_KEY_STORAGE);
} catch {
  /* ignore */
}

export const getCurrentAccountId = (): string => {
  return localStorage.getItem(CURRENT_ACCOUNT_ID_KEY) || '';
};

export const setCurrentAccountId = (id: string): void => {
  localStorage.setItem(CURRENT_ACCOUNT_ID_KEY, id);
};

export const clearCurrentAccountId = (): void => {
  localStorage.removeItem(CURRENT_ACCOUNT_ID_KEY);
};

/** 登录相关请求使用独立实例，避免 401 拦截器递归触发。 */
export const authApi: AxiosInstance = axios.create({
  baseURL: API_URL,
  timeout: API_TIMEOUT,
  withCredentials: true,
  headers: { 'Content-Type': 'application/json' },
});

const apiClient: AxiosInstance = axios.create({
  baseURL: API_URL,
  timeout: API_TIMEOUT,
  withCredentials: true,
  headers: { 'Content-Type': 'application/json' },
});

apiClient.interceptors.request.use(
  (config) => {
    const accId = getCurrentAccountId();
    if (accId) {
      config.headers['X-OVH-Account'] = accId;
    }
    return config;
  },
  (error) => Promise.reject(error)
);

apiClient.interceptors.response.use(
  (response) => response,
  async (error: AxiosError) => {
    const status = error.response?.status;
    const url = error.config?.url || '';
    const respData = (error.response?.data || {}) as Record<string, any>;
    const code = respData?.code;

    if (status === 401 && code === 'AUTH_REQUIRED' && !url.includes('/auth/')) {
      window.dispatchEvent(new CustomEvent('auth:expired'));
      return Promise.reject(error);
    }

    // 2FA Gatekeeper: 尚未绑定 2FA 时拦截高危操作并引导前往设置页
    if (status === 403 && code === 'TOTP_SETUP_REQUIRED') {
      const msg =
        respData?.message ||
        '安全策略限制：未启用双因素认证 (2FA) 时禁止执行高危操作。请前往「系统设置 → 双因素认证」完成绑定。';
      window.dispatchEvent(
        new CustomEvent<TotpSetupRequiredDetail>('totp:setup-required', {
          detail: {
            tier: respData?.tier,
            message: msg,
          },
        })
      );
      toast.warning(msg);
      return Promise.reject(error);
    }

    // 2FA Gatekeeper: 已启用 2FA，需要输入动态码进行 Step-Up (strict 每次必验 / sudo 5分钟免密窗口)
    if (
      status === 403 &&
      code === 'TOTP_STEP_UP_REQUIRED' &&
      error.config &&
      !(error.config as any)._totpRetried
    ) {
      try {
        const tier: 'strict' | 'sudo' =
          respData?.tier === 'strict' ? 'strict' : 'sudo';
        const promptMsg =
          respData?.message ||
          (tier === 'strict'
            ? '该操作属于不可逆高危操作，必须输入当前最新的 6 位 2FA 动态码或应急恢复码进行二次确认。'
            : '执行此高危运维操作需验证 2FA 动态码（验证通过后 5 分钟内免重复输入）。');

        const totpCode = await requestTotpStepUp(tier, promptMsg);
        const retryConfig: AxiosRequestConfig & { _totpRetried?: boolean } = {
          ...error.config,
          _totpRetried: true,
          headers: {
            ...(error.config.headers || {}),
            'X-TOTP-Code': totpCode,
          },
        };
        const retryResp = await apiClient.request(retryConfig);
        window.dispatchEvent(new CustomEvent('totp:sudo-updated'));
        return retryResp;
      } catch (stepUpCancelOrErr) {
        return Promise.reject(stepUpCancelOrErr || error);
      }
    }

    if (error.response) {
      const message =
        respData?.message || respData?.error || error.message;
      switch (status) {
        case 403:
          toast.error(message || '访问被拒绝：权限不足');
          break;
        case 404: {
          const notAvailable = respData?.notAvailable;
          if (!notAvailable) {
            // 404 通常由页面自行处理，保持静默
          }
          break;
        }
        case 400: {
          const notAvailable400 = respData?.notAvailable;
          if (!notAvailable400) {
            console.error('API错误 [400]:', message);
          }
          break;
        }
        case 429:
          toast.error(respData?.message || '请求过于频繁，请稍后重试');
          break;
        case 500: {
          const errorMsg = respData?.error || '';
          const isFeatureUnsupported =
            errorMsg.includes('does not exist') ||
            errorMsg.includes('not available') ||
            errorMsg.includes('not supported');
          if (!isFeatureUnsupported) toast.error('服务器错误');
          break;
        }
        default:
          console.error(`API错误 [${status}]:`, message);
      }
    } else if (error.request) {
      console.error('网络错误:', error.request);
      toast.error('网络连接失败，请检查网络');
    } else {
      console.error('请求配置错误:', error.message);
    }

    return Promise.reject(error);
  }
);

export const logout = async (): Promise<void> => {
  try {
    await authApi.post('/auth/logout');
  } catch {
    /* 忽略网络错误，前端仍然退出 */
  }
  clearCurrentAccountId();
  window.dispatchEvent(new CustomEvent('auth:logout'));
};

export default apiClient;

export const api = {
  get: <T = any>(url: string, config?: AxiosRequestConfig) =>
    apiClient.get<T>(url, config),
  post: <T = any>(url: string, data?: any, config?: AxiosRequestConfig) =>
    apiClient.post<T>(url, data, config),
  put: <T = any>(url: string, data?: any, config?: AxiosRequestConfig) =>
    apiClient.put<T>(url, data, config),
  delete: <T = any>(url: string, config?: AxiosRequestConfig) =>
    apiClient.delete<T>(url, config),
  patch: <T = any>(url: string, data?: any, config?: AxiosRequestConfig) =>
    apiClient.patch<T>(url, data, config),
  /**
   * Fetch configuration-level options, pricing, and datacenter stock for a server plan.
   * Calls GET /api/catalog/plans/${encodeURIComponent(planCode)}/configs
   */
  getPlanConfigs: (planCode: string, subsidiary?: string) =>
    apiClient.get<PlanConfigsResponse>(
      `/catalog/plans/${encodeURIComponent(planCode)}/configs`,
      {
        params: subsidiary ? { subsidiary } : undefined,
      }
    ),
};
