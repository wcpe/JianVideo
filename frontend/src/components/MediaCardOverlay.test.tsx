import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { MantineProvider } from '@mantine/core'
import MediaCardOverlay from './MediaCardOverlay'
import type { MediaFile } from '@/types'

// 构造测试用媒体：video 为可控时长的视频、image 为图片
function video(over: Partial<MediaFile> = {}): MediaFile {
  return {
    id: 1, library_id: 1, file_path: '/x/v.mp4', file_name: 'v.mp4',
    file_size: 1024 * 1024 * 5, format: 'mp4', video_codec: 'h264', audio_codec: 'aac',
    duration: 95, width: 1920, height: 1080, bitrate: 0, subtitle_tracks: '',
    added_at: '2025-01-09T00:00:00Z', modified_at: '2025-01-09T00:00:00Z', ...over,
  }
}

function renderOverlay(props: Partial<React.ComponentProps<typeof MediaCardOverlay>> = {}) {
  return render(
    <MantineProvider>
      <MediaCardOverlay
        file={video()}
        isImage={false}
        selected={false}
        checkboxMode={false}
        {...props}
      />
    </MantineProvider>,
  )
}

describe('MediaCardOverlay（FR-99 媒体卡叠层）', () => {
  it('视频卡渲染时长角标（格式化时长）与中心播放叠层', () => {
    renderOverlay({ file: video({ duration: 95 }), isImage: false })
    // 时长角标：95 秒 = 1:35（formatDuration 不补时位）
    expect(screen.getByText('1:35')).toBeTruthy()
    // 中心播放叠层（以无障碍标签暴露）
    expect(screen.getByLabelText('视频')).toBeTruthy()
  })

  it('图片卡不渲染时长角标与播放叠层', () => {
    renderOverlay({ file: video({ format: 'png', duration: 0 }), isImage: true })
    expect(screen.queryByLabelText('视频')).toBeNull()
    // 图片无时长角标
    expect(screen.queryByText('1:35')).toBeNull()
  })

  it('底部信息层展示文件名与类型/大小', () => {
    renderOverlay({ file: video({ file_name: 'clip.mp4', format: 'mp4' }), isImage: false })
    expect(screen.getByText('clip.mp4')).toBeTruthy()
    // 类型徽标（大写格式）
    expect(screen.getByText('MP4')).toBeTruthy()
  })

  it('选中态渲染勾选叠层', () => {
    renderOverlay({ selected: true })
    expect(screen.getByLabelText('已选中')).toBeTruthy()
  })

  it('未选中时不渲染勾选叠层', () => {
    renderOverlay({ selected: false })
    expect(screen.queryByLabelText('已选中')).toBeNull()
  })
})
