import { useState, useEffect, useCallback } from 'react';
import { Group, Switch, NativeSelect, ActionIcon, TextInput, Button } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { IconStar, IconTag, IconPlus } from '@tabler/icons-react';
import * as libApi from '@/api/library';
import { extractErrorMessage } from '@/utils/error';
import type { Tag } from '@/types';

interface MediaFilterBarProps {
  // 仅收藏开关
  favorite: boolean;
  onFavoriteChange: (v: boolean) => void;
  // 标签筛选（0 表示不筛选）
  tagId: number;
  onTagIdChange: (id: number) => void;
}

/**
 * 媒体筛选栏（FR-41）：仅收藏开关 + 标签筛选下拉 + 新建标签入口。
 * 标签列表自取自管，新建后刷新下拉。
 */
export default function MediaFilterBar({
  favorite,
  onFavoriteChange,
  tagId,
  onTagIdChange,
}: MediaFilterBarProps) {
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

  const handleCreateTag = async () => {
    const name = newTagName.trim();
    if (!name) return;
    setCreating(true);
    try {
      const tag = await libApi.createTag(name);
      setNewTagName('');
      setNewTagOpened(false);
      await loadTags();
      // 新建后直接按该标签筛选
      onTagIdChange(tag.id);
    } catch (err) {
      notifications.show({ color: 'red', message: extractErrorMessage(err, '创建标签失败') });
    } finally {
      setCreating(false);
    }
  };

  return (
    <Group gap="sm">
      <Switch
        label="仅收藏"
        aria-label="仅收藏"
        checked={favorite}
        onChange={(e) => onFavoriteChange(e.currentTarget.checked)}
        thumbIcon={<IconStar size={10} />}
      />

      <NativeSelect
        aria-label="标签筛选"
        leftSection={<IconTag size={14} />}
        data={[
          { value: '0', label: '全部标签' },
          ...tags.map((t) => ({ value: String(t.id), label: t.name })),
        ]}
        value={String(tagId)}
        onChange={(e) => onTagIdChange(Number(e.currentTarget.value))}
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
