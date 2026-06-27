import { useEffect } from 'react'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { describe, it, expect, beforeEach, vi } from 'vitest'

import AppLayout from './AppLayout'
import { useAuthStore } from '@/stores/auth'
import { useCinemaMode } from '@/hooks/cinema-context'

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

// 指定初始路由渲染，用于断言激活态 pill
function renderLayoutAt(initialPath: string) {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={[initialPath]}>
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
    // 默认应提供「收起导航」按钮（FR-115 后 logo 与 navbar 底部各一个同名按钮，限定 navbar 内断言）
    expect(within(getNavbar()).getByRole('button', { name: '收起导航' })).toBeInTheDocument()
  })

  it('点切换按钮进入收缩态：navbar 收缩、桌面导航名文字隐藏、按钮 aria 切换', async () => {
    const user = userEvent.setup()
    renderLayout()

    await user.click(within(getNavbar()).getByRole('button', { name: '收起导航' }))

    expect(getNavbar()).toHaveAttribute('data-collapsed', 'true')
    // 收缩态切换按钮变为「展开导航」
    expect(within(getNavbar()).getByRole('button', { name: '展开导航' })).toBeInTheDocument()
    // 桌面侧边栏导航名文字隐藏；移动端抽屉默认关闭不渲染，故文字应完全不在文档中
    expect(screen.queryByText('时间轴')).not.toBeInTheDocument()
  })

  it('收缩后写入 localStorage', async () => {
    const user = userEvent.setup()
    renderLayout()

    await user.click(within(getNavbar()).getByRole('button', { name: '收起导航' }))

    expect(localStorage.getItem('jianvideo-nav-collapsed')).toBe('1')
  })

  it('预置 localStorage 收缩值，mount 后初始即为收缩态', () => {
    localStorage.setItem('jianvideo-nav-collapsed', '1')

    renderLayout()

    expect(getNavbar()).toHaveAttribute('data-collapsed', 'true')
    expect(within(getNavbar()).getByRole('button', { name: '展开导航' })).toBeInTheDocument()
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

  it('页眉品牌处展示当前版本号（取自系统信息，移到页眉）', async () => {
    renderLayout()

    // 版本来自 MSW 的 /api/system/info（app_version=0.3.0）；移到页眉品牌右侧后以 v 前缀小号 dimmed 展示
    await waitFor(() => {
      expect(screen.getByTestId('app-version')).toHaveTextContent(/v0\.3\.0/)
    })
  })

  it('展开态导航底部提供「开源协议」链接，指向 /licenses', async () => {
    renderLayout()

    const link = await within(getNavbar()).findByRole('link', { name: '开源协议' })
    expect(link).toHaveAttribute('href', '/licenses')
  })

  it('导航底部不再平铺版本号长文本（版本已移至页眉）', () => {
    renderLayout()

    // navbar 内不应再出现版本号文本（已移到页眉）
    expect(within(getNavbar()).queryByText(/0\.3\.0/)).not.toBeInTheDocument()
  })

  it('收缩态：导航底部不再展示开源协议入口（仅留收缩/展开按钮）', async () => {
    const user = userEvent.setup()
    renderLayout()

    await user.click(within(getNavbar()).getByRole('button', { name: '收起导航' }))

    expect(getNavbar()).toHaveAttribute('data-collapsed', 'true')
    // 收缩态完全隐藏开源协议入口（FR-115 后续修复），navbar 内不再有该链接
    expect(within(getNavbar()).queryByRole('link', { name: '开源协议' })).toBeNull()
    // 收缩/展开按钮仍在底部
    expect(within(getNavbar()).getByRole('button', { name: '展开导航' })).toBeInTheDocument()
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

describe('AppLayout 左侧导航分组（FR-83）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    useAuthStore.setState({ initialized: true, isAuthenticated: true, username: 'admin' })
  })

  // 桌面 Navbar 用 data-collapsed 标识，便于把断言限定在桌面侧边栏（排除移动抽屉重复项）
  const getNavbar = () => document.querySelector('[data-collapsed]') as HTMLElement

  it('展开态：桌面 navbar 含「浏览 / 管理 / 系统」三组小标题', () => {
    renderLayout()

    const navbar = within(getNavbar())
    // 「管理」「系统」既是组标题又是同名导航项，故用 getAllByText 断言至少存在一处
    expect(navbar.getByText('浏览')).toBeInTheDocument()
    expect(navbar.getAllByText('管理').length).toBeGreaterThan(0)
    expect(navbar.getAllByText('系统').length).toBeGreaterThan(0)
  })

  it('展开态：12 个导航项全部仍在桌面 navbar 中渲染', () => {
    renderLayout()

    const navbar = within(getNavbar())
    // 概览（FR-117）置于浏览组首项；时间轴随之保留
    const labels = ['概览', '时间轴', '目录', '相册', '地图', '统计', '管理', '回收站', '巡检', '重复项', '转码', '系统']
    labels.forEach((label) => {
      // 「系统」既是组标题又是导航项，故只断言至少存在
      expect(navbar.getAllByText(label).length).toBeGreaterThan(0)
    })
  })

  it('展开态：三组各项归入正确分组（按 navbar 内文档顺序校验组界）', () => {
    renderLayout()

    const navbar = getNavbar()
    const text = navbar.textContent ?? ''
    const idxBrowse = text.indexOf('浏览')
    const idxManage = text.indexOf('管理')
    const idxSystem = text.lastIndexOf('系统')

    // 三个组标题按「浏览 < 管理 < 系统」顺序出现
    expect(idxBrowse).toBeGreaterThanOrEqual(0)
    expect(idxManage).toBeGreaterThan(idxBrowse)
    expect(idxSystem).toBeGreaterThan(idxManage)

    // 浏览组成员落在「浏览」与「管理」标题之间（概览为首项，FR-117）
    ;['概览', '时间轴', '目录', '相册', '地图', '统计'].forEach((label) => {
      const i = text.indexOf(label)
      expect(i).toBeGreaterThan(idxBrowse)
      expect(i).toBeLessThan(idxManage)
    })
    // 管理组成员落在「管理」标题之后、「系统」组标题之前
    ;['回收站', '巡检', '重复项', '转码'].forEach((label) => {
      const i = text.indexOf(label)
      expect(i).toBeGreaterThan(idxManage)
      expect(i).toBeLessThan(idxSystem)
    })
  })

  it('收缩态：组标题文字隐藏、以分隔线区分组、图标态链接仍可达且不破版', async () => {
    const user = userEvent.setup()
    renderLayout()

    await user.click(within(getNavbar()).getByRole('button', { name: '收起导航' }))

    const navbar = getNavbar()
    expect(navbar).toHaveAttribute('data-collapsed', 'true')
    // 收缩态不平铺组标题文字（64px 放不下），但仍以分隔线区分组（至少 2 条：浏览|管理、管理|系统）
    expect(within(navbar).queryByText('浏览')).toBeNull()
    expect(within(navbar).queryByText('管理')).toBeNull()
    expect(within(navbar).getAllByRole('separator').length).toBeGreaterThanOrEqual(2)
    // 12 个图标态导航链接仍在桌面 navbar 中（按 path href 校验可达）；概览 '/' 与时间轴 '/timeline'（FR-117）
    const paths = ['/', '/timeline', '/browse', '/albums', '/map', '/stats', '/library-manager', '/recycle', '/inspect', '/duplicates', '/transcode', '/system']
    paths.forEach((p) => {
      expect(navbar.querySelector(`a[href="${p}"]`)).not.toBeNull()
    })
  })

  it('收缩态：图标态导航链接仍有可访问名（FR-97：收起后 link-name 缺失修复）', () => {
    // 预置收缩态，mount 即收起：此时标签文字隐藏，链接须以 aria-label 提供可访问名
    localStorage.setItem('jianvideo-nav-collapsed', '1')
    renderLayout()

    const navbar = within(getNavbar())
    // 收起态下仍能按无障碍名定位到各导航链接（修复前图标态链接无名，此处会失败）；含概览（FR-117）
    const names = ['概览', '时间轴', '目录', '相册', '地图', '统计', '管理', '回收站', '巡检', '重复项', '转码', '系统']
    names.forEach((name) => {
      expect(navbar.getByRole('link', { name }).getAttribute('href')).toBeTruthy()
    })
  })

  it('移动端抽屉同样按组渲染（含三组小标题）', async () => {
    const user = userEvent.setup()
    renderLayout()

    await user.click(screen.getByRole('button', { name: '导航菜单' }))

    // 抽屉打开后三组小标题可见（桌面 navbar 也各一份，故至少一处）
    await waitFor(() => {
      expect(screen.getAllByText('浏览').length).toBeGreaterThan(0)
    })
    expect(screen.getAllByText('管理').length).toBeGreaterThan(0)
    expect(screen.getAllByText('系统').length).toBeGreaterThan(0)
  })
})

describe('AppLayout 导航纵向滚动（修复导航栏无法滚动）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    useAuthStore.setState({ initialized: true, isAuthenticated: true, username: 'admin' })
  })

  const getNavbar = () => document.querySelector('[data-collapsed]') as HTMLElement

  it('导航组容器可纵向滚动（flex:1 + minHeight:0 + overflowY:auto），矮视口下溢出内容可达', () => {
    renderLayout()

    // 导航组的滚动容器：内容超过 navbar 可用高度时须能内部滚动，而非溢出视口被截断
    const scroll = within(getNavbar()).getByTestId('nav-scroll-area')
    const style = getComputedStyle(scroll)
    expect(style.overflowY).toBe('auto')
    // flex 子项须 minHeight:0 才能收缩到内容以下并触发滚动
    expect(style.minHeight).toBe('0px')
  })

  it('版本号/开源协议入口仍在滚动容器之外（底部常驻，不随导航列表滚走）', () => {
    renderLayout()

    const scroll = within(getNavbar()).getByTestId('nav-scroll-area')
    // 「开源协议」链接置于滚动容器外（navbar 底部常驻），不被包含在可滚动的导航列表内
    const license = within(getNavbar()).getByRole('link', { name: '开源协议' })
    expect(scroll.contains(license)).toBe(false)
  })
})

