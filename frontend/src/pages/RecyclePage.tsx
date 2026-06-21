import { useState, useEffect, useCallback } from 'react'
import { Stack, Title, Group, Card, Text, SimpleGrid, Button, Loader, Center } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconArrowBackUp } from '@tabler/icons-react'
import * as libApi from '@/api/library'
import MediaThumbnail from '@/components/MediaThumbnail'
import type { MediaFile } from '@/types'

/** 回收站页（FR-25）：展示被软删的媒体并支持一键还原 */
export default function RecyclePage() {
  const [items, setItems] = useState<MediaFile[]>([])
  const [loading, setLoading] = useState(false)
  const [restoringID, setRestoringID] = useState<number | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setItems(await libApi.getRecycleMediaFiles())
    } catch (err) {
      notifications.show({ title: '加载失败', message: err instanceof Error ? err.message : '无法加载回收站', color: 'red', autoClose: 3000 })
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  const handleRestore = useCallback(async (media: MediaFile) => {
    setRestoringID(media.id)
    try {
      await libApi.restoreMediaFile(media.id)
      // 还原成功后从回收站列表移除
      setItems(prev => prev.filter(m => m.id !== media.id))
    } catch (err) {
      notifications.show({ title: '还原失败', message: err instanceof Error ? err.message : '还原媒体失败', color: 'red', autoClose: 3000 })
    } finally {
      setRestoringID(null)
    }
  }, [])

  return (
    <Stack gap="md">
      <Title order={2}>回收站</Title>
      <Text size="sm" c="dimmed">这里的媒体已从媒体库移除但源文件仍在磁盘，可随时还原。</Text>

      {loading && items.length === 0 ? (
        <Center py="xl"><Loader color="purple" /></Center>
      ) : items.length === 0 ? (
        <Text c="dimmed">回收站是空的。</Text>
      ) : (
        <SimpleGrid cols={{ base: 1, sm: 2, md: 3, lg: 4 }} spacing="md">
          {items.map(media => (
            <Card key={media.id} withBorder padding="xs" radius="md" className="mantine-Card-root">
              <Card.Section>
                <MediaThumbnail mediaID={media.id} fileName={media.file_name} />
              </Card.Section>
              <Group justify="space-between" mt="xs" wrap="nowrap">
                <Text size="sm" truncate title={media.file_name}>{media.file_name}</Text>
                <Button size="compact-xs" variant="light" color="purple"
                  leftSection={<IconArrowBackUp size={14} />}
                  loading={restoringID === media.id}
                  onClick={() => handleRestore(media)}>
                  还原
                </Button>
              </Group>
            </Card>
          ))}
        </SimpleGrid>
      )}
    </Stack>
  )
}
