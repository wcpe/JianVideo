import { useState, useEffect, useCallback } from 'react'
import { Box, Group, Text, UnstyledButton, Loader, ActionIcon } from '@mantine/core'
import { IconChevronRight, IconFolder, IconFolderOpen, IconServer } from '@tabler/icons-react'
import * as libApi from '@/api/library'
import { BROWSE_ROOT } from '@/hooks/useDirectoryBrowse'
import type { DirInfo } from '@/types'

interface DirectoryTreeProps {
  /** 当前浏览路径（高亮该节点） */
  currentPath: string
  /** 点击节点切换路径 */
  onNavigate: (path: string) => void
}

interface TreeNodeProps {
  dir: DirInfo
  depth: number
  currentPath: string
  onNavigate: (path: string) => void
  /** 需要展开到的目标路径（用于自动展开当前路径祖先链） */
  expandTo: string | null
}

/** 某路径是否为目标路径的祖先（或自身），用于自动展开当前路径所在链。 */
function isAncestorOf(path: string, target: string | null): boolean {
  if (!target) return false
  return target === path || target.startsWith(path + '/')
}

/**
 * 左导航树单个节点（FR-121）：懒展开——首次展开才调 browse 取子目录。
 * 点击名称切换路径；点击三角仅展开/收起。当前路径节点高亮。
 */
function TreeNode({ dir, depth, currentPath, onNavigate, expandTo }: TreeNodeProps) {
  const [expanded, setExpanded] = useState(false)
  const [children, setChildren] = useState<DirInfo[] | null>(null)
  const [loading, setLoading] = useState(false)

  const selected = dir.path === currentPath

  const loadChildren = useCallback(async () => {
    if (children !== null) return children
    setLoading(true)
    try {
      const res = await libApi.browseDirectory(dir.path, 'name')
      setChildren(res.directories)
      return res.directories
    } catch {
      setChildren([])
      return []
    } finally {
      setLoading(false)
    }
  }, [children, dir.path])

  const toggle = useCallback(async () => {
    if (!expanded) await loadChildren()
    setExpanded((v) => !v)
  }, [expanded, loadChildren])

  // 自动展开当前路径的祖先链：本节点是目标祖先（非自身）时自动展开并加载子目录。
  useEffect(() => {
    if (!expanded && expandTo && dir.path !== expandTo && isAncestorOf(dir.path, expandTo)) {
      void loadChildren().then(() => setExpanded(true))
    }
    // 仅在目标路径变化时尝试展开
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [expandTo])

  return (
    <Box>
      <Group
        gap={2}
        wrap="nowrap"
        style={{
          paddingLeft: depth * 14 + 4,
          borderRadius: 'var(--mantine-radius-sm)',
          background: selected ? 'var(--mantine-color-purple-light)' : undefined,
        }}
      >
        <ActionIcon
          variant="subtle"
          color="gray"
          size="sm"
          aria-label={expanded ? `收起 ${dir.name}` : `展开 ${dir.name}`}
          onClick={toggle}
          style={{ flexShrink: 0 }}
        >
          <IconChevronRight
            size={14}
            style={{ transform: expanded ? 'rotate(90deg)' : 'none', transition: 'transform 0.15s' }}
          />
        </ActionIcon>
        <UnstyledButton
          onClick={() => onNavigate(dir.path)}
          style={{ flex: 1, minWidth: 0, padding: '4px 2px' }}
        >
          <Group gap={6} wrap="nowrap">
            {expanded
              ? <IconFolderOpen size={16} color="var(--mantine-color-purple-4)" style={{ flexShrink: 0 }} />
              : <IconFolder size={16} color="var(--mantine-color-purple-4)" style={{ flexShrink: 0 }} />}
            <Text size="sm" truncate c={selected ? 'purple' : undefined} fw={selected ? 600 : undefined}>
              {dir.name}
            </Text>
          </Group>
        </UnstyledButton>
      </Group>

      {expanded && (
        <Box>
          {loading ? (
            <Group gap={6} style={{ paddingLeft: (depth + 1) * 14 + 8 }} py={4}>
              <Loader size="xs" />
              <Text size="xs" c="dimmed">加载中…</Text>
            </Group>
          ) : children && children.length > 0 ? (
            children.map((child) => (
              <TreeNode
                key={child.path}
                dir={child}
                depth={depth + 1}
                currentPath={currentPath}
                onNavigate={onNavigate}
                expandTo={expandTo}
              />
            ))
          ) : children && children.length === 0 ? (
            <Text size="xs" c="dimmed" style={{ paddingLeft: (depth + 1) * 14 + 8 }} py={2}>
              （无子目录）
            </Text>
          ) : null}
        </Box>
      )}
    </Box>
  )
}

/**
 * 左导航树（FR-121）：聚合根 → 盘符/共享 → 子目录懒展开，当前路径高亮。
 * 顶层卷根由根浏览（parent_path=__root__）取得；点击「此电脑」回到根。
 */
export default function DirectoryTree({ currentPath, onNavigate }: DirectoryTreeProps) {
  const [roots, setRoots] = useState<DirInfo[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let active = true
    setLoading(true)
    libApi.browseDirectory(BROWSE_ROOT, 'name')
      .then((res) => { if (active) setRoots(res.directories) })
      .catch(() => { if (active) setRoots([]) })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [])

  const atRoot = currentPath === BROWSE_ROOT

  return (
    <Box role="tree" aria-label="目录树">
      {/* 根「此电脑」：点击回到卷根列表 */}
      <UnstyledButton
        onClick={() => onNavigate(BROWSE_ROOT)}
        style={{
          display: 'block',
          width: '100%',
          padding: '4px 6px',
          borderRadius: 'var(--mantine-radius-sm)',
          background: atRoot ? 'var(--mantine-color-purple-light)' : undefined,
        }}
      >
        <Group gap={6} wrap="nowrap">
          <IconServer size={16} color="var(--mantine-color-purple-4)" style={{ flexShrink: 0 }} />
          <Text size="sm" fw={atRoot ? 600 : 500} c={atRoot ? 'purple' : undefined}>此电脑</Text>
        </Group>
      </UnstyledButton>

      {loading ? (
        <Group gap={6} px="sm" py={4}>
          <Loader size="xs" />
          <Text size="xs" c="dimmed">加载中…</Text>
        </Group>
      ) : (
        roots.map((root) => (
          <TreeNode
            key={root.path}
            dir={root}
            depth={1}
            currentPath={currentPath}
            onNavigate={onNavigate}
            expandTo={atRoot ? null : currentPath}
          />
        ))
      )}
    </Box>
  )
}
