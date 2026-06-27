import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MantineProvider } from '@mantine/core'

import AnchorNav, { pickActiveSection } from './AnchorNav'

// 在 MantineProvider 下渲染锚点导航（组件用到 Mantine 样式）
function renderNav(props: Parameters<typeof AnchorNav>[0]) {
  return render(
    <MantineProvider>
      <AnchorNav {...props} />
    </MantineProvider>,
  )
}

describe('pickActiveSection（纯逻辑：选当前高亮锚点）', () => {
  const ids = ['a', 'b', 'c']

  it('无任何可见区块时回退首个', () => {
    expect(pickActiveSection(ids, [])).toBe('a')
  })

  it('选可见区块中文档序最靠前的一个', () => {
    // b、c 同时可见 → 取靠前的 b
    expect(pickActiveSection(ids, ['c', 'b'])).toBe('b')
  })

  it('忽略不在锚点列表里的可见 id', () => {
    expect(pickActiveSection(ids, ['x', 'c'])).toBe('c')
  })
})

describe('AnchorNav（锚点导航交互）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // jsdom 无 IntersectionObserver，注入空 stub 避免组件挂载报错
    vi.stubGlobal('IntersectionObserver', class {
      observe() {}
      unobserve() {}
      disconnect() {}
      takeRecords() { return [] }
    })
  })

  it('渲染各锚点标签', () => {
    renderNav({ sections: [{ id: 'sec-a', label: '账户安全' }, { id: 'sec-b', label: '扫描' }] })
    expect(screen.getByRole('button', { name: '账户安全' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '扫描' })).toBeInTheDocument()
  })

  it('点击锚点滚动到对应区块', async () => {
    const target = document.createElement('div')
    target.id = 'sec-a'
    const scrollSpy = vi.fn()
    target.scrollIntoView = scrollSpy
    document.body.appendChild(target)

    const user = userEvent.setup()
    renderNav({ sections: [{ id: 'sec-a', label: '账户安全' }] })
    await user.click(screen.getByRole('button', { name: '账户安全' }))

    expect(scrollSpy).toHaveBeenCalled()
    document.body.removeChild(target)
  })
})
