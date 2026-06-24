import { useState, useEffect, useCallback } from 'react'
import {
  Stack, Title, Group, Button, Card, Text, SimpleGrid, Modal, TextInput,
  NumberInput, Select, ActionIcon, Loader, Center, Badge, Divider,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconMovie, IconTrash, IconPlus, IconPencil } from '@tabler/icons-react'
import ConfirmModal from '@/components/ConfirmModal'
import EmptyState from '@/components/EmptyState'
import * as transcodeApi from '@/api/transcode'
import type { PresetInput } from '@/api/transcode'
import type { TranscodePreset, TranscodeTask, TranscodeCodec } from '@/types'

// 目标编码下拉选项（与后端可参数化输出编码一致）
const CODEC_OPTIONS: { value: TranscodeCodec; label: string }[] = [
  { value: 'h264', label: 'H.264' },
  { value: 'h265', label: 'H.265 / HEVC' },
  { value: 'av1', label: 'AV1' },
  { value: 'vp9', label: 'VP9' },
]

// 任务状态 → 徽标颜色与中文文案
const TASK_STATUS_META: Record<string, { color: string; label: string }> = {
  pending: { color: 'gray', label: '排队中' },
  running: { color: 'blue', label: '进行中' },
  completed: { color: 'green', label: '已完成' },
  error: { color: 'red', label: '失败' },
}

// 任务列表轮询周期（毫秒）
const TASK_POLL_INTERVAL_MS = 3000

type EditState = { opened: boolean; editing: TranscodePreset | null; name: string; codec: TranscodeCodec; width: number; height: number; loading: boolean }

const initEdit: EditState = { opened: false, editing: null, name: '', codec: 'h264', width: 0, height: 0, loading: false }
const initDel = { opened: false, preset: null as TranscodePreset | null, loading: false }

