import { useState, useCallback, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { Stack, Title } from '@mantine/core'
import { useLibraryPaths } from '@/hooks/useLibraryPaths'
import { useDirectoryBrowse } from '@/hooks/useDirectoryBrowse'
import DirectoryBrowser from '@/components/DirectoryBrowser'
import ImagePreviewModal from '@/components/ImagePreviewModal'
import { isImageFile } from '@/utils/media'
import type { MediaFile } from '@/types'

/** 目录浏览页：只读浏览，图片预览、视频跳播放 */
export default function BrowsePage() {
  const navigate = useNavigate()
  const [preview, setPreview] = useState<MediaFile | null>(null)
  const paths = useLibraryPaths(undefined)
  const browse = useDirectoryBrowse()
  const exts = paths.customImageExtensions

  // 路径就绪后用第一个目录初始化浏览
  useEffect(() => {
    if (!browse.browseLibraryID && paths.paths.length > 0)
      browse.initIfNeeded(paths.paths[0].id, paths.paths[0].path)
  }, [paths.paths, browse.browseLibraryID])

  const handleOpen = useCallback((f: MediaFile) => {
    if (isImageFile(f, exts)) setPreview(f)
    else navigate(`/play/${f.id}`)
  }, [navigate, exts])

  return (
    <Stack gap="md">
      <Title order={2}>目录浏览</Title>
      <DirectoryBrowser breadcrumbs={browse.breadcrumbs} directories={browse.directories} files={browse.files}
        loading={browse.loading} error={browse.error} customImageExtensions={exts}
        onEnterDir={browse.handleEnterDir} onBreadcrumbNavigate={browse.handleBreadcrumbNavigate}
        onErrorClose={() => browse.setError(null)} onOpenFile={handleOpen} />
      <ImagePreviewModal file={preview} onClose={() => setPreview(null)} />
    </Stack>
  )
}
