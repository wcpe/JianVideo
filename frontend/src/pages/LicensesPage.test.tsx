import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { describe, it, expect, beforeEach, vi } from 'vitest'

import LicensesPage from './LicensesPage'
import type { LicensesData } from '@/types'

// 小型 fixture：替代真实大 JSON，断言渲染 / 搜索 / 展开 / 外链
// 用 vi.hoisted 提升到 vi.mock 之前，供 mock 工厂引用（vi.mock 被提升到文件顶部）
const fixture = vi.hoisted<LicensesData>(() => ({
  project: {
    name: 'JianVideo',
    license: 'MIT',
    author: '2025 JianVideo Contributors',
    text: 'MIT License\n\n这是项目自身的 MIT 协议全文样例。',
  },
  frontend: [
    { name: 'react', version: '19.2.6', license: 'MIT', author: 'Meta', text: 'react 的 MIT 协议全文样例。' },
    { name: 'axios', version: '1.18.0', license: 'MIT', author: 'Matt Zabriskie', text: 'axios 的 MIT 协议全文样例。' },
  ],
  backend: [
    { name: 'github.com/gin-gonic/gin', version: 'v1.12.0', license: 'MIT', text: 'gin 的 MIT 协议全文样例。' },
    {
      name: 'github.com/example/no-text',
      version: 'v0.1.0',
      license: '',
      url: 'https://pkg.go.dev/github.com/example/no-text@v0.1.0?tab=licenses',
    },
  ],
}))

// 替换数据模块为 fixture，避免依赖构建期生成的真实 licenses.json
vi.mock('@/data/licenses', () => ({ default: fixture }))

function renderPage() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/licenses']}>
        <LicensesPage />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('LicensesPage（FR-57）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('渲染项目自身 MIT 协议与作者', () => {
    renderPage()

    expect(screen.getByText('JianVideo')).toBeInTheDocument()
    // 项目作者与 MIT 标识可见
    expect(screen.getByText(/2025 JianVideo Contributors/)).toBeInTheDocument()
    expect(screen.getAllByText('MIT').length).toBeGreaterThan(0)
  })

  it('渲染前端依赖列表（含包名与版本）', () => {
    renderPage()

    // 标题含前端依赖数量
    expect(screen.getByText(/前端依赖（2）/)).toBeInTheDocument()
    expect(screen.getByText('react')).toBeInTheDocument()
    expect(screen.getByText('axios')).toBeInTheDocument()
    expect(screen.getByText('19.2.6')).toBeInTheDocument()
  })

  it('渲染后端依赖列表（含数量）', () => {
    renderPage()

    expect(screen.getByText(/后端依赖（2）/)).toBeInTheDocument()
    expect(screen.getByText('github.com/gin-gonic/gin')).toBeInTheDocument()
  })

  it('搜索框按包名过滤依赖', async () => {
    const user = userEvent.setup()
    renderPage()

    // 过滤前两个前端依赖都在
    expect(screen.getByText('react')).toBeInTheDocument()
    expect(screen.getByText('axios')).toBeInTheDocument()

    const search = screen.getByPlaceholderText('搜索软件包')
    await user.type(search, 'axios')

    // 过滤后仅 axios 命中，react 不再展示
    expect(screen.getByText('axios')).toBeInTheDocument()
    expect(screen.queryByText('react')).not.toBeInTheDocument()
  })

  it('点击前端依赖展开其协议全文', async () => {
    const user = userEvent.setup()
    renderPage()

    // 协议全文样例存在于面板（Mantine Accordion 保留挂载、折叠时隐藏）
    expect(screen.getByText('axios 的 MIT 协议全文样例。')).toBeInTheDocument()

    // 点 axios 的控制条触发展开：aria-expanded 由 false 变 true
    const control = screen.getByText('axios').closest('button') as HTMLButtonElement
    expect(control).toHaveAttribute('aria-expanded', 'false')

    await user.click(control)

    expect(control).toHaveAttribute('aria-expanded', 'true')
  })

  it('后端无全文依赖渲染 pkg.go.dev 外链（安全新标签页）', () => {
    renderPage()

    const link = screen.getByRole('link', { name: /查看协议/ })
    expect(link).toHaveAttribute(
      'href',
      'https://pkg.go.dev/github.com/example/no-text@v0.1.0?tab=licenses',
    )
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'))
  })

  it('点击项目协议可展开自身 MIT 全文', async () => {
    const user = userEvent.setup()
    renderPage()

    // 项目协议全文存在（Accordion 折叠时保留挂载、隐藏展示）
    expect(screen.getByText(/这是项目自身的 MIT 协议全文样例/)).toBeInTheDocument()

    // 项目卡片内的展开触发器：点击后 aria-expanded 切为 true
    const control = screen.getByText('JianVideo').closest('button') as HTMLButtonElement
    expect(control).toHaveAttribute('aria-expanded', 'false')

    await user.click(control)

    expect(control).toHaveAttribute('aria-expanded', 'true')
  })
})
