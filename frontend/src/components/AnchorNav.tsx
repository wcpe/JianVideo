import { useEffect, useRef, useState } from 'react'
import { Box, NavLink } from '@mantine/core'

// 锚点项：id 对应区块 DOM 元素 id，label 为导航显示文案
export interface AnchorSection {
  id: string
  label: string
}

/**
 * pickActiveSection 纯逻辑：在「当前可见区块 id 集合」里挑出应高亮的锚点。
 * 规则：取可见区块中按 sections 文档序最靠前的一个；无可见区块时回退首个。
 * 抽为纯函数便于单测高亮策略，UI 只负责喂入观测结果。
 */
export function pickActiveSection(sectionIds: string[], visibleIds: string[]): string {
  for (const id of sectionIds) {
    if (visibleIds.includes(id)) return id
  }
  return sectionIds[0] ?? ''
}

/**
 * AnchorNav 左侧锚点导航（FR-113）：渲染锚点列，点击平滑滚动到对应区块，
 * 滚动时用 IntersectionObserver 观测各区块、高亮当前可视区块。
 * 通用、无业务依赖，供控制台各 tab（设置 / 运行环境）复用。
 */
export default function AnchorNav({ sections }: { sections: AnchorSection[] }) {
  const sectionIds = sections.map((s) => s.id)
  const [activeId, setActiveId] = useState(sectionIds[0] ?? '')
  // 用 ref 持有当前可见 id 集合，避免观测回调闭包捕获过期状态
  const visibleRef = useRef<Set<string>>(new Set())

  useEffect(() => {
    // 环境不支持 IntersectionObserver（如部分测试环境）时跳过高亮观测，锚点点击仍可用
    if (typeof IntersectionObserver === 'undefined') return
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          const id = entry.target.id
          if (entry.isIntersecting) visibleRef.current.add(id)
          else visibleRef.current.delete(id)
        }
        setActiveId(pickActiveSection(sectionIds, Array.from(visibleRef.current)))
      },
      // 顶部留出余量，区块上沿进入视口上 1/3 即视为当前
      { rootMargin: '0px 0px -66% 0px', threshold: 0 },
    )
    const observed: Element[] = []
    for (const id of sectionIds) {
      const el = document.getElementById(id)
      if (el) {
        observer.observe(el)
        observed.push(el)
      }
    }
    return () => observer.disconnect()
    // sectionIds 由 sections 派生，依赖其序列化值即可，避免每渲染重建
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sectionIds.join('|')])

  // 点击锚点：平滑滚动到对应区块并即时高亮
  const handleClick = (id: string) => {
    const el = document.getElementById(id)
    if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' })
    setActiveId(id)
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
          onClick={() => handleClick(s.id)}
        />
      ))}
    </Box>
  )
}
