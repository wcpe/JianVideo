import { useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { Stack, Title, TextInput, Group, Pagination } from '@mantine/core'
import { IconSearch } from '@tabler/icons-react'
import { useLibraryPaths } from '@/hooks/useLibraryPaths'
import { useMediaList } from '@/hooks/useMediaList'
import { useScanProgress } from '@/hooks/useScanProgress'
import TimelineView from '@/components/TimelineView'
import ImagePreviewModal from '@/components/ImagePreviewModal'
import { isImageFile } from '@/utils/media'
import type { MediaFile } from '@/types'

/** 时间轴页：按日期分组的时间线浏览，图片预览、视频跳播放 */
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

      {/* 搜索 */}
      <TextInput
        placeholder="搜索文件名..."
        leftSection={<IconSearch size={14} />}
        value={media.searchInput}
        onChange={(e) => { media.setSearchInput(e.target.value); media.setPage(1) }}
        size="sm"
      />

      <TimelineView
        mediaFiles={media.mediaFiles}
        loading={media.loading}
        error={media.error}
        customImageExtensions={exts}
        onErrorClose={() => media.setError(null)}
        onOpenFile={handleOpen}
      />

      {media.totalPages > 1 && (
        <Group justify="center" mt="md">
          <Pagination total={media.totalPages} value={media.page} onChange={media.setPage} size="sm" color="purple" />
        </Group>
      )}

      <ImagePreviewModal file={preview} onClose={() => setPreview(null)} />
    </Stack>
  )
}
