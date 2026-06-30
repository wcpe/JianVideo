import { useState, useMemo, useCallback } from 'react';
import { Group, NativeSelect, ActionIcon, Checkbox, Stack, Tooltip } from '@mantine/core';
import { IconPhoto, IconChecklist } from '@tabler/icons-react';
import { useLibraryPaths } from '@/hooks/useLibraryPaths';

interface LibraryFilterProps {
  // 当前筛选的图库 id（0 表示全部图库，不过滤）
  libraryId: number;
  // 选择图库时回调，复用后端 library_id 过滤（扩 FR-06）
  onLibraryIdChange: (id: number) => void;
}

// 「全部图库」哨兵值（library_id=0 表示不过滤）
const ALL_LIBRARIES = 0;

/**
 * 媒体库（图库）筛选（FR-144）：时间轴按媒体库目录筛选，复用后端 `library_id` 过滤。
 * - 单选下拉：「全部图库」+ 各启用图库；选某库映射其 id、「全部图库」映射 0 恢复全量。
 * - 「多选模式」入口（参考 Windows 资源管理器式图库切换）：进入后以复选框列出各库勾选。
 *   后端当前仅支持单 `library_id`：恰勾选一个库即按该库过滤，未选 / 全选 / 多选回退全量。
 *
 * 图库列表复用 useLibraryPaths（GET /api/library/paths 真源），不另起数据源。
 */
export default function LibraryFilter({ libraryId, onLibraryIdChange }: LibraryFilterProps) {
  const { paths } = useLibraryPaths(undefined);
  // 多选模式开关：默认单选下拉，点入口切换为复选框列表
  const [multiMode, setMultiMode] = useState(false);
  // 多选模式下已勾选的库 id 集合（仅 UI 态；恰一个时映射后端单 library_id）
  const [checked, setChecked] = useState<number[]>(() => (libraryId ? [libraryId] : []));

  // 启用的图库选项（下拉与复选框共用）
  const libraries = useMemo(() => paths.filter((p) => p.enabled), [paths]);

  // 单选下拉：值为库 id 字符串，0 = 全部图库
  const handleSelect = useCallback(
    (v: string) => {
      onLibraryIdChange(Number(v));
    },
    [onLibraryIdChange],
  );

  // 多选勾选变化：恰选一个库映射该库 library_id，否则（0 个 / 多个）回退全量 0。
  const handleCheckChange = useCallback(
    (ids: number[]) => {
      setChecked(ids);
      onLibraryIdChange(ids.length === 1 ? ids[0] : ALL_LIBRARIES);
    },
    [onLibraryIdChange],
  );

  // 切换多选模式：进入时以当前 libraryId 初始化勾选，退出时不清空筛选（保留当前 library_id）
  const toggleMulti = useCallback(() => {
    setMultiMode((m) => {
      const next = !m;
      if (next) setChecked(libraryId ? [libraryId] : []);
      return next;
    });
  }, [libraryId]);

  return (
    <Group gap="sm" align="flex-start">
      {multiMode ? (
        // 多选模式（Windows 资源管理器式图库切换入口）：复选框列出各库
        <Checkbox.Group
          aria-label="图库多选"
          value={checked.map(String)}
          onChange={(vals) => handleCheckChange(vals.map(Number))}
        >
          <Stack gap={4}>
            {libraries.map((lib) => (
              <Checkbox key={lib.id} value={String(lib.id)} label={lib.label || lib.path} size="sm" />
            ))}
          </Stack>
        </Checkbox.Group>
      ) : (
        <NativeSelect
          aria-label="图库"
          leftSection={<IconPhoto size={14} />}
          data={[
            { value: String(ALL_LIBRARIES), label: '全部图库' },
            ...libraries.map((lib) => ({ value: String(lib.id), label: lib.label || lib.path })),
          ]}
          value={String(libraryId)}
          onChange={(e) => handleSelect(e.currentTarget.value)}
          size="sm"
          w={200}
        />
      )}

      {/* 多选模式入口：切换单选下拉 ↔ 复选框列表 */}
      <Tooltip label={multiMode ? '退出多选' : '多选模式'}>
        <ActionIcon
          variant={multiMode ? 'filled' : 'light'}
          color="purple"
          aria-label="多选模式"
          onClick={toggleMulti}
        >
          <IconChecklist size={16} />
        </ActionIcon>
      </Tooltip>
    </Group>
  );
}
