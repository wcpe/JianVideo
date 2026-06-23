import { useState, useEffect } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { AppShell, Text, Group, ActionIcon, Burger, Drawer, Stack, Tooltip, useMantineColorScheme, useComputedColorScheme } from '@mantine/core'
import { useDisclosure, useHotkeys } from '@mantine/hooks'
import { IconVideo, IconLogout, IconSettings, IconClock, IconFolderOpen, IconPhoto, IconSun, IconMoon, IconDeviceDesktopAnalytics, IconTrash, IconMapPin, IconStethoscope, IconCopy, IconChartBar, IconLayoutSidebarLeftCollapse, IconLayoutSidebarLeftExpand, IconLicense, IconCommand, IconSearch, IconRefresh, IconPalette } from '@tabler/icons-react'
import { useAuthStore } from '@/stores/auth'
import { useNavCollapsed } from '@/hooks/useNavCollapsed'
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
  const [drawerOpened, { toggle: toggleDrawer, close: closeDrawer }] = useDisclosure(false)
  // 命令面板（FR-74）：全局 Ctrl/Cmd+K 打开、header 入口按钮亦可触发
  const [paletteOpened, { open: openPalette, close: closePalette }] = useDisclosure(false)
  // 桌面导航收缩态（FR-54）：持久化到 localStorage，刷新后保持；仅影响桌面 Navbar，移动端抽屉不受影响
  const [navCollapsed, toggleNavCollapsed] = useNavCollapsed()
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

  const handleNavigate = (path: string) => {
    navigate(path)
    closeDrawer()
  }

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
    // 系统信息与设置合并为单页两 tab（FR-55），导航合并为一个「系统」入口
    { path: '/system', label: '系统', icon: IconDeviceDesktopAnalytics },
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
    const link = (
      <Link key={path} to={path} onClick={onNavigate} style={{ textDecoration: 'none' }}>
        <Group
          gap={8}
          p="xs"
          justify={collapsed ? 'center' : undefined}
          style={{ borderRadius: 'var(--mantine-radius-sm)', cursor: 'pointer' }}
        >
          <Icon size={16} />
          {!collapsed && <Text size="sm">{label}</Text>}
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
      navbar={{ width: navCollapsed ? NAVBAR_WIDTH_COLLAPSED : NAVBAR_WIDTH_EXPANDED, breakpoint: 'sm' }}
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
            <Link to="/" style={{ textDecoration: 'none' }}>
              <Group gap={6}>
                <IconVideo size={22} style={{ color: 'var(--mantine-color-purple-4)' }} />
                <Text fw={700} size="lg" style={{ color: 'var(--mantine-color-purple-4)' }}>
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
            <Text size="sm" c="dimmed">{username}</Text>
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
            <ActionIcon
              variant="subtle"
              color="gray"
              onClick={handleLogout}
              title="退出登录"
              aria-label="退出登录"
            >
              <IconLogout size={18} />
            </ActionIcon>
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
          {navItems.map((item) => renderNavLink(item, () => handleNavigate(item.path)))}
          {/* 版本号 + 「开源协议」入口（FR-61）：原页脚在移动端可见，移除后于抽屉底部补回 */}
          {renderVersionLicense(false, closeDrawer)}
        </Stack>
      </Drawer>

      <AppShell.Navbar p="xs" visibleFrom="sm" data-collapsed={navCollapsed}>
        <Stack gap="xs" style={{ flex: 1 }}>
          {navItems.map((item) => renderNavLink(item, undefined, navCollapsed))}
        </Stack>
        {/* 版本号 + 「开源协议」入口（FR-61）：取代原页脚，置于收缩按钮上方，适配收缩态 */}
        {renderVersionLicense(navCollapsed)}
        {/* 收缩 / 展开切换按钮（FR-54）：置于 navbar 底部，随状态切换图标与无障碍标签 */}
        <Group justify={navCollapsed ? 'center' : 'flex-end'} mt="xs">
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
      </AppShell.Navbar>

      <AppShell.Main>{children}</AppShell.Main>

      {/* 全局命令面板（FR-74）：Ctrl/Cmd+K 或 header 入口打开 */}
      <CommandPalette opened={paletteOpened} onClose={closePalette} commands={commands} />
    </AppShell>
  )
}
