import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { VitePWA } from 'vite-plugin-pwa'
import path from 'path'
import { pwaManifest } from './src/utils/pwa-manifest'

export default defineConfig({
  plugins: [
    react(),
    // PWA：生成 manifest.webmanifest + Service Worker，支持添加到主屏与离线壳
    VitePWA({
      // autoUpdate：新版本就绪后自动接管，无需用户手动刷新
      registerType: 'autoUpdate',
      // 单一真源的 manifest 配置（与单元测试共享，见 src/utils/pwa-manifest.ts）
      manifest: pwaManifest,
      // 把 public 下的图标与 favicon 纳入预缓存清单
      includeAssets: ['favicon.svg', 'apple-touch-icon.png'],
      workbox: {
        // 仅预缓存构建产物中的应用壳静态资源；/api 与媒体流运行时仍走网络
        globPatterns: ['**/*.{js,css,html,svg,png,ico,webmanifest}'],
        // SPA 离线导航回退到应用壳
        navigateFallback: 'index.html',
        // 媒体流与接口请求不走离线回退，交给网络（避免离线时返回错误的壳）
        navigateFallbackDenylist: [/^\/api/, /^\/assets\//, /^\/health/],
      },
    }),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  optimizeDeps: {
    include: ['@emotion/react', '@emotion/cache', '@mantine/core', '@mantine/hooks', '@mantine/notifications', '@mantine/form'],
  },
})
