import { useEffect } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { AppShell, Text, Group, ActionIcon, Burger, Drawer, Stack } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { IconVideo, IconLogout, IconBooks, IconClock } from '@tabler/icons-react'
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
            <Link to="/library" style={{ textDecoration: 'none' }}>
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
          <Link
            to="/timeline"
            onClick={() => handleNavigate('/timeline')}
            style={{ textDecoration: 'none' }}
          >
            <Group gap={8} p="xs" style={{ borderRadius: 'var(--mantine-radius-sm)', cursor: 'pointer' }}>
              <IconClock size={16} />
              <Text size="sm">时间轴</Text>
            </Group>
          </Link>
          <Link
            to="/library"
            onClick={() => handleNavigate('/library')}
            style={{ textDecoration: 'none' }}
          >
            <Group gap={8} p="xs" style={{ borderRadius: 'var(--mantine-radius-sm)', cursor: 'pointer' }}>
              <IconBooks size={16} />
              <Text size="sm">媒体库</Text>
            </Group>
          </Link>
        </Stack>
      </Drawer>

      <AppShell.Navbar p="xs" visibleFrom="sm">
        <Stack gap="xs">
          <Link to="/timeline" style={{ textDecoration: 'none' }}>
            <Group gap={8} p="xs" style={{ borderRadius: 'var(--mantine-radius-sm)', cursor: 'pointer' }}>
              <IconClock size={16} />
              <Text size="sm">时间轴</Text>
            </Group>
          </Link>
          <Link to="/library" style={{ textDecoration: 'none' }}>
            <Group gap={8} p="xs" style={{ borderRadius: 'var(--mantine-radius-sm)', cursor: 'pointer' }}>
              <IconBooks size={16} />
              <Text size="sm">媒体库</Text>
            </Group>
          </Link>
        </Stack>
      </AppShell.Navbar>

      <AppShell.Main>{children}</AppShell.Main>
    </AppShell>
  )
}
