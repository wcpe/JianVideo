import { Stack, Text, TextInput, Button, Group, Card, ActionIcon, Skeleton, Alert, Box, Badge } from '@mantine/core'
import { IconPlus, IconTrash, IconRefresh, IconFolder, IconEye, IconLoader, IconListSearch } from '@tabler/icons-react'
import type { LibraryPath, MediaExtensionType, ScanMode } from '@/types'

interface LibraryPathManagerProps {
  paths: LibraryPath[]
  loading: boolean
  scanLoading: Record<number, boolean>
  newPath: string
  addingPath: boolean
  extensionInputs: Record<number, string>
  extensionTypes: Record<number, MediaExtensionType>
  extensionLoading: Record<number, boolean>
  onNewPathChange: (value: string) => void
  onAddPath: () => void
  onDeletePath: (path: LibraryPath) => void
  onScan: (id: number, mode: ScanMode) => void
  onBrowsePath: (path: LibraryPath) => void
  onExtensionInputChange: (pathId: number, value: string) => void
  onExtensionTypeChange: (pathId: number, type: MediaExtensionType) => void
  onAddExtension: (path: LibraryPath) => void
}

/** 路径管理 UI 组件：添加、浏览、扫描、删除路径及自定义后缀管理 */
export default function LibraryPathManager({
  paths, loading, scanLoading, newPath, addingPath,
  extensionInputs, extensionTypes, extensionLoading,
  onNewPathChange, onAddPath, onDeletePath, onScan, onBrowsePath,
  onExtensionInputChange, onExtensionTypeChange, onAddExtension,
}: LibraryPathManagerProps) {
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
        <Stack gap={4}>
          {paths.map((p) => (
            <Card key={p.id} withBorder p="xs" radius="sm" bg="var(--mantine-color-default)">
              <Group justify="space-between" wrap="nowrap">
                <Box
                  style={{ flex: 1, minWidth: 0, cursor: 'pointer' }}
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
                <Group gap={4} wrap="nowrap">
                  <Button size="xs" variant="subtle" color="blue" leftSection={<IconEye size={12} />}
                    onClick={() => onBrowsePath(p)}>浏览</Button>
                  <Button size="xs" variant="subtle" color="purple" leftSection={<IconRefresh size={12} />}
                    onClick={() => onScan(p.id, 'incremental')} loading={scanLoading[p.id]}>增量更新</Button>
                  <Button size="xs" variant="subtle" color="purple" leftSection={<IconListSearch size={12} />}
                    onClick={() => onScan(p.id, 'full')} loading={scanLoading[p.id]}>全量扫描</Button>
                  <ActionIcon size="sm" variant="subtle" color="red" onClick={() => onDeletePath(p)}>
                    <IconTrash size={14} />
                  </ActionIcon>
                </Group>
              </Group>
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
          ))}
        </Stack>
      )}
    </Box>
  )
}
