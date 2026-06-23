import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { describe, it, expect, beforeEach, vi } from 'vitest'

import AppLayout from './AppLayout'
import { useAuthStore } from '@/stores/auth'

// mock react-router-dom 的 useNavigate，避免真实跳转
const mockNavigate = vi.fn()
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>()
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  }
})

function renderLayout() {
  return render(
    <MantineProvider>
      <MemoryRouter>
        <AppLayout>
          <div>页面内容</div>
        </AppLayout>
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('AppLayout 主题切换', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // 标记已认证，避免守卫相关逻辑干扰
    useAuthStore.setState({ initialized: true, isAuthenticated: true, username: 'admin' })
  })

  it('渲染切换主题按钮', () => {
    renderLayout()

    expect(screen.getByRole('button', { name: '切换主题' })).toBeInTheDocument()
  })

  it('点击切换主题按钮不报错', async () => {
    const user = userEvent.setup()
    renderLayout()

    const toggle = screen.getByRole('button', { name: '切换主题' })
    await user.click(toggle)

    // 点击后按钮仍在文档中，确认交互未抛错
    expect(toggle).toBeInTheDocument()
  })
})

describe('AppLayout 收缩导航（FR-54）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    useAuthStore.setState({ initialized: true, isAuthenticated: true, username: 'admin' })
  })

  // 桌面 Navbar 用 data-collapsed 标识收缩态，便于断言
  const getNavbar = () => document.querySelector('[data-collapsed]') as HTMLElement

  it('默认展开：导航名文字可见、navbar 非收缩态', () => {
    renderLayout()

    // 展开态导航名文字可见（侧边栏 + 抽屉各一份，至少一处）
    expect(screen.getAllByText('时间轴').length).toBeGreaterThan(0)
    expect(getNavbar()).toHaveAttribute('data-collapsed', 'false')
    // 默认应提供「收起导航」按钮
    expect(screen.getByRole('button', { name: '收起导航' })).toBeInTheDocument()
  })

  it('点切换按钮进入收缩态：navbar 收缩、桌面导航名文字隐藏、按钮 aria 切换', async () => {
    const user = userEvent.setup()
    renderLayout()

    await user.click(screen.getByRole('button', { name: '收起导航' }))

    expect(getNavbar()).toHaveAttribute('data-collapsed', 'true')
    // 收缩态切换按钮变为「展开导航」
    expect(screen.getByRole('button', { name: '展开导航' })).toBeInTheDocument()
    // 桌面侧边栏导航名文字隐藏；移动端抽屉默认关闭不渲染，故文字应完全不在文档中
    expect(screen.queryByText('时间轴')).not.toBeInTheDocument()
  })

  it('收缩后写入 localStorage', async () => {
    const user = userEvent.setup()
    renderLayout()

    await user.click(screen.getByRole('button', { name: '收起导航' }))

    expect(localStorage.getItem('jianvideo-nav-collapsed')).toBe('1')
  })

  it('预置 localStorage 收缩值，mount 后初始即为收缩态', () => {
    localStorage.setItem('jianvideo-nav-collapsed', '1')

    renderLayout()

    expect(getNavbar()).toHaveAttribute('data-collapsed', 'true')
    expect(screen.getByRole('button', { name: '展开导航' })).toBeInTheDocument()
    expect(screen.queryByText('时间轴')).not.toBeInTheDocument()
  })

  it('移动端汉堡按钮与抽屉行为不受收缩影响（回归）', async () => {
    const user = userEvent.setup()
    localStorage.setItem('jianvideo-nav-collapsed', '1')
    renderLayout()

    // 汉堡按钮始终存在
    const burger = screen.getByRole('button', { name: '导航菜单' })
    expect(burger).toBeInTheDocument()

    // 点开抽屉后导航名文字可见（抽屉内固定展开，不受收缩态影响）
    await user.click(burger)
    expect(screen.getAllByText('时间轴').length).toBeGreaterThan(0)
  })
})

