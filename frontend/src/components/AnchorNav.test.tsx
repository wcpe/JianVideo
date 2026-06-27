import { render, screen, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { MantineProvider } from '@mantine/core'

import AnchorNav, { pickActiveByScroll, type SectionOffset } from './AnchorNav'

// 在 MantineProvider 下渲染锚点导航（组件用到 Mantine 样式）
function renderNav(props: Parameters<typeof AnchorNav>[0]) {
  return render(
    <MantineProvider>
      <AnchorNav {...props} />
    </MantineProvider>,
  )
}

describe('pickActiveByScroll（纯逻辑：按滚动位置选高亮锚点，无死区）', () => {
  // 三区块顶部偏移：账户安全=0、工具路径=900、诊断=1800
  const offsets: SectionOffset[] = [
    { id: 'account', top: 0 },
    { id: 'tools', top: 900 },
    { id: 'diag', top: 1800 },
  ]

  it('滚到最顶（scrollY=0）：取第一个区块', () => {
    expect(pickActiveByScroll(offsets, 0, 'account')).toBe('account')
  })

  it('滚到工具路径区域（scrollY=973）：取工具路径，绝不回退到无关的账户安全（死区修复）', () => {
    // 复现真机证据：滚到 973，工具路径(900) 已越线、诊断(1800) 未到 → 当前为工具路径
    expect(pickActiveByScroll(offsets, 973, 'account')).toBe('tools')
  })

  it('滚到两区块之间的中部空隙：归属上一个已越过的区块（不是首个、不是无关项）', () => {
    // 1200 介于 tools(900) 与 diag(1800) 之间 → 取已越过的最后一个 tools
    expect(pickActiveByScroll(offsets, 1200, 'account')).toBe('tools')
  })

  it('滚到最底（scrollY 超过末区块顶部）：取最后一个区块', () => {
    expect(pickActiveByScroll(offsets, 5000, 'tools')).toBe('diag')
  })

  it('触底（atBottom=true）：即便末区块顶部未越线，也钳位到最后一个区块', () => {
    // 末节太短、其顶部(1800)永远到不了可达的最大滚动位置(如 1679) → 不钳位会停在倒数第二个；
    // atBottom 钳位后取最后一个 diag。
    expect(pickActiveByScroll(offsets, 1679, 'tools', true)).toBe('diag')
  })

  it('恰好越过区块顶部（含阈值提前量）即归属该区块', () => {
    // 阈值 SPY_THRESHOLD=8：scrollY=892 → 892+8=900 >= tools.top(900) → tools
    expect(pickActiveByScroll(offsets, 892, 'account')).toBe('tools')
    // scrollY=891 → 891+8=899 < 900 → 仍为 account
    expect(pickActiveByScroll(offsets, 891, 'account')).toBe('account')
  })

  it('offsets 为空（DOM 未就绪/无区块）：保留当前 active，不强行回退首项', () => {
    expect(pickActiveByScroll([], 500, 'tools')).toBe('tools')
    expect(pickActiveByScroll([], 0, 'diag')).toBe('diag')
  })
})

describe('AnchorNav（锚点导航交互）', () => {
  // 还原被各用例改写的 scrollY
  const originalScrollY = Object.getOwnPropertyDescriptor(window, 'scrollY')

  function setScrollY(value: number) {
    Object.defineProperty(window, 'scrollY', { value, configurable: true, writable: true })
  }

  // 在 body 内放置带 id 的区块元素，并按 top 偏移桩定 getBoundingClientRect（相对当前视口）
  function mountSections(layout: { id: string; absoluteTop: number }[]) {
    for (const { id, absoluteTop } of layout) {
      const el = document.createElement('div')
      el.id = id
      el.scrollIntoView = vi.fn()
      // rect.top = 绝对偏移 - 当前 scrollY（视口相对）；measureOffsets 再 + scrollY 还原绝对偏移
      el.getBoundingClientRect = () =>
        ({ top: absoluteTop - window.scrollY, left: 0, right: 0, bottom: 0, width: 0, height: 0, x: 0, y: 0, toJSON() {} }) as DOMRect
      document.body.appendChild(el)
    }
  }

  beforeEach(() => {
    vi.clearAllMocks()
    setScrollY(0)
    // jsdom 的 requestAnimationFrame 由 setup 提供（setTimeout 0），这里无需额外桩
  })

  afterEach(() => {
    // 清理本用例插入的区块元素
    for (const el of Array.from(document.querySelectorAll('div[id^="sec-"]'))) el.remove()
    if (originalScrollY) Object.defineProperty(window, 'scrollY', originalScrollY)
  })

  it('渲染各锚点标签', () => {
    renderNav({ sections: [{ id: 'sec-a', label: '账户安全' }, { id: 'sec-b', label: '扫描' }] })
    expect(screen.getByRole('button', { name: '账户安全' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '扫描' })).toBeInTheDocument()
  })

  it('点击锚点滚动到对应区块', async () => {
    mountSections([{ id: 'sec-a', absoluteTop: 0 }])

    const user = userEvent.setup()
    renderNav({ sections: [{ id: 'sec-a', label: '账户安全' }] })
    await user.click(screen.getByRole('button', { name: '账户安全' }))

    expect(document.getElementById('sec-a')!.scrollIntoView).toHaveBeenCalled()
  })

  // 死区修复回归：滚动到工具路径区域后，scroll-spy 应高亮工具路径，绝不回退到账户安全
  it('滚动到工具路径区域：高亮稳定为工具路径，不回退到账户安全（死区修复）', async () => {
    mountSections([
      { id: 'sec-account', absoluteTop: 0 },
      { id: 'sec-tools', absoluteTop: 900 },
      { id: 'sec-diag', absoluteTop: 1800 },
    ])
    renderNav({
      sections: [
        { id: 'sec-account', label: '账户安全' },
        { id: 'sec-tools', label: '工具路径' },
        { id: 'sec-diag', label: '诊断' },
      ],
    })

    // 模拟用户滚动到 973（真机证据位置）并触发 scroll 事件 → rAF 重算
    await act(async () => {
      setScrollY(973)
      window.dispatchEvent(new Event('scroll'))
      // 等 rAF（setup 中以 setTimeout 0 实现）落定
      await new Promise((r) => setTimeout(r, 0))
    })

    expect(screen.getByRole('button', { name: '工具路径' })).toHaveAttribute('data-active', 'true')
    expect(screen.getByRole('button', { name: '账户安全' })).not.toHaveAttribute('data-active', 'true')
  })

  // 点击锁定：点击后即时高亮被点项，且锁定窗内发生的滚动事件不把高亮抢回首项
  it('点击工具路径后即时高亮该项，锁定窗内滚动事件不覆盖（不被抢回账户安全）', async () => {
    mountSections([
      { id: 'sec-account', absoluteTop: 0 },
      { id: 'sec-tools', absoluteTop: 900 },
    ])
    const user = userEvent.setup()
    renderNav({
      sections: [
        { id: 'sec-account', label: '账户安全' },
        { id: 'sec-tools', label: '工具路径' },
      ],
    })

    await user.click(screen.getByRole('button', { name: '工具路径' }))
    expect(screen.getByRole('button', { name: '工具路径' })).toHaveAttribute('data-active', 'true')

    // 锁定窗内（700ms 未到）：平滑滚动尚未落定，scrollY 仍接近顶部（account 区），触发 scroll —— 应被锁定屏蔽
    await act(async () => {
      setScrollY(100)
      window.dispatchEvent(new Event('scroll'))
      await new Promise((r) => setTimeout(r, 0))
    })
    expect(screen.getByRole('button', { name: '工具路径' })).toHaveAttribute('data-active', 'true')
    expect(screen.getByRole('button', { name: '账户安全' })).not.toHaveAttribute('data-active', 'true')
  })

  // 落定后一致性：锁释放后 scroll-spy 的结果应正好等于被点项（滚动已停在被点区块）
  it('落定后 scroll-spy 结果等于被点项（锁释放后高亮不跳变）', async () => {
    mountSections([
      { id: 'sec-account', absoluteTop: 0 },
      { id: 'sec-tools', absoluteTop: 900 },
    ])
    const user = userEvent.setup()
    renderNav({
      sections: [
        { id: 'sec-account', label: '账户安全' },
        { id: 'sec-tools', label: '工具路径' },
      ],
    })

    await user.click(screen.getByRole('button', { name: '工具路径' }))

    // 模拟平滑滚动已落定到工具路径区，等锁定窗（700ms）过期后 scroll-spy 接管重算
    await act(async () => {
      setScrollY(900)
      await new Promise((r) => setTimeout(r, 750))
      window.dispatchEvent(new Event('scroll'))
      await new Promise((r) => setTimeout(r, 0))
    })
    expect(screen.getByRole('button', { name: '工具路径' })).toHaveAttribute('data-active', 'true')
    expect(screen.getByRole('button', { name: '账户安全' })).not.toHaveAttribute('data-active', 'true')
  })
})
