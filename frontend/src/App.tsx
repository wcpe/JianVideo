import { useEffect } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { useAuthStore } from '@/stores/auth'
import AppLayout from '@/components/AppLayout'
import LoginPage from '@/pages/LoginPage'
import LibraryPage from '@/pages/LibraryPage'
import PlayPage from '@/pages/PlayPage'

/** 路由守卫 — 未认证用户只能访问 /login */
function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated, loading } = useAuthStore()

  if (loading) {
    return (
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh', background: '#101113', color: '#9ca3af' }}>
        加载中...
      </div>
    )
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />
  }

  return <AppLayout>{children}</AppLayout>
}

/** 已认证用户访问 /login 时自动跳转 */
function PublicRoute({ children }: { children: React.ReactNode }) {
  const { isAuthenticated, loading } = useAuthStore()

  if (loading) {
    return (
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh', background: '#101113', color: '#9ca3af' }}>
        加载中...
      </div>
    )
  }

  if (isAuthenticated) {
    return <Navigate to="/library" replace />
  }

  return <>{children}</>
}

export default function App() {
  const init = useAuthStore((s) => s.init)

  // 恢复认证状态
  useEffect(() => {
    init()
  }, [init])

  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<PublicRoute><LoginPage /></PublicRoute>} />
        <Route path="/library" element={<ProtectedRoute><LibraryPage /></ProtectedRoute>} />
        <Route path="/play/:id" element={<ProtectedRoute><PlayPage /></ProtectedRoute>} />
        <Route path="*" element={<Navigate to="/library" replace />} />
      </Routes>
    </BrowserRouter>
  )
}
