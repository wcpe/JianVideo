import { Menu } from '@mantine/core';
import {
  IconTrash,
  IconChecks,
  IconArrowsExchange,
  IconCheckbox,
  IconSquare,
  IconPhotoPlus,
  IconTag,
  IconDownload,
} from '@tabler/icons-react';

/** 右键菜单的打开位置（视口坐标）与关联的右键项 id；null 表示关闭 */
export interface ContextMenuState {
  x: number;
  y: number;
  /** 触发右键的那一项 id（用于未选中时「删除选中」退化为删该项） */
  targetId: number;
}

interface MediaContextMenuProps {
  state: ContextMenuState | null;
  onClose: () => void;
  /** 当前选中数量，用于「删除选中」文案与禁用判断 */
  selectedCount: number;
  checkboxMode: boolean;
  onDelete: (targetId: number) => void;
  onSelectAll: () => void;
  onInvert: () => void;
  onToggleCheckboxMode: () => void;
  // 批量操作（FR-91）：父组件提供任一回调时显示对应菜单项，操作对象为选中集（无选中则退化为右键项）
  onAddToAlbum?: (targetId: number) => void;
  onAddTag?: (targetId: number) => void;
  onDownload?: (targetId: number) => void;
}

/**
 * 列表/网格的右键常用菜单（FR-69 删除/全选/反选/复选框模式 + FR-91 加相册/打标签/打包下载）。
 * 用一个零尺寸的 fixed 定位 target 承载光标位置，单实例渲染，避免每卡一个 Menu。
 */
export default function MediaContextMenu({
  state,
  onClose,
  selectedCount,
  checkboxMode,
  onDelete,
  onSelectAll,
  onInvert,
  onToggleCheckboxMode,
  onAddToAlbum,
  onAddTag,
  onDownload,
}: MediaContextMenuProps) {
  const opened = state !== null;
  // 未选中任何项时各操作退化为对右键项；有选中时按选中集批量操作
  const suffix = selectedCount > 0 ? `选中（${selectedCount}）` : '';
  const deleteLabel = selectedCount > 0 ? `删除选中（${selectedCount}）` : '删除';
  const hasBatchActions = !!(onAddToAlbum || onAddTag || onDownload);

  return (
    <Menu
      opened={opened}
      onClose={onClose}
      position="bottom-start"
      withinPortal
      shadow="md"
      width={180}
    >
      <Menu.Target>
        <div
          style={{
            position: 'fixed',
            left: state?.x ?? 0,
            top: state?.y ?? 0,
            width: 0,
            height: 0,
          }}
        />
      </Menu.Target>
      <Menu.Dropdown>
        <Menu.Item
          color="red"
          leftSection={<IconTrash size={14} />}
          onClick={() => {
            if (state) onDelete(state.targetId);
          }}
        >
          {deleteLabel}
        </Menu.Item>
        {hasBatchActions && (
          <>
            <Menu.Divider />
            {onAddToAlbum && (
              <Menu.Item
                leftSection={<IconPhotoPlus size={14} />}
                onClick={() => {
                  if (state) onAddToAlbum(state.targetId);
                }}
              >
                {`加入相册${suffix}`}
              </Menu.Item>
            )}
            {onAddTag && (
              <Menu.Item
                leftSection={<IconTag size={14} />}
                onClick={() => {
                  if (state) onAddTag(state.targetId);
                }}
              >
                {`打标签${suffix}`}
              </Menu.Item>
            )}
            {onDownload && (
              <Menu.Item
                leftSection={<IconDownload size={14} />}
                onClick={() => {
                  if (state) onDownload(state.targetId);
                }}
              >
                {`打包下载${suffix}`}
              </Menu.Item>
            )}
          </>
        )}
        <Menu.Divider />
        <Menu.Item leftSection={<IconChecks size={14} />} onClick={onSelectAll}>
          全选
        </Menu.Item>
        <Menu.Item leftSection={<IconArrowsExchange size={14} />} onClick={onInvert}>
          反选
        </Menu.Item>
        <Menu.Item
          leftSection={checkboxMode ? <IconSquare size={14} /> : <IconCheckbox size={14} />}
          onClick={onToggleCheckboxMode}
        >
          {checkboxMode ? '退出复选框模式' : '复选框模式'}
        </Menu.Item>
      </Menu.Dropdown>
    </Menu>
  );
}
