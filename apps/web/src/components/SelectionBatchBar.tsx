import { Paper, Group, Text, Button, ActionIcon, Tooltip } from '@mantine/core';
import {
  IconTrash,
  IconPhotoPlus,
  IconTag,
  IconDownload,
  IconX,
  IconMovie,
  IconFolderShare,
} from '@tabler/icons-react';

interface SelectionBatchBarProps {
  // 当前选中数量，0 时不渲染
  count: number;
  // 清除选择（必有）
  onClear?: () => void;
  // 批量删除（必有）
  onDelete?: () => void;
  // 批量操作（FR-91 / FR2-053）：提供才渲染对应按钮
  onAddToAlbum?: () => void;
  onAddTag?: () => void;
  onDownload?: () => void;
  onTranscode?: () => void;
  onMove?: () => void;
}

/**
 * 选中态 sticky 批量操作条（FR-99）：选中 ≥1 项时浮现在页面底部，
 * 消费父页已持有的多选 state 与 FR-91 批量回调，使批量操作不再只靠右键菜单。
 * 本组件只展示与转发，不持有任何多选逻辑。
 */
export default function SelectionBatchBar({
  count,
  onClear,
  onDelete,
  onAddToAlbum,
  onAddTag,
  onDownload,
  onTranscode,
  onMove,
}: SelectionBatchBarProps) {
  if (count <= 0) return null;

  return (
    <Paper
      role="toolbar"
      aria-label="批量操作"
      shadow="md"
      radius="md"
      withBorder
      p="xs"
      style={{
        position: 'sticky',
        bottom: 16,
        zIndex: 50,
        // 居中浮条，窄屏自适应不溢出
        marginLeft: 'auto',
        marginRight: 'auto',
        maxWidth: 'fit-content',
        background: 'var(--mantine-color-body)',
      }}
    >
      <Group gap="xs" wrap="nowrap" align="center">
        <Tooltip label="清除选择">
          <ActionIcon variant="subtle" color="gray" aria-label="清除选择" onClick={onClear}>
            <IconX size={16} />
          </ActionIcon>
        </Tooltip>
        <Text size="sm" fw={600} c="purple" style={{ whiteSpace: 'nowrap' }}>
          已选 {count} 项
        </Text>
        {onAddToAlbum && (
          <Button
            size="xs"
            variant="light"
            color="purple"
            leftSection={<IconPhotoPlus size={14} />}
            onClick={onAddToAlbum}
          >
            加入相册
          </Button>
        )}
        {onAddTag && (
          <Button
            size="xs"
            variant="light"
            color="purple"
            leftSection={<IconTag size={14} />}
            onClick={onAddTag}
          >
            打标签
          </Button>
        )}
        {onDownload && (
          <Button
            size="xs"
            variant="light"
            color="purple"
            leftSection={<IconDownload size={14} />}
            onClick={onDownload}
          >
            打包下载
          </Button>
        )}
        {onTranscode && (
          <Button
            size="xs"
            variant="light"
            color="purple"
            leftSection={<IconMovie size={14} />}
            onClick={onTranscode}
          >
            转码
          </Button>
        )}
        {onMove && (
          <Button
            size="xs"
            variant="light"
            color="purple"
            leftSection={<IconFolderShare size={14} />}
            onClick={onMove}
          >
            移动
          </Button>
        )}
        <Button
          size="xs"
          variant="light"
          color="red"
          leftSection={<IconTrash size={14} />}
          onClick={onDelete}
        >
          删除
        </Button>
      </Group>
    </Paper>
  );
}
