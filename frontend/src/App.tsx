import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { MantineProvider, localStorageColorSchemeManager } from '@mantine/core'
import { appTheme, themeCssVariablesResolver } from './theme'
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
import InspectPage from './pages/InspectPage'
import DuplicatesPage from './pages/DuplicatesPage'
import StatsPage from './pages/StatsPage'
import PlayPage from './pages/PlayPage'
import ConsolePage from './pages/ConsolePage'
import SharePage from './pages/SharePage'
import MapPage from './pages/MapPage'
import TranscodePage from './pages/TranscodePage'
import LicensesPage from './pages/LicensesPage'
import '@mantine/core/styles.css'
import '@mantine/dates/styles.css'
import './index.css'

// 主题色方案持久化到 localStorage，刷新后保留用户选择
const colorSchemeManager = localStorageColorSchemeManager({ key: 'jianvideo-color-scheme' })

export default function App() {
  return (
    <MantineProvider
      theme={appTheme}
      cssVariablesResolver={themeCssVariablesResolver}
      defaultColorScheme="dark"
      colorSchemeManager={colorSchemeManager}
    >
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
          <Route path="/map" element={<ProtectedRoute><AppLayout><MapPage /></AppLayout></ProtectedRoute>} />
          <Route path="/recycle" element={<ProtectedRoute><AppLayout><RecyclePage /></AppLayout></ProtectedRoute>} />
          {/* 问题媒体 / 健康巡检页（FR-73） */}
          <Route path="/inspect" element={<ProtectedRoute><AppLayout><InspectPage /></AppLayout></ProtectedRoute>} />
          {/* 感知哈希去重「重复项」页（FR-70） */}
          <Route path="/duplicates" element={<ProtectedRoute><AppLayout><DuplicatesPage /></AppLayout></ProtectedRoute>} />
          {/* 观看热力与统计页（FR-75） */}
          <Route path="/stats" element={<ProtectedRoute><AppLayout><StatsPage /></AppLayout></ProtectedRoute>} />
          {/* 转码预设与预生成队列页（FR-77） */}
          <Route path="/transcode" element={<ProtectedRoute><AppLayout><TranscodePage /></AppLayout></ProtectedRoute>} />
          <Route path="/play/:id" element={<ProtectedRoute><AppLayout><PlayPage /></AppLayout></ProtectedRoute>} />
          {/* 系统信息与设置合并为单页两 tab（FR-55）：/system 进控制台页 */}
          <Route path="/system" element={<ProtectedRoute><AppLayout><ConsolePage /></AppLayout></ProtectedRoute>} />
          {/* 旧 /settings 链接重定向到控制台页的设置 tab，避免死链 */}
          <Route path="/settings" element={<Navigate to="/system?tab=settings" replace />} />
          {/* 开源协议页（FR-57）：页脚「开源协议」链接进入 */}
          <Route path="/licenses" element={<ProtectedRoute><AppLayout><LicensesPage /></AppLayout></ProtectedRoute>} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
    </MantineProvider>
  )
}