describe('AppLayout 影院模式（FR-85）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    useAuthStore.setState({ initialized: true, isAuthenticated: true, username: 'admin' })
  })

  const getNavbar = () => document.querySelector('[data-collapsed]') as HTMLElement

  // 子组件：从影院上下文取 setCinema，渲染一个切换按钮，模拟播放页切入/切出影院态
  function CinemaToggler() {
    const { cinema, setCinema } = useCinemaMode()
    return (
      <button type="button" onClick={() => setCinema(!cinema)}>
        切影院:{String(cinema)}
      </button>
    )
  }

  function renderWithToggler() {
    return render(
      <MantineProvider>
        <MemoryRouter>
          <AppLayout>
            <CinemaToggler />
          </AppLayout>
        </MemoryRouter>
      </MantineProvider>,
    )
  }

  it('影院态切入：导航有效收缩（data-collapsed=true），不写 localStorage 持久态', async () => {
    const user = userEvent.setup()
    renderWithToggler()

    // 初始展开
    expect(getNavbar()).toHaveAttribute('data-collapsed', 'false')

    await user.click(screen.getByRole('button', { name: /切影院:false/ }))

    // 有效收缩态为 true（navCollapsed=false 但 cinema=true）
    expect(getNavbar()).toHaveAttribute('data-collapsed', 'true')
    // 影院态不污染全局持久态：localStorage 不被写入
    expect(localStorage.getItem('jianvideo-nav-collapsed')).toBeNull()
  })

  it('影院态切出：导航恢复展开', async () => {
    const user = userEvent.setup()
    renderWithToggler()

    await user.click(screen.getByRole('button', { name: /切影院:false/ }))
    expect(getNavbar()).toHaveAttribute('data-collapsed', 'true')

    await user.click(screen.getByRole('button', { name: /切影院:true/ }))
    expect(getNavbar()).toHaveAttribute('data-collapsed', 'false')
  })

  it('全局持久收缩态优先生效：navCollapsed=true 时无论影院态导航均收缩', async () => {
    localStorage.setItem('jianvideo-nav-collapsed', '1')
    const user = userEvent.setup()
    renderWithToggler()

    // 预置持久收缩
    expect(getNavbar()).toHaveAttribute('data-collapsed', 'true')

    // 切入再切出影院态，导航始终收缩（持久态主导），且持久值不被影院切换改写
    await user.click(screen.getByRole('button', { name: /切影院:false/ }))
    expect(getNavbar()).toHaveAttribute('data-collapsed', 'true')
    await user.click(screen.getByRole('button', { name: /切影院:true/ }))
    expect(getNavbar()).toHaveAttribute('data-collapsed', 'true')
    expect(localStorage.getItem('jianvideo-nav-collapsed')).toBe('1')
  })
})

