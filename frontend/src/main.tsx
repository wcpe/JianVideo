import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { MantineProvider, createTheme } from '@mantine/core'
import { Notifications } from '@mantine/notifications'
import type { MantineColorsTuple } from '@mantine/core'
import '@mantine/core/styles.css'
import '@mantine/notifications/styles.css'
import './index.css'
import App from './App.tsx'

/** Mantine 暗色主题配置 */
const purple: MantineColorsTuple = [
  '#f3e8ff', '#e9d5ff', '#d8b4fe', '#c084fc', '#a855f7',
  '#9333ea', '#7e22ce', '#6b21a8', '#581c87', '#3b0764',
]

const theme = createTheme({
  primaryColor: 'purple',
  colors: {
    purple,
    dark: [
      '#C1C2C5', '#A6A7AB', '#909296', '#5C5F66', '#373A40',
      '#2C2E33', '#25262B', '#1A1B1E', '#141517', '#101113',
    ],
  },
  fontFamily: "system-ui, 'Segoe UI', Roboto, sans-serif",
  headings: { fontFamily: "system-ui, 'Segoe UI', Roboto, sans-serif" },
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <MantineProvider theme={theme} defaultColorScheme="dark">
      <Notifications position="top-right" />
      <App />
    </MantineProvider>
  </StrictMode>,
)
