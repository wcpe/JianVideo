import { describe, it, expect } from 'vitest'
import { mediaDisplayName } from './media'
import type { MediaFile } from '@/types'

function makeFile(overrides: Partial<MediaFile>): MediaFile {
  return {
    id: 1, library_id: 1, file_path: '/x/real.mp4', file_name: 'real.mp4',
    file_size: 0, format: 'mp4', video_codec: '', audio_codec: '',
    duration: 0, width: 0, height: 0, bitrate: 0, subtitle_tracks: '',
    added_at: '', modified_at: '', ...overrides,
  }
}

describe('mediaDisplayName', () => {
  it('有显示名时优先用 display_name', () => {
    expect(mediaDisplayName(makeFile({ display_name: '我的影片' }))).toBe('我的影片')
  })

  it('显示名为空时回退 file_name', () => {
    expect(mediaDisplayName(makeFile({ display_name: '' }))).toBe('real.mp4')
  })

  it('显示名缺省时回退 file_name', () => {
    expect(mediaDisplayName(makeFile({}))).toBe('real.mp4')
  })

  it('显示名仅空白时回退 file_name', () => {
    expect(mediaDisplayName(makeFile({ display_name: '   ' }))).toBe('real.mp4')
  })
})
