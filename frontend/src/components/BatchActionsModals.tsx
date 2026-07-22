import { useState, useEffect } from 'react';
import { Modal, Stack, Button, Text, NativeSelect, TextInput, Group, Loader } from '@mantine/core';
import type { BatchModalState } from '@/hooks/useBatchActions';

interface BatchActionsModalsProps {
  state: BatchModalState;
}

/**
 * 批量操作弹窗（FR-91 + FR2-053）：相册 / 标签 / 转码预设 / 目标媒体库。
 * 由 useBatchActions 驱动状态，本组件只负责呈现与提交。
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
    transcodeOpened,
    presets,
    loadingPresets,
    confirmTranscode,
    closeTranscode,
    moveOpened,
    libraries,
    loadingLibraries,
    confirmMove,
    closeMove,
  } = state;

  const [albumID, setAlbumID] = useState('');
  const [tagID, setTagID] = useState('');
  const [newTagName, setNewTagName] = useState('');
  const [presetID, setPresetID] = useState('');
  const [libraryID, setLibraryID] = useState('');

  useEffect(() => {
    if (albumOpened) setAlbumID(albums[0] ? String(albums[0].id) : '');
  }, [albumOpened, albums]);
  useEffect(() => {
    if (tagOpened) {
      setTagID(tags[0] ? String(tags[0].id) : '');
      setNewTagName('');
    }
  }, [tagOpened, tags]);
  useEffect(() => {
    if (transcodeOpened) setPresetID(presets[0] ? String(presets[0].id) : '');
  }, [transcodeOpened, presets]);
  useEffect(() => {
    if (moveOpened) setLibraryID(libraries[0] ? String(libraries[0].id) : '');
  }, [moveOpened, libraries]);

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

      <Modal opened={transcodeOpened} onClose={closeTranscode} title="批量转码" centered>
        {loadingPresets ? (
          <Group justify="center" py="md">
            <Loader size="sm" />
          </Group>
        ) : presets.length === 0 ? (
          <Text c="dimmed" size="sm">
            暂无转码预设，请先到转码页创建
          </Text>
        ) : (
          <Stack gap="md">
            <Text size="sm" c="dimmed">
              仅对视频入队转码任务，图片会自动跳过。
            </Text>
            <NativeSelect
              label="选择预设"
              aria-label="选择转码预设"
              value={presetID}
              onChange={(e) => setPresetID(e.currentTarget.value)}
              data={presets.map((p) => ({
                value: String(p.id),
                label: `${p.name}（${p.codec}${p.width || p.height ? ` ${p.width}×${p.height}` : ''}）`,
              }))}
            />
            <Group justify="flex-end">
              <Button variant="default" onClick={closeTranscode}>
                取消
              </Button>
              <Button disabled={!presetID} onClick={() => confirmTranscode(Number(presetID))}>
                入队
              </Button>
            </Group>
          </Stack>
        )}
      </Modal>

      <Modal opened={moveOpened} onClose={closeMove} title="批量移动到媒体库" centered>
        {loadingLibraries ? (
          <Group justify="center" py="md">
            <Loader size="sm" />
          </Group>
        ) : libraries.length === 0 ? (
          <Text c="dimmed" size="sm">
            暂无媒体库
          </Text>
        ) : (
          <Stack gap="md">
            <Text size="sm" c="dimmed">
              仅修改库归属（索引层），不会搬移磁盘上的原文件。
            </Text>
            <NativeSelect
              label="目标媒体库"
              aria-label="目标媒体库"
              value={libraryID}
              onChange={(e) => setLibraryID(e.currentTarget.value)}
              data={libraries.map((l) => ({
                value: String(l.id),
                label: l.label || l.path,
              }))}
            />
            <Group justify="flex-end">
              <Button variant="default" onClick={closeMove}>
                取消
              </Button>
              <Button disabled={!libraryID} onClick={() => confirmMove(Number(libraryID))}>
                移动
              </Button>
            </Group>
          </Stack>
        )}
      </Modal>
    </>
  );
}
