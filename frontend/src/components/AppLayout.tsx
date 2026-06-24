import { useState, useEffect, useMemo, useCallback } from 'react'
import { Link, useNavigate, useLocation } from 'react-router-dom'
import { AppShell, Text, Group, ActionIcon, Burger, Drawer, Stack, Tooltip, Divider, Menu, Avatar, UnstyledButton, useMantineColorScheme, useComputedColorScheme } from '@mantine/core'
import { useDisclosure, useHotkeys } from '@mantine/hooks'
import { IconVideo, IconLogout, IconSettings, IconClock, IconFolderOpen, IconPhoto, IconSun, IconMoon, IconDeviceDesktopAnalytics, IconTrash, IconMapPin, IconStethoscope, IconCopy, IconChartBar, IconLayoutSidebarLeftCollapse, IconLayoutSidebarLeftExpand, IconLicense, IconCommand, IconSearch, IconRefresh, IconPalette, IconMovie } from '@tabler/icons-react'
import { useAuthStore } from '@/stores/auth'
import { useNavCollapsed } from '@/hooks/useNavCollapsed'
import { CinemaContext } from '@/hooks/cinema-context'
import { getSystemInfo } from '@/api/system'
import ScanTaskIndicator from './ScanTaskIndicator'
import UpdateIndicator from './UpdateIndicator'
import CommandPalette, { type Command } from './CommandPalette'

// 桌面导航展开 / 收缩两态的 navbar 宽度（像素）：收缩仅留图标，展开容纳图标 + 文字
const NAVBAR_WIDTH_EXPANDED = 180
const NAVBAR_WIDTH_COLLAPSED = 64

