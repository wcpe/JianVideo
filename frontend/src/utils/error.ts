import { notifications } from '@mantine/notifications'

/** 从 axios/通用错误中提取用户可读信息 */
export function extractErrorMessage(error: unknown, fallback = '请求失败'): string {
  if (error && typeof error === 'object' && 'response' in error) {
    const resp = (error as { response?: { data?: { message?: string } } }).response
    if (resp?.data?.message) return resp.data.message
  }
  if (error instanceof Error && error.message) return error.message
  return fallback
}

/** 统一错误提示：显示红色 toast 并返回提取到的信息 */
export function handleApiError(error: unknown, fallback = '请求失败'): string {
  const message = extractErrorMessage(error, fallback)
  notifications.show({ title: '操作失败', message, color: 'red', autoClose: 4000 })
  return message
}
