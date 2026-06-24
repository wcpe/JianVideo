import { useState } from 'react'
import { SimpleGrid, Text, TextInput, Button, Group, Card, ActionIcon, Skeleton, Alert, Box, Badge, SegmentedControl, Collapse, UnstyledButton } from '@mantine/core'
import { IconPlus, IconTrash, IconRefresh, IconFolder, IconEye, IconLoader, IconListSearch, IconX, IconChevronRight, IconTags } from '@tabler/icons-react'
import type { LibraryPath, MediaExtension, MediaExtensionType, ScanMode } from '@/types'

/** 后缀筛选档位（FR-64）：全部 / 仅视频 / 仅图片 */
type ExtFilter = 'all' | 'video' | 'image'

/** 单个后缀徽标（FR-100）：内置以描边样式标识不可删、自定义带删除按钮 */
function ExtensionBadge({
  ext, onDelete,
}: {
  ext: MediaExtension
  onDelete?: () => void
}) {
  return (
    <Badge
      size="sm"
      variant={ext.is_builtin ? 'outline' : 'light'}
      color={ext.type === 'image' ? 'teal' : 'blue'}
      rightSection={ext.is_builtin || !onDelete ? undefined : (
        <ActionIcon size="xs" variant="transparent" color="red"
          aria-label={`删除后缀 ${ext.extension}`}
          onClick={onDelete}>
          <IconX size={10} />
        </ActionIcon>
      )}
    >
      {ext.extension}{ext.is_builtin ? '（内置）' : ''}
    </Badge>
  )
}

interface LibraryPathManagerProps {
  paths: LibraryPath[]
  loading: boolean
  scanLoading: Record<number, boolean>
  newPath: string
  addingPath: boolean
  extensionInputs: Record<number, string>
  extensionTypes: Record<number, MediaExtensionType>
  extensionLoading: Record<number, boolean>
  /** 各库完整后缀列表（内置 + 自定义），供后缀管理列出与删除（FR-64） */
  extensionsByLibrary: Record<number, MediaExtension[]>
  onNewPathChange: (value: string) => void
  onAddPath: () => void
  onDeletePath: (path: LibraryPath) => void
  onScan: (id: number, mode: ScanMode) => void
  onBrowsePath: (path: LibraryPath) => void
  onExtensionInputChange: (pathId: number, value: string) => void
  onExtensionTypeChange: (pathId: number, type: MediaExtensionType) => void
  onAddExtension: (path: LibraryPath) => void
  /** 删除某库的自定义后缀（FR-64） */
  onDeleteExtension: (path: LibraryPath, extension: string) => void
}

