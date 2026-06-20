import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { registerSW } from 'virtual:pwa-register'
import { registerPwa } from './utils/pwa'
import './index.css'
import App from './App.tsx'

// 注册 Service Worker，启用「添加到主屏」与离线应用壳（注册失败静默吞掉）
registerPwa(registerSW)

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
