import axios from 'axios'
import { useAuthStore } from '@/stores/auth'
import { handleApiError } from '@/utils/error'

// 错误处理分工：
// - 401：清除认证并跳转登录页，不额外 toast（避免与跳转重复打扰）。
// - 网络错误/无响应（超时、连接失败）：在此统一弹一次 toast，调用方通常无从感知。
// - 有响应的 4xx/5xx：此处不自动 toast，交由调用方按需用 handleApiError，
//   避免与各组件已有的错误处理重复弹两次。

/** Axios 实例 — 同端口代理，认证拦截 */
const client = axios.create({
  baseURL: '',
  timeout: 15000,
})

// 请求拦截器：自动附加 token
client.interceptors.request.use((config) => {
  const token = useAuthStore.getState().token as string | undefined
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// 响应拦截器：401 自动登出；网络错误统一提示
client.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      useAuthStore.getState().clearAuth()
      window.location.href = '/login'
    } else if (!error.response) {
      // 无响应：网络异常/超时/连接失败，统一弹一次 toast
      handleApiError(error, '网络异常，请检查连接后重试')
    }
    return Promise.reject(error)
  },
)

export default client
