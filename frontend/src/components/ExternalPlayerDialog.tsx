import { useState } from 'react'
import { Modal, Button, Select, Group, TextInput, Stack, Text } from '@mantine/core'
import { useClipboard } from '@mantine/hooks'
import { IconExternalLink, IconCopy, IconCheck, IconPlayerPlay } from '@tabler/icons-react'
import { notifications } from '@mantine/notifications'
import { createShare } from '@/api/share'
import { buildExternalStreamURL } from '@/utils/external-player'

interface ExternalPlayerDialogProps {
  opened: boolean
  onClose: () => void
  mediaID: number
  // 续播位置（秒）：>0 时生成地址附 #t= 续播点（FR-44）
  lastPosition?: number
}

// 有效期选项（小时）：0 表示永不过期。与分享对话框（FR-43）一致。
const EXPIRY_OPTIONS = [
  { value: '0', label: '永不过期' },
  { value: '24', label: '1 天' },
  { value: '168', label: '7 天' },
  { value: '720', label: '30 天' },
]

/**
 * 外部播放器深链弹窗（FR-79）：选有效期 → 复用 FR-43 createShare 生成免登 token →
 * 拼出 VLC / IINA 可消费的网络串流地址（原文件直链 + Range），可复制或在浏览器打开。
 * 注意：扫码（二维码）本期不做（无 QR 库），留待 FR-78 引入 QR 依赖时合并。
 */
export default function ExternalPlayerDialog({ opened, onClose, mediaID, lastPosition }: ExternalPlayerDialogProps) {
  const [expiry, setExpiry] = useState('0')
  const [url, setUrl] = useState('')
  const [loading, setLoading] = useState(false)
  const clipboard = useClipboard({ timeout: 2000 })

  async function handleGenerate() {
    setLoading(true)
    try {
      const share = await createShare('media', mediaID, Number(expiry))
      setUrl(buildExternalStreamURL(window.location.origin, share.token, mediaID, lastPosition))
    } catch (err) {
      notifications.show({ color: 'red', message: err instanceof Error ? err.message : '生成地址失败' })
    } finally {
      setLoading(false)
    }
  }

  function handleClose() {
    setUrl('')
    setExpiry('0')
    onClose()
  }

  return (
    <Modal opened={opened} onClose={handleClose} title="用外部播放器打开" centered>
      <Stack>
        <Text size="sm" c="dimmed">
          生成一个免登的网络串流地址，可在 VLC / IINA 等外部播放器中「打开网络串流」并粘贴此地址播放。
          任何持有该地址者均可只读访问此媒体原文件，请按需选择有效期。
        </Text>
        <Select label="有效期" data={EXPIRY_OPTIONS} value={expiry} onChange={v => setExpiry(v ?? '0')} allowDeselect={false} />
        {!url ? (
          <Button onClick={handleGenerate} loading={loading} leftSection={<IconExternalLink size={16} />}>
            生成地址
          </Button>
        ) : (
          <>
            <Group gap="xs" wrap="nowrap">
              <TextInput value={url} readOnly style={{ flex: 1 }} aria-label="流地址" />
              <Button
                color={clipboard.copied ? 'teal' : 'blue'}
                onClick={() => clipboard.copy(url)}
                leftSection={clipboard.copied ? <IconCheck size={16} /> : <IconCopy size={16} />}
              >
                {clipboard.copied ? '已复制' : '复制'}
              </Button>
            </Group>
            <Button
              variant="light"
              leftSection={<IconPlayerPlay size={16} />}
              onClick={() => window.open(url, '_blank', 'noopener,noreferrer')}
            >
              在浏览器打开
            </Button>
            <Text size="xs" c="dimmed">
              在 VLC 中选「文件 → 打开网络串流」，在 IINA 中选「打开 URL」，粘贴上面的地址即可播放。
              带续播点的地址会在支持 media fragment 的播放器中从上次位置起播。
            </Text>
          </>
        )}
      </Stack>
    </Modal>
  )
}
