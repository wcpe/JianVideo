import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { Container, Title, Text, Loader, Center, SimpleGrid, Card, Image, Button, Group, Stack, Modal } from '@mantine/core'
import { IconDownload } from '@tabler/icons-react'
import { getShareInfo, shareRawURL, shareThumbnailURL, shareDownloadURL, shareStreamURL } from '@/api/share'
import { isImageFile, mediaDisplayName } from '@/utils/media'
import type { ShareInfo, MediaFile } from '@/types'

/** 单个被分享媒体的查看：图片直出、视频渐进式在线播放，附原文件下载（FR-43） */
function SharedMedia({ token, media }: { token: string; media: MediaFile }) {
  const isImage = isImageFile(media)
  return (
    <Stack>
      <Title order={4}>{mediaDisplayName(media)}</Title>
      {isImage ? (
        <Image src={shareRawURL(token, media.id)} alt={media.file_name} fit="contain" mah="70vh" />
      ) : (
        // 公开播放走渐进式 stream（非转码/HLS），原生 video 适配常见格式
        <video src={shareStreamURL(token, media.id)} controls style={{ width: '100%', maxHeight: '70vh', background: '#000' }} />
      )}
      <Group justify="center">
        <Button component="a" href={shareDownloadURL(token, media.id)} download variant="light" leftSection={<IconDownload size={16} />}>
          下载原文件
        </Button>
      </Group>
    </Stack>
  )
}

/** 公开分享查看页（FR-43，免登）：按类型展示图片 / 视频 / 相册网格 */
export default function SharePage() {
  const { token = '' } = useParams()
  const [info, setInfo] = useState<ShareInfo | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [selected, setSelected] = useState<MediaFile | null>(null)

  useEffect(() => {
    let active = true
    setLoading(true)
    getShareInfo(token)
      .then(d => { if (active) setInfo(d) })
      .catch(() => { if (active) setError('分享不存在或已过期') })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [token])

  if (loading) return <Center h="100vh"><Loader /></Center>
  if (error || !info) return <Center h="100vh"><Text c="dimmed">{error || '分享不存在或已过期'}</Text></Center>

  return (
    <Container py="xl" size="lg">
      {info.resource_type === 'media' && info.media && <SharedMedia token={token} media={info.media} />}

      {info.resource_type === 'album' && (
        <Stack>
          <Title order={3}>{info.album?.name ?? '相册分享'}</Title>
          {(info.items ?? []).length === 0 ? (
            <Text c="dimmed">该相册暂无可访问的内容</Text>
          ) : (
            <SimpleGrid cols={{ base: 2, sm: 3, md: 4 }}>
              {(info.items ?? []).map(m => (
                <Card key={m.id} padding="xs" withBorder style={{ cursor: 'pointer' }} onClick={() => setSelected(m)}>
                  <Card.Section>
                    <Image src={shareThumbnailURL(token, m.id)} alt={m.file_name} h={140} fit="cover" />
                  </Card.Section>
                  <Text size="xs" mt="xs" truncate>{mediaDisplayName(m)}</Text>
                </Card>
              ))}
            </SimpleGrid>
          )}
          <Modal opened={!!selected} onClose={() => setSelected(null)} size="xl" centered title={selected ? mediaDisplayName(selected) : ''}>
            {selected && <SharedMedia token={token} media={selected} />}
          </Modal>
        </Stack>
      )}
    </Container>
  )
}
