import { describe, it, expect } from 'vitest'

// 直接测试 parseWebVTT 纯函数，不导入 VideoPlayer 组件（避免 mpegts.js 依赖）
// 从源码中提取 parseWebVTT 的逻辑进行测试

interface SubtitleEntry {
  start: number
  end: number
  text: string
}

/**
 * 解析 WebVTT 字幕内容，返回字幕条目列表。
 * 复制自 VideoPlayer.tsx 中的 parseWebVTT 函数。
 */
function parseWebVTT(content: string): SubtitleEntry[] {
  if (!content || !content.trim()) return []

  const entries: SubtitleEntry[] = []
  const blocks = content.split(/\n\n+/)

  for (const block of blocks) {
    const lines = block.trim().split('\n')
    if (lines.length < 2) continue

    // 找到包含 --> 的时间轴行
    let timelineLine = ''
    let timelineIdx = -1
    for (let i = 0; i < lines.length; i++) {
      if (lines[i].includes('-->')) {
        timelineLine = lines[i]
        timelineIdx = i
        break
      }
    }

    if (!timelineLine || timelineIdx === -1) continue

    const timeParts = timelineLine.split('-->')
    if (timeParts.length !== 2) continue

    const start = parseTimestamp(timeParts[0].trim())
    const end = parseTimestamp(timeParts[1].trim())

    if (start === null || end === null) continue

    const text = lines.slice(timelineIdx + 1).join('\n').trim()
    if (!text) continue

    entries.push({ start, end, text })
  }

  return entries
}

function parseTimestamp(ts: string): number | null {
  const parts = ts.split(':')
  if (parts.length === 3) {
    const h = parseFloat(parts[0])
    const m = parseFloat(parts[1])
    const s = parseFloat(parts[2])
    if (isNaN(h) || isNaN(m) || isNaN(s)) return null
    return h * 3600 + m * 60 + s
  }
  if (parts.length === 2) {
    const m = parseFloat(parts[0])
    const s = parseFloat(parts[1])
    if (isNaN(m) || isNaN(s)) return null
    return m * 60 + s
  }
  return null
}

describe('parseWebVTT', () => {
  it('解析标准 WebVTT 内容', () => {
    const vtt = `WEBVTT

1
00:00:01.000 --> 00:00:03.000
第一行字幕

2
00:00:04.000 --> 00:00:06.000
第二行字幕
`
    const entries = parseWebVTT(vtt)
    expect(entries).toHaveLength(2)
    expect(entries[0].start).toBe(1.0)
    expect(entries[0].end).toBe(3.0)
    expect(entries[0].text).toBe('第一行字幕')
    expect(entries[1].text).toBe('第二行字幕')
  })

  it('解析多行字幕文本', () => {
    const vtt = `WEBVTT

1
00:00:01.000 --> 00:00:03.000
第一行
第二行
`
    const entries = parseWebVTT(vtt)
    expect(entries).toHaveLength(1)
    expect(entries[0].text).toBe('第一行\n第二行')
  })

  it('返回空数组当输入为空', () => {
    const entries = parseWebVTT('')
    expect(entries).toEqual([])
  })

  it('跳过无效块', () => {
    const vtt = `WEBVTT

这不是有效字幕块

1
00:00:01.000 --> 00:00:03.000
有效字幕
`
    const entries = parseWebVTT(vtt)
    expect(entries).toHaveLength(1)
    expect(entries[0].text).toBe('有效字幕')
  })
})