describe('AppLayout 导航可拖拽调宽（FR-115 扩展）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    useAuthStore.setState({ initialized: true, isAuthenticated: true, username: 'admin' })
  })

  const getNavbar = () => document.querySelector('[data-collapsed]') as HTMLElement

  it('展开态在 navbar 提供拖拽手柄（role=separator + 调宽无障碍名）', () => {
    renderLayout()

    const handle = within(getNavbar()).getByRole('separator', { name: '拖拽调整导航宽度' })
    expect(handle).toBeInTheDocument()
    expect(handle).toHaveAttribute('aria-valuemin', '160')
    expect(handle).toHaveAttribute('aria-valuemax', '360')
  })

  it('拖拽手柄按下并移动指针，更新展开宽度并持久化（夹紧 160–360）', () => {
    renderLayout()

    const handle = within(getNavbar()).getByRole('separator', { name: '拖拽调整导航宽度' })
    // 模拟一次拖拽：按下手柄 → 移动指针到 X=240 → 松开
    fireEvent.mouseDown(handle)
    fireEvent.mouseMove(window, { clientX: 240 })
    fireEvent.mouseUp(window)

    expect(localStorage.getItem('jianvideo.nav.width')).toBe('240')
    expect(handle).toHaveAttribute('aria-valuenow', '240')
  })

  it('拖拽到超出上限被夹紧到 360', () => {
    renderLayout()

    const handle = within(getNavbar()).getByRole('separator', { name: '拖拽调整导航宽度' })
    fireEvent.mouseDown(handle)
    fireEvent.mouseMove(window, { clientX: 1000 })
    fireEvent.mouseUp(window)

    expect(localStorage.getItem('jianvideo.nav.width')).toBe('360')
  })

  it('收缩态不显示拖拽手柄（固定图标宽度，不可拖）', async () => {
    const user = userEvent.setup()
    renderLayout()

    await user.click(within(getNavbar()).getByRole('button', { name: '收起导航' }))

    expect(getNavbar()).toHaveAttribute('data-collapsed', 'true')
    expect(within(getNavbar()).queryByRole('separator', { name: '拖拽调整导航宽度' })).toBeNull()
  })

  it('预置持久宽度，mount 后 navbar 采用该宽度（CSS 变量）', () => {
    localStorage.setItem('jianvideo.nav.width', '300')
    renderLayout()

    // Mantine AppShell 把 navbar 宽度写入 --app-shell-navbar-width CSS 变量
    const handle = within(getNavbar()).getByRole('separator', { name: '拖拽调整导航宽度' })
    expect(handle).toHaveAttribute('aria-valuenow', '300')
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

  it('命令面板内点击「时间轴」命令触发跳转（FR-117 后指向 /timeline）', async () => {
    const user = userEvent.setup()
    renderLayout()

    fireEvent.keyDown(document.body, { key: 'k', ctrlKey: true })
    const dialog = await screen.findByRole('dialog')

    // 输入过滤到「时间轴」后在面板内点击，断言 navigate 被调用到 '/timeline'（时间轴已迁址）
    const input = within(dialog).getByRole('textbox', { name: '命令' })
    await user.type(input, '时间轴')
    await user.click(within(dialog).getByText('时间轴'))

    expect(mockNavigate).toHaveBeenCalledWith('/timeline')
  })

  it('命令面板内点击「概览」命令跳转 /（FR-117）', async () => {
    const user = userEvent.setup()
    renderLayout()

    fireEvent.keyDown(document.body, { key: 'k', ctrlKey: true })
    const dialog = await screen.findByRole('dialog')

    const input = within(dialog).getByRole('textbox', { name: '命令' })
    await user.type(input, '概览')
    await user.click(within(dialog).getByText('概览'))

    expect(mockNavigate).toHaveBeenCalledWith('/')
  })
})

