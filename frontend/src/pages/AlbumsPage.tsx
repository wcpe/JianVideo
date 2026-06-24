import { useState, useEffect, useCallback } from 'react'
import {
  Stack, Title, Group, Button, Card, Text, SimpleGrid, Modal, TextInput, Textarea,
  ActionIcon, Box, Anchor, Skeleton,
} from '@mantine/core'
import { IconPhoto, IconTrash, IconPlus, IconArrowLeft, IconShare, IconPhotoOff, IconAlertTriangle } from '@tabler/icons-react'
import ShareDialog from '@/components/ShareDialog'
import * as albumApi from '@/api/albums'
import * as libApi from '@/api/library'
import ConfirmModal from '@/components/ConfirmModal'
import EmptyState from '@/components/EmptyState'
import MediaThumbnail from '@/components/MediaThumbnail'
import PageBreadcrumbs from '@/components/PageBreadcrumbs'
import { notifyError } from '@/utils/notify'
import { mediaDisplayName } from '@/utils/media'
import type { Album, MediaFile } from '@/types'

const initCreate = { opened: false, name: '', description: '', loading: false }
const initDelAlbum = { opened: false, album: null as Album | null, loading: false }

/** 相册页（FR-40）：列相册、建相册、看相册内容、跨目录把媒体加入/移出 */
export default function AlbumsPage() {
  const [albums, setAlbums] = useState<Album[]>([])
  const [loading, setLoading] = useState(false)
  // 列表加载失败标记（FR-98）：非空即渲染「插画 + 重试」错误态
  const [loadError, setLoadError] = useState(false)
  const [selected, setSelected] = useState<Album | null>(null)
  const [create, setCreate] = useState<typeof initCreate>(initCreate)
  const [delAlbum, setDelAlbum] = useState<typeof initDelAlbum>(initDelAlbum)
  // 分享的相册（FR-43）：null 表示分享弹窗关闭
  const [shareAlbum, setShareAlbum] = useState<Album | null>(null)

  const loadAlbums = useCallback(async () => {
    setLoading(true)
    setLoadError(false)
    try {
      setAlbums(await albumApi.listAlbums())
    } catch (err) {
      setLoadError(true)
      notifyError(err instanceof Error ? err.message : '无法加载相册', '加载失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { loadAlbums() }, [loadAlbums])

  const handleCreate = useCallback(async () => {
    const name = create.name.trim()
    if (!name) return
    setCreate(c => ({ ...c, loading: true }))
    try {
      await albumApi.createAlbum(name, create.description)
      setCreate(initCreate)
      await loadAlbums()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : '创建相册失败', '创建失败')
      setCreate(c => ({ ...c, loading: false }))
    }
  }, [create.name, create.description, loadAlbums])

  const handleDelAlbum = useCallback(async () => {
    if (!delAlbum.album) return
    setDelAlbum(d => ({ ...d, loading: true }))
    try {
      await albumApi.deleteAlbum(delAlbum.album.id)
      setDelAlbum(initDelAlbum)
      await loadAlbums()
    } catch {
      setDelAlbum(initDelAlbum)
    }
  }, [delAlbum.album, loadAlbums])

  // 进入相册详情视图
  if (selected) {
    return (
      <AlbumDetail
        album={selected}
        onBack={() => { setSelected(null); loadAlbums() }}
      />
    )
  }

  return (
    <Stack gap="md">
      {/* 页面级路由面包屑（FR-95）：首页 › 相册 */}
      <PageBreadcrumbs items={[{ label: '首页', to: '/' }, { label: '相册' }]} />
      <Group justify="space-between">
        <Title order={2}>相册</Title>
        <Button leftSection={<IconPlus size={16} />} color="purple"
          onClick={() => setCreate({ ...initCreate, opened: true })}>
          新建相册
        </Button>
      </Group>

      {loading && albums.length === 0 ? (
        // 首屏加载骨架屏（FR-98）：以卡片占位替代空白闪现
        <SimpleGrid cols={{ base: 1, sm: 2, md: 3, lg: 4 }} spacing="md">
          {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} height={92} radius="md" />)}
        </SimpleGrid>
      ) : loadError ? (
        // 加载失败态（FR-98）：插画 + 重试
        <EmptyState
          icon={<IconAlertTriangle size={72} stroke={1.2} style={{ color: 'var(--mantine-color-red-5)', opacity: 0.8 }} />}
          title="加载相册失败"
          description="可能是网络或服务暂时不可用，请稍后重试。"
          action={{ label: '重试', onClick: () => loadAlbums() }}
        />
      ) : albums.length === 0 ? (
        // 空相册列表（FR-98）：引导新建第一个相册
        <EmptyState
          icon={<IconPhotoOff size={72} stroke={1.2} style={{ color: 'var(--mantine-color-dimmed)', opacity: 0.7 }} />}
          title="还没有相册"
          description="相册可以把跨目录的照片和视频归到一起，点击下方按钮创建第一个。"
          action={{ label: '新建第一个相册', leftIcon: <IconPlus size={16} />, onClick: () => setCreate({ ...initCreate, opened: true }) }}
        />
      ) : (
        <SimpleGrid cols={{ base: 1, sm: 2, md: 3, lg: 4 }} spacing="md">
          {albums.map(album => (
            <Card key={album.id} withBorder padding="md" radius="md"
              style={{ cursor: 'pointer' }}>
              <Group justify="space-between" mb="xs" wrap="nowrap">
                <Group gap={8} wrap="nowrap" style={{ minWidth: 0 }} onClick={() => setSelected(album)}>
                  <IconPhoto size={20} style={{ color: 'var(--mantine-color-purple-4)', flexShrink: 0 }} />
                  <Text fw={600} truncate>{album.name}</Text>
                </Group>
                <Group gap={4} wrap="nowrap">
                  <ActionIcon variant="subtle" color="blue" aria-label="分享相册"
                    onClick={() => setShareAlbum(album)}>
                    <IconShare size={16} />
                  </ActionIcon>
                  <ActionIcon variant="subtle" color="red" aria-label="删除相册"
                    onClick={() => setDelAlbum({ opened: true, album, loading: false })}>
                    <IconTrash size={16} />
                  </ActionIcon>
                </Group>
              </Group>
              <Box onClick={() => setSelected(album)}>
                {album.description && <Text size="sm" c="dimmed" truncate mb={4}>{album.description}</Text>}
                <Text size="xs" c="dimmed">{album.item_count ?? 0} 个媒体</Text>
              </Box>
            </Card>
          ))}
        </SimpleGrid>
      )}

      <Modal opened={create.opened} onClose={() => setCreate(initCreate)} title="新建相册" centered size="sm">
        <Stack gap="md">
          <TextInput label="相册名称" value={create.name} required
            onChange={(e) => setCreate(c => ({ ...c, name: e.target.value }))}
            onKeyDown={(e) => e.key === 'Enter' && handleCreate()} />
          <Textarea label="描述（可选）" value={create.description} autosize minRows={2}
            onChange={(e) => setCreate(c => ({ ...c, description: e.target.value }))} />
          <Group justify="flex-end">
            <Button variant="subtle" color="gray" onClick={() => setCreate(initCreate)} disabled={create.loading}>取消</Button>
            <Button color="purple" onClick={handleCreate} loading={create.loading} disabled={!create.name.trim()}>创建</Button>
          </Group>
        </Stack>
      </Modal>

      <ConfirmModal opened={delAlbum.opened} title="删除相册"
        message={`确定要删除相册 "${delAlbum.album?.name}" 吗？仅移除相册，源文件与媒体记录不受影响。`}
        confirmLabel="删除" cancelLabel="取消" confirmColor="red" loading={delAlbum.loading}
        onConfirm={handleDelAlbum} onCancel={() => setDelAlbum(initDelAlbum)} />

      {/* 分享相册弹窗（FR-43） */}
      <ShareDialog
        opened={!!shareAlbum}
        onClose={() => setShareAlbum(null)}
        resourceType="album"
        resourceID={shareAlbum?.id ?? 0}
        title={shareAlbum ? `分享相册「${shareAlbum.name}」` : '分享相册'}
      />
    </Stack>
  )
}

/** 相册详情：展示成员、加入/移出媒体 */
function AlbumDetail({ album, onBack }: { album: Album; onBack: () => void }) {
  const [items, setItems] = useState<MediaFile[]>([])
  const [loading, setLoading] = useState(false)
  const [pickerOpened, setPickerOpened] = useState(false)

  const loadItems = useCallback(async () => {
    setLoading(true)
    try {
      setItems(await albumApi.listAlbumItems(album.id))
    } catch (err) {
      notifyError(err instanceof Error ? err.message : '无法加载相册内容', '加载失败')
    } finally {
      setLoading(false)
    }
  }, [album.id])

  useEffect(() => { loadItems() }, [loadItems])

  const handleRemove = useCallback(async (media: MediaFile) => {
    try {
      await albumApi.removeAlbumItem(album.id, media.id)
      await loadItems()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : '移出媒体失败', '移出失败')
    }
  }, [album.id, loadItems])

  const handleAdded = useCallback(async () => {
    setPickerOpened(false)
    await loadItems()
  }, [loadItems])

  return (
    <Stack gap="md">
      {/* 页面级路由面包屑（FR-95）：首页 › 相册（返回列表） › 当前相册 */}
      <PageBreadcrumbs
        items={[
          { label: '首页', to: '/' },
          { label: '相册', onClick: onBack },
          { label: album.name },
        ]}
      />
      <Group justify="space-between">
        <Group gap="xs">
          <ActionIcon variant="subtle" color="gray" aria-label="返回" onClick={onBack}>
            <IconArrowLeft size={18} />
          </ActionIcon>
          <Title order={2}>{album.name}</Title>
        </Group>
        <Button leftSection={<IconPlus size={16} />} color="purple" onClick={() => setPickerOpened(true)}>
          加入媒体
        </Button>
      </Group>
      {album.description && <Text c="dimmed">{album.description}</Text>}

      {loading && items.length === 0 ? (
        // 首屏加载骨架屏（FR-98）
        <SimpleGrid cols={{ base: 1, sm: 2, md: 3, lg: 4 }} spacing="md">
          {Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} height={180} radius="md" />)}
        </SimpleGrid>
      ) : items.length === 0 ? (
        // 空相册详情（FR-98）：引导加入媒体
        <EmptyState
          icon={<IconPhotoOff size={72} stroke={1.2} style={{ color: 'var(--mantine-color-dimmed)', opacity: 0.7 }} />}
          title="这个相册还没有媒体"
          description="从媒体库挑选照片或视频加入相册，集中收藏与分享。"
          action={{ label: '从媒体库加入', leftIcon: <IconPlus size={16} />, onClick: () => setPickerOpened(true) }}
        />
      ) : (
        <SimpleGrid cols={{ base: 1, sm: 2, md: 3, lg: 4 }} spacing="md">
          {items.map(media => (
            <Card key={media.id} withBorder padding="xs" radius="md" className="mantine-Card-root">
              <Card.Section>
                <MediaThumbnail mediaID={media.id} fileName={media.file_name} />
              </Card.Section>
              <Group justify="space-between" mt="xs" wrap="nowrap">
                <Text size="sm" truncate title={mediaDisplayName(media)}>{mediaDisplayName(media)}</Text>
                <Button size="compact-xs" variant="subtle" color="red" onClick={() => handleRemove(media)}>
                  移出
                </Button>
              </Group>
            </Card>
          ))}
        </SimpleGrid>
      )}

      <MediaPickerModal
        albumID={album.id}
        opened={pickerOpened}
        existingIDs={items.map(i => i.id)}
        onClose={() => setPickerOpened(false)}
        onAdded={handleAdded}
      />
    </Stack>
  )
}

