import { useEffect } from 'react'
import { Navigate } from 'react-router-dom'
import { Center, Loader } from '@mantine/core'
import { useAuthStore } from '@/stores/auth'

/** 匿名路由：已登录用户访问 /login 时跳转首页，避免重复登录 */
export default function RequireAnon({ children }: { children: React.ReactNode }) {
  const { initialized, isAuthenticated, init } = useAuthStore()

  // 仅在尚未初始化时触发一次认证恢复
  useEffect(() => {
    if (!initialized) init()
  }, [initialized, init])

  // 初始化进行中：全屏居中加载指示
  if (!initialized) {
    return (
      <Center h="100vh">
        <Loader />
      </Center>
    )
  }

  // 已认证：重定向到首页
  if (isAuthenticated) {
    return <Navigate to="/" replace />
  }

  return <>{children}</>
}
