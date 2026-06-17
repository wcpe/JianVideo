import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { TimelinePage } from '@/pages/TimelinePage'

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/timeline" element={<TimelinePage />} />
        <Route path="*" element={<Navigate to="/timeline" replace />} />
      </Routes>
    </BrowserRouter>
  )
}
