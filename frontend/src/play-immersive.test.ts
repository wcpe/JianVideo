import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import path from 'path'

// FR-103 守护（index.css 规则层面）：播放页全屏沉浸布局靠 body.play-immersive 类协调
// AppShell.Main——去 padding、锁高到 100dvh、禁外层滚动，使播放页正好铺满、不可纵向滚动。
// dvh 单位 jsdom 不识别（无法在组件渲染层断言），故在 index.css 文本层面守护该机制。

const css = readFileSync(path.join(path.resolve(__dirname), 'index.css'), 'utf-8')

describe('FR-103 播放页全屏沉浸布局（CSS 协调）', () => {
  it('body.play-immersive 禁外层纵向滚动', () => {
    expect(css).toMatch(/body\.play-immersive\s*\{[\s\S]*?overflow:\s*hidden/)
  })

  it('play-immersive 下 AppShell.Main 去 padding、锁高 100dvh、禁滚动', () => {
    const idx = css.indexOf('body.play-immersive .mantine-AppShell-main')
    expect(idx).toBeGreaterThanOrEqual(0)
    const block = css.slice(idx)
    expect(block).toMatch(/padding:\s*0/)
    // 用 dvh 避开移动端地址栏高度抖动
    expect(block).toMatch(/height:\s*100dvh/)
    expect(block).toMatch(/overflow:\s*hidden/)
  })
})
