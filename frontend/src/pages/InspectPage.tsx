import { useState, useEffect, useCallback, useRef } from 'react'
import { Stack, Title, Group, Card, Text, Button, Loader, Center, Progress, Badge, Checkbox } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconStethoscope, IconTrash } from '@tabler/icons-react'
import * as healthApi from '@/api/health'
import { batchDeleteMediaFiles } from '@/api/library'
import ConfirmModal from '@/components/ConfirmModal'
import type { HealthIssue, HealthIssueType, HealthScanStatus } from '@/types'

// 轮询周期（毫秒）：巡检进行中时刷新进度
const POLL_INTERVAL_MS = 1500

// 问题类型 → 中文文案与徽标颜色
const ISSUE_META: Record<HealthIssueType, { label: string; color: string }> = {
  broken: { label: '视频损坏', color: 'red' },
  zero_byte: { label: '0 字节文件', color: 'orange' },
  missing: { label: '源文件丢失', color: 'red' },
  no_thumbnail: { label: '缩略图无法生成', color: 'yellow' },
}

// 分组顺序：按严重程度从重到轻展示
const GROUP_ORDER: HealthIssueType[] = ['missing', 'broken', 'zero_byte', 'no_thumbnail']

/** 问题媒体页（FR-73）：触发健康巡检（带进度），按问题类型分组列出问题媒体，可批量删除进回收站 */
export default function InspectPage() {
  const [issues, setIssues] = useState<HealthIssue[]>([])
  const [status, setStatus] = useState<HealthScanStatus | null>(null)
  const [loading, setLoading] = useState(false)
  const [selected, setSelected] = useState<Set<number>>(new Set())
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const timerRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined)

  const scanning = status?.status === 'scanning'

  const loadIssues = useCallback(async () => {
    setLoading(true)
    try {
      setIssues(await healthApi.getHealthIssues())
    } catch (err) {
      notifications.show({ title: '加载失败', message: err instanceof Error ? err.message : '无法加载问题清单', color: 'red', autoClose: 3000 })
    } finally {
      setLoading(false)
    }
  }, [])

  // 拉一次当前状态与问题清单
  useEffect(() => {
    healthApi.getHealthStatus().then(setStatus).catch(() => { /* 状态拉取失败不阻塞页面 */ })
    loadIssues()
  }, [loadIssues])

  // 巡检进行中时轮询状态；完成后停止轮询并刷新问题清单
  useEffect(() => {
    if (!scanning) {
      if (timerRef.current) { clearInterval(timerRef.current); timerRef.current = undefined }
      return
    }
    timerRef.current = setInterval(async () => {
      try {
        const next = await healthApi.getHealthStatus()
        setStatus(next)
        if (next.status === 'completed' || next.status === 'error') {
          await loadIssues()
        }
      } catch {
        // 轮询失败静默重试
      }
    }, POLL_INTERVAL_MS)
    return () => { if (timerRef.current) clearInterval(timerRef.current) }
  }, [scanning, loadIssues])

  const handleScan = useCallback(async () => {
    try {
      const res = await healthApi.startHealthScan()
      if (res.status === 'already_running') {
        notifications.show({ title: '巡检进行中', message: '已有一轮巡检在进行，请稍候。', color: 'blue', autoClose: 3000 })
      }
      setStatus(await healthApi.getHealthStatus())
    } catch (err) {
      notifications.show({ title: '触发失败', message: err instanceof Error ? err.message : '无法触发巡检', color: 'red', autoClose: 3000 })
    }
  }, [])

  const toggleSelect = useCallback((mediaID: number) => {
    setSelected(prev => {
      const next = new Set(prev)
      if (next.has(mediaID)) next.delete(mediaID)
      else next.add(mediaID)
      return next
    })
  }, [])

  const handleDelete = useCallback(async () => {
    setDeleting(true)
    try {
      const ids = Array.from(selected)
      const deleted = await batchDeleteMediaFiles(ids)
      setConfirmOpen(false)
      setSelected(new Set())
      notifications.show({ title: '已删除', message: `已将 ${deleted} 个媒体移入回收站`, color: 'green', autoClose: 3000 })
      await loadIssues()
    } catch (err) {
      notifications.show({ title: '删除失败', message: err instanceof Error ? err.message : '批量删除失败', color: 'red', autoClose: 3000 })
    } finally {
      setDeleting(false)
    }
  }, [selected, loadIssues])

  // 按问题类型分组（去重 media：同一媒体可能命中多类问题，分组内各自展示）
  const grouped = GROUP_ORDER
    .map(type => ({ type, items: issues.filter(i => i.issue_type === type) }))
    .filter(g => g.items.length > 0)

  const percent = status && status.total > 0 ? Math.round((status.checked / status.total) * 100) : 0

  return (
    <Stack gap="md">
      <Group justify="space-between" align="flex-end">
        <Title order={2}>问题媒体</Title>
        <Group gap="sm">
          {selected.size > 0 && (
            <Button color="red" variant="light" leftSection={<IconTrash size={16} />} onClick={() => setConfirmOpen(true)}>
              删除选中（{selected.size}）
            </Button>
          )}
          <Button color="purple" leftSection={<IconStethoscope size={16} />} loading={scanning} onClick={handleScan}>
            {scanning ? '巡检中…' : '开始巡检'}
          </Button>
        </Group>
      </Group>
      <Text size="sm" c="dimmed">巡检会后台扫描所有媒体，找出源文件丢失、0 字节、视频损坏、缩略图无法生成的问题媒体。巡检只读、不会自动删除；可在下方选中后删除进回收站。</Text>

      {scanning && (
        <Card withBorder padding="sm" radius="md">
          <Group justify="space-between" mb={4}>
            <Text size="sm">巡检进行中…</Text>
            <Text size="xs" c="dimmed">{status?.checked ?? 0} / {status?.total ?? 0}</Text>
          </Group>
          <Progress value={percent} size="sm" animated />
        </Card>
      )}

      {loading && issues.length === 0 ? (
        <Center py="xl"><Loader color="purple" /></Center>
      ) : grouped.length === 0 ? (
        <Text c="dimmed">{status?.status === 'completed' ? '太好了，未发现问题媒体。' : '尚未巡检或暂无问题媒体。点击「开始巡检」检查媒体库。'}</Text>
      ) : (
        <Stack gap="lg">
          {grouped.map(group => (
            <Stack key={group.type} gap="xs">
              <Group gap="xs">
                <Badge color={ISSUE_META[group.type].color} variant="light">{ISSUE_META[group.type].label}</Badge>
                <Text size="sm" c="dimmed">{group.items.length} 个</Text>
              </Group>
              <Stack gap={4}>
                {group.items.map(issue => (
                  <Card key={issue.id} withBorder padding="xs" radius="md" className="issue-card">
                    <Group justify="space-between" wrap="nowrap">
                      <Group gap="sm" wrap="nowrap" style={{ minWidth: 0 }}>
                        <Checkbox
                          checked={selected.has(issue.media_id)}
                          onChange={() => toggleSelect(issue.media_id)}
                          aria-label={`选择 ${issue.file_name ?? issue.media_id}`}
                        />
                        <Stack gap={0} style={{ minWidth: 0 }}>
                          <Text size="sm" truncate title={issue.file_path}>
                            {issue.display_name || issue.file_name || `媒体 #${issue.media_id}`}
                          </Text>
                          {issue.detail && <Text size="xs" c="dimmed" truncate title={issue.detail}>{issue.detail}</Text>}
                        </Stack>
                      </Group>
                    </Group>
                  </Card>
                ))}
              </Stack>
            </Stack>
          ))}
        </Stack>
      )}

      <ConfirmModal
        opened={confirmOpen}
        title="删除问题媒体"
        message={`将把选中的 ${selected.size} 个媒体移入回收站（源文件不动，可还原）。确定继续？`}
        confirmLabel="删除"
        cancelLabel="取消"
        confirmColor="red"
        loading={deleting}
        onConfirm={handleDelete}
        onCancel={() => setConfirmOpen(false)}
      />
    </Stack>
  )
}
