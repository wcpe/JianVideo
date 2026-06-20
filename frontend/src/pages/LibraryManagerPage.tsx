import { useState, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import { Stack, Title, Box, Text, Progress, Modal, TextInput, Button, Group } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { useLibraryPaths } from '@/hooks/useLibraryPaths'
import { useMediaList } from '@/hooks/useMediaList'
import { useScanProgress } from '@/hooks/useScanProgress'
import LibraryPathManager from '@/components/LibraryPathManager'
import MediaTimeline from '@/components/MediaTimeline'
import ImagePreviewModal from '@/components/ImagePreviewModal'
import ConfirmModal from '@/components/ConfirmModal'
import * as libApi from '@/api/library'
import { isImageFile } from '@/utils/media'
import type { LibraryPath, MediaFile } from '@/types'

const initDelPath = { opened: false, path: null as LibraryPath | null, loading: false }
const initDelMedia = { opened: false, file: null as MediaFile | null, loading: false }
const initRename = { opened: false, file: null as MediaFile | null, name: '', loading: false }

/** 存储库管理页：路径增删 + 扫描 + 进度 + 媒体文件删除/重命名 */
export default function LibraryManagerPage() {
  const navigate = useNavigate()
  const [delPath, setDelPath] = useState<typeof initDelPath>(initDelPath)
  const [delMedia, setDelMedia] = useState<typeof initDelMedia>(initDelMedia)
  const [rename, setRename] = useState<typeof initRename>(initRename)
  const [preview, setPreview] = useState<MediaFile | null>(null)
  const media = useMediaList()
  const paths = useLibraryPaths(undefined)
  const exts = paths.customImageExtensions
  // 扫描完成后刷新媒体列表与路径
  const progress = useScanProgress(() => { media.loadMedia(media.page, media.search); paths.loadPaths() })

  const handleOpen = useCallback((f: MediaFile) => {
    if (isImageFile(f, exts)) setPreview(f)
    else navigate(`/play/${f.id}`)
  }, [navigate, exts])

  const handleDelPath = useCallback(async () => {
    if (!delPath.path) return
    setDelPath(p => ({ ...p, loading: true }))
    try { await libApi.deleteLibraryPath(delPath.path.id) } catch { /* 静默 */ }
    setDelPath(initDelPath); await paths.loadPaths(); await media.loadMedia(media.page, media.search)
  }, [delPath.path, paths, media])

  const handleDelMedia = useCallback(async () => {
    if (!delMedia.file) return
    setDelMedia(p => ({ ...p, loading: true }))
    try { await libApi.deleteMediaFile(delMedia.file.id) } catch { /* 静默 */ }
    setDelMedia(initDelMedia); await media.loadMedia(media.page, media.search)
  }, [delMedia.file, media])

  const handleRename = useCallback(async () => {
    if (!rename.file) return
    const newName = rename.name.trim()
    if (!newName) return
    setRename(p => ({ ...p, loading: true }))
    try {
      await libApi.renameMediaFile(rename.file.id, newName)
      notifications.show({ title: '重命名成功', message: `已重命名为 "${newName}"`, color: 'green', autoClose: 3000 })
      setRename(initRename)
      await media.loadMedia(media.page, media.search)
    } catch (err) {
      const message = err instanceof Error ? err.message : '重命名失败'
      notifications.show({ title: '重命名失败', message, color: 'red', autoClose: 3000 })
      setRename(p => ({ ...p, loading: false }))
    }
  }, [rename.file, rename.name, media])

  return (
    <Stack gap="md">
      <Title order={2}>存储库管理</Title>
      <LibraryPathManager paths={paths.paths} loading={paths.loading} scanLoading={paths.scanLoading}
        newPath={paths.newPath} addingPath={paths.addingPath} extensionInputs={paths.extensionInputs}
        extensionTypes={paths.extensionTypes} extensionLoading={paths.extensionLoading}
        onNewPathChange={paths.setNewPath} onAddPath={paths.handleAddPath}
        onDeletePath={(p) => setDelPath({ opened: true, path: p, loading: false })}
        onScan={(id) => paths.handleScan(id)} onBrowsePath={() => navigate('/browse')}
        onExtensionInputChange={(id, v) => paths.setExtensionInputs(p => ({ ...p, [id]: v }))}
        onExtensionTypeChange={(id, t) => paths.setExtensionTypes(p => ({ ...p, [id]: t }))}
        onAddExtension={paths.handleAddExtension} />
      {progress?.status === 'scanning' && (
        <Box>
          <Text size="sm" c="dimmed" mb={4}>
            正在扫描… 已扫描 {progress.scanned_files} / {progress.total_files} 个文件
          </Text>
          <Progress
            value={progress.total_files > 0 ? (progress.scanned_files / progress.total_files) * 100 : 0}
            color="purple" animated size="sm" />
        </Box>
      )}
      <MediaTimeline mediaFiles={media.mediaFiles} total={media.total} page={media.page}
        searchInput={media.searchInput} loading={media.loading} error={media.error}
        totalPages={media.totalPages} customImageExtensions={exts}
        onSearchChange={(v) => { media.setSearchInput(v); media.setPage(1) }}
        onPageChange={media.setPage} onErrorClose={() => media.setError(null)}
        onOpenFile={handleOpen}
        onDeleteFile={(f) => setDelMedia({ opened: true, file: f, loading: false })}
        onRenameFile={(f) => setRename({ opened: true, file: f, name: f.file_name, loading: false })} />
      <ConfirmModal opened={delPath.opened} title="删除目录"
        message={`确定要删除目录 "${delPath.path?.label || delPath.path?.path}" 吗？关联的媒体文件也会被删除。`}
        confirmLabel="删除" cancelLabel="取消" confirmColor="red" loading={delPath.loading}
        onConfirm={handleDelPath} onCancel={() => setDelPath(initDelPath)} />
      <ConfirmModal opened={delMedia.opened} title="删除文件"
        message={`确定要删除 "${delMedia.file?.file_name}" 吗？`}
        confirmLabel="删除" cancelLabel="取消" confirmColor="red" loading={delMedia.loading}
        onConfirm={handleDelMedia} onCancel={() => setDelMedia(initDelMedia)} />
      <Modal opened={rename.opened} onClose={() => setRename(initRename)} title="重命名文件" centered size="sm">
        <Stack gap="md">
          <TextInput label="新文件名" value={rename.name}
            onChange={(e) => setRename(p => ({ ...p, name: e.target.value }))}
            onKeyDown={(e) => e.key === 'Enter' && handleRename()} />
          <Group justify="flex-end">
            <Button variant="subtle" color="gray" onClick={() => setRename(initRename)} disabled={rename.loading}>取消</Button>
            <Button color="purple" onClick={handleRename} loading={rename.loading} disabled={!rename.name.trim()}>确认</Button>
          </Group>
        </Stack>
      </Modal>
      <ImagePreviewModal file={preview} onClose={() => setPreview(null)} />
    </Stack>
  )
}
