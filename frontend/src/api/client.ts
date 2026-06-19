import axios from 'axios'
import { useAuthStore } from '@/stores/auth'

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

// 响应拦截器：401 自动登出
client.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      useAuthStore.getState().clearAuth()
      window.location.href = '/login'
    }
    return Promise.reject(error)
  },
)

export default client
