import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { MantineProvider, localStorageColorSchemeManager } from '@mantine/core'
import { Notifications } from '@mantine/notifications'
import AppLayout from './components/AppLayout'
import ProtectedRoute from './components/ProtectedRoute'
import RequireAnon from './components/RequireAnon'
import LoginPage from './pages/LoginPage'
import LibraryManagerPage from './pages/LibraryManagerPage'
import TimelinePage from './pages/TimelinePage'
import BrowsePage from './pages/BrowsePage'
import AlbumsPage from './pages/AlbumsPage'
import RecyclePage from './pages/RecyclePage'
import PlayPage from './pages/PlayPage'
import SystemPage from './pages/SystemPage'
import SettingsPage from './pages/SettingsPage'
import SharePage from './pages/SharePage'
import '@mantine/core/styles.css'
import './index.css'

// 主题色方案持久化到 localStorage，刷新后保留用户选择
const colorSchemeManager = localStorageColorSchemeManager({ key: 'jianvideo-color-scheme' })

export default function App() {
  return (
    <MantineProvider defaultColorScheme="dark" colorSchemeManager={colorSchemeManager}>
      <Notifications position="top-right" />
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<RequireAnon><LoginPage /></RequireAnon>} />
          {/* 公开分享查看页（FR-43）：免登、不套 AppLayout / ProtectedRoute */}
          <Route path="/s/:token" element={<SharePage />} />
          <Route path="/library-manager" element={<ProtectedRoute><AppLayout><LibraryManagerPage /></AppLayout></ProtectedRoute>} />
          <Route path="/" element={<ProtectedRoute><AppLayout><TimelinePage /></AppLayout></ProtectedRoute>} />
          <Route path="/browse" element={<ProtectedRoute><AppLayout><BrowsePage /></AppLayout></ProtectedRoute>} />
          <Route path="/albums" element={<ProtectedRoute><AppLayout><AlbumsPage /></AppLayout></ProtectedRoute>} />
          <Route path="/recycle" element={<ProtectedRoute><AppLayout><RecyclePage /></AppLayout></ProtectedRoute>} />
          <Route path="/play/:id" element={<ProtectedRoute><AppLayout><PlayPage /></AppLayout></ProtectedRoute>} />
          <Route path="/system" element={<ProtectedRoute><AppLayout><SystemPage /></AppLayout></ProtectedRoute>} />
          <Route path="/settings" element={<ProtectedRoute><AppLayout><SettingsPage /></AppLayout></ProtectedRoute>} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
    </MantineProvider>
  )
}
