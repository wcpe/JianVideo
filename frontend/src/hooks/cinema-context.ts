import { createContext, useContext } from 'react'

// 影院模式上下文（FR-85）：让播放页这种 AppShell.Main 内的深层子组件，
// 临时收起左导航以扩大视频区——纯本地会话态，不写 localStorage、不污染全局 useNavCollapsed 持久语义。
export interface CinemaContextValue {
  // 当前是否处于影院态（临时收起导航）
  cinema: boolean
  // 设置影院态；离开播放页/退出影院时置 false 恢复
  setCinema: (value: boolean) => void
}

// 默认值：无 Provider 时安全降级——非影院态、setCinema 为空操作（不抛错、不影响布局）。
// 由 AppLayout 自持本地态并经 CinemaContext.Provider 下发（AppLayout 自身需用有效收缩态、不消费自身 Provider）。
export const CinemaContext = createContext<CinemaContextValue>({
  cinema: false,
  setCinema: () => {},
})

/** 读取影院模式上下文：供播放页切换影院态、供 AppLayout 计算有效收缩态 */
export function useCinemaMode(): CinemaContextValue {
  return useContext(CinemaContext)
}
