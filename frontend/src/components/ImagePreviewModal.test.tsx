import { render, screen } from '@testing-library/react'
import { MantineProvider } from '@mantine/core'
import { describe, it, expect } from 'vitest'

import ImagePreviewModal from './ImagePreviewModal'
import type { MediaFile } from '@/types'

const file = {
  id: 7,
  library_id: 1,
  file_path: 'D:/Photos/风景.jpg',
  file_name: '风景.jpg',
  media_type: 'image',
} as unknown as MediaFile

function renderModal() {
  return render(
    <MantineProvider>
      <ImagePreviewModal file={file} onClose={() => {}} />
    </MantineProvider>,
  )
}

describe('ImagePreviewModal 图片预览', () => {
  it('提供下载原文件入口，指向下载端点（FR-42）', () => {
    renderModal()
    const link = screen.getByRole('link', { name: /下载原文件/ })
    expect(link).toHaveAttribute('href', '/api/library/media/7/download')
    expect(link).toHaveAttribute('download')
  })
})