/** 路径管理 UI 组件：添加、浏览、扫描、删除路径及自定义后缀管理 */
export default function LibraryPathManager({
  paths, loading, scanLoading, newPath, addingPath,
  extensionInputs, extensionTypes, extensionLoading, extensionsByLibrary,
  onNewPathChange, onAddPath, onDeletePath, onScan, onBrowsePath,
  onExtensionInputChange, onExtensionTypeChange, onAddExtension, onDeleteExtension,
}: LibraryPathManagerProps) {
  // 各库后缀筛选档位（FR-64），缺省「全部」
  const [extFilters, setExtFilters] = useState<Record<number, ExtFilter>>({})
  // 各库后缀徽标墙折叠态（FR-100），缺省折叠以收起信息噪声
  const [extExpanded, setExtExpanded] = useState<Record<number, boolean>>({})

  return (
    <Box>
      <Text fw={600} mb="sm">目录管理</Text>
      <Group gap="xs" mb="sm">
        <TextInput placeholder="输入目录路径，如 D:\Videos" value={newPath}
          onChange={(e) => onNewPathChange(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && onAddPath()}
          style={{ flex: 1 }} size="sm" />
        <Button size="sm" leftSection={<IconPlus size={14} />} onClick={onAddPath}
          disabled={addingPath || !newPath.trim()} loading={addingPath}>添加</Button>
      </Group>
      {Object.values(scanLoading).some(Boolean) && (
        <Box role="status" mb="sm">
          <Alert color="purple" icon={<IconLoader size={16} />}>正在扫描目录，请稍候…</Alert>
        </Box>
      )}
      {loading ? <Skeleton height={40} /> : paths.length === 0 ? (
        <Text c="dimmed" size="sm">暂无目录，请添加</Text>
      ) : (
        // 存储库卡片网格（FR-65）：一行 2-3 个，窄列下卡内信息与操作纵向堆叠避免拥挤
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="sm">
          {paths.map((p) => {
            const filter = extFilters[p.id] || 'all'
            const exts = extensionsByLibrary[p.id] || []
            const visibleExts = filter === 'all' ? exts : exts.filter((e) => e.type === filter)
            // 折叠态展开后按内置/自定义分组（FR-100）
            const builtinExts = visibleExts.filter((e) => e.is_builtin)
            const customExts = visibleExts.filter((e) => !e.is_builtin)
            const expanded = extExpanded[p.id] || false
            return (
            <Card key={p.id} withBorder p="sm" radius="sm" bg="var(--mantine-color-default)">
              {/* 库信息：占整行、可点进浏览 */}
              <Box
                style={{ minWidth: 0, cursor: 'pointer' }}
                onClick={() => onBrowsePath(p)}
                role="button"
                aria-label={`打开 ${p.label || p.path} 目录`}
              >
                <Group gap={6} wrap="nowrap">
                  <IconFolder size={14} color="var(--mantine-color-purple-4)" />
                  <Text size="sm" truncate>{p.label || p.path}</Text>
                  <Badge size="xs" variant="light" color="purple">{p.media_count ?? 0} 个媒体</Badge>
                </Group>
                <Text size="xs" c="dimmed" truncate>{p.path}</Text>
              </Box>

              {/* 操作按钮：纵向堆叠在信息下方、窄列可换行不拥挤 */}
              <Group gap={4} mt="xs" wrap="wrap">
                <Button size="xs" variant="subtle" color="blue" leftSection={<IconEye size={12} />}
                  onClick={() => onBrowsePath(p)}>浏览</Button>
                <Button size="xs" variant="subtle" color="purple" leftSection={<IconRefresh size={12} />}
                  onClick={() => onScan(p.id, 'incremental')} loading={scanLoading[p.id]}>增量更新</Button>
                <Button size="xs" variant="subtle" color="purple" leftSection={<IconListSearch size={12} />}
                  onClick={() => onScan(p.id, 'full')} loading={scanLoading[p.id]}>全量扫描</Button>
                <ActionIcon size="sm" variant="subtle" color="red"
                  aria-label={`删除目录 ${p.label || p.path}`} onClick={() => onDeletePath(p)}>
                  <IconTrash size={14} />
                </ActionIcon>
              </Group>

              {/* 后缀管理（FR-64/FR-100）：默认折叠为摘要行，展开后按内置/自定义分组列出 */}
              <UnstyledButton
                aria-label={`${p.label || p.path} 后缀列表`}
                aria-expanded={expanded}
                mt="xs"
                onClick={() => setExtExpanded((prev) => ({ ...prev, [p.id]: !prev[p.id] }))}
                style={{ display: 'flex', alignItems: 'center', gap: 6, width: '100%' }}
              >
                <IconTags size={14} color="var(--mantine-color-purple-4)" />
                <Text size="xs" c="dimmed">{exts.length} 个后缀</Text>
                <IconChevronRight
                  size={14}
                  style={{
                    marginLeft: 'auto',
                    transition: 'transform 120ms ease',
                    transform: expanded ? 'rotate(90deg)' : 'none',
                  }}
                />
              </UnstyledButton>
              <Collapse in={expanded}>
                {/* 视频/图片/全部 筛选（FR-64）：作用于展开后的分组内容 */}
                <Group gap={6} mt="xs" align="center">
                  <SegmentedControl
                    aria-label={`${p.label || p.path} 后缀筛选`} size="xs" value={filter}
                    onChange={(v) => setExtFilters((prev) => ({ ...prev, [p.id]: v as ExtFilter }))}
                    data={[
                      { value: 'all', label: '全部' },
                      { value: 'video', label: '视频' },
                      { value: 'image', label: '图片' },
                    ]}
                  />
                </Group>
                {builtinExts.length > 0 && (
                  <Box mt={6}>
                    <Text size="xs" c="dimmed" mb={4}>内置</Text>
                    <Group gap={4} wrap="wrap">
                      {builtinExts.map((e) => <ExtensionBadge key={e.extension} ext={e} />)}
                    </Group>
                  </Box>
                )}
                {customExts.length > 0 && (
                  <Box mt={6}>
                    <Text size="xs" c="dimmed" mb={4}>自定义</Text>
                    <Group gap={4} wrap="wrap">
                      {customExts.map((e) => (
                        <ExtensionBadge key={e.extension} ext={e} onDelete={() => onDeleteExtension(p, e.extension)} />
                      ))}
                    </Group>
                  </Box>
                )}
                {visibleExts.length === 0 && (
                  <Text size="xs" c="dimmed" mt={6}>该筛选下暂无后缀</Text>
                )}
              </Collapse>

              {/* 添加自定义后缀（仍 video/image 二选一，不改数据模型）：常显以突出添加入口（FR-100） */}
              <Group gap={4} mt="xs" wrap="nowrap">
                <TextInput aria-label={`${p.label || p.path} 自定义后缀`} placeholder="自定义后缀，如 .foo" size="xs"
                  value={extensionInputs[p.id] || ''}
                  onChange={(e) => onExtensionInputChange(p.id, e.target.value)}
                  onKeyDown={(e) => e.key === 'Enter' && onAddExtension(p)}
                  style={{ flex: 1 }} />
                <Button.Group>
                  <Button size="xs" variant={(extensionTypes[p.id] || 'video') === 'video' ? 'filled' : 'light'}
                    onClick={() => onExtensionTypeChange(p.id, 'video')}>视频</Button>
                  <Button size="xs" variant={extensionTypes[p.id] === 'image' ? 'filled' : 'light'}
                    onClick={() => onExtensionTypeChange(p.id, 'image')}>图片</Button>
                </Button.Group>
                <Button size="xs" variant="light" onClick={() => onAddExtension(p)}
                  loading={extensionLoading[p.id]}
                  disabled={extensionLoading[p.id] || !(extensionInputs[p.id] || '').trim()}>添加后缀</Button>
              </Group>
            </Card>
            )
          })}
        </SimpleGrid>
      )}
    </Box>
  )
}
