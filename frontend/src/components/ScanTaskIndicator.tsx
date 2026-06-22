import { useEffect, useRef, useState } from 'react'
import { ActionIcon, Stack, Group, Text, Badge, Progress, Indicator, Paper } from '@mantine/core'
import { IconRefresh } from '@tabler/icons-react'

import { getScanTasks } from '@/api/library'
import type { ScanTask } from '@/types'

// 轮询周期（毫秒）：页眉常驻、有进行中任务时实时刷新进度
const POLL_INTERVAL_MS = 2000

// 任务状态 → 徽标颜色与中文文案
const STATUS_META: Record<string, { color: string; label: string }> = {
  pending: { color: 'gray', label: '排队中' },
  running: { color: 'blue', label: '进行中' },
  completed: { color: 'green', label: '已完成' },
  error: { color: 'red', label: '失败' },
}

/** 是否为活动任务（排队或进行中），用于判断指示器是否常驻展示 */
function isActive(t: ScanTask): boolean {
  return t.status === 'pending' || t.status === 'running'
}

/**
 * 页眉扫描任务指示器（FR-29）。
 * 有进行中/排队任务时常驻展示，点开下拉面板列出任务列表与各自进度。
 * 用原生定位下拉而非 Mantine Popover，避免 jsdom 下 floating-ui 依赖 getComputedStyle（同 ContinueWatching 规避）。
 */
export default function ScanTaskIndicator() {
  const [tasks, setTasks] = useState<ScanTask[]>([])
  const [opened, setOpened] = useState(false)
  const timerRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined)

  useEffect(() => {
    let cancelled = false
    const poll = async () => {
      try {
        const res = await getScanTasks()
        if (!cancelled) setTasks(res.tasks)
      } catch {
        // 轮询失败静默忽略，下个周期重试，不打断页面
      }
    }
    poll()
    timerRef.current = setInterval(poll, POLL_INTERVAL_MS)
    return () => {
      cancelled = true
      if (timerRef.current) clearInterval(timerRef.current)
    }
  }, [])

  const activeCount = tasks.filter(isActive).length

  // 无活动任务时不展示指示器（页眉保持简洁）
  if (activeCount === 0) return null

  return (
    <div style={{ position: 'relative' }}>
      <Indicator label={activeCount} size={16} color="blue" offset={4}>
        <ActionIcon
          variant="subtle"
          color="gray"
          onClick={() => setOpened((o) => !o)}
          title="扫描任务"
          aria-label="扫描任务"
        >
          <IconRefresh size={18} />
        </ActionIcon>
      </Indicator>

      {opened && (
        <Paper
          shadow="md"
          p="sm"
          withBorder
          style={{
            position: 'absolute',
            top: '100%',
            right: 0,
            zIndex: 200,
            width: 320,
            maxHeight: 360,
            overflowY: 'auto',
            marginTop: 4,
          }}
        >
          <Text fw={600} size="sm" mb="xs">扫描任务</Text>
          <Stack gap="xs">
            {tasks.map((t) => (
              <TaskRow key={t.id} task={t} />
            ))}
          </Stack>
        </Paper>
      )}
    </div>
  )
}

// 单条任务行：库标识 + 状态徽标 + 进度
function TaskRow({ task }: { task: ScanTask }) {
  const meta = STATUS_META[task.status] ?? { color: 'gray', label: task.status }
  const percent = task.total_files > 0 ? Math.round((task.scanned_files / task.total_files) * 100) : 0

  return (
    <Stack gap={4} p="xs" style={{ borderRadius: 'var(--mantine-radius-sm)', border: '1px solid var(--mantine-color-default-border)' }}>
      <Group justify="space-between" wrap="nowrap">
        <Text size="sm" truncate>库 {task.library_id}</Text>
        <Badge size="sm" color={meta.color} variant="light">{meta.label}</Badge>
      </Group>
      {task.status === 'running' && (
        <>
          <Progress value={percent} size="sm" animated />
          <Text size="xs" c="dimmed">{task.scanned_files} / {task.total_files}</Text>
        </>
      )}
      {task.status === 'completed' && (
        <Text size="xs" c="dimmed">已索引 {task.scanned_files} 个文件</Text>
      )}
      {task.status === 'error' && task.error && (
        <Text size="xs" c="red" truncate>{task.error}</Text>
      )}
    </Stack>
  )
}
