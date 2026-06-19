import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { Notifications } from '@mantine/notifications'
import AppLayout from './components/AppLayout'
import LoginPage from './pages/LoginPage'
import LibraryPage from './pages/LibraryPage'
import PlayPage from './pages/PlayPage'
import '@mantine/core/styles.css'
import './index.css'

export default function App() {
  return (
    <MantineProvider defaultColorScheme="dark">
      <Notifications position="top-right" />
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/" element={<AppLayout><LibraryPage /></AppLayout>} />
          <Route path="/library" element={<AppLayout><LibraryPage /></AppLayout>} />
          <Route path="/timeline" element={<AppLayout><LibraryPage initialTab="timeline" /></AppLayout>} />
          <Route path="/play/:id" element={<AppLayout><PlayPage /></AppLayout>} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
    </MantineProvider>
  )
}
