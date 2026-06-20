import { useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { Stack, Title } from '@mantine/core'
import { useLibraryPaths } from '@/hooks/useLibraryPaths'
import { useMediaList } from '@/hooks/useMediaList'
import { useScanProgress } from '@/hooks/useScanProgress'
import MediaTimeline from '@/components/MediaTimeline'
import ImagePreviewModal from '@/components/ImagePreviewModal'
import { isImageFile } from '@/utils/media'
import type { MediaFile } from '@/types'

/** 时间轴页：只读浏览，图片预览、视频跳播放 */
export default function TimelinePage() {
  const navigate = useNavigate()
  const [preview, setPreview] = useState<MediaFile | null>(null)
  const media = useMediaList()
  const paths = useLibraryPaths(undefined)
  const exts = paths.customImageExtensions
  // 扫描完成后刷新当前列表
  useScanProgress(() => media.loadMedia(media.page, media.search))

  const handleOpen = useCallback((f: MediaFile) => {
    if (isImageFile(f, exts)) setPreview(f)
    else navigate(`/play/${f.id}`)
  }, [navigate, exts])

  return (
    <Stack gap="md">
      <Title order={2}>时间轴</Title>
      <MediaTimeline mediaFiles={media.mediaFiles} total={media.total} page={media.page}
        searchInput={media.searchInput} loading={media.loading} error={media.error}
        totalPages={media.totalPages} customImageExtensions={exts}
        onSearchChange={(v) => { media.setSearchInput(v); media.setPage(1) }}
        onPageChange={media.setPage} onErrorClose={() => media.setError(null)}
        onOpenFile={handleOpen} />
      <ImagePreviewModal file={preview} onClose={() => setPreview(null)} />
    </Stack>
  )
}
