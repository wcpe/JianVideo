import { describe, it, expect } from 'vitest'
import { parseWebVTT } from '@/utils/subtitle'

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
