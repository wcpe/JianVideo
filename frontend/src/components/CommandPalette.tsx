import { useEffect, useMemo, useState } from 'react'
import { Modal, TextInput, Stack, Group, Text, UnstyledButton } from '@mantine/core'
import type { Icon } from '@tabler/icons-react'

/** 单条命令：唯一 id、中文展示名、图标、执行回调 */
export interface Command {
  id: string
  label: string
  icon: Icon
  run: () => void
}

interface CommandPaletteProps {
  /** 是否打开 */
  opened: boolean
  /** 关闭回调 */
  onClose: () => void
  /** 命令清单（由 AppLayout 构造并注入，便于纯展示组件独立单测） */
  commands: Command[]
}

/**
 * 全局命令面板（FR-74）。
 * 受控弹窗：输入框模糊过滤命令、↑↓ 移动高亮、Enter 执行、Esc 关闭、点击执行。
 */
export default function CommandPalette({ opened, onClose, commands }: CommandPaletteProps) {
  const [query, setQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(0)

  // 每次打开时清空查询并重置高亮，避免上次输入残留
  useEffect(() => {
    if (opened) {
      setQuery('')
      setActiveIndex(0)
    }
  }, [opened])

  // 按命令中文 label 大小写无关子串过滤（中文无大小写，统一小写化以兼容潜在英文）
  const filtered = useMemo(() => {
    const keyword = query.trim().toLowerCase()
    if (keyword === '') return commands
    return commands.filter((cmd) => cmd.label.toLowerCase().includes(keyword))
  }, [commands, query])

  // 过滤结果变化后，高亮收敛到首项，避免下标越界
  useEffect(() => {
    setActiveIndex(0)
  }, [query])

  // 执行指定下标命令并关闭面板
  const runAt = (index: number) => {
    const cmd = filtered[index]
    if (!cmd) return
    cmd.run()
    onClose()
  }

  // 输入框键盘导航：↑↓ 移动高亮（边界回绕）、Enter 执行高亮项、Esc 关闭
  const handleKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (filtered.length === 0) {
      if (event.key === 'Escape') onClose()
      return
    }
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      setActiveIndex((i) => (i + 1) % filtered.length)
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      setActiveIndex((i) => (i - 1 + filtered.length) % filtered.length)
    } else if (event.key === 'Enter') {
      event.preventDefault()
      runAt(activeIndex)
    } else if (event.key === 'Escape') {
      onClose()
    }
  }

  return (
    <Modal opened={opened} onClose={onClose} title="命令面板" centered size="md">
      <Stack gap="sm">
        <TextInput
          aria-label="命令"
          placeholder="输入命令名快速跳转或执行…"
          value={query}
          onChange={(e) => setQuery(e.currentTarget.value)}
          onKeyDown={handleKeyDown}
          data-autofocus
        />
        <Stack gap={2}>
          {filtered.length === 0 ? (
            <Text size="sm" c="dimmed" ta="center" py="xs">
              无匹配命令
            </Text>
          ) : (
            filtered.map((cmd, index) => {
              const Icon = cmd.icon
              const active = index === activeIndex
              return (
                <UnstyledButton
                  key={cmd.id}
                  data-active={active || undefined}
                  onClick={() => runAt(index)}
                  onMouseEnter={() => setActiveIndex(index)}
                  p="xs"
                  style={{
                    borderRadius: 'var(--mantine-radius-sm)',
                    backgroundColor: active ? 'var(--mantine-color-default-hover)' : undefined,
                  }}
                >
                  <Group gap={8}>
                    <Icon size={16} />
                    <Text size="sm">{cmd.label}</Text>
                  </Group>
                </UnstyledButton>
              )
            })
          )}
        </Stack>
      </Stack>
    </Modal>
  )
}
