import { useMemo, useEffect, useState } from 'react';
import {
  SimpleGrid,
  Card,
  Text,
  Group,
  Box,
  Skeleton,
  Alert,
  Stack,
  Badge,
  Checkbox,
  ActionIcon,
} from '@mantine/core';
import {
  IconFolder,
  IconAlertCircle,
  IconSearchOff,
  IconPlayerPlay,
  IconDots,
} from '@tabler/icons-react';
import EmptyState from '@/components/EmptyState';
import { formatSize, formatDuration } from '@/utils/format';
import { isImageFile, mediaDisplayName } from '@/utils/media';
import MediaThumbnail from '@/components/MediaThumbnail';
import MediaCardOverlay from '@/components/MediaCardOverlay';
import MediaContextMenu, { type ContextMenuState } from '@/components/MediaContextMenu';
import SelectionBatchBar from '@/components/SelectionBatchBar';
import { useMultiSelect } from '@/hooks/useMultiSelect';
import { type DisplayMode, type DirSort, sortFiles } from '@/components/DirectoryBrowser.helpers';
import type { MediaFile, BreadcrumbItem, DirInfo } from '@/types';

/** 把后端 modified_at 渲染为本地日期时间；无值返回 '—'。 */
function formatModified(s?: string): string {
  if (!s) return '—';
  const d = new Date(s);
  return isNaN(d.getTime()) ? '—' : d.toLocaleString();
}

interface DirectoryBrowserProps {
  // 地址栏接管路径导航后，面包屑改由 BrowsePage 顶部渲染；此处保留可选以兼容旧调用，不再内嵌渲染。
  breadcrumbs?: BreadcrumbItem[];
  directories: DirInfo[];
  files: MediaFile[];
  loading: boolean;
  error: string | null;
  customImageExtensions: Record<number, string[]>;
  onEnterDir: (dir: DirInfo) => void;
  onBreadcrumbNavigate?: (path: string) => void;
  onErrorClose: () => void;
  /** 双击文件触发打开（FR-33）；参数为该文件在排序后 files 中的下标 */
  onOpenFile: (file: MediaFile, index: number) => void;
  // 展示方式与排序（FR-33），缺省 list / name
  displayMode?: DisplayMode;
  sort?: DirSort;
  // 搜索/筛选生效标记（FR-98）：为真时 0 结果显示「无匹配结果」而非「空目录」
  filtered?: boolean;
  // 清除筛选回调（FR-98）：无结果态「清除筛选」CTA，传入才渲染
  onClearFilter?: () => void;
  // 多选与批量删除（FR-69），均可选，缺省退化为纯高亮选择、无右键菜单
  onSelectionChange?: (ids: number[]) => void;
  onBatchDelete?: (ids: number[]) => void;
  onDeleteOne?: (file: MediaFile) => void;
  // 批量操作（FR-91 / FR2-053）：以 id 集为对象（无选中退化为右键项），均可选
  onBatchAddToAlbum?: (ids: number[]) => void;
  onBatchAddTag?: (ids: number[]) => void;
  onBatchDownload?: (ids: number[]) => void;
  onBatchTranscode?: (ids: number[]) => void;
  onBatchMove?: (ids: number[]) => void;
  // 隐藏内置 sticky 批量条（FR-121）：资源管理器工具栏已承载批量动作时传 true 避免双批量 UI。
  hideSelectionBar?: boolean;
}

// 各图标档的响应式列数（FR-99）：按容器宽度断点自适应增/减列，超宽屏增列、窄屏减列。
// 档位（大/中/小）通过不同断点阈值控制每列目标宽度（小图标列更密）。
const GRID_COLS: Record<Exclude<DisplayMode, 'list' | 'details'>, Record<string, number>> = {
  large: { '180px': 2, '480px': 3, '760px': 4, '1080px': 5, '1400px': 6 },
  medium: { '160px': 3, '480px': 4, '720px': 6, '1080px': 8, '1400px': 10 },
  small: { '160px': 4, '420px': 6, '720px': 8, '1080px': 10, '1400px': 12 },
};

/**
 * 目录浏览 UI（FR-33 资源管理器视图 + FR-69 多选/右键批量）：面包屑 + 多展示档位 + 排序。
 * 目录恒在文件前、目录按名称排序；双击文件打开详情面板。多选手势复用 useMultiSelect，
 * 右键弹常用菜单（删除选中/全选/反选/复选框模式）。目录项不参与选择（仅文件可选）。
 */
