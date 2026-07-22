import { useEffect } from 'react';
import { Navigate } from 'react-router-dom';
import { Center, Loader } from '@mantine/core';
import { useAuthStore } from '@/stores/auth';

/** 匿名路由：已登录跳首页；需初始化跳 /setup，避免在登录页停留 */
export default function RequireAnon({ children }: { children: React.ReactNode }) {
  const { initialized, isAuthenticated, needsSetup, init } = useAuthStore();

  // 仅在尚未初始化时触发一次认证恢复
  useEffect(() => {
    if (!initialized) init();
  }, [initialized, init]);

  // 初始化进行中：全屏居中加载指示
  if (!initialized) {
    return (
      <Center h="100vh">
        <Loader />
      </Center>
    );
  }

  // 系统尚未初始化（无任何用户）：导向首次初始化引导页（FR-109）
  if (needsSetup) {
    return <Navigate to="/setup" replace />;
  }

  // 已认证：重定向到首页
  if (isAuthenticated) {
    return <Navigate to="/" replace />;
  }

  return <>{children}</>;
}
