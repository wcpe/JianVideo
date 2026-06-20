// PWA Web App Manifest 配置（单一真源）。
// 同时被 vite.config.ts 注入构建产物与单元测试引用，避免两处定义漂移。

// 图标条目类型，对应 Web App Manifest 的 icons 字段。
export interface PwaIcon {
  src: string
  sizes: string
  type: string
  // purpose 可选，maskable 用于自适应安装图标
  purpose?: string
}

// Manifest 配置类型（仅声明本项目用到的字段）。
export interface PwaManifest {
  name: string
  short_name: string
  description: string
  start_url: string
  display: 'standalone' | 'fullscreen' | 'minimal-ui' | 'browser'
  theme_color: string
  background_color: string
  lang: string
  icons: PwaIcon[]
}

// 品牌主题色与背景色（与 index.css body 暗色背景一致）。
const THEME_COLOR = '#7e14ff'
const BACKGROUND_COLOR = '#16171d'

// 应用清单：控制「添加到主屏」后的名称、图标、启动方式与配色。
export const pwaManifest: PwaManifest = {
  name: 'JianVideo 视频媒体库',
  short_name: 'JianVideo',
  description: '本地与 NAS 视频媒体库，支持流式转码、追播与时间轴浏览',
  start_url: '/',
  display: 'standalone',
  theme_color: THEME_COLOR,
  background_color: BACKGROUND_COLOR,
  lang: 'zh-CN',
  icons: [
    {
      src: 'pwa-192x192.png',
      sizes: '192x192',
      type: 'image/png',
    },
    {
      src: 'pwa-512x512.png',
      sizes: '512x512',
      type: 'image/png',
    },
    {
      // maskable 图标：安装到主屏时按系统形状裁切，保证留白不被切掉内容
      src: 'pwa-maskable-512x512.png',
      sizes: '512x512',
      type: 'image/png',
      purpose: 'maskable',
    },
  ],
}
