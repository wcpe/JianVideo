import { useEffect, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'

// 进度条阶段时序（毫秒）：启动后快速爬到 ~80%，短暂停留后补满并淡出
const CLIMB_TO = 80
const CLIMB_DELAY = 80
const FINISH_DELAY = 240
const FADE_DELAY = 200

/**
 * 全局顶部加载条（FR-96，nprogress 风格，自实现不引第三方库）：
 * 路由切换时触发「启动→爬升至 ~80%→补满→淡出」状态机，
 * 固定于视口顶部、宽度随进度变化、复用品牌紫。组件卸载时清理全部定时器。
 * `prefers-reduced-motion: reduce` 下不做缓动（瞬时变化），由内联过渡按需关闭。
 */
export default function TopProgressBar() {
  const { pathname } = useLocation()
  // progress: 0 表示空闲/隐藏，>0 表示进行中或补满
  const [progress, setProgress] = useState(0)
  // visible: 控制淡出（补满后短暂保留再隐藏）
  const [visible, setVisible] = useState(false)
  // 是否首次渲染：首次不触发进度条（避免初次进入闪一下）
  const firstRender = useRef(true)
  // 集中管理本轮所有定时器，便于卸载/重启时清理
  const timers = useRef<ReturnType<typeof setTimeout>[]>([])
  // 减少动态效果：关闭爬升缓动，瞬时填充
  const reducedMotion = useRef(false)

  useEffect(() => {
    if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
      reducedMotion.current = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    }
  }, [])

  useEffect(() => {
    // 首次渲染不启动进度条
    if (firstRender.current) {
      firstRender.current = false
      return
    }

    // 清理上一轮残留定时器，重置为起始态
    const clearTimers = () => {
      timers.current.forEach(clearTimeout)
      timers.current = []
    }
    clearTimers()

    setVisible(true)
    setProgress(10)
    // 爬升至 ~80%
    timers.current.push(
      setTimeout(() => setProgress(CLIMB_TO), CLIMB_DELAY),
    )
    // 补满
    timers.current.push(
      setTimeout(() => setProgress(100), FINISH_DELAY),
    )
    // 补满后淡出并复位
    timers.current.push(
      setTimeout(() => {
        setVisible(false)
        setProgress(0)
      }, FINISH_DELAY + FADE_DELAY),
    )

    return clearTimers
  }, [pathname])

  if (!visible) return null

  return (
    <div
      role="progressbar"
      aria-label="页面加载进度"
      aria-hidden={!visible}
      style={{
        position: 'fixed',
        top: 0,
        left: 0,
        height: 3,
        width: `${progress}%`,
        // 复用品牌紫（FR-93）
        backgroundColor: 'var(--mantine-color-purple-6)',
        boxShadow: '0 0 8px var(--mantine-color-purple-6)',
        zIndex: 2000,
        // 减少动态效果时瞬时变化，否则平滑过渡宽度与透明度
        transition: reducedMotion.current
          ? 'none'
          : 'width 180ms ease, opacity 200ms ease',
        opacity: progress >= 100 ? 0 : 1,
        pointerEvents: 'none',
      }}
    />
  )
}
