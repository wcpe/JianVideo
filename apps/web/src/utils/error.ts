import { notifications } from '@mantine/notifications';

/** 从 axios/通用错误中提取用户可读信息 */
export function extractErrorMessage(error: unknown, fallback = '请求失败'): string {
  if (error && typeof error === 'object' && 'response' in error) {
    const resp = (error as { response?: { data?: { message?: string } } }).response;
    if (resp?.data?.message) return resp.data.message;
  }
  if (error instanceof Error && error.message) return error.message;
  return fallback;
}

/** 从 axios 响应体提取业务错误码（如 LOGIN_LOCKED） */
export function extractErrorCode(error: unknown): string | null {
  if (error && typeof error === 'object' && 'response' in error) {
    const resp = (error as { response?: { data?: { code?: string } } }).response;
    const code = resp?.data?.code;
    if (typeof code === 'string' && code.trim()) return code.trim();
  }
  return null;
}

/**
 * 从 axios 响应头提取 Retry-After（秒）。
 * 仅接受正整数秒；不解析 HTTP-date 形式（本仓库登录锁定只发秒数）。
 */
export function extractRetryAfterSeconds(error: unknown): number | null {
  if (!error || typeof error !== 'object' || !('response' in error)) return null;
  const headers = (error as { response?: { headers?: Record<string, unknown> } }).response
    ?.headers;
  if (!headers || typeof headers !== 'object') return null;
  const raw =
    headers['retry-after'] ??
    headers['Retry-After'] ??
    (typeof (headers as { get?: (k: string) => unknown }).get === 'function'
      ? (headers as { get: (k: string) => unknown }).get('retry-after')
      : undefined);
  if (raw == null) return null;
  const n = typeof raw === 'number' ? raw : parseInt(String(raw).trim(), 10);
  if (!Number.isFinite(n) || n <= 0) return null;
  return Math.floor(n);
}

/** 统一错误提示：显示红色 toast 并返回提取到的信息 */
export function handleApiError(error: unknown, fallback = '请求失败'): string {
  const message = extractErrorMessage(error, fallback);
  notifications.show({ title: '操作失败', message, color: 'red', autoClose: 4000 });
  return message;
}
