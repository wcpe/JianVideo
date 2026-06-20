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
import PlayPage from './pages/PlayPage'
import SystemPage from './pages/SystemPage'
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
          <Route path="/library-manager" element={<ProtectedRoute><AppLayout><LibraryManagerPage /></AppLayout></ProtectedRoute>} />
          <Route path="/" element={<ProtectedRoute><AppLayout><TimelinePage /></AppLayout></ProtectedRoute>} />
          <Route path="/browse" element={<ProtectedRoute><AppLayout><BrowsePage /></AppLayout></ProtectedRoute>} />
          <Route path="/play/:id" element={<ProtectedRoute><AppLayout><PlayPage /></AppLayout></ProtectedRoute>} />
          <Route path="/system" element={<ProtectedRoute><AppLayout><SystemPage /></AppLayout></ProtectedRoute>} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
    </MantineProvider>
  )
}
