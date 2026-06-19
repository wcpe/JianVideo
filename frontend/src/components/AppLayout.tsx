import { Link, useNavigate } from 'react-router-dom'
import { AppShell, Text, Group, ActionIcon } from '@mantine/core'
import { IconVideo, IconLogout, IconBooks } from '@tabler/icons-react'
import { useAuthStore } from '@/stores/auth'

/** 全局布局 — Mantine AppShell */
export default function AppLayout({ children }: { children: React.ReactNode }) {
  const { username, logout } = useAuthStore()
  const navigate = useNavigate()

  const handleLogout = async () => {
    await logout()
    navigate('/login')
  }

  return (
    <AppShell
      header={{ height: 56 }}
      navbar={{ width: 180, breakpoint: 'sm' }}
      padding="md"
    >
      <AppShell.Header>
        <Group justify="space-between" h="100%" px="md">
          <Link to="/library" style={{ textDecoration: 'none' }}>
            <Group gap={6}>
              <IconVideo size={22} style={{ color: 'var(--mantine-color-purple-4)' }} />
              <Text fw={700} size="lg" style={{ color: 'var(--mantine-color-purple-4)' }}>
                JianVideo
              </Text>
            </Group>
          </Link>

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

      <AppShell.Navbar p="xs">
        <Link to="/library" style={{ textDecoration: 'none' }}>
          <Group gap={8} p="xs" style={{ borderRadius: 'var(--mantine-radius-sm)', cursor: 'pointer' }}>
            <IconBooks size={16} />
            <Text size="sm">媒体库</Text>
          </Group>
        </Link>
      </AppShell.Navbar>

      <AppShell.Main>{children}</AppShell.Main>
    </AppShell>
  )
}