describe('AppLayout 用户头像下拉菜单（FR-95）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    useAuthStore.setState({ initialized: true, isAuthenticated: true, username: 'admin' })
  })

  it('顶栏提供用户菜单触发按钮（含用户名无障碍标签）', () => {
    renderLayout()

    expect(screen.getByRole('button', { name: /admin/ })).toBeInTheDocument()
  })

  it('默认不直接渲染「退出登录」项（收进菜单）', () => {
    renderLayout()

    // 菜单未展开时，退出登录项不在文档中
    expect(screen.queryByText('退出登录')).toBeNull()
  })

  it('点击头像展开菜单，菜单含用户名与「退出登录」项', async () => {
    const user = userEvent.setup()
    renderLayout()

    await user.click(screen.getByRole('button', { name: /admin/ }))

    // 展开后出现「退出登录」菜单项
    expect(await screen.findByText('退出登录')).toBeInTheDocument()
  })

  it('点击菜单内「退出登录」触发登出并跳转 /login', async () => {
    const user = userEvent.setup()
    renderLayout()

    await user.click(screen.getByRole('button', { name: /admin/ }))
    const item = await screen.findByText('退出登录')
    await user.click(item)

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/login')
    })
  })
})

describe('AppLayout 侧栏激活态 pill（FR-95）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    useAuthStore.setState({ initialized: true, isAuthenticated: true, username: 'admin' })
  })

  const getNavbar = () => document.querySelector('[data-collapsed]') as HTMLElement

  it('当前路由 /browse：桌面 navbar 中「目录」项标记为激活态', () => {
    renderLayoutAt('/browse')

    const navbar = getNavbar()
    const active = navbar.querySelectorAll('[data-active="true"]')
    // 恰有一个激活项，且其链接指向 /browse
    expect(active.length).toBe(1)
    expect(active[0].querySelector('a[href="/browse"]') ?? active[0].closest('a[href="/browse"]') ?? navbar.querySelector('a[href="/browse"][data-active="true"]')).not.toBeNull()
  })

  it('根路由 /：仅「概览」激活，不误激活其他项（前缀匹配排除根；FR-117）', () => {
    renderLayoutAt('/')

    const navbar = getNavbar()
    const active = navbar.querySelectorAll('a[data-active="true"]')
    // 概览为根路由 '/'，精确匹配下恰一个激活项且 href 为 '/'，时间轴 /timeline 不被误激活
    expect(active.length).toBe(1)
    expect(active[0]).toHaveAttribute('href', '/')
  })

  it('收缩态下激活项仍带激活标记（收缩态可辨）', async () => {
    const user = userEvent.setup()
    renderLayoutAt('/albums')

    await user.click(within(getNavbar()).getByRole('button', { name: '收起导航' }))

    const navbar = getNavbar()
    expect(navbar).toHaveAttribute('data-collapsed', 'true')
    const active = navbar.querySelector('a[data-active="true"]')
    expect(active).toHaveAttribute('href', '/albums')
  })
})

