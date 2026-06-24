import { useState, useMemo, useEffect, useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Stack, Title, Group, SegmentedControl, NativeSelect, Text, TextInput, Loader, Button, Drawer, Box } from '@mantine/core'
import { useDisclosure } from '@mantine/hooks'
import { notifications } from '@mantine/notifications'
import { IconSearch, IconFilter } from '@tabler/icons-react'
import { useLibraryPaths } from '@/hooks/useLibraryPaths'
import { useDirectoryBrowse } from '@/hooks/useDirectoryBrowse'
import DirectoryBrowser, { sortFiles, type DisplayMode, type DirSort } from '@/components/DirectoryBrowser'
import MediaQueryFilters from '@/components/MediaQueryFilters'
import MediaDetailPanel from '@/components/MediaDetailPanel'
import ConfirmModal from '@/components/ConfirmModal'
import BatchActionsModals from '@/components/BatchActionsModals'
import { useBatchActions } from '@/hooks/useBatchActions'
import { extractErrorMessage } from '@/utils/error'
import * as libApi from '@/api/library'
import type { MediaFile } from '@/types'

/**
 * 目录浏览页（FR-33 资源管理器视图 + FR-36 筛选/搜索接入）。
 * 无筛选：浏览文件夹树；有筛选/搜索：按当前目录路径（前缀）查媒体接口（消费 FR-35 引擎）展示匹配结果。
 */
