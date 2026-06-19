import { useState, useEffect, useCallback } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  SimpleGrid, Card, Text, Button, TextInput, ActionIcon, Group,
  Pagination, Stack, Title, Box, Badge, Skeleton, Alert,
} from '@mantine/core'
import { IconPlus, IconSearch, IconTrash, IconRefresh, IconFolder, IconAlertCircle } from '@tabler/icons-react'
import { notifications } from '@mantine/notifications'
import * as libApi from '@/api/library'
import ConfirmModal from '@/components/ConfirmModal'
import type { LibraryPath, MediaFile } from '@/types'

/** 格式化文件大小为人类可读字符串 */
export function formatSize(bytes: number): string {
  if (bytes === 0) return '-'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
}

/** 格式化时长（秒）为 HH:MM:SS 或 MM:SS */
export function formatDuration(seconds: number): string {
  if (!seconds || seconds <= 0) return '-'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = Math.floor(seconds % 60)
  if (h > 0) return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`
  return `${m}:${s.toString().padStart(2, '0')}`
}

export default function LibraryPage() {
  const navigate = useNavigate()

  // 路径状态
  const [paths, setPaths] = useState<LibraryPath[]>([])
  const [newPath, setNewPath] = useState('')
  const [addingPath, setAddingPath] = useState(false)
  const [pathsLoading, setPathsLoading] = useState(false)
  const [scanLoading, setScanLoading] = useState<Record<number, boolean>>({})

  // 媒体列表状态
  const [mediaFiles, setMediaFiles] = useState<MediaFile[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [search, setSearch] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [mediaLoading, setMediaLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // 确认弹窗状态
  const [deletePathModal, setDeletePathModal] = useState<{ opened: boolean; path: LibraryPath | null; loading: boolean }>({ opened: false, path: null, loading: false })
  const [deleteMediaModal, setDeleteMediaModal] = useState<{ opened: boolean; file: MediaFile | null; loading: boolean }>({ opened: false, file: null, loading: false })

  const pageSize = 20

  const loadPaths = useCallback(async () => {
    setPathsLoading(true)
    try {
      const items = await libApi.getLibraryPaths()
      setPaths(items)
    } catch {
      // 静默失败
    } finally {
      setPathsLoading(false)
    }
  }, [])

  const loadMedia = useCallback(async () => {
    setMediaLoading(true)
    setError(null)
    try {
      const res = await libApi.getMediaFiles({
        page,
        page_size: pageSize,
        search: search || undefined,
      })
      setMediaFiles(res.items)
      setTotal(res.total)
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载失败')
      setMediaFiles([])
      setTotal(0)
    } finally {
      setMediaLoading(false)
    }
  }, [page, search])

  useEffect(() => { loadPaths() }, [loadPaths])
  useEffect(() => { loadMedia() }, [loadMedia])

  // 添加路径
  const handleAddPath = async () => {
    if (!newPath.trim()) return
    setAddingPath(true)
    try {
      const created = await libApi.createLibraryPath(newPath.trim())
      setNewPath('')
      notifications.show({ title: '添加成功', message: `目录 "${created.label || created.path}" 已添加`, color: 'green', autoClose: 3000 })
      await loadPaths()
      await loadMedia()
    } catch {
      notifications.show({ title: '添加失败', message: '无法添加目录，请检查路径是否正确', color: 'red', autoClose: 3000 })
    } finally {
      setAddingPath(false)
    }
  }

  // 删除路径
  const handleDeletePath = async () => {
    if (!deletePathModal.path) return
    setDeletePathModal(prev => ({ ...prev, loading: true }))
    try {
      await libApi.deleteLibraryPath(deletePathModal.path.id)
      notifications.show({ title: '删除成功', message: `目录 "${deletePathModal.path.label || deletePathModal.path.path}" 已删除`, color: 'green', autoClose: 3000 })
      setDeletePathModal({ opened: false, path: null, loading: false })
      await loadPaths()
      await loadMedia()
    } catch {
      notifications.show({ title: '删除失败', message: '无法删除目录', color: 'red', autoClose: 3000 })
      setDeletePathModal(prev => ({ ...prev, loading: false }))
    }
  }

  // 扫描路径
  const handleScan = async (id: number) => {
    setScanLoading(prev => ({ ...prev, [id]: true }))
    try {
      const res = await libApi.scanLibrary(id)
      notifications.show({ title: '扫描完成', message: `发现 ${res.scanned} 个新文件`, color: 'green', autoClose: 3000 })
      await loadMedia()
    } catch {
      notifications.show({ title: '扫描失败', message: '扫描目录时出错', color: 'red', autoClose: 3000 })
    } finally {
      setScanLoading(prev => ({ ...prev, [id]: false }))
    }
  }

  // 删除媒体
  const handleDeleteMedia = async () => {
    if (!deleteMediaModal.file) return
    setDeleteMediaModal(prev => ({ ...prev, loading: true }))
    try {
      await libApi.deleteMediaFile(deleteMediaModal.file.id)
      notifications.show({ title: '删除成功', message: `"${deleteMediaModal.file.file_name}" 已删除`, color: 'green', autoClose: 3000 })
      setDeleteMediaModal({ opened: false, file: null, loading: false })
      await loadMedia()
    } catch {
      notifications.show({ title: '删除失败', message: '无法删除文件', color: 'red', autoClose: 3000 })
      setDeleteMediaModal(prev => ({ ...prev, loading: false }))
    }
  }

  // 搜索（防抖 400ms）
  useEffect(() => {
    const timer = setTimeout(() => setSearch(searchInput), 400)
    return () => clearTimeout(timer)
  }, [searchInput])

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <Stack gap="md">
      <Title order={2}>媒体库</Title>

      {/* 路径管理 */}
      <Box>
        <Text fw={600} mb="sm">目录管理</Text>

        <Group gap="xs" mb="sm">
          <TextInput
            placeholder="输入目录路径，如 D:\Videos"
            value={newPath}
            onChange={(e) => setNewPath(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && handleAddPath()}
            style={{ flex: 1 }}
            size="sm"
          />
          <Button
            size="sm"
            leftSection={<IconPlus size={14} />}
            onClick={handleAddPath}
            disabled={addingPath || !newPath.trim()}
            loading={addingPath}
          >
            添加
          </Button>
        </Group>

        {pathsLoading ? (
          <Skeleton height={40} />
        ) : paths.length === 0 ? (
          <Text c="dimmed" size="sm">暂无目录，请添加</Text>
        ) : (
          <Stack gap={4}>
            {paths.map((p) => (
              <Card key={p.id} withBorder p="xs" radius="sm" bg="dark.7">
                <Group justify="space-between" wrap="nowrap">
                  <Box style={{ flex: 1, minWidth: 0 }}>
                    <Group gap={6} wrap="nowrap">
                      <IconFolder size={14} color="var(--mantine-color-purple-4)" />
                      <Text size="sm" truncate>{p.label || p.path}</Text>
                    </Group>
                    <Text size="xs" c="dimmed" truncate>{p.path}</Text>
                  </Box>
                  <Group gap={4} wrap="nowrap">
                    <Button
                      size="xs"
                      variant="subtle"
                      color="purple"
                      leftSection={<IconRefresh size={12} />}
                      onClick={() => handleScan(p.id)}
                      loading={scanLoading[p.id]}
                    >
                      扫描
                    </Button>
                    <ActionIcon
                      size="sm"
                      variant="subtle"
                      color="red"
                      onClick={() => setDeletePathModal({ opened: true, path: p, loading: false })}
                    >
                      <IconTrash size={14} />
                    </ActionIcon>
                  </Group>
                </Group>
              </Card>
            ))}
          </Stack>
        )}
      </Box>

      {/* 搜索 */}
      <TextInput
        placeholder="搜索文件名..."
        leftSection={<IconSearch size={14} />}
        value={searchInput}
        onChange={(e) => { setSearchInput(e.target.value); setPage(1) }}
        size="sm"
      />

      {error && (
        <Alert icon={<IconAlertCircle size={16} />} color="red" withCloseButton onClose={() => setError(null)}>
          {error}
        </Alert>
      )}

      {/* 媒体列表 */}
      {mediaLoading ? (
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} height={100} radius="md" />
          ))}
        </SimpleGrid>
      ) : mediaFiles.length === 0 ? (
        <Box py="xl" ta="center">
          <Box mb="sm" style={{ textAlign: 'center' }}>
            <IconFolder size={48} color="var(--mantine-color-dark-3)" />
          </Box>
          <Text c="dimmed">{search ? '未找到匹配的文件' : '暂无媒体文件'}</Text>
          <Text c="dimmed" size="sm">添加目录后点击"扫描"按钮索引视频</Text>
        </Box>
      ) : (
        <>
          <Text size="sm" c="dimmed">共 {total} 个文件</Text>
          <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
            {mediaFiles.map((file) => (
              <Card
                key={file.id}
                withBorder p="md" radius="md" bg="dark.7"
                style={{ cursor: 'pointer' }}
                onClick={() => navigate(`/play/${file.id}`)}
                className="hover-card"
              >
                <Text fw={500} truncate mb="xs">{file.file_name}</Text>
                <Group gap="xs" wrap="nowrap">
                  <Badge size="xs" variant="light" color="purple">{formatSize(file.file_size)}</Badge>
                  <Badge size="xs" variant="light" color="blue">{file.format.toUpperCase()}</Badge>
                  <Badge size="xs" variant="light" color="gray">{formatDuration(file.duration)}</Badge>
                  {file.width > 0 && file.height > 0 && (
                    <Badge size="xs" variant="light" color="gray">{file.width}x{file.height}</Badge>
                  )}
                </Group>
                <Group justify="flex-end" mt="xs">
                  <ActionIcon
                    size="sm" variant="subtle" color="red"
                    onClick={(e) => { e.stopPropagation(); setDeleteMediaModal({ opened: true, file, loading: false }) }}
                  >
                    <IconTrash size={14} />
                  </ActionIcon>
                </Group>
              </Card>
            ))}
          </SimpleGrid>
        </>
      )}

      {totalPages > 1 && (
        <Group justify="center" mt="md">
          <Pagination total={totalPages} value={page} onChange={setPage} size="sm" color="purple" />
        </Group>
      )}

      {/* 确认删除路径弹窗 */}
      <ConfirmModal
        opened={deletePathModal.opened}
        title="删除目录"
        message={`确定要删除目录 "${deletePathModal.path?.label || deletePathModal.path?.path}" 吗？关联的媒体文件也会被删除。`}
        confirmLabel="删除"
        cancelLabel="取消"
        confirmColor="red"
        loading={deletePathModal.loading}
        onConfirm={handleDeletePath}
        onCancel={() => setDeletePathModal({ opened: false, path: null, loading: false })}
      />

      {/* 确认删除媒体弹窗 */}
      <ConfirmModal
        opened={deleteMediaModal.opened}
        title="删除文件"
        message={`确定要删除 "${deleteMediaModal.file?.file_name}" 吗？`}
        confirmLabel="删除"
        cancelLabel="取消"
        confirmColor="red"
        loading={deleteMediaModal.loading}
        onConfirm={handleDeleteMedia}
        onCancel={() => setDeleteMediaModal({ opened: false, file: null, loading: false })}
      />
    </Stack>
  )
}
