import { useForm } from '@mantine/form'
import { TextInput, Button, Paper, Title, Text, Alert, Center, Box } from '@mantine/core'
import { IconLock, IconUser, IconAlertCircle } from '@tabler/icons-react'
import { useNavigate } from 'react-router-dom'
import { useAuthStore } from '@/stores/auth'
import { notifications } from '@mantine/notifications'

/** 登录页 — Mantine 居中卡片表单 */
export default function LoginPage() {
  const { login, loading, error, clearError } = useAuthStore()
  const navigate = useNavigate()

  const form = useForm({
    initialValues: { username: '', password: '' },
    validate: {
      username: (v) => v.trim() ? null : '请输入用户名',
      password: (v) => v.trim() ? null : '请输入密码',
    },
  })

  const handleSubmit = async (values: { username: string; password: string }) => {
    try {
      await login(values.username.trim(), values.password)
      notifications.show({
        title: '登录成功',
        message: `欢迎回来，${values.username}`,
        color: 'green',
        autoClose: 2000,
      })
      navigate('/library')
    } catch {
      // 错误已在 store 中设置
    }
  }

  return (
    <Center h="100vh" w="100%">
      <Box w={360} maw="90vw">
        <Paper withBorder p="xl" radius="md" bg="dark.7">
          <Title order={2} ta="center" mb={4} c="white">
            JianVideo
          </Title>
          <Text size="sm" c="dimmed" ta="center" mb="lg">
            请登录以继续
          </Text>

          {error && (
            <Alert
              icon={<IconAlertCircle size={16} />}
              title="登录失败"
              color="red"
              mb="md"
              withCloseButton
              onClose={clearError}
            >
              {error}
            </Alert>
          )}

          <form onSubmit={form.onSubmit(handleSubmit)}>
            <TextInput
              label="用户名"
              placeholder="输入用户名"
              leftSection={<IconUser size={16} />}
              {...form.getInputProps('username')}
              autoFocus
              mb="sm"
            />
            <TextInput
              label="密码"
              placeholder="输入密码"
              type="password"
              leftSection={<IconLock size={16} />}
              {...form.getInputProps('password')}
              mb="lg"
            />
            <Button
              type="submit"
              fullWidth
              loading={loading}
              disabled={!form.isValid()}
            >
              {loading ? '登录中...' : '登录'}
            </Button>
          </form>
        </Paper>
      </Box>
    </Center>
  )
}
