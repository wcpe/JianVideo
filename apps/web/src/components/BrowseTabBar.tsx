import { Group, Box, CloseButton, ActionIcon, Tooltip, Text } from '@mantine/core';
import { IconPlus, IconFolder } from '@tabler/icons-react';
import { BROWSE_ROOT } from '@/hooks/useDirectoryBrowse';
import type { BrowseTab } from '@/stores/browse-tabs';

interface BrowseTabBarProps {
  /** 打开的标签列表（FR-150），顺序即标签栏顺序 */
  tabs: BrowseTab[];
  /** 当前激活标签 id */
  activeTabId: string;
  /** 切换激活标签 */
  onSelect: (id: string) => void;
  /** 关闭标签（仅多于一个时可关） */
  onClose: (id: string) => void;
  /** 新建标签 */
  onAdd: () => void;
}

/** 标签标题（FR-150）：根标签显示「全部」，其余取路径末段，取不到则回退完整路径。 */
function tabLabel(path: string): string {
  if (path === BROWSE_ROOT) return '全部';
  const seg = path.split(/[/\\]/).filter(Boolean).pop();
  return seg || path;
}

/**
 * 目录浏览标签栏（FR-150，参考 Windows 资源管理器）：横排标签 + 末尾「+」新建。
 * 每个标签代表一个独立浏览会话（目录位置 + 筛选/排序态由 store 持有）。
 * 仅一个标签时关闭按钮禁用，始终保留至少一个标签。
 */
export default function BrowseTabBar({
  tabs,
  activeTabId,
  onSelect,
  onClose,
  onAdd,
}: BrowseTabBarProps) {
  const closable = tabs.length > 1;
  return (
    <Group gap={4} wrap="nowrap" align="center" aria-label="浏览标签栏" role="tablist">
      {tabs.map((tab) => {
        const active = tab.id === activeTabId;
        const label = tabLabel(tab.path);
        return (
          <Box
            key={tab.id}
            role="tab"
            aria-selected={active}
            aria-label={`标签 ${label}`}
            onClick={() => onSelect(tab.id)}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 4,
              maxWidth: 200,
              padding: '4px 8px',
              cursor: 'pointer',
              borderRadius: 'var(--mantine-radius-sm)',
              border: '1px solid var(--mantine-color-default-border)',
              borderBottomColor: active ? 'var(--mantine-color-blue-filled)' : undefined,
              background: active
                ? 'var(--mantine-color-default-hover)'
                : 'var(--mantine-color-default)',
            }}
          >
            <IconFolder size={14} style={{ flexShrink: 0 }} />
            <Text size="xs" truncate style={{ minWidth: 0 }}>
              {label}
            </Text>
            <CloseButton
              size="xs"
              aria-label={`关闭标签 ${label}`}
              disabled={!closable}
              onClick={(e) => {
                e.stopPropagation();
                onClose(tab.id);
              }}
            />
          </Box>
        );
      })}
      <Tooltip label="新建标签">
        <ActionIcon variant="default" size="md" aria-label="新建标签" onClick={onAdd}>
          <IconPlus size={16} />
        </ActionIcon>
      </Tooltip>
    </Group>
  );
}
