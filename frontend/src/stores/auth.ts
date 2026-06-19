import { create } from 'zustand'
import * as authApi from '@/api/auth'

const AUTH_KEY = 'jianvideo_auth'

interface AuthState {
  username: string | null
  isAuthenticated: boolean
  loading: boolean
  error: string | null
  /** 登录 */
  login: (username: string, password: string) => Promise<void>
  /** 登出 */
  logout: () => Promise<void>
  /** 初始化认证状态 */
  init: () => Promise<void>
  /** 清除认证 */
  clearAuth: () => void
  /** 清除错误 */
  clearError: () => void
}

export const useAuthStore = create<AuthState>((set) => ({
  username: null,
  isAuthenticated: false,
  loading: false,
  error: null,

  login: async (username, password) => {
    set({ loading: true, error: null })
    try {
      await authApi.login(username, password)
      // 后端使用 cookie 认证，store 中用布尔值标记已认证即可
      localStorage.setItem(AUTH_KEY, '1')
      set({ username, isAuthenticated: true, loading: false })
    } catch (err) {
      const msg = err instanceof Error ? err.message : '登录失败'
      set({ loading: false, error: msg })
      throw err
    }
  },

  logout: async () => {
    await authApi.logout()
    set({ username: null, isAuthenticated: false, error: null })
    localStorage.removeItem(AUTH_KEY)
  },

  init: async () => {
    const authed = localStorage.getItem(AUTH_KEY) === '1'
    if (!authed) {
      set({ isAuthenticated: false })
      return
    }
    try {
      const user = await authApi.getMe()
      set({ username: user.username, isAuthenticated: true })
    } catch {
      localStorage.removeItem(AUTH_KEY)
      set({ username: null, isAuthenticated: false })
    }
  },

  clearAuth: () => {
    localStorage.removeItem(AUTH_KEY)
    set({ username: null, isAuthenticated: false, error: null })
  },

  clearError: () => set({ error: null }),
}))