/** 媒体选择弹窗：从媒体库挑选媒体加入当前相册 */
function MediaPickerModal({
  albumID, opened, existingIDs, onClose, onAdded,
}: {
  albumID: number
  opened: boolean
  existingIDs: number[]
  onClose: () => void
  onAdded: () => void
}) {
  const [candidates, setCandidates] = useState<MediaFile[]>([])
  const [loading, setLoading] = useState(false)
  const [addingID, setAddingID] = useState<number | null>(null)

  useEffect(() => {
    if (!opened) return
    setLoading(true)
    libApi.getMediaFiles({ page: 1, page_size: 50 })
      .then(res => setCandidates(res.items))
      .catch(err => notifyError(err instanceof Error ? err.message : '无法加载媒体库', '加载失败'))
      .finally(() => setLoading(false))
  }, [opened])

  const handleAdd = useCallback(async (media: MediaFile) => {
    setAddingID(media.id)
    try {
      await albumApi.addAlbumItem(albumID, media.id)
      onAdded()
    } catch (err) {
      notifyError(err instanceof Error ? err.message : '加入相册失败', '加入失败')
    } finally {
      setAddingID(null)
    }
  }, [albumID, onAdded])

  const existing = new Set(existingIDs)

  return (
    <Modal opened={opened} onClose={onClose} title="加入媒体" centered size="lg">
      {loading ? (
        // 加载骨架屏（FR-98）：以行占位替代空白
        <Stack gap={6}>
          {Array.from({ length: 5 }).map((_, i) => <Skeleton key={i} height={28} radius="sm" />)}
        </Stack>
      ) : candidates.length === 0 ? (
        <Text c="dimmed" ta="center" py="md">媒体库中没有可加入的媒体。</Text>
      ) : (
        <Stack gap={4} mah={420} style={{ overflowY: 'auto' }}>
          {candidates.map(media => {
            const already = existing.has(media.id)
            return (
              <Group key={media.id} justify="space-between" wrap="nowrap"
                px="xs" py={6} style={{ borderRadius: 'var(--mantine-radius-sm)' }}>
                {already ? (
                  <Text size="sm" truncate style={{ minWidth: 0 }} c="dimmed">{mediaDisplayName(media)}</Text>
                ) : (
                  <Anchor size="sm" truncate style={{ minWidth: 0 }} c="inherit"
                    onClick={() => handleAdd(media)}>{mediaDisplayName(media)}</Anchor>
                )}
                {already
                  ? <Text size="xs" c="dimmed" style={{ flexShrink: 0 }}>已在相册</Text>
                  : <Button size="compact-xs" variant="light" color="purple" loading={addingID === media.id}
                      onClick={() => handleAdd(media)}>加入</Button>}
              </Group>
            )
          })}
        </Stack>
      )}
    </Modal>
  )
}
