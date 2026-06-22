import { useState, useMemo, useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Stack, Title, Group, SegmentedControl, NativeSelect, Text, TextInput, Loader } from '@mantine/core'
import { IconSearch } from '@tabler/icons-react'
import { useLibraryPaths } from '@/hooks/useLibraryPaths'
import { useDirectoryBrowse } from '@/hooks/useDirectoryBrowse'
import DirectoryBrowser, { sortFiles, type DisplayMode, type DirSort } from '@/components/DirectoryBrowser'
import MediaQueryFilters from '@/components/MediaQueryFilters'
import MediaDetailPanel from '@/components/MediaDetailPanel'
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

  // 搜索防抖 400ms
  useEffect(() => {
    const t = setTimeout(() => setSearch(searchInput), 400)
    return () => clearTimeout(t)
  }, [searchInput])

  const filterActive = !!(search.trim() || mediaType || sizeMin || timeFrom || timeTo)

  // 带库定位查询参数（library_id + path）时优先用其初始化；否则回退首个库根目录
  useEffect(() => {
    if (browse.browseLibraryID) return
    const libraryID = Number(searchParams.get('library_id'))
    const path = searchParams.get('path')
    if (libraryID > 0 && path) {
      browse.initIfNeeded(libraryID, path)
    } else if (paths.paths.length > 0) {
      browse.initIfNeeded(paths.paths[0].id, paths.paths[0].path)
    }
  }, [paths.paths, browse.browseLibraryID, searchParams])

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

  return (
    <Stack gap="md">
      <Title order={2}>目录浏览</Title>

      {/* 搜索 + 筛选（FR-36，消费 FR-35 引擎，按当前目录递归筛选） */}
      <TextInput
        placeholder="在当前目录下搜索：文件名，或 ext:jpg type:image size:>10mb"
        leftSection={<IconSearch size={14} />}
        value={searchInput}
        onChange={(e) => setSearchInput(e.currentTarget.value)}
        size="sm"
      />
      <MediaQueryFilters
        mediaType={mediaType} onMediaTypeChange={setMediaType}
        sizeMin={sizeMin} onSizeMinChange={setSizeMin}
        timeFrom={timeFrom} onTimeFromChange={setTimeFrom}
        timeTo={timeTo} onTimeToChange={setTimeTo}
      />

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
        />
      )}

      <MediaDetailPanel
        files={sortedFiles}
        initialIndex={detailIndex}
        onClose={() => setDetailIndex(null)}
        customImageExtensions={exts}
      />
    </Stack>
  )
}