describe('AppLayout 页眉刷新按钮（FR-114）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    useAuthStore.setState({ initialized: true, isAuthenticated: true, username: 'admin' })
  })

  // 统计子内容挂载次数的探针：每次挂载自增一个外部计数，用于验证刷新触发重挂载
  function MountProbe({ onMount }: { onMount: () => void }) {
    useEffect(() => {
      onMount()
    }, [onMount])
    return <div>探针内容</div>
  }

  it('页眉渲染刷新按钮（含无障碍标签）', () => {
    renderLayout()
    expect(screen.getByRole('button', { name: '刷新当前页面' })).toBeInTheDocument()
  })

  it('点击刷新使主内容区重挂载（重跑数据拉取），导航/页眉不重置登录态', async () => {
    const user = userEvent.setup()
    const onMount = vi.fn()

    render(
      <MantineProvider>
        <MemoryRouter>
          <AppLayout>
            <MountProbe onMount={onMount} />
          </AppLayout>
        </MemoryRouter>
      </MantineProvider>,
    )

    // 初始挂载一次
    expect(onMount).toHaveBeenCalledTimes(1)

    // 点击刷新：内容区 key 变化致重挂载，探针再次挂载
    await user.click(screen.getByRole('button', { name: '刷新当前页面' }))
    expect(onMount).toHaveBeenCalledTimes(2)

    // 不整页 reload、不重置登录态：auth store 仍为已认证、用户菜单仍在
    expect(useAuthStore.getState().isAuthenticated).toBe(true)
    expect(screen.getByRole('button', { name: /admin/ })).toBeInTheDocument()
  })

  it('刷新不触发路由跳转（仅重载内容，不动导航）', async () => {
    const user = userEvent.setup()
    renderLayout()

    await user.click(screen.getByRole('button', { name: '刷新当前页面' }))
    expect(mockNavigate).not.toHaveBeenCalled()
  })
})

