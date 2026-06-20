import { useEffect } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { AppShell, Text, Group, ActionIcon, Burger, Drawer, Stack } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { IconVideo, IconLogout, IconSettings, IconClock, IconFolderOpen } from '@tabler/icons-react'
import { useAuthStore } from '@/stores/auth'

/** 全局布局 — Mantine AppShell */
export default function AppLayout({ children }: { children: React.ReactNode }) {
  const { username, logout, init } = useAuthStore()
  const navigate = useNavigate()
  const [drawerOpened, { toggle: toggleDrawer, close: closeDrawer }] = useDisclosure(false)

  // 应用挂载时恢复认证状态
  useEffect(() => {
    init()
  }, [init])

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
            <Text size="sm" c="dimmed">{username}</Text>
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
