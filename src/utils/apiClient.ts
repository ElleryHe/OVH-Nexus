/**
 * API 客户端。
 *
 * 浏览器身份由 HttpOnly 会话 Cookie 承载；
 * 这里只附加当前 OVH 账户 ID，不再保存或发送 API 密钥。
 */

import axios, { AxiosInstance, AxiosRequestConfig, AxiosError } from 'axios';
import { API_URL, API_TIMEOUT } from '@/config/constants';
import { toast } from 'sonner';

const LEGACY_KEY_STORAGE = 'api_secret_key';
const CURRENT_ACCOUNT_ID_KEY = 'current_account_id';

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
  (error: AxiosError) => {
    const status = error.response?.status;
    const url = error.config?.url || '';

    const code = (error.response?.data as any)?.code;
    if (status === 401 && code === 'AUTH_REQUIRED' && !url.includes('/auth/')) {
      window.dispatchEvent(new CustomEvent('auth:expired'));
      return Promise.reject(error);
    }

    if (error.response) {
      const message = (error.response.data as any)?.error || error.message;
      switch (status) {
        case 403:
          toast.error('访问被拒绝：权限不足');
          break;
        case 404: {
          const notAvailable = (error.response.data as any)?.notAvailable;
          if (!notAvailable) {
            // 404 通常由页面自行处理，保持静默
          }
          break;
        }
        case 400: {
          const notAvailable400 = (error.response.data as any)?.notAvailable;
          if (!notAvailable400) {
            console.error('API错误 [400]:', message);
          }
          break;
        }
        case 429:
          toast.error('请求过于频繁，请稍后重试');
          break;
        case 500: {
          const errorMsg = (error.response.data as any)?.error || '';
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
};
