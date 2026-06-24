import type { ReactNode } from 'react'
import { useLocation } from 'react-router-dom'

interface RouteTransitionProps {
  children: ReactNode
}

/**
 * 路由切换过渡（FR-96）：以当前路径为 key 包裹页面内容，
 * 路由变化时容器重新挂载，触发 index.css 的 `.route-fade` 轻量渐入动画。
 * `prefers-reduced-motion: reduce` 下动画由 index.css 兜底关闭。
 */
export default function RouteTransition({ children }: RouteTransitionProps) {
  const { pathname } = useLocation()
  return (
    <div key={pathname} className="route-fade">
      {children}
    </div>
  )
}
