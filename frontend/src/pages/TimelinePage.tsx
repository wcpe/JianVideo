import { useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { Stack, Title, TextInput } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconSearch } from '@tabler/icons-react'
import { useLibraryPaths } from '@/hooks/useLibraryPaths'
import { useInfiniteMedia } from '@/hooks/useInfiniteMedia'
import { useScanProgress } from '@/hooks/useScanProgress'
import TimelineView from '@/components/TimelineView'
import MediaFilterBar from '@/components/MediaFilterBar'
import ImagePreviewModal from '@/components/ImagePreviewModal'
import { isImageFile } from '@/utils/media'
import { extractErrorMessage } from '@/utils/error'
import * as libApi from '@/api/library'
import type { MediaFile } from '@/types'

/** 时间轴页：按日期分组的时间线浏览，虚拟滚动 + 滚动加载更多 */
export default function TimelinePage() {
  const navigate = useNavigate()
  const [preview, setPreview] = useState<MediaFile | null>(null)
  // 收藏/标签筛选（FR-41）
  const [favorite, setFavorite] = useState(false)
  const [tagId, setTagId] = useState(0)
  const infinite = useInfiniteMedia({ favorite, tagId })
  const paths = useLibraryPaths(undefined)
  const exts = paths.customImageExtensions
  // 扫描完成后重载第一页
  useScanProgress(() => infinite.reload())

  const handleOpen = useCallback((f: MediaFile) => {
    if (isImageFile(f, exts)) setPreview(f)
    else navigate(`/play/${f.id}`)
  }, [navigate, exts])

  // 切换收藏（FR-41）：调用接口后刷新列表
  const handleToggleFavorite = useCallback(async (f: MediaFile) => {
    try {
      await libApi.setMediaFavorite(f.id, !f.favorite)
      infinite.reload()
    } catch (err) {
      notifications.show({ color: 'red', message: extractErrorMessage(err, '更新收藏失败') })
    }
  }, [infinite])

  return (
    <Stack gap="md">
      <Title order={2}>时间轴</Title>

      {/* 搜索 */}
      <TextInput
        placeholder="搜索文件名..."
        leftSection={<IconSearch size={14} />}
        value={infinite.searchInput}
        onChange={(e) => infinite.setSearchInput(e.target.value)}
        size="sm"
      />

      {/* 收藏与标签筛选（FR-41） */}
      <MediaFilterBar
        favorite={favorite}
        onFavoriteChange={setFavorite}
        tagId={tagId}
        onTagIdChange={setTagId}
      />

      <TimelineView
        mediaFiles={infinite.items}
        loading={infinite.loading && infinite.items.length === 0}
        error={infinite.error}
        customImageExtensions={exts}
        onErrorClose={() => infinite.setError(null)}
        onOpenFile={handleOpen}
        onToggleFavorite={handleToggleFavorite}
        onLoadMore={infinite.loadMore}
        hasMore={infinite.hasMore}
        loadingMore={infinite.loading && infinite.items.length > 0}
      />

      <ImagePreviewModal file={preview} onClose={() => setPreview(null)} />
    </Stack>
  )
}
