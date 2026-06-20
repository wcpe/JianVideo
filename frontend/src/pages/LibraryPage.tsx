import { useState, useCallback, useEffect, useRef } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { Stack, Title, Tabs, Box, Text, Progress } from '@mantine/core'
import { IconClock, IconFolderOpen } from '@tabler/icons-react'
import { useLibraryPaths } from '@/hooks/useLibraryPaths'
import { useMediaList } from '@/hooks/useMediaList'
import { useDirectoryBrowse } from '@/hooks/useDirectoryBrowse'
import LibraryPathManager from '@/components/LibraryPathManager'
import MediaTimeline from '@/components/MediaTimeline'
import DirectoryBrowser from '@/components/DirectoryBrowser'
import ImagePreviewModal from '@/components/ImagePreviewModal'
import ConfirmModal from '@/components/ConfirmModal'
import * as libApi from '@/api/library'
import { isImageFile } from '@/utils/media'
import type { LibraryPath, MediaFile, ScanStatus } from '@/types'

const initDelPath = { opened: false, path: null as LibraryPath | null, loading: false }
const initDelMedia = { opened: false, file: null as MediaFile | null, loading: false }

export default function LibraryPage({ initialTab = 'timeline' }: { initialTab?: 'timeline' | 'directory' }) {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const activeTab = searchParams.get('tab') || initialTab
  const setActiveTab = (tab: string) => setSearchParams({ tab })
  const [delPath, setDelPath] = useState<typeof initDelPath>(initDelPath)
  const [delMedia, setDelMedia] = useState<typeof initDelMedia>(initDelMedia)
  const [preview, setPreview] = useState<MediaFile | null>(null)
  const [scanProgress, setScanProgress] = useState<ScanStatus | null>(null)
  const media = useMediaList()
  const browse = useDirectoryBrowse()
  const paths = useLibraryPaths(undefined)
  const exts = paths.customImageExtensions

  // 初始化目录浏览
  useEffect(() => {
    if (activeTab === 'directory' && !browse.browseLibraryID && paths.paths.length > 0)
      browse.initIfNeeded(paths.paths[0].id, paths.paths[0].path)
  }, [activeTab, paths.paths, browse.browseLibraryID])

  // 用 ref 持有最新的刷新逻辑，避免 SSE 订阅因依赖变化反复重建
  const reloadRef = useRef<() => void>(() => {})
  reloadRef.current = () => { media.loadMedia(media.page, media.search); paths.loadPaths() }

  // 订阅扫描进度 SSE：扫描中展示进度条，完成后自动刷新列表
  useEffect(() => {
    return libApi.createScanProgressSSE((status) => {
      setScanProgress(status)
      if (status.status === 'completed') reloadRef.current()
    })
  }, [])

  const handleOpen = useCallback((f: MediaFile) => { isImageFile(f, exts) ? setPreview(f) : navigate(`/play/${f.id}`) }, [navigate, exts])
  const handleBrowse = useCallback((p: LibraryPath) => { browse.handleBrowsePath(p.id, p.path); setActiveTab('directory') }, [browse, setActiveTab])
  const handleTab = useCallback((tab: string | null) => { if (!tab) return; setActiveTab(tab) }, [setActiveTab])

  const handleDelPath = useCallback(async () => {
    if (!delPath.path) return
    setDelPath(p => ({ ...p, loading: true }))
    try { await libApi.deleteLibraryPath(delPath.path.id) } catch {}
    setDelPath(initDelPath); await paths.loadPaths(); await media.loadMedia(media.page, media.search)
  }, [delPath.path, paths, media])

  const handleDelMedia = useCallback(async () => {
    if (!delMedia.file) return
    setDelMedia(p => ({ ...p, loading: true }))
    try { await libApi.deleteMediaFile(delMedia.file.id) } catch {}
    setDelMedia(initDelMedia); await media.loadMedia(media.page, media.search)
  }, [delMedia.file, media])

  return (
    <Stack gap="md">
      <Title order={2}>媒体库</Title>
      <LibraryPathManager paths={paths.paths} loading={paths.loading} scanLoading={paths.scanLoading}
        newPath={paths.newPath} addingPath={paths.addingPath} extensionInputs={paths.extensionInputs}
        extensionTypes={paths.extensionTypes} extensionLoading={paths.extensionLoading}
        onNewPathChange={paths.setNewPath} onAddPath={paths.handleAddPath}
        onDeletePath={(p) => setDelPath({ opened: true, path: p, loading: false })}
        onScan={(id) => paths.handleScan(id, async () => { if (browse.browseLibraryID === id) browse.loadBrowse(browse.browseLibraryID, browse.browseParentPath) })} onBrowsePath={handleBrowse}
        onExtensionInputChange={(id, v) => paths.setExtensionInputs(p => ({ ...p, [id]: v }))}
        onExtensionTypeChange={(id, t) => paths.setExtensionTypes(p => ({ ...p, [id]: t }))}
        onAddExtension={paths.handleAddExtension} />
      {scanProgress?.status === 'scanning' && (
        <Box>
          <Text size="sm" c="dimmed" mb={4}>
            正在扫描… 已扫描 {scanProgress.scanned_files} / {scanProgress.total_files} 个文件
          </Text>
          <Progress
            value={scanProgress.total_files > 0 ? (scanProgress.scanned_files / scanProgress.total_files) * 100 : 0}
            color="purple" animated size="sm" />
        </Box>
      )}
      <Tabs value={activeTab} onChange={handleTab} color="purple">
        <Tabs.List>
          <Tabs.Tab value="timeline" leftSection={<IconClock size={14} />}>时间轴</Tabs.Tab>
          <Tabs.Tab value="directory" leftSection={<IconFolderOpen size={14} />}>文件目录</Tabs.Tab>
        </Tabs.List>
        <Tabs.Panel value="timeline" pt="sm">
          <MediaTimeline mediaFiles={media.mediaFiles} total={media.total} page={media.page}
            searchInput={media.searchInput} loading={media.loading} error={media.error}
            totalPages={media.totalPages} customImageExtensions={exts}
            onSearchChange={(v) => { media.setSearchInput(v); media.setPage(1) }}
            onPageChange={media.setPage} onErrorClose={() => media.setError(null)}
            onOpenFile={handleOpen} onDeleteFile={(f) => setDelMedia({ opened: true, file: f, loading: false })} />
        </Tabs.Panel>
        <Tabs.Panel value="directory" pt="sm">
          <DirectoryBrowser breadcrumbs={browse.breadcrumbs} directories={browse.directories} files={browse.files}
            loading={browse.loading} error={browse.error} customImageExtensions={exts}
            onEnterDir={browse.handleEnterDir} onBreadcrumbNavigate={browse.handleBreadcrumbNavigate}
            onErrorClose={() => browse.setError(null)} onOpenFile={handleOpen}
            onDeleteFile={(f) => setDelMedia({ opened: true, file: f, loading: false })} />
        </Tabs.Panel>
      </Tabs>
      <ConfirmModal opened={delPath.opened} title="删除目录"
        message={`确定要删除目录 "${delPath.path?.label || delPath.path?.path}" 吗？关联的媒体文件也会被删除。`}
        confirmLabel="删除" cancelLabel="取消" confirmColor="red" loading={delPath.loading}
        onConfirm={handleDelPath} onCancel={() => setDelPath(initDelPath)} />
      <ConfirmModal opened={delMedia.opened} title="删除文件"
        message={`确定要删除 "${delMedia.file?.file_name}" 吗？`}
        confirmLabel="删除" cancelLabel="取消" confirmColor="red" loading={delMedia.loading}
        onConfirm={handleDelMedia} onCancel={() => setDelMedia(initDelMedia)} />
      <ImagePreviewModal file={preview} onClose={() => setPreview(null)} />
    </Stack>
  )
}