describe('AppLayout 路由切换淡入范围（FIX-2）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    useAuthStore.setState({ initialized: true, isAuthenticated: true, username: 'admin' })
  })

  const getNavbar = () => document.querySelector('[data-collapsed]') as HTMLElement

  it('淡入容器 .route-fade 仅包裹主内容区，不包含导航栏与页眉', () => {
    renderLayout()

    const fade = document.querySelector('.route-fade') as HTMLElement
    // 淡入容器存在且包裹页面内容
    expect(fade).not.toBeNull()
    expect(within(fade).getByText('页面内容')).toBeInTheDocument()

    // 导航栏（侧边栏）与页眉（含 logo）不在淡入容器内，避免随路由切换一起淡入/闪动
    const navbar = getNavbar()
    expect(fade.contains(navbar)).toBe(false)
    // 页眉 logo 与命令面板入口均在淡入容器之外
    const commandBtn = screen.getByRole('button', { name: '命令面板' })
    expect(fade.contains(commandBtn)).toBe(false)
  })
})

describe('AppLayout 导航交互完善（FR-115）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    useAuthStore.setState({ initialized: true, isAuthenticated: true, username: 'admin' })
  })

  const getNavbar = () => document.querySelector('[data-collapsed]') as HTMLElement

  it('「收起导航」按钮位于 navbar 底部（晚于最后一个导航链接出现）', () => {
    renderLayout()

    const navbar = getNavbar()
    const collapseBtn = within(navbar).getByRole('button', { name: '收起导航' })
    const systemNavLink = navbar.querySelector('a[href="/system"]') as HTMLElement

    // 文档顺序：收起按钮在最后一个导航链接（系统）之后（底部）
    const pos = collapseBtn.compareDocumentPosition(systemNavLink)
    expect(pos & Node.DOCUMENT_POSITION_PRECEDING).toBeTruthy()
  })

  it('点击 logo 切换导航收缩态并持久化（不再回首页）', async () => {
    const user = userEvent.setup()
    renderLayout()

    // logo 以「收起导航」无障碍标签的按钮承载（展开态）；点击后进入收缩态
    const logoBtn = screen.getAllByRole('button', { name: '收起导航' })[0]
    await user.click(logoBtn)

    expect(getNavbar()).toHaveAttribute('data-collapsed', 'true')
    expect(localStorage.getItem('jianvideo-nav-collapsed')).toBe('1')
    // logo 不再触发路由跳转
    expect(mockNavigate).not.toHaveBeenCalled()
  })

  it('点击 logo 再次切换可展开（两态切换）', async () => {
    const user = userEvent.setup()
    localStorage.setItem('jianvideo-nav-collapsed', '1')
    renderLayout()

    expect(getNavbar()).toHaveAttribute('data-collapsed', 'true')
    // 收缩态 logo 标签为「展开导航」
    const logoBtn = screen.getAllByRole('button', { name: '展开导航' })[0]
    await user.click(logoBtn)

    expect(getNavbar()).toHaveAttribute('data-collapsed', 'false')
    expect(localStorage.getItem('jianvideo-nav-collapsed')).toBe('0')
  })

  it('「概览」导航项指向 / 首页，「时间轴」指向 /timeline（FR-117 迁址）', () => {
    renderLayout()

    const navbar = getNavbar()
    // 概览取代时间轴成为首页入口；时间轴迁至 /timeline
    expect(within(navbar).getByRole('link', { name: '概览' })).toHaveAttribute('href', '/')
    expect(within(navbar).getByRole('link', { name: '时间轴' })).toHaveAttribute('href', '/timeline')
  })

  it('所有导航项均带 hover 反馈类 nav-link（非激活项亦有可见 hover 背景）', () => {
    renderLayoutAt('/browse')

    const navbar = getNavbar()
    // 12 个导航项均挂 nav-link 类（hover 浅底 + 过渡由 index.css 的 .nav-link 承接）；新增概览（FR-117）
    expect(navbar.querySelectorAll('.nav-link').length).toBe(12)
    // 激活项的外层 <a data-active> 内含 nav-link；hover 浅底由 `a:not([data-active]) .nav-link:hover` 排除激活项
    expect(navbar.querySelectorAll('a[data-active="true"] .nav-link').length).toBe(1)
  })
})
