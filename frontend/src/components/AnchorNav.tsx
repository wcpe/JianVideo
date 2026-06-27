import { useEffect, useMemo, useRef, useState } from 'react'
import { Box, NavLink } from '@mantine/core'

// 锚点项：id 对应区块 DOM 元素 id，label 为导航显示文案
export interface AnchorSection {
  id: string
  label: string
}

// 区块的滚动定位信息：id 与其顶部相对滚动原点的偏移（像素）
export interface SectionOffset {
  id: string
  top: number
}

// scroll-spy 判定阈值（像素）：滚动位置再下探这点距离才算「越过」某区块顶部，
// 给一点提前量，使区块顶部接近视口顶部时即归属该区块（与点击后平滑滚动落点一致）。
const SPY_THRESHOLD = 8

/**
 * pickActiveByScroll 纯逻辑：按滚动位置判定当前应高亮的锚点（健壮 scroll-spy，无死区）。
 *
 * 规则（任何位置都有确定结果、绝不无故回退到无关首项）：
 * - 已触达页面底部（atBottom）→ 取最后一个区块（兜底「末节太短、其顶部永远到不了阈值」的边角，
 *   否则滚到底会停在倒数第二节）；
 * - 否则取「顶部已越过当前滚动位置 + 阈值」的**最后一个**区块，即视口顶部当前所在区块；
 * - 滚到最顶（没有任何区块越过阈值）→ 取第一个区块；
 * - offsets 为空（DOM 尚未就绪/无区块）→ 保留传入的 currentActive，不强行回退首项。
 *
 * offsets 须按文档序（top 升序）传入；抽为纯函数便于穷举单测「中部空隙/未越带/触底」等场景。
 */
export function pickActiveByScroll(
  offsets: SectionOffset[],
  scrollPos: number,
  currentActive: string,
  atBottom = false,
): string {
  if (offsets.length === 0) return currentActive
  if (atBottom) return offsets[offsets.length - 1].id
  const line = scrollPos + SPY_THRESHOLD
  let active = offsets[0].id
  for (const o of offsets) {
    if (o.top <= line) active = o.id
    else break
  }
  return active
}

/**
 * AnchorNav 左侧锚点导航（FR-113）：渲染锚点列，点击平滑滚动到对应区块，
 * 滚动时按滚动位置高亮当前视口顶部所在区块（健壮 scroll-spy，消除窄带观测死区）。
 * 通用、无业务依赖，供控制台各 tab（设置 / 运行环境）复用。
 */
// 点击锚点后锁定高亮的时间窗（毫秒）：平滑滚动落定前锁定为被点项，避免滚动过程中途高亮跳变。
// 平滑滚动通常数百毫秒内落定，700ms 足以覆盖；落定后 scroll-spy 接管（结果应正好等于被点项）。
const CLICK_LOCK_MS = 700

export default function AnchorNav({ sections }: { sections: AnchorSection[] }) {
  // sectionIds 的稳定序列化键：派生自 sections，仅当 id 序列变化时才变，作为副作用依赖避免每渲染重建
  const idsKey = useMemo(() => sections.map((s) => s.id).join('|'), [sections])
  const [activeId, setActiveId] = useState(() => sections[0]?.id ?? '')
  // 用 ref 持有最新 activeId，供滚动回调/计时器读取而不必进依赖、不捕获过期值（在 effect 中同步，不在渲染期写 ref）
  const activeIdRef = useRef(activeId)
  useEffect(() => { activeIdRef.current = activeId }, [activeId])
  // 点击锁定（FR-113 后续修复）：点击后锁定为被点项，锁定期间忽略 scroll-spy 覆盖；
  // 由一次性计时器在 CLICK_LOCK_MS 后解除，之后由 scroll-spy 接管。
  const lockedRef = useRef(false)
  const lockTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  // recompute 以 ref 暴露给事件处理器，使 handleClick 与监听器始终调到最新实现而不必进依赖
  const recomputeRef = useRef<() => void>(() => {})

  useEffect(() => {
    const ids = idsKey ? idsKey.split('|') : []
    // 计算各区块顶部相对页面滚动原点的偏移（升序）：getBoundingClientRect + 当前滚动量换算为绝对偏移。
    // 滚动容器为页面（AppShell.Main 整体滚动），故以 window.scrollY 为基准。
    const measureOffsets = (): SectionOffset[] => {
      const result: SectionOffset[] = []
      const scrollY = typeof window !== 'undefined' ? window.scrollY : 0
      for (const id of ids) {
        const el = document.getElementById(id)
        if (el) result.push({ id, top: el.getBoundingClientRect().top + scrollY })
      }
      result.sort((a, b) => a.top - b.top)
      return result
    }
    // 按当前滚动位置重算高亮（锁定期内跳过，避免覆盖被点项）
    const recompute = () => {
      if (lockedRef.current) return
      const scrollY = typeof window !== 'undefined' ? window.scrollY : 0
      // 触底判定：仅当页面确实可滚动（内容高于视口）且视口底部已到达文档底部时成立——
      // 用于钳位高亮到最后一节。要求 scrollHeight>innerHeight 可避免不可滚动场景（含 jsdom 缺省高度）误判触底。
      const doc = document.documentElement
      const viewport = typeof window !== 'undefined' ? window.innerHeight : 0
      const atBottom =
        doc.scrollHeight > viewport &&
        viewport + scrollY >= doc.scrollHeight - SPY_THRESHOLD
      setActiveId(pickActiveByScroll(measureOffsets(), scrollY, activeIdRef.current, atBottom))
    }
    recomputeRef.current = recompute

    // 滚动/缩放时 rAF 节流重算（合并高频事件到下一帧）；初次挂载也算一次确定初始高亮
    let raf: number | undefined
    const onScrollOrResize = () => {
      if (raf !== undefined) return
      raf = requestAnimationFrame(() => {
        raf = undefined
        recompute()
      })
    }
    recompute()
    window.addEventListener('scroll', onScrollOrResize, { passive: true })
    window.addEventListener('resize', onScrollOrResize)
    return () => {
      window.removeEventListener('scroll', onScrollOrResize)
      window.removeEventListener('resize', onScrollOrResize)
      if (raf !== undefined) cancelAnimationFrame(raf)
    }
  }, [idsKey])

  // 卸载时清理锁定计时器，避免泄漏
  useEffect(() => () => {
    if (lockTimerRef.current) clearTimeout(lockTimerRef.current)
  }, [])

  // 点击锚点：即时高亮被点项并锁定（屏蔽平滑滚动过程中的 scroll-spy 覆盖），再平滑滚动到对应区块；
  // 落定后解锁，由 scroll-spy 接管（此时其结果应正好等于被点项）。
  const handleClick = (id: string) => {
    lockedRef.current = true
    if (lockTimerRef.current) clearTimeout(lockTimerRef.current)
    lockTimerRef.current = setTimeout(() => {
      lockedRef.current = false
      recomputeRef.current()
    }, CLICK_LOCK_MS)
    setActiveId(id)
    const el = document.getElementById(id)
    if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  return (
    <Box component="nav" aria-label="区块导航">
      {sections.map((s) => (
        <NavLink
          key={s.id}
          component="button"
          type="button"
          label={s.label}
          active={s.id === activeId}
          // 激活态显式用品牌紫（FR-115 后续修复·按钮配色统一）：与主导航激活态品牌紫一致，
          // 不依赖隐式 primaryColor，避免 NavLink 默认 active 串成蓝色。复用色板 token，不写死颜色。
          color="purple"
          onClick={() => handleClick(s.id)}
        />
      ))}
    </Box>
  )
}
