import axios from 'axios'
import { useAuthStore } from '@/stores/auth'
import { handleApiError } from '@/utils/error'

// 静默标记（FR-115 后续修复·通知白屏）：后台轮询类请求（扫描任务/更新进度/更新检查等）
// 持续失败会每周期刷一条网络 toast，无上限堆积撑爆 DOM 致白屏。给这类请求 config 标 silent=true，
// 拦截器对其网络错误不弹全局 toast（轮询失败本就静默重试，用户无需逐条提示）。
// 经模块增强把 silent 注入 axios 请求配置类型，调用方可直接传 `{ silent: true }` 而无需类型断言。
declare module 'axios' {
  export interface AxiosRequestConfig {
    silent?: boolean
  }
}

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
      // 无响应：网络异常/超时/连接失败，统一弹一次 toast；
      // 但带 silent 标记的后台轮询请求不弹（避免持续失败时 toast 无上限堆积致白屏）
      if (!error.config?.silent) {
        handleApiError(error, '网络异常，请检查连接后重试')
      }
    }
    return Promise.reject(error)
  },
)

export default client
