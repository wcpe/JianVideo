import client from './client'

// 使用构建时环境变量决定是否启用 mock 模式
const useMock = import.meta.env.VITE_USE_MOCK === 'true'

// ─── 真实 API ────────────────────────────────────────

async function realLogin(username: string, password: string): Promise<void> {
  await client.post('/api/auth/login', { username, password })
}

async function realLogout(): Promise<void> {
  try { await client.post('/api/auth/logout') } catch { /* 静默 */ }
}

async function realGetMe(): Promise<{ username: string }> {
  const res = await client.get('/api/me')
  return res.data
}

async function realGetSetupStatus(): Promise<{ needs_setup: boolean }> {
  const res = await client.get('/api/auth/setup-status')
  return res.data
}

async function realSetup(username: string, password: string): Promise<void> {
  await client.post('/api/auth/setup', { username, password })
}

// ─── Mock API ────────────────────────────────────────

function mockDelay(ms: number): Promise<void> {
  return new Promise(r => setTimeout(r, ms))
}

async function mockLogin(username: string, password: string): Promise<void> {
  await mockDelay(300)
  if (username === 'admin' && password === 'admin') return
  throw new Error('用户名或密码错误')
}

async function mockLogout(): Promise<void> {
  await mockDelay(100)
}

async function mockGetMe(): Promise<{ username: string }> {
  await mockDelay(100)
  return { username: 'admin' }
}

async function mockGetSetupStatus(): Promise<{ needs_setup: boolean }> {
  await mockDelay(100)
  // mock 模式默认视为已初始化（admin 存在），不打扰本地开发
  return { needs_setup: false }
}

async function mockSetup(_username: string, _password: string): Promise<void> {
  await mockDelay(300)
}

// ─── 导出（构建时决定 mock 模式）──────────────────────

export function login(username: string, password: string) { return useMock ? mockLogin(username, password) : realLogin(username, password) }
export function logout() { return useMock ? mockLogout() : realLogout() }
export function getMe() { return useMock ? mockGetMe() : realGetMe() }
export function getSetupStatus() { return useMock ? mockGetSetupStatus() : realGetSetupStatus() }
export function setup(username: string, password: string) { return useMock ? mockSetup(username, password) : realSetup(username, password) }
