/**
 * Mock 认证 API — 返回模拟数据
 */

export async function login(username: string, password: string): Promise<void> {
  await delay(300)
  if (username === 'admin' && password === 'admin') return
  throw new Error('用户名或密码错误')
}

export async function logout(): Promise<void> {
  await delay(100)
}

export async function getMe(): Promise<{ username: string }> {
  await delay(100)
  return { username: 'admin' }
}

function delay(ms: number): Promise<void> {
  return new Promise(r => setTimeout(r, ms))
}
