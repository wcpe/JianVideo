import { describe, it, expect } from 'vitest'
import { groupMediaByDate } from './timeline'
import type { MediaFile } from '@/types'

/** 构造测试用媒体文件，仅关心 id 与 added_at */
function makeFile(id: number, addedAt: string): MediaFile {
  return {
    id,
    library_id: 1,
    file_path: `D:\\Videos\\${id}.mp4`,
    file_name: `${id}.mp4`,
    file_size: 0,
    format: 'mp4',
    video_codec: '',
    audio_codec: '',
    duration: 0,
    width: 0,
    height: 0,
    bitrate: 0,
    subtitle_tracks: '',
    added_at: addedAt,
    modified_at: addedAt,
  }
}

describe('groupMediaByDate', () => {
  it('按日期分组并倒序排列', () => {
    const groups = groupMediaByDate([
      makeFile(1, '2025-01-09T12:00:00Z'),
      makeFile(2, '2025-01-01T08:00:00Z'),
      makeFile(3, '2025-03-15T20:00:00Z'),
    ])
    expect(groups.map((g) => g.date)).toEqual(['2025-03-15', '2025-01-09', '2025-01-01'])
  })

  it('同一日期的文件合并到同一组并保持输入顺序', () => {
    const groups = groupMediaByDate([
      makeFile(1, '2025-01-09T01:00:00Z'),
      makeFile(2, '2025-01-09T23:00:00Z'),
      makeFile(3, '2025-01-09T12:00:00Z'),
    ])
    expect(groups).toHaveLength(1)
    expect(groups[0].date).toBe('2025-01-09')
    expect(groups[0].files.map((f) => f.id)).toEqual([1, 2, 3])
  })

  it('空字符串与非法 added_at 归入“未知日期”组', () => {
    const groups = groupMediaByDate([
      makeFile(1, ''),
      makeFile(2, 'not-a-date'),
      makeFile(3, '2025'),
    ])
    expect(groups).toHaveLength(1)
    expect(groups[0].date).toBe('未知日期')
    expect(groups[0].files.map((f) => f.id)).toEqual([1, 2, 3])
  })

  it('“未知日期”组始终排在所有有效日期之后', () => {
    const groups = groupMediaByDate([
      makeFile(1, ''),
      makeFile(2, '2025-01-01T00:00:00Z'),
      makeFile(3, '2024-12-31T00:00:00Z'),
    ])
    expect(groups.map((g) => g.date)).toEqual(['2025-01-01', '2024-12-31', '未知日期'])
  })

  it('空输入返回空数组', () => {
    expect(groupMediaByDate([])).toEqual([])
  })

  // FR-32：缩放粒度与媒体时间组织
  it('media_time 优先于 added_at 作为时间源', () => {
    const f = makeFile(1, '2020-01-01T00:00:00Z')
    f.media_time = '2023-08-15T10:00:00Z'
    const groups = groupMediaByDate([f])
    expect(groups[0].date).toBe('2023-08-15')
  })

  it('按月粒度分组（YYYY-MM）', () => {
    const groups = groupMediaByDate([
      makeFile(1, '2025-01-09T12:00:00Z'),
      makeFile(2, '2025-01-20T12:00:00Z'),
      makeFile(3, '2024-12-31T12:00:00Z'),
    ], 'month')
    expect(groups.map((g) => g.date)).toEqual(['2025-01', '2024-12'])
    expect(groups[0].files.map((x) => x.id)).toEqual([1, 2])
  })

  it('按年粒度分组（YYYY）', () => {
    const groups = groupMediaByDate([
      makeFile(1, '2025-01-09T12:00:00Z'),
      makeFile(2, '2024-06-01T12:00:00Z'),
      makeFile(3, '2024-12-31T12:00:00Z'),
    ], 'year')
    expect(groups.map((g) => g.date)).toEqual(['2025', '2024'])
    expect(groups[1].files.map((x) => x.id)).toEqual([2, 3])
  })
})
