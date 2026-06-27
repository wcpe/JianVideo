import { useEffect } from 'react'
import { Navigate } from 'react-router-dom'
import { Center, Loader } from '@mantine/core'
import { useAuthStore } from '@/stores/auth'

/** 初始化引导守卫（FR-109）：仅当系统需首次初始化时展示 /setup，否则重定向到登录页。 */
export default function RequireSetup({ children }: { children: React.ReactNode }) {
  const { initialized, needsSetup, init } = useAuthStore()

  useEffect(() => {
    if (!initialized) init()
  }, [initialized, init])

  if (!initialized) {
    return (
      <Center h="100vh">
        <Loader />
      </Center>
    )
  }

  // 已初始化（已有用户）：不再展示初始化引导，回到登录页
  if (!needsSetup) {
    return <Navigate to="/login" replace />
  }

  return <>{children}</>
}
