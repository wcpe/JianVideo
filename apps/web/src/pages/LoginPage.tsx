import { useForm } from '@mantine/form';
import {
  TextInput,
  Button,
  Paper,
  Title,
  Text,
  Alert,
  Center,
  Box,
  Tooltip,
  Group,
} from '@mantine/core';
import { IconLock, IconUser, IconAlertCircle, IconClock } from '@tabler/icons-react';
import { useNavigate } from 'react-router-dom';
import { useAuthStore } from '@/stores/auth';
import { notifications } from '@mantine/notifications';
import BrandLogo from '@/components/BrandLogo';

/** 将锁定秒数格式化为简短中文（FR2-062） */
function formatRetryAfter(sec: number): string {
  if (sec < 60) return `约 ${sec} 秒`;
  const min = Math.ceil(sec / 60);
  return `约 ${min} 分钟`;
}

/** 登录页 — Mantine 居中卡片表单 */
export default function LoginPage() {
  const { login, loading, error, errorCode, loginRetryAfterSec, clearError } = useAuthStore();
  const navigate = useNavigate();
  const isLocked = errorCode === 'LOGIN_LOCKED';

  const form = useForm({
    initialValues: { username: '', password: '' },
    validate: {
      username: (v) => (v.trim() ? null : '请输入用户名'),
      password: (v) => (v.trim() ? null : '请输入密码'),
    },
  });

  const handleSubmit = async (values: { username: string; password: string }) => {
    try {
      await login(values.username.trim(), values.password);
      notifications.show({
        title: '登录成功',
        message: `欢迎回来，${values.username}`,
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
            JianVideo
          </Title>
          <Text size="sm" c="dimmed" ta="center" mb="lg">
            请登录以继续
          </Text>

          {error && (
            <Alert
              icon={isLocked ? <IconClock size={16} /> : <IconAlertCircle size={16} />}
              title={isLocked ? '登录已暂时锁定' : '登录失败'}
              color={isLocked ? 'orange' : 'red'}
              mb="md"
              withCloseButton
              onClose={clearError}
              data-testid={isLocked ? 'login-locked-alert' : 'login-error-alert'}
            >
              {error}
              {isLocked && loginRetryAfterSec != null && loginRetryAfterSec > 0 && (
                <Text size="sm" mt={6}>
                  请等待 {formatRetryAfter(loginRetryAfterSec)} 后再试
                </Text>
              )}
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
            <Tooltip
              label="请先填写用户名和密码"
              disabled={form.isValid()}
              withArrow
              events={{ hover: true, focus: true, touch: true }}
            >
              <Button type="submit" fullWidth loading={loading} disabled={!form.isValid()}>
                {loading ? '登录中...' : '登录'}
              </Button>
            </Tooltip>
            {!form.isValid() && (
              <Text size="xs" c="dimmed" ta="center" mt="xs">
                填写用户名和密码后即可登录
              </Text>
            )}
          </form>
        </Paper>
      </Box>
    </Center>
  );
}