export default function BrowsePage() {
  const [searchParams] = useSearchParams()
  const paths = useLibraryPaths(undefined)
  const browse = useDirectoryBrowse()
  const exts = paths.customImageExtensions

  // 展示方式 / 排序（FR-33）
  const [displayMode, setDisplayMode] = useState<DisplayMode>('list')
  const [sort, setSort] = useState<DirSort>('name')
  // 筛选/搜索（FR-36）：表达式搜索 + 结构化筛选
  const [searchInput, setSearchInput] = useState('')
  const [search, setSearch] = useState('')
  const [mediaType, setMediaType] = useState<'' | 'image' | 'video'>('')
  const [sizeMin, setSizeMin] = useState(0)
  const [timeFrom, setTimeFrom] = useState('')
  const [timeTo, setTimeTo] = useState('')
  // 详情面板选中下标（FR-34）
  const [detailIndex, setDetailIndex] = useState<number | null>(null)
  // 筛选结果（FR-36）
  const [filteredFiles, setFilteredFiles] = useState<MediaFile[]>([])
  const [filtering, setFiltering] = useState(false)
  // 批量删除（FR-69）：待确认删除的 id 列表（非空即弹确认框）+ 删除进行中
  const [pendingDelete, setPendingDelete] = useState<number[]>([])
  const [deleting, setDeleting] = useState(false)
  // 批量操作（FR-91）：加相册 / 打标签 / 打包下载
  const batch = useBatchActions()
  // 移动端筛选抽屉开合（FR-86）：窄屏将结构化筛选收进抽屉，搜索框常驻
  const [filterDrawerOpened, filterDrawer] = useDisclosure(false)

  // 搜索防抖 400ms
  useEffect(() => {
    const t = setTimeout(() => setSearch(searchInput), 400)
    return () => clearTimeout(t)
  }, [searchInput])

  const filterActive = !!(search.trim() || mediaType || sizeMin || timeFrom || timeTo)

  // 带库定位查询参数（library_id + path）时直接进该库浏览；否则以聚合虚拟根初始化（FR-66）
  useEffect(() => {
    const libraryID = Number(searchParams.get('library_id'))
    const path = searchParams.get('path')
    if (libraryID > 0 && path) {
      browse.handleBrowsePath(libraryID, path)
    } else {
      browse.initRoot()
    }
    // 仅依查询参数初始化一次（hook 内部以 initialized 守卫防重复）
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchParams])

  // 筛选生效时按当前目录路径（前缀，递归）查媒体接口（消费 FR-35），否则清空
  useEffect(() => {
    if (!filterActive || browse.browseLibraryID == null) {
      setFilteredFiles([])
      return
    }
    let active = true
    setFiltering(true)
    libApi.getMediaFiles({
      library_id: browse.browseLibraryID,
      path: browse.browseParentPath,
      search: search.trim() || undefined,
      type: mediaType || undefined,
      size_min: sizeMin || undefined,
      time_from: timeFrom || undefined,
      time_to: timeTo || undefined,
      page_size: 100,
    })
      .then((res) => { if (active) setFilteredFiles(res.items) })
      .catch(() => { if (active) setFilteredFiles([]) })
      .finally(() => { if (active) setFiltering(false) })
    return () => { active = false }
  }, [filterActive, search, mediaType, sizeMin, timeFrom, timeTo, browse.browseLibraryID, browse.browseParentPath])

  // 详情面板与浏览器用同一套排序，保证双击下标一致
  const activeFiles = filterActive ? filteredFiles : browse.files
  const sortedFiles = useMemo(() => sortFiles(activeFiles, sort), [activeFiles, sort])

  // 删除后刷新当前视图：筛选模式重跑筛选、浏览模式重载目录
  const refreshAfterDelete = useCallback(() => {
    if (filterActive && browse.browseLibraryID != null) {
      libApi.getMediaFiles({
        library_id: browse.browseLibraryID, path: browse.browseParentPath,
        search: search.trim() || undefined, type: mediaType || undefined,
        size_min: sizeMin || undefined, time_from: timeFrom || undefined, time_to: timeTo || undefined,
        page_size: 100,
      }).then((res) => setFilteredFiles(res.items)).catch(() => {})
    } else {
      browse.loadBrowse(browse.browseLibraryID, browse.browseParentPath)
    }
  }, [filterActive, browse, search, mediaType, sizeMin, timeFrom, timeTo])

  // 执行批量软删（FR-69）：确认后调端点，成功刷新 + 清空选择
  const confirmDelete = useCallback(async () => {
    setDeleting(true)
    try {
      const n = await libApi.batchDeleteMediaFiles(pendingDelete)
      notifications.show({ color: 'green', message: `已删除 ${n} 项到回收站` })
      setPendingDelete([])
      refreshAfterDelete()
    } catch (err) {
      notifications.show({ color: 'red', message: extractErrorMessage(err, '批量删除失败') })
    } finally {
      setDeleting(false)
    }
  }, [pendingDelete, refreshAfterDelete])

  return (
    <Stack gap="md">
      <Title order={2}>目录浏览</Title>

      {/* 搜索 + 筛选（FR-36，消费 FR-35 引擎，按当前目录递归筛选）：搜索框移动端亦常驻（FR-86） */}
      <Group gap="xs" align="center" wrap="nowrap">
        <TextInput
          placeholder="在当前目录下搜索：文件名，或 ext:jpg type:image size:>10mb"
          leftSection={<IconSearch size={14} />}
          value={searchInput}
          onChange={(e) => setSearchInput(e.currentTarget.value)}
          size="sm"
          style={{ flex: 1 }}
        />
        {/* 移动端「筛选」入口（FR-86）：打开抽屉，桌面隐藏 */}
        <Button
          variant="default"
          size="sm"
          leftSection={<IconFilter size={16} />}
          onClick={filterDrawer.open}
          hiddenFrom="sm"
        >
          筛选
        </Button>
      </Group>

      {/* 桌面端结构化筛选内联铺开（FR-86）：窄屏隐藏，改由抽屉承载 */}
      <Box visibleFrom="sm">
        <MediaQueryFilters
          mediaType={mediaType} onMediaTypeChange={setMediaType}
          sizeMin={sizeMin} onSizeMinChange={setSizeMin}
          timeFrom={timeFrom} onTimeFromChange={setTimeFrom}
          timeTo={timeTo} onTimeToChange={setTimeTo}
        />
      </Box>

      {/* 移动端筛选抽屉（FR-86）：承载与桌面同一套受控结构化筛选 */}
      <Drawer
        opened={filterDrawerOpened}
        onClose={filterDrawer.close}
        title="筛选"
        position="right"
        padding="md"
        hiddenFrom="sm"
        closeButtonProps={{ 'aria-label': '关闭筛选' }}
      >
        <MediaQueryFilters
          mediaType={mediaType} onMediaTypeChange={setMediaType}
          sizeMin={sizeMin} onSizeMinChange={setSizeMin}
          timeFrom={timeFrom} onTimeFromChange={setTimeFrom}
          timeTo={timeTo} onTimeToChange={setTimeTo}
        />
      </Drawer>

      {/* 展示方式 + 排序（FR-33） */}
      <Group gap="md" align="center">
        <SegmentedControl
          aria-label="展示方式" size="xs" value={displayMode}
          onChange={(v) => setDisplayMode(v as DisplayMode)}
          data={[
            { value: 'list', label: '列表' },
            { value: 'large', label: '大图标' },
            { value: 'medium', label: '中图标' },
            { value: 'small', label: '小图标' },
          ]}
        />
        <Group gap={4} align="center">
          <Text size="xs" c="dimmed">排序</Text>
          <NativeSelect
            aria-label="排序方式" size="xs" value={sort}
            onChange={(e) => setSort(e.currentTarget.value as DirSort)}
            data={[
              { value: 'name', label: '名称' },
              { value: 'size', label: '大小' },
              { value: 'type', label: '类型' },
              { value: 'time', label: '修改时间' },
            ]}
          />
        </Group>
        {filterActive && (
          <Group gap={6} align="center">
            {filtering ? <Loader size="xs" /> : <Text size="xs" c="dimmed">筛选结果 {filteredFiles.length} 条</Text>}
          </Group>
        )}
      </Group>

      {filterActive ? (
        // 筛选模式：展示当前目录（递归）下的匹配媒体，不展示子目录
        <DirectoryBrowser
          breadcrumbs={[]} directories={[]} files={filteredFiles}
          loading={filtering} error={null} customImageExtensions={exts}
          onEnterDir={() => {}} onBreadcrumbNavigate={() => {}}
          onErrorClose={() => {}}
          onOpenFile={(_, index) => setDetailIndex(index)}
          displayMode={displayMode} sort={sort}
          onBatchDelete={(ids) => setPendingDelete(ids)}
          onDeleteOne={(f) => setPendingDelete([f.id])}
          onBatchAddToAlbum={batch.openAddToAlbum}
          onBatchAddTag={batch.openAddTag}
          onBatchDownload={batch.download}
        />
      ) : (
        // 浏览模式：文件夹树
        <DirectoryBrowser
          breadcrumbs={browse.breadcrumbs} directories={browse.directories} files={browse.files}
          loading={browse.loading} error={browse.error} customImageExtensions={exts}
          onEnterDir={browse.handleEnterDir} onBreadcrumbNavigate={browse.handleBreadcrumbNavigate}
          onErrorClose={() => browse.setError(null)}
          onOpenFile={(_, index) => setDetailIndex(index)}
          displayMode={displayMode} sort={sort}
          onBatchDelete={(ids) => setPendingDelete(ids)}
          onDeleteOne={(f) => setPendingDelete([f.id])}
          onBatchAddToAlbum={batch.openAddToAlbum}
          onBatchAddTag={batch.openAddTag}
          onBatchDownload={batch.download}
        />
      )}

      <BatchActionsModals state={batch.modalState} />

      <MediaDetailPanel
        files={sortedFiles}
        initialIndex={detailIndex}
        onClose={() => setDetailIndex(null)}
        customImageExtensions={exts}
      />

      {/* 批量删除二次确认（FR-69）：删除进回收站，可在回收站还原 */}
      <ConfirmModal
        opened={pendingDelete.length > 0}
        title="删除媒体"
        message={`确定删除选中的 ${pendingDelete.length} 项？删除后进入回收站，可在回收站还原。`}
        confirmLabel="删除"
        onConfirm={confirmDelete}
        onCancel={() => setPendingDelete([])}
        loading={deleting}
      />
    </Stack>
  )
}
