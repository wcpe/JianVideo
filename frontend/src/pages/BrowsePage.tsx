import { useState, useMemo, useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Stack, Title, Group, SegmentedControl, NativeSelect, Text } from '@mantine/core'
import { useLibraryPaths } from '@/hooks/useLibraryPaths'
import { useDirectoryBrowse } from '@/hooks/useDirectoryBrowse'
import DirectoryBrowser, { sortFiles, type DisplayMode, type DirSort } from '@/components/DirectoryBrowser'
import MediaDetailPanel from '@/components/MediaDetailPanel'

/** 目录浏览页（FR-33 资源管理器视图）：多展示档位 + 排序 + 多选，双击打开详情面板 */
export default function BrowsePage() {
  const [searchParams] = useSearchParams()
  const paths = useLibraryPaths(undefined)
  const browse = useDirectoryBrowse()
  const exts = paths.customImageExtensions

  // 展示方式 / 排序（FR-33）
  const [displayMode, setDisplayMode] = useState<DisplayMode>('list')
  const [sort, setSort] = useState<DirSort>('name')
  // 详情面板选中下标（FR-34）
  const [detailIndex, setDetailIndex] = useState<number | null>(null)

  // 面板用与 DirectoryBrowser 同一套排序，保证下标一致
  const sortedFiles = useMemo(() => sortFiles(browse.files, sort), [browse.files, sort])

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

  return (
    <Stack gap="md">
      <Title order={2}>目录浏览</Title>

      {/* 工具栏：展示方式 + 排序（FR-33） */}
      <Group gap="md" align="center">
        <SegmentedControl
          aria-label="展示方式"
          size="xs"
          value={displayMode}
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
            aria-label="排序方式"
            size="xs"
            value={sort}
            onChange={(e) => setSort(e.currentTarget.value as DirSort)}
            data={[
              { value: 'name', label: '名称' },
              { value: 'size', label: '大小' },
              { value: 'type', label: '类型' },
              { value: 'time', label: '修改时间' },
            ]}
          />
        </Group>
      </Group>

      <DirectoryBrowser
        breadcrumbs={browse.breadcrumbs} directories={browse.directories} files={browse.files}
        loading={browse.loading} error={browse.error} customImageExtensions={exts}
        onEnterDir={browse.handleEnterDir} onBreadcrumbNavigate={browse.handleBreadcrumbNavigate}
        onErrorClose={() => browse.setError(null)}
        onOpenFile={(_, index) => setDetailIndex(index)}
        displayMode={displayMode} sort={sort}
      />

      <MediaDetailPanel
        files={sortedFiles}
        initialIndex={detailIndex}
        onClose={() => setDetailIndex(null)}
        customImageExtensions={exts}
      />
    </Stack>
  )
}