describe('AppLayout 导航底部版本与开源协议入口（FR-61）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    useAuthStore.setState({ initialized: true, isAuthenticated: true, username: 'admin' })
  })

  // 桌面 Navbar 用 data-collapsed 标识收缩态，便于断言
  const getNavbar = () => document.querySelector('[data-collapsed]') as HTMLElement

  it('不再渲染全局页脚（AppShell.Footer 已移除）', () => {
    renderLayout()

    // Mantine 页脚以 footer 元素呈现；FR-61 移除后文档中不应存在 footer
    expect(document.querySelector('footer')).toBeNull()
  })

  it('展开态导航底部展示当前版本号（取自系统信息）', async () => {
    renderLayout()

    // 版本来自 MSW 的 /api/system/info（app_version=0.3.0）
    await waitFor(() => {
      expect(screen.getByText(/0\.3\.0/)).toBeInTheDocument()
    })
  })

  it('展开态导航底部提供「开源协议」链接，指向 /licenses', async () => {
    renderLayout()

    const link = await screen.findByRole('link', { name: '开源协议' })
    expect(link).toHaveAttribute('href', '/licenses')
  })

  it('收缩态：导航底部仍提供开源协议入口（图标 + Tooltip），无版本号长文本', async () => {
    const user = userEvent.setup()
    renderLayout()

    await user.click(screen.getByRole('button', { name: '收起导航' }))

    expect(getNavbar()).toHaveAttribute('data-collapsed', 'true')
    // 收缩态以「开源协议」无障碍标签的链接承载入口（图标态）
    const link = await screen.findByRole('link', { name: '开源协议' })
    expect(link).toHaveAttribute('href', '/licenses')
    // 收缩态不平铺版本号长文本，避免 64px 内截断
    expect(screen.queryByText(/0\.3\.0/)).not.toBeInTheDocument()
  })

  it('移动端抽屉底部含版本号与「开源协议」链接', async () => {
    const user = userEvent.setup()
    renderLayout()

    await user.click(screen.getByRole('button', { name: '导航菜单' }))

    // 抽屉打开后，版本号与协议链接均可见（桌面 navbar 也各一份，故至少一处）
    await waitFor(() => {
      expect(screen.getAllByText(/0\.3\.0/).length).toBeGreaterThan(0)
    })
    expect(screen.getAllByRole('link', { name: '开源协议' }).length).toBeGreaterThan(0)
  })
})

describe('AppLayout 命令面板（FR-74）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    useAuthStore.setState({ initialized: true, isAuthenticated: true, username: 'admin' })
  })

  it('默认不渲染命令面板', () => {
    renderLayout()
    // 面板输入框（aria-label「命令」）默认不在文档中
    expect(screen.queryByRole('textbox', { name: '命令' })).toBeNull()
  })

  it('按 Ctrl+K 打开命令面板', async () => {
    renderLayout()

    // useHotkeys 监听 document.documentElement 的 keydown，事件须从非输入元素冒泡
    fireEvent.keyDown(document.body, { key: 'k', ctrlKey: true })

    expect(await screen.findByRole('textbox', { name: '命令' })).toBeInTheDocument()
  })

  it('点击 header 命令面板按钮打开', async () => {
    const user = userEvent.setup()
    renderLayout()

    await user.click(screen.getByRole('button', { name: '命令面板' }))

    expect(await screen.findByRole('textbox', { name: '命令' })).toBeInTheDocument()
  })

  it('命令面板含切换主题命令', async () => {
    renderLayout()

    fireEvent.keyDown(document.body, { key: 'k', ctrlKey: true })
    const dialog = await screen.findByRole('dialog')

    // 面板内列出「切换主题」命令（导航命令清单 + 直接执行命令）
    expect(within(dialog).getByText('切换主题')).toBeInTheDocument()
  })

  it('命令面板内点击「时间轴」命令触发跳转', async () => {
    const user = userEvent.setup()
    renderLayout()

    fireEvent.keyDown(document.body, { key: 'k', ctrlKey: true })
    const dialog = await screen.findByRole('dialog')

    // 输入过滤到「时间轴」后在面板内点击，断言 navigate 被调用到 '/'
    const input = within(dialog).getByRole('textbox', { name: '命令' })
    await user.type(input, '时间轴')
    await user.click(within(dialog).getByText('时间轴'))

    expect(mockNavigate).toHaveBeenCalledWith('/')
  })
})
