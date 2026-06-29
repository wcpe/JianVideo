import { useState, useEffect } from 'react';
import { Modal, Stack, Button, Text, NativeSelect, TextInput, Group, Loader } from '@mantine/core';
import type { BatchModalState } from '@/hooks/useBatchActions';

interface BatchActionsModalsProps {
  state: BatchModalState;
}

/**
 * 批量操作弹窗（FR-91）：选相册弹窗 + 选/建标签弹窗。
 * 由 useBatchActions 驱动状态，本组件只负责呈现与提交，逐项调用与 toast 计数在 hook 内完成。
 */
export default function BatchActionsModals({ state }: BatchActionsModalsProps) {
  const {
    albumOpened,
    albums,
    loadingAlbums,
    confirmAlbum,
    closeAlbum,
    tagOpened,
    tags,
    loadingTags,
    confirmTag,
    closeTag,
  } = state;

  // 选相册：受控选中的相册 id
  const [albumID, setAlbumID] = useState('');
  // 打标签：选已有标签 id 或新建标签名（二选一）
  const [tagID, setTagID] = useState('');
  const [newTagName, setNewTagName] = useState('');

  // 每次打开重置表单，避免残留上次选择
  useEffect(() => {
    if (albumOpened) setAlbumID(albums[0] ? String(albums[0].id) : '');
  }, [albumOpened, albums]);
  useEffect(() => {
    if (tagOpened) {
      setTagID(tags[0] ? String(tags[0].id) : '');
      setNewTagName('');
    }
  }, [tagOpened, tags]);

  const handleConfirmTag = () => {
    const name = newTagName.trim();
    if (name) {
      confirmTag({ name });
    } else if (tagID) {
      confirmTag({ tag_id: Number(tagID) });
    }
  };

  return (
    <>
      {/* 加入相册 */}
      <Modal opened={albumOpened} onClose={closeAlbum} title="加入相册" centered>
        {loadingAlbums ? (
          <Group justify="center" py="md">
            <Loader size="sm" />
          </Group>
        ) : albums.length === 0 ? (
          <Text c="dimmed" size="sm">
            暂无相册，请先到相册页创建
          </Text>
        ) : (
          <Stack gap="md">
            <NativeSelect
              label="选择相册"
              aria-label="选择相册"
              value={albumID}
              onChange={(e) => setAlbumID(e.currentTarget.value)}
              data={albums.map((a) => ({ value: String(a.id), label: a.name }))}
            />
            <Group justify="flex-end">
              <Button variant="default" onClick={closeAlbum}>
                取消
              </Button>
              <Button disabled={!albumID} onClick={() => confirmAlbum(Number(albumID))}>
                加入
              </Button>
            </Group>
          </Stack>
        )}
      </Modal>

      {/* 打标签 */}
      <Modal opened={tagOpened} onClose={closeTag} title="打标签" centered>
        {loadingTags ? (
          <Group justify="center" py="md">
            <Loader size="sm" />
          </Group>
        ) : (
          <Stack gap="md">
            {tags.length > 0 && (
              <NativeSelect
                label="选择已有标签"
                aria-label="选择已有标签"
                value={tagID}
                onChange={(e) => {
                  setTagID(e.currentTarget.value);
                  setNewTagName('');
                }}
                data={[
                  { value: '', label: '（不选）' },
                  ...tags.map((t) => ({ value: String(t.id), label: t.name })),
                ]}
              />
            )}
            <TextInput
              label="或新建标签"
              aria-label="新建标签名"
              placeholder="输入新标签名"
              value={newTagName}
              onChange={(e) => setNewTagName(e.currentTarget.value)}
            />
            <Group justify="flex-end">
              <Button variant="default" onClick={closeTag}>
                取消
              </Button>
              <Button disabled={!newTagName.trim() && !tagID} onClick={handleConfirmTag}>
                打标签
              </Button>
            </Group>
          </Stack>
        )}
      </Modal>
    </>
  );
}
