import { useState, useEffect, useCallback, useRef } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import {
  SimpleGrid, Card, Text, Button, TextInput, ActionIcon, Group,
  Pagination, Stack, Title, Box, Badge, Skeleton, Alert, Tabs, Modal, Loader,
} from '@mantine/core'
import { IconPlus, IconSearch, IconTrash, IconRefresh, IconFolder, IconAlertCircle, IconClock, IconFolderOpen, IconEye } from '@tabler/icons-react'
import { notifications } from '@mantine/notifications'
import * as libApi from '@/api/library'
import ConfirmModal from '@/components/ConfirmModal'
import DirectoryBreadcrumb from '@/components/DirectoryBreadcrumb'
import { formatSize, formatDuration } from '@/utils/format'
import type { LibraryPath, MediaFile, BreadcrumbItem, DirInfo, MediaExtension, MediaExtensionType } from '@/types'

interface LibraryPageProps {
  initialTab?: 'timeline' | 'directory'
}

export default function LibraryPage({ initialTab = 'timeline' }: LibraryPageProps) {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  // Tab 状态同步到 URL
  const activeTab = searchParams.get('tab') || initialTab
  const setActiveTab = (tab: string) => setSearchParams({ tab })

  // 路径状态
  const [paths, setPaths] = useState<LibraryPath[]>([])
  const [newPath, setNewPath] = useState('')
  const [addingPath, setAddingPath] = useState(false)
  const addingPathRef = useRef(false)
  const [pathsLoading, setPathsLoading] = useState(false)
  const [scanLoading, setScanLoading] = useState<Record<number, boolean>>({})
  const [extensionInputs, setExtensionInputs] = useState<Record<number, string>>({})
  const [extensionTypes, setExtensionTypes] = useState<Record<number, MediaExtensionType>>({})
  const [extensionLoading, setExtensionLoading] = useState<Record<number, boolean>>({})
  const [customImageExtensions, setCustomImageExtensions] = useState<Record<number, string[]>>({})

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

  // 目录浏览状态
  const [browseLibraryID, setBrowseLibraryID] = useState<number | null>(null)
  const [browseParentPath, setBrowseParentPath] = useState('/')
  const [browseBreadcrumbs, setBrowseBreadcrumbs] = useState<BreadcrumbItem[]>([])
  const [browseDirectories, setBrowseDirectories] = useState<DirInfo[]>([])
  const [browseFiles, setBrowseFiles] = useState<MediaFile[]>([])
  const [browseLoading, setBrowseLoading] = useState(false)
  const [browseError, setBrowseError] = useState<string | null>(null)
  const [previewFile, setPreviewFile] = useState<MediaFile | null>(null)

  const pageSize = 20

  const loadExtensionPolicies = useCallback(async (items: LibraryPath[], replace = true) => {
    const entries = await Promise.all(items.map(async (path) => {
      try {
        const extensions = await libApi.listMediaExtensions(path.id)
        const imageExts = extensions
          .filter((ext: MediaExtension) => ext.type === 'image')
          .map(ext => ext.extension.toLowerCase().replace(/^\./, ''))
        return [path.id, imageExts] as const
      } catch {
        return [path.id, []] as const
      }
    }))
    const next = Object.fromEntries(entries)
    setCustomImageExtensions(prev => (replace ? next : { ...prev, ...next }))
  }, [])

  const loadPaths = useCallback(async () => {
    setPathsLoading(true)
    try {
      const items = await libApi.getLibraryPaths()
      setPaths(items)
      await loadExtensionPolicies(items)
    } catch {
      setError('加载目录列表失败，请刷新页面重试')
    } finally {
      setPathsLoading(false)
    }
  }, [loadExtensionPolicies])

  const loadMedia = useCallback(async () => {
    setMediaLoading(true)
    setError(null)
    try {
      const res = await libApi.getMediaFiles({
        page,
        page_size: pageSize,
        search: search || undefined,
        sort: 'time_desc',
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

  const loadBrowse = useCallback(async () => {
    if (browseLibraryID === null) return
    setBrowseLoading(true)
    setBrowseError(null)
    try {
      const res = await libApi.browseDirectory(browseLibraryID, browseParentPath)
      setBrowseBreadcrumbs(res.breadcrumbs)
      setBrowseDirectories(res.directories)
      setBrowseFiles(res.files)
    } catch (err) {
      const message = err instanceof Error ? err.message : '加载目录内容失败，请重试'
      setBrowseError(message)
      setBrowseBreadcrumbs([])
      setBrowseDirectories([])
      setBrowseFiles([])
    } finally {
      setBrowseLoading(false)
    }
  }, [browseLibraryID, browseParentPath])

  useEffect(() => { loadPaths() }, [loadPaths])
  useEffect(() => { loadMedia() }, [loadMedia])

  // 目录浏览是否已初始化
  const browseInitialized = useRef(false)

  // 当选中的路径变化时，自动加载目录浏览（仅在首次初始化时设置默认路径）
  useEffect(() => {
    if (!browseInitialized.current && activeTab === 'directory' && paths.length > 0) {
      const first = paths[0]
      setBrowseLibraryID(first.id)
      setBrowseParentPath(first.path)
      browseInitialized.current = true
    }
  }, [activeTab, paths, browseLibraryID])

  useEffect(() => {
    if (activeTab === 'directory') {
      loadBrowse()
    }
  }, [activeTab, loadBrowse])

  // 进入子目录
  const handleEnterDir = (dirPath: string) => {
    setBrowseParentPath(dirPath)
  }

  const handleBrowsePath = (path: LibraryPath) => {
    setBrowseLibraryID(path.id)
    setBrowseParentPath(path.path)
    browseInitialized.current = true
    setActiveTab('directory')
  }

  // 面包屑导航
  const handleBreadcrumbNavigate = (path: string) => {
    setBrowseParentPath(path)
  }

  // 添加路径
  const handleAddPath = async () => {
    if (!newPath.trim() || addingPathRef.current) return
    addingPathRef.current = true
    setAddingPath(true)
    try {
      const created = await libApi.createLibraryPath(newPath.trim())
      setNewPath('')
      notifications.show({ title: '添加成功', message: `目录 "${created.label || created.path}" 已添加`, color: 'green', autoClose: 3000 })
      await loadPaths()
      await loadMedia()
    } catch (err) {
      const message = err instanceof Error ? err.message : '无法添加目录，请检查路径是否正确'
      notifications.show({ title: '添加失败', message, color: 'red', autoClose: 3000 })
    } finally {
      addingPathRef.current = false
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
      if (activeTab === 'directory' && browseLibraryID === id) {
        await loadBrowse()
      }
    } catch {
      notifications.show({ title: '扫描失败', message: '扫描目录时出错', color: 'red', autoClose: 3000 })
    } finally {
      setScanLoading(prev => ({ ...prev, [id]: false }))
    }
  }

  const handleAddExtension = async (path: LibraryPath) => {
    const extension = (extensionInputs[path.id] || '').trim()
    if (!extension) return
    const mediaType = extensionTypes[path.id] || 'video'
    setExtensionLoading(prev => ({ ...prev, [path.id]: true }))
    try {
      await libApi.addMediaExtension(path.id, extension, mediaType)
      setExtensionInputs(prev => ({ ...prev, [path.id]: '' }))
      await loadExtensionPolicies([path], false)
      notifications.show({ title: '后缀已添加', message: `${extension} 已绑定到 "${path.label || path.path}"`, color: 'green', autoClose: 3000 })
    } catch (err) {
      const message = err instanceof Error ? err.message : '添加后缀失败，请检查格式'
      notifications.show({ title: '添加失败', message, color: 'red', autoClose: 3000 })
    } finally {
      setExtensionLoading(prev => ({ ...prev, [path.id]: false }))
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
  const isImageFile = (file: MediaFile) => {
    const format = file.format.toLowerCase().replace(/^\./, '')
    const builtInImages = ['jpg', 'jpeg', 'png', 'gif', 'webp', 'bmp', 'svg']
    return builtInImages.includes(format) || (customImageExtensions[file.library_id] || []).includes(format)
  }
  const handleOpenFile = (file: MediaFile) => {
    if (isImageFile(file)) {
      setPreviewFile(file)
      return
    }
    navigate(`/play/${file.id}`)
  }

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

        {Object.values(scanLoading).some(Boolean) && (
          <Box role="status" mb="sm">
            <Alert color="purple" icon={<Loader size={16} />}>
              正在扫描目录，请稍候…
            </Alert>
          </Box>
        )}

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
                      color="blue"
                      leftSection={<IconEye size={12} />}
                      onClick={() => handleBrowsePath(p)}
                    >
                      浏览
                    </Button>
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
                <Group gap={4} mt="xs" wrap="nowrap">
                  <TextInput
                    aria-label={`${p.label || p.path} 自定义后缀`}
                    placeholder="自定义后缀，如 .foo"
                    size="xs"
                    value={extensionInputs[p.id] || ''}
                    onChange={(e) => setExtensionInputs(prev => ({ ...prev, [p.id]: e.target.value }))}
                    onKeyDown={(e) => e.key === 'Enter' && handleAddExtension(p)}
                    style={{ flex: 1 }}
                  />
                  <Button.Group>
                    <Button size="xs" variant={(extensionTypes[p.id] || 'video') === 'video' ? 'filled' : 'light'} onClick={() => setExtensionTypes(prev => ({ ...prev, [p.id]: 'video' }))}>视频</Button>
                    <Button size="xs" variant={extensionTypes[p.id] === 'image' ? 'filled' : 'light'} onClick={() => setExtensionTypes(prev => ({ ...prev, [p.id]: 'image' }))}>图片</Button>
                  </Button.Group>
                  <Button
                    size="xs"
                    variant="light"
                    onClick={() => handleAddExtension(p)}
                    loading={extensionLoading[p.id]}
                    disabled={extensionLoading[p.id] || !(extensionInputs[p.id] || '').trim()}
                  >
                    添加后缀
                  </Button>
                </Group>
              </Card>
            ))}
          </Stack>
        )}
      </Box>

      {/* Tab 切换 */}
      <Tabs value={activeTab} onChange={(v) => setActiveTab(v as 'timeline' | 'directory')} color="purple">
        <Tabs.List>
          <Tabs.Tab value="timeline" leftSection={<IconClock size={14} />}>时间轴</Tabs.Tab>
          <Tabs.Tab value="directory" leftSection={<IconFolderOpen size={14} />}>文件目录</Tabs.Tab>
        </Tabs.List>

        <Tabs.Panel value="timeline" pt="sm">
          {/* 搜索 */}
          <TextInput
            placeholder="搜索文件名..."
            leftSection={<IconSearch size={14} />}
            value={searchInput}
            onChange={(e) => { setSearchInput(e.target.value); setPage(1) }}
            size="sm"
            mb="sm"
          />

          {error && (
            <Alert icon={<IconAlertCircle size={16} />} color="red" withCloseButton onClose={() => setError(null)} mb="sm">
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
                    onClick={() => handleOpenFile(file)}
                    className="hover-card"
                  >
                    {isImageFile(file) && (
                      <Box
                        mb="xs"
                        style={{
                          aspectRatio: '16 / 9',
                          background: 'var(--mantine-color-dark-6)',
                          overflow: 'hidden',
                          borderRadius: 4,
                        }}
                      >
                        <img
                          src={`/api/library/media/${file.id}/raw`}
                          alt={file.file_name}
                          loading="lazy"
                          style={{ width: '100%', height: '100%', objectFit: 'cover', display: 'block' }}
                        />
                      </Box>
                    )}
                    <Text fw={500} truncate mb="xs">{file.file_name}</Text>
                    <Group gap="xs" wrap="nowrap">
                      <Badge size="xs" variant="light" color="purple">{formatSize(file.file_size)}</Badge>
                      <Badge size="xs" variant="light" color="blue">{file.format.toUpperCase()}</Badge>
                      {!isImageFile(file) && <Badge size="xs" variant="light" color="gray">{formatDuration(file.duration)}</Badge>}
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
        </Tabs.Panel>

        <Tabs.Panel value="directory" pt="sm">
          {browseError && (
            <Alert icon={<IconAlertCircle size={16} />} color="red" withCloseButton onClose={() => setBrowseError(null)} mb="sm">
              {browseError}
            </Alert>
          )}

          {/* 面包屑导航 */}
          {browseBreadcrumbs.length > 0 && (
            <Box mb="sm">
              <DirectoryBreadcrumb items={browseBreadcrumbs} onNavigate={handleBreadcrumbNavigate} />
            </Box>
          )}

          {/* 目录浏览内容 */}
          {browseLoading ? (
            <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} height={60} radius="md" />
              ))}
            </SimpleGrid>
          ) : (
            <Stack gap="xs">
              {/* 子目录列表 */}
              {browseDirectories.map((dir) => (
                <Card
                  key={dir.path}
                  withBorder p="sm" radius="sm" bg="dark.7"
                  style={{ cursor: 'pointer' }}
                  onClick={() => handleEnterDir(dir.path)}
                  className="hover-card"
                >
                  <Group gap="xs">
                    <IconFolder size={16} color="var(--mantine-color-purple-4)" />
                    <Text size="sm">{dir.name}</Text>
                  </Group>
                </Card>
              ))}

              {/* 当前目录下的文件列表 */}
              {browseFiles.map((file) => (
                <Card
                  key={file.id}
                  withBorder p="md" radius="md" bg="dark.7"
                  style={{ cursor: 'pointer' }}
                  onClick={() => handleOpenFile(file)}
                  className="hover-card"
                >
                  {isImageFile(file) && (
                    <Box
                      mb="xs"
                      style={{
                        aspectRatio: '16 / 9',
                        background: 'var(--mantine-color-dark-6)',
                        overflow: 'hidden',
                        borderRadius: 4,
                      }}
                    >
                      <img
                        src={`/api/library/media/${file.id}/raw`}
                        alt={file.file_name}
                        loading="lazy"
                        style={{ width: '100%', height: '100%', objectFit: 'cover', display: 'block' }}
                      />
                    </Box>
                  )}
                  <Text fw={500} truncate mb="xs">{file.file_name}</Text>
                  <Group gap="xs" wrap="nowrap">
                    <Badge size="xs" variant="light" color="purple">{formatSize(file.file_size)}</Badge>
                    <Badge size="xs" variant="light" color="blue">{file.format.toUpperCase()}</Badge>
                    {!isImageFile(file) && <Badge size="xs" variant="light" color="gray">{formatDuration(file.duration)}</Badge>}
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

              {/* 空状态 */}
              {browseDirectories.length === 0 && browseFiles.length === 0 && (
                <Box py="xl" ta="center">
                  <Box mb="sm" style={{ textAlign: 'center' }}>
                    <IconFolder size={48} color="var(--mantine-color-dark-3)" />
                  </Box>
                  <Text c="dimmed">此目录暂无内容</Text>
                </Box>
              )}
            </Stack>
          )}
        </Tabs.Panel>
      </Tabs>

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

      <Modal opened={!!previewFile} onClose={() => setPreviewFile(null)} title={previewFile?.file_name} centered size="xl">
        {previewFile && (
          <Box component="img" src={`/api/library/media/${previewFile.id}/raw`} alt={previewFile.file_name} style={{ width: '100%', height: 'auto' }} />
        )}
      </Modal>
    </Stack>
  )
}
