import { useState, useEffect, useCallback } from 'react'
import {
  Stack, Title, Group, Button, Card, Text, SimpleGrid, Modal, TextInput, Textarea,
  ActionIcon, Loader, Center, Box, Anchor,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconPhoto, IconTrash, IconPlus, IconArrowLeft, IconShare } from '@tabler/icons-react'
import ShareDialog from '@/components/ShareDialog'
import * as albumApi from '@/api/albums'
import * as libApi from '@/api/library'
import ConfirmModal from '@/components/ConfirmModal'
import MediaThumbnail from '@/components/MediaThumbnail'
import { mediaDisplayName } from '@/utils/media'
import type { Album, MediaFile } from '@/types'

const initCreate = { opened: false, name: '', description: '', loading: false }
const initDelAlbum = { opened: false, album: null as Album | null, loading: false }

/** 相册页（FR-40）：列相册、建相册、看相册内容、跨目录把媒体加入/移出 */
export default function AlbumsPage() {
  const [albums, setAlbums] = useState<Album[]>([])
  const [loading, setLoading] = useState(false)
  const [selected, setSelected] = useState<Album | null>(null)
  const [create, setCreate] = useState<typeof initCreate>(initCreate)
  const [delAlbum, setDelAlbum] = useState<typeof initDelAlbum>(initDelAlbum)
  // 分享的相册（FR-43）：null 表示分享弹窗关闭
  const [shareAlbum, setShareAlbum] = useState<Album | null>(null)

  const loadAlbums = useCallback(async () => {
    setLoading(true)
    try {
      setAlbums(await albumApi.listAlbums())
    } catch (err) {
      notifications.show({ title: '加载失败', message: err instanceof Error ? err.message : '无法加载相册', color: 'red', autoClose: 3000 })
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
      notifications.show({ title: '创建失败', message: err instanceof Error ? err.message : '创建相册失败', color: 'red', autoClose: 3000 })
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
      <Group justify="space-between">
        <Title order={2}>相册</Title>
        <Button leftSection={<IconPlus size={16} />} color="purple"
          onClick={() => setCreate({ ...initCreate, opened: true })}>
          新建相册
        </Button>
      </Group>

      {loading && albums.length === 0 ? (
        <Center py="xl"><Loader color="purple" /></Center>
      ) : albums.length === 0 ? (
        <Text c="dimmed">还没有相册，点击「新建相册」创建第一个吧。</Text>
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
      notifications.show({ title: '加载失败', message: err instanceof Error ? err.message : '无法加载相册内容', color: 'red', autoClose: 3000 })
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
      notifications.show({ title: '移出失败', message: err instanceof Error ? err.message : '移出媒体失败', color: 'red', autoClose: 3000 })
    }
  }, [album.id, loadItems])

  const handleAdded = useCallback(async () => {
    setPickerOpened(false)
    await loadItems()
  }, [loadItems])

  return (
    <Stack gap="md">
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
        <Center py="xl"><Loader color="purple" /></Center>
      ) : items.length === 0 ? (
        <Text c="dimmed">这个相册还没有媒体，点击「加入媒体」从媒体库添加。</Text>
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
      .catch(err => notifications.show({ title: '加载失败', message: err instanceof Error ? err.message : '无法加载媒体库', color: 'red', autoClose: 3000 }))
      .finally(() => setLoading(false))
  }, [opened])

  const handleAdd = useCallback(async (media: MediaFile) => {
    setAddingID(media.id)
    try {
      await albumApi.addAlbumItem(albumID, media.id)
      onAdded()
    } catch (err) {
      notifications.show({ title: '加入失败', message: err instanceof Error ? err.message : '加入相册失败', color: 'red', autoClose: 3000 })
    } finally {
      setAddingID(null)
    }
  }, [albumID, onAdded])

  const existing = new Set(existingIDs)

  return (
    <Modal opened={opened} onClose={onClose} title="加入媒体" centered size="lg">
      {loading ? (
        <Center py="xl"><Loader color="purple" /></Center>
      ) : candidates.length === 0 ? (
        <Text c="dimmed">媒体库中没有可加入的媒体。</Text>
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
