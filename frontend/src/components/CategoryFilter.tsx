import { useState, useEffect, useCallback } from 'react';
import { Group, NativeSelect, ActionIcon, TextInput, Button } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { IconCategory, IconPlus } from '@tabler/icons-react';
import * as libApi from '@/api/library';
import { extractErrorMessage } from '@/utils/error';
import type { Tag } from '@/types';

interface CategoryFilterProps {
  // 仅收藏（FR-41）：由内置「收藏夹」分类映射
  favorite: boolean;
  onFavoriteChange: (v: boolean) => void;
  // 标签筛选（FR-41，0 表示不按标签筛选）：由各标签分类映射
  tagId: number;
  onTagIdChange: (id: number) => void;
}

// 内置分类选项值（FR-139）：与标签项值（tag:<id>）区分，避免值冲突
const CATEGORY_ALL = 'all';
const CATEGORY_FAVORITE = 'favorite';

/**
 * 分类筛选（FR-139）：把收藏与标签（FR-41）统一为单一「分类」入口。
 * - 内置「收藏夹」分类映射 favorite=true；
 * - 复用现有标签 tags（API 已支持 tag_id），每个标签即一个分类；
 * - 「全部」清空收藏与标签。
 * 三态互斥：选收藏夹清标签、选标签关收藏、选全部全清。
 * 仍内置新建标签入口，便于即建即筛。
 */
export default function CategoryFilter({
  favorite,
  onFavoriteChange,
  tagId,
  onTagIdChange,
}: CategoryFilterProps) {
  const [tags, setTags] = useState<Tag[]>([]);
  const [newTagOpened, setNewTagOpened] = useState(false);
  const [newTagName, setNewTagName] = useState('');
  const [creating, setCreating] = useState(false);

  const loadTags = useCallback(async () => {
    try {
      setTags(await libApi.getTags());
    } catch (err) {
      notifications.show({ color: 'red', message: extractErrorMessage(err, '加载标签失败') });
    }
  }, []);

  useEffect(() => {
    loadTags();
  }, [loadTags]);

  // 当前选中分类值：收藏优先于标签，二者皆无则为「全部」
  const value = favorite ? CATEGORY_FAVORITE : tagId ? `tag:${tagId}` : CATEGORY_ALL;

  // 选择分类 → 映射为受控的 favorite / tagId（互斥三态）
  const handleSelect = (v: string) => {
    if (v === CATEGORY_FAVORITE) {
      onFavoriteChange(true);
      onTagIdChange(0);
    } else if (v.startsWith('tag:')) {
      onFavoriteChange(false);
      onTagIdChange(Number(v.slice(4)));
    } else {
      onFavoriteChange(false);
      onTagIdChange(0);
    }
  };

  const handleCreateTag = async () => {
    const name = newTagName.trim();
    if (!name) return;
    setCreating(true);
    try {
      const tag = await libApi.createTag(name);
      setNewTagName('');
      setNewTagOpened(false);
      await loadTags();
      // 新建后直接按该标签分类筛选
      onFavoriteChange(false);
      onTagIdChange(tag.id);
    } catch (err) {
      notifications.show({ color: 'red', message: extractErrorMessage(err, '创建标签失败') });
    } finally {
      setCreating(false);
    }
  };

  return (
    <Group gap="sm">
      <NativeSelect
        aria-label="分类"
        leftSection={<IconCategory size={14} />}
        data={[
          { value: CATEGORY_ALL, label: '全部' },
          { value: CATEGORY_FAVORITE, label: '收藏夹' },
          ...tags.map((t) => ({ value: `tag:${t.id}`, label: t.name })),
        ]}
        value={value}
        onChange={(e) => handleSelect(e.currentTarget.value)}
        size="sm"
        w={200}
      />

      {/* 新建标签：内联展开，避免浮层定位依赖 */}
      <ActionIcon
        variant="light"
        color="purple"
        aria-label="新建标签"
        title="新建标签"
        onClick={() => setNewTagOpened((o) => !o)}
      >
        <IconPlus size={16} />
      </ActionIcon>

      {newTagOpened && (
        <Group gap="xs">
          <TextInput
            placeholder="标签名"
            aria-label="标签名"
            value={newTagName}
            onChange={(e) => setNewTagName(e.currentTarget.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') handleCreateTag();
            }}
            size="sm"
          />
          <Button size="xs" loading={creating} onClick={handleCreateTag}>
            创建
          </Button>
        </Group>
      )}
    </Group>
  );
}
