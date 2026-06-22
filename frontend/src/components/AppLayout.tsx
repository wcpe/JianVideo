import { Link, useNavigate } from 'react-router-dom'
import { AppShell, Text, Group, ActionIcon, Burger, Drawer, Stack, useMantineColorScheme, useComputedColorScheme } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { IconVideo, IconLogout, IconSettings, IconClock, IconFolderOpen, IconPhoto, IconSun, IconMoon, IconDeviceDesktopAnalytics, IconAdjustments, IconTrash, IconMapPin } from '@tabler/icons-react'
import { useAuthStore } from '@/stores/auth'
import ScanTaskIndicator from './ScanTaskIndicator'

/** 全局布局 — Mantine AppShell */
export default function AppLayout({ children }: { children: React.ReactNode }) {
  const { username, logout } = useAuthStore()
  const navigate = useNavigate()
  const [drawerOpened, { toggle: toggleDrawer, close: closeDrawer }] = useDisclosure(false)
  // 主题切换：当前色方案与切换方法（认证恢复已交由 ProtectedRoute 负责）
  const { toggleColorScheme } = useMantineColorScheme()
  const computedColorScheme = useComputedColorScheme('dark', { getInitialValueInEffect: true })

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
    { path: '/system', label: '系统信息', icon: IconDeviceDesktopAnalytics },
    { path: '/settings', label: '设置', icon: IconAdjustments },
  ]

  // 单个导航链接，onNavigate 用于移动端点击后关闭抽屉
  const renderNavLink = ({ path, label, icon: Icon }: (typeof navItems)[number], onNavigate?: () => void) => (
    <Link key={path} to={path} onClick={onNavigate} style={{ textDecoration: 'none' }}>
      <Group gap={8} p="xs" style={{ borderRadius: 'var(--mantine-radius-sm)', cursor: 'pointer' }}>
        <Icon size={16} />
        <Text size="sm">{label}</Text>
      </Group>
    </Link>
  )

  return (
    <AppShell
      header={{ height: 56 }}
      navbar={{ width: 180, breakpoint: 'sm' }}
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

      <AppShell.Navbar p="xs" visibleFrom="sm">
        <Stack gap="xs">
          {navItems.map((item) => renderNavLink(item))}
        </Stack>
      </AppShell.Navbar>

      <AppShell.Main>{children}</AppShell.Main>
    </AppShell>
  )
}
