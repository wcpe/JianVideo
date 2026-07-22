import axios from 'axios';
import { useAuthStore } from '@/stores/auth';
import { extractErrorMessage, handleApiError } from '@/utils/error';

// 静默标记（FR-115 后续修复·通知白屏）：后台轮询类请求（扫描任务/更新进度/更新检查等）
// 持续失败会每周期刷一条网络 toast，无上限堆积撑爆 DOM 致白屏。给这类请求 config 标 silent=true，
// 拦截器对其网络错误不弹全局 toast（轮询失败本就静默重试，用户无需逐条提示）。
// 经模块增强把 silent 注入 axios 请求配置类型，调用方可直接传 `{ silent: true }` 而无需类型断言。
declare module 'axios' {
  export interface AxiosRequestConfig {
    silent?: boolean;
  }
}

// 错误处理分工：
// - 401（登录/初始化/改密等认证表单接口）：不跳转、不 toast，交由页面/store 展示 message。
// - 401（其余受保护接口）：清除认证、toast 展示后端原因，并用 SPA 软跳 /login（禁止硬刷）。
// - 网络错误/无响应（超时、连接失败）：在此统一弹一次 toast，调用方通常无从感知。
// - 有响应的其它 4xx/5xx：此处不自动 toast，交由调用方按需用 handleApiError。

/** 由页面自行展示错误的认证表单路径（失败时不应整页跳转） */
const AUTH_FORM_PATHS = ['/api/auth/login', '/api/auth/setup', '/api/me/password'] as const;

/** 判断是否为认证表单请求（登录失败等必须把 message 留给 UI） */
function isAuthFormRequest(url: string | undefined): boolean {
  if (!url) return false;
  return AUTH_FORM_PATHS.some((path) => url.includes(path));
}

/**
 * 当前路径读取（抽成对象便于单测 stub；jsdom 的 location.pathname 不可 redefine）。
 * 生产代码只读 window.location.pathname。
 */
export const locationPath = {
  get: (): string => window.location.pathname,
};

/** SPA 软跳登录页：保留 React 状态与 toast，避免 location.href 整页刷新。 */
function softNavigateToLogin(): void {
  const path = locationPath.get();
  if (
    path === '/login' ||
    path.startsWith('/login/') ||
    path === '/setup' ||
    path.startsWith('/setup/')
  ) {
    return;
  }
  window.history.pushState({}, '', '/login');
  // 使用通用 Event，兼容 jsdom / 旧浏览器（无需 PopStateEvent 构造器）
  window.dispatchEvent(new Event('popstate'));
}

/** Axios 实例 — 同端口代理，认证拦截 */
const client = axios.create({
  baseURL: '',
  timeout: 15000,
});

// 请求拦截器：自动附加 token
client.interceptors.request.use((config) => {
  const token = useAuthStore.getState().token as string | undefined;
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

// 响应拦截器：401 按场景分流；网络错误统一提示
client.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      // 登录/初始化/改密失败：不跳转，让调用方提取 message 展示
      if (!isAuthFormRequest(error.config?.url)) {
        // 会话失效或未登录访问受保护资源：清态 + 说明原因 + 软跳登录
        const reason = extractErrorMessage(error, '请先登录');
        useAuthStore.getState().clearAuth();
        if (!error.config?.silent) {
          handleApiError(error, reason);
        }
        softNavigateToLogin();
      }
    } else if (!error.response) {
      // 无响应：网络异常/超时/连接失败，统一弹一次 toast；
      // 但带 silent 标记的后台轮询请求不弹（避免持续失败时 toast 无上限堆积致白屏）
      if (!error.config?.silent) {
        handleApiError(error, '网络异常，请检查连接后重试');
      }
    }
    return Promise.reject(error);
  },
);

export default client;
