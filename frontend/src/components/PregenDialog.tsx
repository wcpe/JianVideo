import { useState, useEffect } from 'react'
import { Modal, Button, Select, Group, Stack, Text, Anchor } from '@mantine/core'
import { useNavigate } from 'react-router-dom'
import { notifications } from '@mantine/notifications'
import { listPresets, enqueueTranscodeTask } from '@/api/transcode'
import type { TranscodePreset } from '@/types'

interface PregenDialogProps {
  opened: boolean
  onClose: () => void
  mediaID: number
}

// 预设分辨率展示：0 表示沿用源分辨率
function presetLabel(p: TranscodePreset): string {
  const dim = p.width > 0 && p.height > 0 ? `${p.width}×${p.height}` : '源分辨率'
  return `${p.name}（${p.codec.toUpperCase()} · ${dim}）`
}

/**
 * 加入预生成队列弹窗（FR-77）：选一个转码预设，把当前媒体加入预生成队列，
 * 后台按预设编码预转码切片预热首播。无预设时引导去转码预设页创建。
 */
export default function PregenDialog({ opened, onClose, mediaID }: PregenDialogProps) {
  const navigate = useNavigate()
  const [presets, setPresets] = useState<TranscodePreset[]>([])
  const [presetID, setPresetID] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  // 打开时拉取可用预设
  useEffect(() => {
    if (!opened) return
    setLoading(true)
    listPresets()
      .then((items) => {
        setPresets(items)
        setPresetID(items.length > 0 ? String(items[0].id) : null)
      })
      .catch((err) => notifications.show({ color: 'red', message: err instanceof Error ? err.message : '加载预设失败' }))
      .finally(() => setLoading(false))
  }, [opened])

  async function handleEnqueue() {
    if (!presetID) return
    setSubmitting(true)
    try {
      await enqueueTranscodeTask(mediaID, Number(presetID))
      notifications.show({ color: 'green', message: '已加入预生成队列，后台将预热首播切片' })
      onClose()
    } catch (err) {
      notifications.show({ color: 'red', message: err instanceof Error ? err.message : '加入预生成队列失败' })
    } finally {
      setSubmitting(false)
    }
  }

  const options = presets.map((p) => ({ value: String(p.id), label: presetLabel(p) }))

  return (
    <Modal opened={opened} onClose={onClose} title="加入预生成队列" centered size="sm">
      <Stack gap="md">
        {loading ? (
          <Text c="dimmed">加载预设中…</Text>
        ) : presets.length === 0 ? (
          <Text c="dimmed">
            还没有转码预设。请先到
            <Anchor onClick={() => { onClose(); navigate('/transcode') }}> 转码预设页 </Anchor>
            创建预设。
          </Text>
        ) : (
          <Select
            label="选择预设"
            data={options}
            value={presetID}
            allowDeselect={false}
            onChange={setPresetID}
          />
        )}
        <Group justify="flex-end">
          <Button variant="subtle" color="gray" onClick={onClose} disabled={submitting}>取消</Button>
          <Button color="purple" onClick={handleEnqueue} loading={submitting} disabled={!presetID}>加入队列</Button>
        </Group>
      </Stack>
    </Modal>
  )
}
