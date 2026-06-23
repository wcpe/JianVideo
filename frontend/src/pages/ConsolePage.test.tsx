import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import ConsolePage from './ConsolePage'

// 复制结果功能依赖 navigator.clipboard，jsdom 默认缺失，这里注入避免 SystemPage 报错
const writeText = vi.fn().mockResolvedValue(undefined)

// 把 MemoryRouter 的内存路由 search 暴露到 DOM，供断言 URL query（MemoryRouter 不更新 window.location）
function LocationProbe() {
  const location = useLocation()
  return <div data-testid="location-search">{location.search}</div>
}

// 在指定初始 URL 下渲染控制台页（默认进系统信息 tab）
function renderPage(initialPath = '/system') {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={[initialPath]}>
        <ConsolePage />
        <LocationProbe />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('ConsolePage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
      writable: true,
    })
  })

  it('渲染「系统信息」「设置」两个 tab', () => {
    renderPage()
    expect(screen.getByRole('tab', { name: '系统信息' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: '设置' })).toBeInTheDocument()
  })

  it('缺省进入系统信息 tab，可见系统信息内容', async () => {
    renderPage()
    // 系统信息 tab 选中
    expect(screen.getByRole('tab', { name: '系统信息' })).toHaveAttribute('aria-selected', 'true')
    // SystemPage 内容渲染：标题 + 四个子 tab（FR-59 拆子 tab 后内容按子 tab 分布）
    expect(await screen.findByText('系统诊断')).toBeVisible()
    expect(screen.getByRole('tab', { name: '运行环境' })).toBeVisible()
    expect(screen.getByRole('tab', { name: '应用更新' })).toBeVisible()
  })

  it('点击「设置」tab 切换、URL query 变为 tab=settings 并展示设置项', async () => {
    const user = userEvent.setup()
    renderPage()

    await user.click(screen.getByRole('tab', { name: '设置' }))

    // 设置 tab 选中
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: '设置' })).toHaveAttribute('aria-selected', 'true')
    })
    // SettingsPage 内容渲染：扫描周期 / 回收站路径输入
    expect(await screen.findByLabelText('扫描周期（秒）')).toBeInTheDocument()
    expect(screen.getByLabelText('每盘符回收站路径（JSON）')).toBeInTheDocument()
    // URL query 已更新
    expect(screen.getByTestId('location-search')).toHaveTextContent('tab=settings')
  })

  it('以 ?tab=settings 进入时定位到设置 tab', async () => {
    renderPage('/system?tab=settings')
    expect(screen.getByRole('tab', { name: '设置' })).toHaveAttribute('aria-selected', 'true')
    // 直接展示设置项，无需手动点击
    expect(await screen.findByLabelText('扫描周期（秒）')).toBeInTheDocument()
  })
})
