import { useState, useEffect } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { AppShell, Text, Group, ActionIcon, Burger, Drawer, Stack, Tooltip, useMantineColorScheme, useComputedColorScheme } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { IconVideo, IconLogout, IconSettings, IconClock, IconFolderOpen, IconPhoto, IconSun, IconMoon, IconDeviceDesktopAnalytics, IconTrash, IconMapPin, IconLayoutSidebarLeftCollapse, IconLayoutSidebarLeftExpand } from '@tabler/icons-react'
import { useAuthStore } from '@/stores/auth'
import { useNavCollapsed } from '@/hooks/useNavCollapsed'
import { getSystemInfo } from '@/api/system'
import ScanTaskIndicator from './ScanTaskIndicator'
import UpdateIndicator from './UpdateIndicator'

// 桌面导航展开 / 收缩两态的 navbar 宽度（像素）：收缩仅留图标，展开容纳图标 + 文字
const NAVBAR_WIDTH_EXPANDED = 180
const NAVBAR_WIDTH_COLLAPSED = 64

/** 全局布局 — Mantine AppShell */
export default function AppLayout({ children }: { children: React.ReactNode }) {
  const { username, logout } = useAuthStore()
  const navigate = useNavigate()
  const [drawerOpened, { toggle: toggleDrawer, close: closeDrawer }] = useDisclosure(false)
  // 桌面导航收缩态（FR-54）：持久化到 localStorage，刷新后保持；仅影响桌面 Navbar，移动端抽屉不受影响
  const [navCollapsed, toggleNavCollapsed] = useNavCollapsed()
  // 主题切换：当前色方案与切换方法（认证恢复已交由 ProtectedRoute 负责）
  const { toggleColorScheme } = useMantineColorScheme()
  const computedColorScheme = useComputedColorScheme('dark', { getInitialValueInEffect: true })
  // 页脚版本号（FR-57）：取自系统信息；失败静默不显，不阻塞布局
  const [appVersion, setAppVersion] = useState('')

  // 拉取应用版本用于页脚展示；失败仅静默（页脚版本缺省，不影响其余布局）
  useEffect(() => {
    let active = true
    getSystemInfo()
      .then((info) => { if (active) setAppVersion(info.app_version) })
      .catch(() => { /* 版本拉取失败不阻塞页面，页脚不显版本即可 */ })
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
    // 系统信息与设置合并为单页两 tab（FR-55），导航合并为一个「系统」入口
    { path: '/system', label: '系统', icon: IconDeviceDesktopAnalytics },
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

  return (
    <AppShell
      header={{ height: 56 }}
      navbar={{ width: navCollapsed ? NAVBAR_WIDTH_COLLAPSED : NAVBAR_WIDTH_EXPANDED, breakpoint: 'sm' }}
      footer={{ height: 36 }}
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
        </Stack>
      </Drawer>

      <AppShell.Navbar p="xs" visibleFrom="sm" data-collapsed={navCollapsed}>
        <Stack gap="xs" style={{ flex: 1 }}>
          {navItems.map((item) => renderNavLink(item, undefined, navCollapsed))}
        </Stack>
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

      {/* 全局页脚（FR-57）：左侧版本号 + 右侧「开源协议」链接，桌面与移动端均可见 */}
      <AppShell.Footer p="xs">
        <Group justify="space-between" h="100%" px="md">
          <Text size="xs" c="dimmed">
            JianVideo{appVersion ? ` v${appVersion}` : ''}
          </Text>
          <Link to="/licenses" style={{ textDecoration: 'none' }}>
            <Text size="xs" c="dimmed">开源协议</Text>
          </Link>
        </Group>
      </AppShell.Footer>
    </AppShell>
  )
}