export default function DirectoryBrowser({
  directories,
  files,
  loading,
  error,
  customImageExtensions,
  onEnterDir,
  onErrorClose,
  onOpenFile,
  displayMode = 'list',
  sort = 'name',
  filtered = false,
  onClearFilter,
  onSelectionChange,
  onBatchDelete,
  onDeleteOne,
  onBatchAddToAlbum,
  onBatchAddTag,
  onBatchDownload,
  onBatchTranscode,
  onBatchMove,
  hideSelectionBar = false,
}: DirectoryBrowserProps) {
  const sortedDirs = useMemo(
    () => [...directories].sort((a, b) => a.name.localeCompare(b.name)),
    [directories],
  );
  const sortedFiles = useMemo(() => sortFiles(files, sort), [files, sort]);
  const orderedIds = useMemo(() => sortedFiles.map((f) => f.id), [sortedFiles]);

  const select = useMultiSelect(orderedIds);
  const { selectedIds } = select;
  const [menu, setMenu] = useState<ContextMenuState | null>(null);
  // 父组件关心选择（提供任一选择相关回调）时启用右键菜单与复选框模式
  const selectionEnabled = !!(
    onSelectionChange ||
    onBatchDelete ||
    onDeleteOne ||
    onBatchAddToAlbum ||
    onBatchAddTag ||
    onBatchDownload ||
    onBatchTranscode ||
    onBatchMove
  );

  // 选中集变化上抛父组件（升序，便于稳定断言/消费）
  useEffect(() => {
    onSelectionChange?.(Array.from(selectedIds).sort((a, b) => a - b));
    // 仅在选中集变化时上抛
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedIds]);

  // 右键文件卡片：阻止系统菜单，记录光标位置与右键项 id。
  // 不改动选择集——已有选中则菜单按选中批量操作；无选中则「删除」退化为删该右键项（onDeleteOne）。
  function handleContextMenu(index: number, e: React.MouseEvent) {
    if (!selectionEnabled) return; // 父组件不关心选择时不接管右键
    e.preventDefault();
    setMenu({ x: e.clientX, y: e.clientY, targetId: sortedFiles[index].id });
  }

  // 「删除选中」：有选中按选中批量删；无选中删右键项
  function handleMenuDelete(targetId: number) {
    setMenu(null);
    if (selectedIds.size > 0) {
      onBatchDelete?.(Array.from(selectedIds).sort((a, b) => a - b));
    } else {
      const f = sortedFiles.find((x) => x.id === targetId);
      if (f) onDeleteOne?.(f);
    }
  }

  // 批量操作（FR-91）：有选中按选中集；无选中退化为右键项。统一关菜单后回调。
  function runBatch(targetId: number, fn?: (ids: number[]) => void) {
    setMenu(null);
    const ids = selectedIds.size > 0 ? Array.from(selectedIds).sort((a, b) => a - b) : [targetId];
    fn?.(ids);
  }

  // sticky 批量条（FR-99）：以当前选中集为对象调用 FR-91 批量回调，复用已有多选 state。
  const selectedArr = () => Array.from(selectedIds).sort((a, b) => a - b);
  const runBarBatch = (fn?: (ids: number[]) => void) => {
    if (selectedIds.size > 0) fn?.(selectedArr());
  };

  if (error) {
    return (
      <Alert
        icon={<IconAlertCircle size={16} />}
        color="red"
        withCloseButton
        onClose={onErrorClose}
        mb="sm"
      >
        {error}
      </Alert>
    );
  }

  const isDetails = displayMode === 'details';
  const isList = displayMode === 'list';
  const cols =
    isDetails || isList
      ? undefined
      : GRID_COLS[displayMode as Exclude<DisplayMode, 'list' | 'details'>];

  return (
    <>
      {loading ? (
        <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }}>
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} height={60} radius="md" />
          ))}
        </SimpleGrid>
      ) : sortedDirs.length === 0 && sortedFiles.length === 0 ? (
        // 区分「搜索/筛选无结果」与「空目录」（FR-98）：前者给清除筛选引导
        filtered ? (
          <EmptyState
            icon={
              <IconSearchOff
                size={72}
                stroke={1.2}
                style={{ color: 'var(--mantine-color-dimmed)', opacity: 0.7 }}
              />
            }
            title="没有匹配的媒体"
            description="当前搜索或筛选条件下没有结果，换个条件或清除筛选试试。"
            action={onClearFilter ? { label: '清除筛选', onClick: onClearFilter } : undefined}
          />
        ) : (
          <EmptyState
            icon={
              <IconFolder
                size={72}
                stroke={1.2}
                style={{ color: 'var(--mantine-color-dimmed)', opacity: 0.7 }}
              />
            }
            title="此目录暂无内容"
            description="这个目录下没有可显示的子目录或媒体文件。"
          />
        )
      ) : isDetails ? (
        // 详情视图（FR-121）：名称 / 修改日期 / 类型 / 大小 列。目录行大小「—」、类型「文件夹」。
        <Box style={{ overflowX: 'auto' }}>
          <Box role="table" aria-label="目录详情" style={{ minWidth: 520 }}>
            {/* 表头 */}
            <Group
              role="row"
              gap="sm"
              wrap="nowrap"
              px="xs"
              py={6}
              style={{
                borderBottom: '1px solid var(--mantine-color-default-border)',
                color: 'var(--mantine-color-dimmed)',
              }}
            >
              <Text role="columnheader" size="xs" fw={600} style={{ flex: 1, minWidth: 0 }}>
                名称
              </Text>
              <Text role="columnheader" size="xs" fw={600} style={{ width: 180, flexShrink: 0 }}>
                修改日期
              </Text>
              <Text role="columnheader" size="xs" fw={600} style={{ width: 96, flexShrink: 0 }}>
                类型
              </Text>
              <Text
                role="columnheader"
                size="xs"
                fw={600}
                ta="right"
                style={{ width: 96, flexShrink: 0 }}
              >
                大小
              </Text>
            </Group>

            {sortedDirs.map((dir) => (
              <Group
                role="row"
                key={`dir-${dir.path}`}
                gap="sm"
                wrap="nowrap"
                px="xs"
                py={6}
                className="hover-card"
                style={{
                  cursor: 'pointer',
                  borderBottom: '1px solid var(--mantine-color-default-border)',
                }}
                onClick={() => onEnterDir(dir)}
              >
                <Group role="cell" gap="xs" wrap="nowrap" style={{ flex: 1, minWidth: 0 }}>
                  <IconFolder
                    size={16}
                    color="var(--mantine-color-purple-4)"
                    style={{ flexShrink: 0 }}
                  />
                  <Text size="sm" truncate title={dir.name}>
                    {dir.name}
                  </Text>
                </Group>
                <Text role="cell" size="sm" c="dimmed" style={{ width: 180, flexShrink: 0 }}>
                  —
                </Text>
                <Text role="cell" size="sm" c="dimmed" style={{ width: 96, flexShrink: 0 }}>
                  文件夹
                </Text>
                <Text
                  role="cell"
                  size="sm"
                  c="dimmed"
                  ta="right"
                  style={{ width: 96, flexShrink: 0 }}
                >
                  —
                </Text>
              </Group>
            ))}

            {sortedFiles.map((file, i) => {
              const selected = selectedIds.has(file.id);
              return (
                <Group
                  role="row"
                  key={`file-${file.id}`}
                  gap="sm"
                  wrap="nowrap"
                  px="xs"
                  py={6}
                  className="hover-card"
                  style={{
                    cursor: 'pointer',
                    borderBottom: '1px solid var(--mantine-color-default-border)',
                    background: selected ? 'var(--mantine-color-purple-light)' : undefined,
                  }}
                  onClick={(e) => select.handleItemClick(i, e)}
                  onDoubleClick={() => onOpenFile(file, i)}
                  onContextMenu={(e) => handleContextMenu(i, e)}
                  data-selected={selected || undefined}
                >
                  <Group role="cell" gap="xs" wrap="nowrap" style={{ flex: 1, minWidth: 0 }}>
                    {select.checkboxMode && (
                      <Checkbox
                        size="xs"
                        checked={selected}
                        readOnly
                        tabIndex={-1}
                        aria-label={`选择 ${mediaDisplayName(file)}`}
                        style={{ flexShrink: 0 }}
                      />
                    )}
                    <Text size="sm" truncate title={mediaDisplayName(file)}>
                      {mediaDisplayName(file)}
                    </Text>
                  </Group>
                  <Text role="cell" size="sm" c="dimmed" style={{ width: 180, flexShrink: 0 }}>
                    {formatModified(file.modified_at)}
                  </Text>
                  <Text role="cell" size="sm" c="dimmed" style={{ width: 96, flexShrink: 0 }}>
                    {file.format.toUpperCase()}
                  </Text>
                  <Text role="cell" size="sm" ta="right" style={{ width: 96, flexShrink: 0 }}>
                    {formatSize(file.file_size)}
                  </Text>
                </Group>
              );
            })}
          </Box>
        </Box>
      ) : isList ? (
        // 列表（详情行）
        <Stack gap={4}>
          {sortedDirs.map((dir) => (
            <Card
              key={`dir-${dir.path}`}
              withBorder
              p="xs"
              radius="sm"
              bg="var(--mantine-color-default)"
              style={{ cursor: 'pointer' }}
              className="hover-card"
              onClick={() => onEnterDir(dir)}
            >
              <Group gap="xs" wrap="nowrap">
                <IconFolder size={18} color="var(--mantine-color-purple-4)" />
                <Text size="sm">{dir.name}</Text>
              </Group>
            </Card>
          ))}
          {sortedFiles.map((file, i) => {
            const isImage = isImageFile(file, customImageExtensions);
            const selected = selectedIds.has(file.id);
            return (
              <Card
                key={`file-${file.id}`}
                withBorder
                p="xs"
                radius="sm"
                bg={selected ? 'var(--mantine-color-purple-light)' : 'var(--mantine-color-default)'}
                style={{
                  cursor: 'pointer',
                  borderColor: selected ? 'var(--mantine-color-purple-5)' : undefined,
                }}
                className="hover-card"
                onClick={(e) => select.handleItemClick(i, e)}
                onDoubleClick={() => onOpenFile(file, i)}
                onContextMenu={(e) => handleContextMenu(i, e)}
                data-selected={selected || undefined}
              >
                <Group gap="sm" wrap="nowrap" justify="space-between">
                  <Group gap="xs" wrap="nowrap" style={{ flex: 1, minWidth: 0 }}>
                    {select.checkboxMode && (
                      <Checkbox
                        size="xs"
                        checked={selected}
                        readOnly
                        tabIndex={-1}
                        aria-label={`选择 ${mediaDisplayName(file)}`}
                        style={{ flexShrink: 0 }}
                      />
                    )}
                    <Text
                      size="sm"
                      truncate
                      style={{ flex: 1, minWidth: 0 }}
                      title={mediaDisplayName(file)}
                    >
                      {mediaDisplayName(file)}
                    </Text>
                  </Group>
                  <Group gap="xs" wrap="nowrap" style={{ flexShrink: 0 }}>
                    <Badge size="xs" variant="light" color="blue">
                      {file.format.toUpperCase()}
                    </Badge>
                    <Badge size="xs" variant="light" color="purple">
                      {formatSize(file.file_size)}
                    </Badge>
                    {!isImage && (
                      <Badge size="xs" variant="light" color="gray">
                        {formatDuration(file.duration)}
                      </Badge>
                    )}
                    {file.width > 0 && file.height > 0 && (
                      <Badge size="xs" variant="light" color="gray">
                        {file.width}x{file.height}
                      </Badge>
                    )}
                  </Group>
                </Group>
              </Card>
            );
          })}
        </Stack>
      ) : (
        // 图标档（大/中/小）：响应式列数 + 缩略图叠层信息卡（FR-99）
        <SimpleGrid type="container" cols={cols}>
          {sortedDirs.map((dir) => (
            <Card
              key={`dir-${dir.path}`}
              withBorder
              p="sm"
              radius="md"
              bg="var(--mantine-color-default)"
              style={{ cursor: 'pointer' }}
              className="hover-card"
              onClick={() => onEnterDir(dir)}
            >
              <Stack gap={4} align="center">
                <IconFolder
                  size={displayMode === 'small' ? 24 : 40}
                  color="var(--mantine-color-purple-4)"
                />
                <Text size="xs" truncate w="100%" ta="center">
                  {dir.name}
                </Text>
              </Stack>
            </Card>
          ))}
          {sortedFiles.map((file, i) => {
            const isImage = isImageFile(file, customImageExtensions);
            const selected = selectedIds.has(file.id);
            return (
              <Card
                key={`file-${file.id}`}
                withBorder
                p={0}
                radius="md"
                style={{
                  cursor: 'pointer',
                  borderColor: selected ? 'var(--mantine-color-purple-6)' : undefined,
                  position: 'relative',
                  overflow: 'hidden',
                }}
                className="hover-card media-card"
                onClick={(e) => select.handleItemClick(i, e)}
                onDoubleClick={() => onOpenFile(file, i)}
                onContextMenu={(e) => handleContextMenu(i, e)}
                data-selected={selected || undefined}
              >
                <Box style={{ position: 'relative' }}>
                  <MediaThumbnail
                    mediaID={file.id}
                    fileName={file.file_name}
                    objectFit="cover"
                    overlay={
                      <MediaCardOverlay
                        file={file}
                        isImage={isImage}
                        selected={selected}
                        checkboxMode={select.checkboxMode}
                      />
                    }
                  />
                  {select.checkboxMode && (
                    <Checkbox
                      size="xs"
                      checked={selected}
                      readOnly
                      tabIndex={-1}
                      aria-label={`选择 ${mediaDisplayName(file)}`}
                      style={{ position: 'absolute', top: 6, left: 6, zIndex: 5 }}
                    />
                  )}
                  {/* hover 快捷操作浮层（FR-99）：双击打开沿用，浮层提供单击打开/更多入口 */}
                  <Group
                    gap={6}
                    wrap="nowrap"
                    className="media-card-actions"
                    style={{ position: 'absolute', top: 6, right: 6, zIndex: 6 }}
                  >
                    <ActionIcon
                      variant="filled"
                      color="dark"
                      size="sm"
                      radius="xl"
                      aria-label="打开"
                      title="打开"
                      onClick={(e) => {
                        e.stopPropagation();
                        onOpenFile(file, i);
                      }}
                    >
                      <IconPlayerPlay size={14} />
                    </ActionIcon>
                    <ActionIcon
                      variant="filled"
                      color="dark"
                      size="sm"
                      radius="xl"
                      aria-label="更多操作"
                      title="更多操作"
                      onClick={(e) => {
                        e.stopPropagation();
                        handleContextMenu(i, e);
                      }}
                    >
                      <IconDots size={14} />
                    </ActionIcon>
                  </Group>
                </Box>
              </Card>
            );
          })}
        </SimpleGrid>
      )}

      {/* sticky 批量操作条（FR-99）：选中 ≥1 项时浮现，复用已有多选 state 与 FR-91 批量回调。
          资源管理器（FR-121）由工具栏承载批量动作时传 hideSelectionBar 抑制，避免双批量 UI。 */}
      {selectionEnabled && !hideSelectionBar && (
        <SelectionBatchBar
          count={selectedIds.size}
          onClear={select.clear}
          onDelete={() => onBatchDelete?.(selectedArr())}
          onAddToAlbum={onBatchAddToAlbum ? () => runBarBatch(onBatchAddToAlbum) : undefined}
          onAddTag={onBatchAddTag ? () => runBarBatch(onBatchAddTag) : undefined}
          onDownload={onBatchDownload ? () => runBarBatch(onBatchDownload) : undefined}
          onTranscode={onBatchTranscode ? () => runBarBatch(onBatchTranscode) : undefined}
          onMove={onBatchMove ? () => runBarBatch(onBatchMove) : undefined}
        />
      )}

      {/* 右键常用菜单（FR-69）：父组件关心选择时挂载 */}
      {selectionEnabled && (
        <MediaContextMenu
          state={menu}
          onClose={() => setMenu(null)}
          selectedCount={selectedIds.size}
          checkboxMode={select.checkboxMode}
          onDelete={handleMenuDelete}
          onSelectAll={() => {
            select.selectAll();
            setMenu(null);
          }}
          onInvert={() => {
            select.invertSelection();
            setMenu(null);
          }}
          onToggleCheckboxMode={() => {
            select.setCheckboxMode(!select.checkboxMode);
            setMenu(null);
          }}
          onAddToAlbum={onBatchAddToAlbum ? (id) => runBatch(id, onBatchAddToAlbum) : undefined}
          onAddTag={onBatchAddTag ? (id) => runBatch(id, onBatchAddTag) : undefined}
          onDownload={onBatchDownload ? (id) => runBatch(id, onBatchDownload) : undefined}
          onTranscode={onBatchTranscode ? (id) => runBatch(id, onBatchTranscode) : undefined}
          onMove={onBatchMove ? (id) => runBatch(id, onBatchMove) : undefined}
        />
      )}
    </>
  );
}
