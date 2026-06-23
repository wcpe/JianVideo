import { useState, useEffect, useCallback, useRef } from 'react'

/** 单击时携带的修饰键（来自 React.MouseEvent，仅取所需三键，便于纯逻辑测试） */
export interface ClickModifiers {
  ctrlKey?: boolean
  shiftKey?: boolean
  metaKey?: boolean
}

export interface MultiSelect {
  /** 当前选中项 id 集合 */
  selectedIds: Set<number>
  /** 复选框模式：开启后任意单击即切换该项（不清其它） */
  checkboxMode: boolean
  setCheckboxMode: (on: boolean) => void
  /** 按下标 + 修饰键处理单击：Shift 区间 / Ctrl(Cmd) 切换 / 普通单选；复选框模式下恒为切换 */
  handleItemClick: (index: number, mods: ClickModifiers) => void
  /** 按 id 切换单项（供复选框组件直接调用） */
  toggle: (id: number) => void
  /** 全选当前全部已加载项 */
  selectAll: () => void
  /** 反选：已选与未选互换（范围为当前全部已加载项） */
  invertSelection: () => void
  /** 清空选择 */
  clear: () => void
  /** 该 id 是否选中 */
  isSelected: (id: number) => boolean
}

/**
 * 可复用的多选基建（FR-69）：按「有序 id 列表」驱动列表多选手势。
 *
 * 由调用方传入按渲染顺序排列的 id 列表（目录浏览=排序后文件、时间轴=全部已加载项按分组展平），
 * Shift 区间与全选/反选均以该列表为范围——时间轴虚拟列表下即「全部已加载项」，未滚动加载的项不在范围内。
 * 列表变化（翻页/筛选/收缩）时自动剔除已不在列表中的陈旧选中项。
 */
export function useMultiSelect(orderedIds: number[]): MultiSelect {
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set())
  const [checkboxMode, setCheckboxMode] = useState(false)
  // 锚点下标：Shift 区间选的起点。用 ref 避免把它纳入回调依赖导致引用频繁变化
  const anchorRef = useRef<number | null>(null)

  // 列表变化时剔除已不存在的陈旧选中项，保证 selectedIds 始终是当前列表子集
  useEffect(() => {
    setSelectedIds((prev) => {
      if (prev.size === 0) return prev
      const valid = new Set(orderedIds)
      let changed = false
      const next = new Set<number>()
      for (const id of prev) {
        if (valid.has(id)) next.add(id)
        else changed = true
      }
      return changed ? next : prev
    })
    // 依赖 id 列表内容；orderedIds 引用每次渲染可能不同，故用其拼接串作为稳定依赖
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [orderedIds.join(',')])

  const handleItemClick = useCallback((index: number, mods: ClickModifiers) => {
    const id = orderedIds[index]
    if (id === undefined) return

    // 复选框模式：任意单击即切换该项，不清其它，并把锚点移到此处
    if (checkboxMode) {
      setSelectedIds((prev) => {
        const next = new Set(prev)
        if (next.has(id)) next.delete(id)
        else next.add(id)
        return next
      })
      anchorRef.current = index
      return
    }

    // Shift 区间选：从锚点到当前下标连选（含两端）；无锚点退化为单选
    if (mods.shiftKey && anchorRef.current !== null) {
      const a = anchorRef.current
      const [lo, hi] = a < index ? [a, index] : [index, a]
      const next = new Set<number>()
      for (let i = lo; i <= hi; i++) next.add(orderedIds[i])
      setSelectedIds(next)
      return
    }

    // Ctrl / Cmd 切换：在已选基础上增减该项，并把锚点移到此处
    if (mods.ctrlKey || mods.metaKey) {
      setSelectedIds((prev) => {
        const next = new Set(prev)
        if (next.has(id)) next.delete(id)
        else next.add(id)
        return next
      })
      anchorRef.current = index
      return
    }

    // 普通单选：仅选中该项，锚点置此
    setSelectedIds(new Set([id]))
    anchorRef.current = index
  }, [orderedIds, checkboxMode])

  const toggle = useCallback((id: number) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  const selectAll = useCallback(() => {
    setSelectedIds(new Set(orderedIds))
  }, [orderedIds])

  const invertSelection = useCallback(() => {
    setSelectedIds((prev) => {
      const next = new Set<number>()
      for (const id of orderedIds) {
        if (!prev.has(id)) next.add(id)
      }
      return next
    })
  }, [orderedIds])

  const clear = useCallback(() => {
    setSelectedIds(new Set())
    anchorRef.current = null
  }, [])

  const isSelected = useCallback((id: number) => selectedIds.has(id), [selectedIds])

  return {
    selectedIds,
    checkboxMode,
    setCheckboxMode,
    handleItemClick,
    toggle,
    selectAll,
    invertSelection,
    clear,
    isSelected,
  }
}
