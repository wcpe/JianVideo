import { useState, useCallback } from 'react'

// 桌面端侧边导航收缩态的 localStorage 键；值 '1' 表收缩、其余（含缺省）表展开
const STORAGE_KEY = 'jianvideo-nav-collapsed'

// 读取持久化的收缩态，读取异常或无值时默认展开（false）
function readCollapsed(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) === '1'
  } catch {
    // localStorage 不可用（如隐私模式）时降级为展开，不影响导航可用性
    return false
  }
}

// 管理桌面端侧边导航的收缩/展开状态并持久化到 localStorage，刷新后保持。
// 返回 [collapsed, toggle]：collapsed 为当前是否收缩，toggle 在两态间切换。
export function useNavCollapsed(): [boolean, () => void] {
  const [collapsed, setCollapsed] = useState<boolean>(readCollapsed)

  const toggle = useCallback(() => {
    setCollapsed((prev) => {
      const next = !prev
      try {
        localStorage.setItem(STORAGE_KEY, next ? '1' : '0')
      } catch {
        // 持久化失败不阻断切换，仅本次会话内生效
      }
      return next
    })
  }, [])

  return [collapsed, toggle]
}
