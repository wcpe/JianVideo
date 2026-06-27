import { create } from 'zustand'
import * as authApi from '@/api/auth'

const AUTH_KEY = 'jianvideo_auth'

interface AuthState {
  username: string | null
  isAuthenticated: boolean
  loading: boolean
  error: string | null
  /** 是否已完成首次认证初始化（init 执行完毕后置 true） */
  initialized: boolean
  /** 系统是否需要首次初始化（尚无任何用户，FR-109） */
  needsSetup: boolean
  /** JWT token（可选，用于 Authorization header） */
  token?: string
  /** 登录 */
  login: (username: string, password: string) => Promise<void>
  /** 首次初始化：创建首个账户并自动登录（FR-109） */
  setup: (username: string, password: string) => Promise<void>
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
  initialized: false,
  needsSetup: false,

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

  setup: async (username, password) => {
    set({ loading: true, error: null })
    try {
      await authApi.setup(username, password)
      // 初始化成功：后端已签发 cookie 自动登录，标记已认证、不再需初始化
      localStorage.setItem(AUTH_KEY, '1')
      set({ username, isAuthenticated: true, needsSetup: false, loading: false })
    } catch (err) {
      const msg = err instanceof Error ? err.message : '初始化失败'
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
    // 先查是否需首次初始化（FR-109）：无任何用户则导向初始化引导，跳过登录态恢复
    try {
      const { needs_setup } = await authApi.getSetupStatus()
      if (needs_setup) {
        localStorage.removeItem(AUTH_KEY)
        set({ needsSetup: true, isAuthenticated: false, username: null, initialized: true })
        return
      }
    } catch {
      // 查询失败不阻塞登录流程：按「无需初始化」继续走认证恢复
    }

    const authed = localStorage.getItem(AUTH_KEY) === '1'
    if (!authed) {
      // 未认证分支：初始化完成
      set({ needsSetup: false, isAuthenticated: false, initialized: true })
      return
    }
    try {
      const user = await authApi.getMe()
      // getMe 成功：恢复登录态并标记初始化完成
      set({ username: user.username, isAuthenticated: true, initialized: true })
    } catch {
      // getMe 失败：清除本地凭据并标记初始化完成
      localStorage.removeItem(AUTH_KEY)
      set({ username: null, isAuthenticated: false, initialized: true })
    }
  },

  clearAuth: () => {
    localStorage.removeItem(AUTH_KEY)
    set({ username: null, isAuthenticated: false, error: null })
  },

  clearError: () => set({ error: null }),
}))
