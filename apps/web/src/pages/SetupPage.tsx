import { useForm } from '@mantine/form';
import { TextInput, Button, Paper, Title, Text, Alert, Center, Box, Group } from '@mantine/core';
import { IconLock, IconUser, IconAlertCircle } from '@tabler/icons-react';
import { useNavigate } from 'react-router-dom';
import { useAuthStore } from '@/stores/auth';
import { notifications } from '@mantine/notifications';
import BrandLogo from '@/components/BrandLogo';

/** 首次初始化引导页（FR-109）：系统无用户时配置管理员账号密码并自动登录。 */
export default function SetupPage() {
  const { setup, loading, error, clearError } = useAuthStore();
  const navigate = useNavigate();

  const form = useForm({
    initialValues: { username: '', password: '', confirm: '' },
    validate: {
      username: (v) => (v.trim() ? null : '请输入用户名'),
      password: (v) => (v.length >= 6 ? null : '密码至少 6 位'),
      confirm: (v, values) => (v === values.password ? null : '两次输入的密码不一致'),
    },
  });

  const handleSubmit = async (values: { username: string; password: string }) => {
    try {
      await setup(values.username.trim(), values.password);
      notifications.show({
        title: '初始化完成',
        message: `欢迎使用 JianVideo，${values.username}`,
        color: 'green',
        autoClose: 2000,
      });
      navigate('/');
    } catch {
      // 错误已在 store 中设置
    }
  };

  return (
    <Center h="100vh" w="100%">
      <Box w={360} maw="90vw">
        <Paper withBorder p="xl" radius="md" bg="var(--mantine-color-default)">
          <Group justify="center" mb="xs">
            <BrandLogo size={64} />
          </Group>
          <Title order={2} ta="center" mb={4} c="purple.4">
            初始化 JianVideo
          </Title>
          <Text size="sm" c="dimmed" ta="center" mb="lg">
            首次使用，请设置管理员账号
          </Text>

          {error && (
            <Alert
              icon={<IconAlertCircle size={16} />}
              title="初始化失败"
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
              placeholder="设置管理员用户名"
              leftSection={<IconUser size={16} />}
              {...form.getInputProps('username')}
              autoFocus
              mb="sm"
            />
            <TextInput
              label="密码"
              placeholder="至少 6 位"
              type="password"
              leftSection={<IconLock size={16} />}
              {...form.getInputProps('password')}
              mb="sm"
            />
            <TextInput
              label="确认密码"
              placeholder="再次输入密码"
              type="password"
              leftSection={<IconLock size={16} />}
              {...form.getInputProps('confirm')}
              mb="lg"
            />
            <Button type="submit" fullWidth loading={loading}>
              {loading ? '创建中...' : '创建账号并进入'}
            </Button>
          </form>
        </Paper>
      </Box>
    </Center>
  );
}
