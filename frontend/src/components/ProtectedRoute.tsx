import { useEffect } from 'react'
import { Navigate } from 'react-router-dom'
import { Center, Loader } from '@mantine/core'
import { useAuthStore } from '@/stores/auth'

/** 受保护路由：未初始化时先触发 init 并显示加载；已初始化未认证则跳转 /login */
export default function ProtectedRoute({ children }: { children: React.ReactNode }) {
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

  // 已初始化但未认证：重定向到登录页
  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }

  return <>{children}</>
}