/** 全局布局 — Mantine AppShell */
export default function AppLayout({ children }: { children: React.ReactNode }) {
  const { username, logout } = useAuthStore()
  const navigate = useNavigate()
  // 当前路由路径，用于侧栏激活态 pill（FR-95）判定
  const { pathname } = useLocation()
  const [drawerOpened, { toggle: toggleDrawer, close: closeDrawer }] = useDisclosure(false)
  // 命令面板（FR-74）：全局 Ctrl/Cmd+K 打开、header 入口按钮亦可触发
  const [paletteOpened, { open: openPalette, close: closePalette }] = useDisclosure(false)
  // 桌面导航收缩态（FR-54）：持久化到 localStorage，刷新后保持；仅影响桌面 Navbar，移动端抽屉不受影响
  const [navCollapsed, toggleNavCollapsed] = useNavCollapsed()
  // 影院模式（FR-85）：播放页临时收起导航的本地会话态，不写 localStorage、不改 navCollapsed 持久语义。
  // 自持本地态并经 CinemaContext 下发给子页面（如播放页）；不消费自身 Provider，便于下方计算有效收缩态。
  const [cinema, setCinemaState] = useState(false)
  const setCinema = useCallback((value: boolean) => setCinemaState(value), [])
  const cinemaValue = useMemo(() => ({ cinema, setCinema }), [cinema, setCinema])
  // 有效收缩态 = 全局持久收缩 或 影院态临时收缩；二者任一为真即收缩，互不污染
  const effectiveCollapsed = navCollapsed || cinema
  // 主题切换：当前色方案与切换方法（认证恢复已交由 ProtectedRoute 负责）
  const { toggleColorScheme } = useMantineColorScheme()
  const computedColorScheme = useComputedColorScheme('dark', { getInitialValueInEffect: true })
  // 导航底部版本号（FR-61）：取自系统信息；失败静默不显，不阻塞布局
  const [appVersion, setAppVersion] = useState('')

  // 拉取应用版本用于导航底部展示；失败仅静默（版本缺省，不影响其余布局）
  useEffect(() => {
    let active = true
    getSystemInfo()
      .then((info) => { if (active) setAppVersion(info.app_version) })
      .catch(() => { /* 版本拉取失败不阻塞页面，不显版本即可 */ })
    return () => { active = false }
  }, [])

  const handleLogout = async () => {
    await logout()
    navigate('/login')
  }

  // 导航项激活判定（FR-95）：根路由 '/' 用精确匹配，避免前缀匹配误命中所有路径；
  // 其余项命中自身或其子路径（如 /albums 命中 /albums 与 /albums/...）。
  const isNavActive = (path: string) =>
    path === '/' ? pathname === '/' : pathname === path || pathname.startsWith(`${path}/`)

  // 导航项定义（桌面 Navbar 与移动 Drawer 共用，避免重复）
  const navItems = [
    { path: '/library-manager', label: '管理', icon: IconSettings },
    { path: '/', label: '时间轴', icon: IconClock },
    { path: '/browse', label: '目录', icon: IconFolderOpen },
    { path: '/albums', label: '相册', icon: IconPhoto },
    { path: '/map', label: '地图', icon: IconMapPin },
    { path: '/recycle', label: '回收站', icon: IconTrash },
    // 问题媒体 / 健康巡检（FR-73）
    { path: '/inspect', label: '巡检', icon: IconStethoscope },
    // 感知哈希去重（FR-70）：检出近似重复媒体、批量清理候选
    { path: '/duplicates', label: '重复项', icon: IconCopy },
    // 观看热力与统计（FR-75）：观看次数、续播位置热力、最近观看时间线
    { path: '/stats', label: '统计', icon: IconChartBar },
    // 转码预设与预生成队列（FR-77）：自定义编码/分辨率预设、预热首播
    { path: '/transcode', label: '转码', icon: IconMovie },
    // 系统信息与设置合并为单页两 tab（FR-55），导航合并为一个「系统」入口
    { path: '/system', label: '系统', icon: IconDeviceDesktopAnalytics },
  ]

  // 左侧导航分组（FR-83）：把扁平 navItems 按「浏览 / 管理 / 系统」三组重排用于渲染。
  // navItems 仍是命令面板（FR-74）的扁平真源；此处仅按 path 引用其中同一对象做视觉分组，不复制项、不改路径/语义。
  const navItemByPath = (path: string) => navItems.find((item) => item.path === path)!
  const navGroups = [
    { title: '浏览', items: ['/', '/browse', '/albums', '/map', '/stats'].map(navItemByPath) },
    { title: '管理', items: ['/library-manager', '/recycle', '/inspect', '/duplicates', '/transcode'].map(navItemByPath) },
    { title: '系统', items: ['/system'].map(navItemByPath) },
  ]

  // 命令面板（FR-74）注册全局快捷键 Ctrl/Cmd+K；useHotkeys 默认对匹配事件 preventDefault
  useHotkeys([['mod+K', openPalette]])

  // 命令清单（FR-74）：跳转类复用 navItems + 开源协议/扫描媒体库/搜索；直接执行类切主题/收展导航/退出登录。
  // 在此构造以拿到 navigate / toggleColorScheme / toggleNavCollapsed / logout 闭包，注入 CommandPalette 做纯展示。
  const commands: Command[] = [
    ...navItems.map((item) => ({
      id: `nav-${item.path}`,
      label: item.label,
      icon: item.icon,
      run: () => navigate(item.path),
    })),
    // 「搜索」与「扫描媒体库」不在面板内直接执行（依赖页面级上下文），仅跳到对应页面承接
    { id: 'search', label: '搜索', icon: IconSearch, run: () => navigate('/') },
    { id: 'scan', label: '扫描媒体库', icon: IconRefresh, run: () => navigate('/library-manager') },
    { id: 'licenses', label: '开源协议', icon: IconLicense, run: () => navigate('/licenses') },
    { id: 'toggle-theme', label: '切换主题', icon: IconPalette, run: () => toggleColorScheme() },
    { id: 'toggle-nav', label: '收起/展开导航', icon: IconLayoutSidebarLeftCollapse, run: () => toggleNavCollapsed() },
    { id: 'logout', label: '退出登录', icon: IconLogout, run: () => { logout(); navigate('/login') } },
  ]

  // 单个导航链接。onNavigate 用于移动端点击后关闭抽屉；collapsed 为收缩态——仅渲染图标并以 Tooltip 提示导航名
  const renderNavLink = (
    { path, label, icon: Icon }: (typeof navItems)[number],
    onNavigate?: () => void,
    collapsed = false,
  ) => {
    // 激活态 pill（FR-95）：当前路由对应项加品牌紫浅底 + 紫字（复用 FR-93 primaryColor purple）、token 圆角；
    // data-active 供测试与样式钩子，收缩态同样高亮底以保可辨。
    const active = isNavActive(path)
    const link = (
      <Link
        key={path}
        to={path}
        onClick={onNavigate}
        data-active={active || undefined}
        style={{ textDecoration: 'none' }}
      >
        <Group
          gap={8}
          p="xs"
          justify={collapsed ? 'center' : undefined}
          style={{
            borderRadius: 'var(--mantine-radius-sm)',
            cursor: 'pointer',
            backgroundColor: active ? 'var(--mantine-color-purple-light)' : undefined,
            color: active ? 'var(--mantine-color-purple-light-color)' : undefined,
          }}
        >
          <Icon size={16} />
          {!collapsed && <Text size="sm" c={active ? 'inherit' : undefined} fw={active ? 600 : undefined}>{label}</Text>}
        </Group>
      </Link>
    )
    // 收缩态文字隐藏，hover 出 Tooltip（label=导航名）以保可用性
    return collapsed ? (
      <Tooltip key={path} label={label} position="right" withArrow>
        {link}
      </Tooltip>
    ) : (
      link
    )
  }

  // 分组渲染（FR-83）：桌面 Navbar 与移动 Drawer 共用。
  // 展开态每组前置 dimmed 小标题；收缩态（64px）不显标题文字，改用 Divider 分隔（首组前不加）以免破版。
  const renderNavGroups = (onNavigate?: () => void, collapsed = false) =>
    navGroups.map((group, groupIndex) => (
      <Stack key={group.title} gap="xs">
        {collapsed
          ? groupIndex > 0 && <Divider my={2} />
          : (
            <Text size="xs" c="dimmed" fw={600} px={4} mt={groupIndex > 0 ? 'xs' : 0}>
              {group.title}
            </Text>
          )}
        {group.items.map((item) => renderNavLink(item, onNavigate, collapsed))}
      </Stack>
    ))

  // 版本号 + 「开源协议」入口（FR-61）：取代原页脚展示。
  // collapsed 收缩态仅渲染协议图标 + Tooltip（含版本号），避免 64px 内文字截断；
  // onNavigate 用于移动端抽屉点击后关闭抽屉。
  const versionLabel = `JianVideo${appVersion ? ` v${appVersion}` : ''}`
  const renderVersionLicense = (collapsed = false, onNavigate?: () => void) => {
    if (collapsed) {
      // 收缩态：图标态协议入口，hover 出 Tooltip 同时展示版本号与「开源协议」
      return (
        <Group justify="center" mb="xs">
          <Tooltip label={`${versionLabel} · 开源协议`} position="right" withArrow>
            <Link to="/licenses" aria-label="开源协议" style={{ textDecoration: 'none' }}>
              <IconLicense size={16} style={{ color: 'var(--mantine-color-dimmed)' }} />
            </Link>
          </Tooltip>
        </Group>
      )
    }
    // 展开态：版本号文本 + 「开源协议」链接
    return (
      <Group justify="space-between" mb="xs" px={4}>
        <Text size="xs" c="dimmed">{versionLabel}</Text>
        <Link to="/licenses" onClick={onNavigate} style={{ textDecoration: 'none' }}>
          <Text size="xs" c="dimmed">开源协议</Text>
        </Link>
      </Group>
    )
  }

  return (
    <AppShell
      header={{ height: 56 }}
      navbar={{ width: effectiveCollapsed ? NAVBAR_WIDTH_COLLAPSED : NAVBAR_WIDTH_EXPANDED, breakpoint: 'sm' }}
      padding="md"
    >
      <AppShell.Header>
        <Group justify="space-between" h="100%" px="md">
          <Group gap="sm">
            {/* 移动端汉堡菜单按钮 */}
            <Burger
              opened={drawerOpened}
              onClick={toggleDrawer}
              size="sm"
              aria-label="导航菜单"
              hiddenFrom="sm"
            />
            {/* 品牌标志（FR-95 放大 logo）：图标加大并以紫浅底圆框托底，提对比、增品牌存在感 */}
            <Link to="/" style={{ textDecoration: 'none' }}>
              <Group gap={8}>
                <Group
                  justify="center"
                  align="center"
                  style={{
                    width: 36,
                    height: 36,
                    borderRadius: 'var(--mantine-radius-md)',
                    backgroundColor: 'var(--mantine-color-purple-light)',
                  }}
                >
                  <IconVideo size={26} style={{ color: 'var(--mantine-color-purple-light-color)' }} />
                </Group>
                <Text fw={700} size="xl" style={{ color: 'var(--mantine-color-purple-4)' }}>
                  JianVideo
                </Text>
              </Group>
            </Link>
          </Group>

          <Group gap="sm">
            {/* 命令面板入口（FR-74）：移动端无物理键盘时点击打开，桌面端 Ctrl/Cmd+K 亦可 */}
            <ActionIcon
              variant="subtle"
              color="gray"
              onClick={openPalette}
              title="命令面板（Ctrl+K）"
              aria-label="命令面板"
            >
              <IconCommand size={18} />
            </ActionIcon>
            {/* 「更新可用」提示（FR-58）：有新版本时常驻展示，点击跳转系统信息 tab 更新区 */}
            <UpdateIndicator />
            {/* 扫描任务队列指示器（FR-29）：有进行中任务时常驻展示 */}
            <ScanTaskIndicator />
            {/* 主题切换：暗色显示太阳（点击切浅色），浅色显示月亮（点击切暗色） */}
            <ActionIcon
              variant="subtle"
              color="gray"
              onClick={toggleColorScheme}
              title="切换主题"
              aria-label="切换主题"
            >
              {computedColorScheme === 'dark' ? <IconSun size={18} /> : <IconMoon size={18} />}
            </ActionIcon>
            {/* 用户头像下拉菜单（FR-95）：把「用户名 + 退出登录」收进头像菜单，减少顶栏拥挤。
                target 头像以用户名首字符为标识，菜单含用户名（dimmed 标签）与「退出登录」项。 */}
            <Menu position="bottom-end" withArrow shadow="md" width={180}>
              <Menu.Target>
                <Tooltip label={username} position="bottom" withArrow>
                  <UnstyledButton aria-label={`用户菜单：${username}`}>
                    <Avatar color="purple" radius="xl" size={32}>
                      {username ? username.charAt(0).toUpperCase() : '?'}
                    </Avatar>
                  </UnstyledButton>
                </Tooltip>
              </Menu.Target>
              <Menu.Dropdown>
                <Menu.Label>{username}</Menu.Label>
                <Menu.Item
                  color="red"
                  leftSection={<IconLogout size={16} />}
                  onClick={handleLogout}
                >
                  退出登录
                </Menu.Item>
              </Menu.Dropdown>
            </Menu>
          </Group>
        </Group>
      </AppShell.Header>

      {/* 移动端抽屉导航 */}
      <Drawer
        opened={drawerOpened}
        onClose={closeDrawer}
        title="导航"
        padding="md"
        size={200}
        hiddenFrom="sm"
      >
        <Stack gap="xs">
          {/* 分组导航（FR-83）：抽屉内固定展开，点击后关闭抽屉（跳转由各项 Link 负责） */}
          {renderNavGroups(closeDrawer, false)}
          {/* 版本号 + 「开源协议」入口（FR-61）：原页脚在移动端可见，移除后于抽屉底部补回 */}
          {renderVersionLicense(false, closeDrawer)}
        </Stack>
      </Drawer>

      <AppShell.Navbar p="xs" visibleFrom="sm" data-collapsed={effectiveCollapsed}>
        {/* 收缩 / 展开切换按钮（FR-54，FR-95 入口前移）：置于 navbar 顶部更易发现，
            随状态切换图标与无障碍标签 */}
        <Group justify={navCollapsed ? 'center' : 'flex-end'} mb="xs">
          <ActionIcon
            variant="subtle"
            color="gray"
            onClick={toggleNavCollapsed}
            title={navCollapsed ? '展开导航' : '收起导航'}
            aria-label={navCollapsed ? '展开导航' : '收起导航'}
          >
            {navCollapsed ? <IconLayoutSidebarLeftExpand size={18} /> : <IconLayoutSidebarLeftCollapse size={18} />}
          </ActionIcon>
        </Group>
        <Stack gap="xs" style={{ flex: 1 }}>
          {/* 分组导航（FR-83）：随有效收缩态（持久收缩 或 影院态，FR-85）切换展开/收缩态渲染 */}
          {renderNavGroups(undefined, effectiveCollapsed)}
        </Stack>
        {/* 版本号 + 「开源协议」入口（FR-61）：取代原页脚，置于 navbar 底部，适配有效收缩态 */}
        {renderVersionLicense(effectiveCollapsed)}
      </AppShell.Navbar>

      {/* 影院模式上下文（FR-85）：仅向页面内容下发本地态，让播放页可临时收起导航扩大视频区 */}
      <AppShell.Main>
        <CinemaContext.Provider value={cinemaValue}>{children}</CinemaContext.Provider>
      </AppShell.Main>

      {/* 全局命令面板（FR-74）：Ctrl/Cmd+K 或 header 入口打开 */}
      <CommandPalette opened={paletteOpened} onClose={closePalette} commands={commands} />
    </AppShell>
  )
}
