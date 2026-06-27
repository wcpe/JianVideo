import { Suspense, lazy } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { MantineProvider, localStorageColorSchemeManager, Center, Loader } from '@mantine/core'
import { appTheme, themeCssVariablesResolver } from './theme'
import { Notifications } from '@mantine/notifications'
import AppLayout from './components/AppLayout'
import TopProgressBar from './components/TopProgressBar'
import ProtectedRoute from './components/ProtectedRoute'
import RequireAnon from './components/RequireAnon'
import RequireSetup from './components/RequireSetup'
import LoginPage from './pages/LoginPage'
import SetupPage from './pages/SetupPage'
import LibraryManagerPage from './pages/LibraryManagerPage'
import OverviewPage from './pages/OverviewPage'
import TimelinePage from './pages/TimelinePage'
import BrowsePage from './pages/BrowsePage'
import AlbumsPage from './pages/AlbumsPage'
import RecyclePage from './pages/RecyclePage'
import InspectPage from './pages/InspectPage'
import DuplicatesPage from './pages/DuplicatesPage'
// 统计页懒加载（ADR-0045）：仅此页引入 Recharts 图表库，按路由 code-split，
// 让 recharts 落入独立 chunk、不进主包（否则主包超 PWA 预缓存 2MiB 上限）。
const StatsPage = lazy(() => import('./pages/StatsPage'))
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
      {/* 全局通知（FR-115 后续修复·通知白屏）：限制同时渲染数量 limit=4，
          防轮询/后台请求反复失败时 toast 无上限堆积撑爆 DOM 导致白屏；位置保持右上 */}
      <Notifications position="top-right" limit={4} />
      <BrowserRouter>
        {/* 全局顶部加载条（FR-96）：路由切换时顶部进度反馈 */}
        <TopProgressBar />
        {/* 路由切换过渡（FR-96/FIX-2）：淡入由 AppLayout 的 AppShell.Main 内部承接，
            仅主内容区渐入，导航栏与页眉不跟随淡入/闪动 */}
        <Routes>
          {/* 首次初始化引导（FR-109）：系统无用户时配置账号密码，免登 */}
          <Route path="/setup" element={<RequireSetup><SetupPage /></RequireSetup>} />
          <Route path="/login" element={<RequireAnon><LoginPage /></RequireAnon>} />
          {/* 公开分享查看页（FR-43）：免登、不套 AppLayout / ProtectedRoute */}
          <Route path="/s/:token" element={<SharePage />} />
          <Route path="/library-manager" element={<ProtectedRoute><AppLayout><LibraryManagerPage /></AppLayout></ProtectedRoute>} />
          {/* 首页概览数据看板（FR-117）：根路由由时间轴改为概览，时间轴迁至 /timeline */}
          <Route path="/" element={<ProtectedRoute><AppLayout><OverviewPage /></AppLayout></ProtectedRoute>} />
          <Route path="/timeline" element={<ProtectedRoute><AppLayout><TimelinePage /></AppLayout></ProtectedRoute>} />
          <Route path="/browse" element={<ProtectedRoute><AppLayout><BrowsePage /></AppLayout></ProtectedRoute>} />
          <Route path="/albums" element={<ProtectedRoute><AppLayout><AlbumsPage /></AppLayout></ProtectedRoute>} />
          <Route path="/map" element={<ProtectedRoute><AppLayout><MapPage /></AppLayout></ProtectedRoute>} />
          <Route path="/recycle" element={<ProtectedRoute><AppLayout><RecyclePage /></AppLayout></ProtectedRoute>} />
          {/* 问题媒体 / 健康巡检页（FR-73） */}
          <Route path="/inspect" element={<ProtectedRoute><AppLayout><InspectPage /></AppLayout></ProtectedRoute>} />
          {/* 感知哈希去重「重复项」页（FR-70） */}
          <Route path="/duplicates" element={<ProtectedRoute><AppLayout><DuplicatesPage /></AppLayout></ProtectedRoute>} />
          {/* 观看热力与统计页（FR-75 + FR-118）：懒加载，套 Suspense 给图表 chunk 加载兜底 */}
          <Route path="/stats" element={<ProtectedRoute><AppLayout><Suspense fallback={<Center py="xl"><Loader color="purple" /></Center>}><StatsPage /></Suspense></AppLayout></ProtectedRoute>} />
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
