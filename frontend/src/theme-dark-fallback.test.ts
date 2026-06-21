import { describe, it, expect } from 'vitest'
import { readFileSync } from 'fs'
import path from 'path'

// 亮色模式残留深色守护（FIX-3）：
// 这些位置此前写死深色（color-scheme: dark、深色背景、var(--mantine-color-dark-N) 作为表面/前景），
// 导致切到亮色模式后仍显深色。修复要求改用随主题切换的 Mantine 语义变量。
// 本测试在 node 环境直接读源码，断言这些写死深色 fallback 不再出现（修复前命中=红）。

const SRC = path.resolve(__dirname)

function read(rel: string): string {
  return readFileSync(path.join(SRC, rel), 'utf-8')
}

describe('亮色模式不残留写死深色（FIX-3）', () => {
  it('index.css 不强制 color-scheme: dark，body 背景用主题感知变量', () => {
    const css = read('index.css')
    // 不得强制锁定为 dark（应跟随主题，如 light dark 或 normal）
    expect(css).not.toMatch(/color-scheme:\s*dark\s*;/)
    // body 背景不得写死 dark-8 / 深色 hex 兜底
    expect(css).not.toContain('--mantine-color-dark-8')
    expect(css).not.toContain('#16171d')
    // 应改用主题感知的 body 背景变量
    expect(css).toContain('--mantine-color-body')
  })

  it('VideoPlayer 控制条不写死深色兜底', () => {
    const tsx = read('components/VideoPlayer.tsx')
    // 控制条背景此前写死 var(--mantine-color-dark-8, #1a1b1e)
    expect(tsx).not.toContain('--mantine-color-dark-8')
    expect(tsx).not.toContain('#1a1b1e')
  })

  it('媒体缩略图占位与时间线表面不写死深色 dark-N', () => {
    expect(read('components/MediaThumbnail.tsx')).not.toContain('--mantine-color-dark-6')
    expect(read('components/TimelineView.tsx')).not.toContain('--mantine-color-dark-4')
  })

  it('空状态文件夹图标用主题感知 dimmed 而非写死 dark-N', () => {
    expect(read('components/DirectoryBrowser.tsx')).not.toContain('--mantine-color-dark-3')
    expect(read('components/TimelineView.tsx')).not.toContain('--mantine-color-dark-3')
  })
})