/** 转码预设管理页（FR-77）：列/建/改/删预设，并展示预生成队列任务状态 */
export default function TranscodePage() {
  const [presets, setPresets] = useState<TranscodePreset[]>([])
  const [tasks, setTasks] = useState<TranscodeTask[]>([])
  const [loading, setLoading] = useState(false)
  const [edit, setEdit] = useState<EditState>(initEdit)
  const [del, setDel] = useState<typeof initDel>(initDel)

  const loadPresets = useCallback(async () => {
    setLoading(true)
    try {
      setPresets(await transcodeApi.listPresets())
    } catch (err) {
      notifications.show({ title: '加载失败', message: err instanceof Error ? err.message : '无法加载预设', color: 'red', autoClose: 3000 })
    } finally {
      setLoading(false)
    }
  }, [])

  // 任务列表轮询：进入页面即拉取并定期刷新，展示预生成进度
  useEffect(() => {
    let cancelled = false
    const poll = async () => {
      try {
        const list = await transcodeApi.listTranscodeTasks()
        if (!cancelled) setTasks(list)
      } catch {
        // 轮询失败静默忽略，下个周期重试
      }
    }
    poll()
    const timer = setInterval(poll, TASK_POLL_INTERVAL_MS)
    return () => { cancelled = true; clearInterval(timer) }
  }, [])

  useEffect(() => { loadPresets() }, [loadPresets])

  const openCreate = () => setEdit({ ...initEdit, opened: true })
  const openEdit = (p: TranscodePreset) => setEdit({ opened: true, editing: p, name: p.name, codec: p.codec, width: p.width, height: p.height, loading: false })

  const handleSubmit = useCallback(async () => {
    const name = edit.name.trim()
    if (!name) return
    setEdit(e => ({ ...e, loading: true }))
    const input: PresetInput = { name, codec: edit.codec, width: edit.width, height: edit.height }
    try {
      if (edit.editing) {
        await transcodeApi.updatePreset(edit.editing.id, input)
      } else {
        await transcodeApi.createPreset(input)
      }
      setEdit(initEdit)
      await loadPresets()
    } catch (err) {
      notifications.show({ title: '保存失败', message: err instanceof Error ? err.message : '保存预设失败', color: 'red', autoClose: 3000 })
      setEdit(e => ({ ...e, loading: false }))
    }
  }, [edit, loadPresets])

  const handleDelete = useCallback(async () => {
    if (!del.preset) return
    setDel(d => ({ ...d, loading: true }))
    try {
      await transcodeApi.deletePreset(del.preset.id)
      setDel(initDel)
      await loadPresets()
    } catch {
      setDel(initDel)
    }
  }, [del.preset, loadPresets])

  // 分辨率展示：0 表示沿用源分辨率
  const dimLabel = (w: number, h: number) => (w > 0 && h > 0 ? `${w} × ${h}` : '源分辨率')

  return (
    <Stack gap="md">
      <Group justify="space-between">
        <Title order={2}>转码预设</Title>
        <Button leftSection={<IconPlus size={16} />} color="purple" onClick={openCreate}>
          新建预设
        </Button>
      </Group>

      {loading && presets.length === 0 ? (
        <Center py="xl"><Loader color="purple" /></Center>
      ) : presets.length === 0 ? (
        <EmptyState
          icon={<IconMovie size={72} stroke={1.2} style={{ color: 'var(--mantine-color-dimmed)', opacity: 0.7 }} />}
          title="还没有转码预设"
          description="创建第一个预设（目标编码 + 分辨率）后，即可在播放页用「加入预生成」预热首播切片，消除首次播放的冷启动等待。"
          action={{ label: '新建预设', leftIcon: <IconPlus size={16} />, onClick: openCreate }}
        />
      ) : (
        <SimpleGrid cols={{ base: 1, sm: 2, md: 3 }} spacing="md">
          {presets.map(p => (
            <Card key={p.id} withBorder padding="md" radius="md">
              <Group justify="space-between" mb="xs" wrap="nowrap">
                <Group gap={8} wrap="nowrap" style={{ minWidth: 0 }}>
                  <IconMovie size={20} style={{ color: 'var(--mantine-color-purple-4)', flexShrink: 0 }} />
                  <Text fw={600} truncate>{p.name}</Text>
                </Group>
                <Group gap={4} wrap="nowrap">
                  <ActionIcon variant="subtle" color="gray" aria-label="编辑预设" onClick={() => openEdit(p)}>
                    <IconPencil size={16} />
                  </ActionIcon>
                  <ActionIcon variant="subtle" color="red" aria-label="删除预设"
                    onClick={() => setDel({ opened: true, preset: p, loading: false })}>
                    <IconTrash size={16} />
                  </ActionIcon>
                </Group>
              </Group>
              <Group gap="xs">
                <Badge size="sm" variant="light" color="blue">{p.codec.toUpperCase()}</Badge>
                <Text size="sm" c="dimmed">{dimLabel(p.width, p.height)}</Text>
              </Group>
            </Card>
          ))}
        </SimpleGrid>
      )}

      <Divider my="sm" />

      {/* 预生成队列任务列表（FR-77）：复用扫描任务展示范式 */}
      <Title order={3}>预生成队列</Title>
      {tasks.length === 0 ? (
        <EmptyState
          title="暂无预生成任务"
          description="在播放页选一个预设「加入预生成」，任务会在这里排队展示进度，预热完成后首播无需等待。"
        />
      ) : (
        <Stack gap="xs">
          {tasks.map(t => {
            const meta = TASK_STATUS_META[t.status] ?? { color: 'gray', label: t.status }
            return (
              <Card key={t.id} withBorder padding="sm" radius="md">
                <Group justify="space-between" wrap="nowrap">
                  <Text size="sm" truncate>媒体 #{t.media_id} · {t.codec.toUpperCase()}</Text>
                  <Badge size="sm" color={meta.color} variant="light">{meta.label}</Badge>
                </Group>
                {t.status === 'error' && t.error && (
                  <Text size="xs" c="red" mt={4} truncate>{t.error}</Text>
                )}
              </Card>
            )
          })}
        </Stack>
      )}

      {/* 建/改预设弹窗 */}
      <Modal opened={edit.opened} onClose={() => setEdit(initEdit)} title={edit.editing ? '编辑预设' : '新建预设'} centered size="sm">
        <Stack gap="md">
          <TextInput label="预设名称" value={edit.name} required
            onChange={(e) => setEdit(s => ({ ...s, name: e.target.value }))} />
          <Select label="目标编码" data={CODEC_OPTIONS} value={edit.codec} allowDeselect={false}
            onChange={(v) => v && setEdit(s => ({ ...s, codec: v as TranscodeCodec }))} />
          <Group grow>
            <NumberInput label="目标宽度（0=源宽）" value={edit.width} min={0} allowNegative={false}
              onChange={(v) => setEdit(s => ({ ...s, width: typeof v === 'number' ? v : 0 }))} />
            <NumberInput label="目标高度（0=源高）" value={edit.height} min={0} allowNegative={false}
              onChange={(v) => setEdit(s => ({ ...s, height: typeof v === 'number' ? v : 0 }))} />
          </Group>
          <Group justify="flex-end">
            <Button variant="subtle" color="gray" onClick={() => setEdit(initEdit)} disabled={edit.loading}>取消</Button>
            <Button color="purple" onClick={handleSubmit} loading={edit.loading} disabled={!edit.name.trim()}>保存</Button>
          </Group>
        </Stack>
      </Modal>

      <ConfirmModal opened={del.opened} title="删除预设"
        message={`确定要删除预设 "${del.preset?.name}" 吗？已预生成的切片缓存不受影响。`}
        confirmLabel="删除" cancelLabel="取消" confirmColor="red" loading={del.loading}
        onConfirm={handleDelete} onCancel={() => setDel(initDel)} />
    </Stack>
  )
}
