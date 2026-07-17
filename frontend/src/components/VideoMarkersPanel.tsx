import { useEffect, useState } from 'react';
import type {
  MediaBookmark,
  MediaBookmarkInput,
  MediaBookmarkUpdate,
  MediaChapter,
} from '../../../packages/media-client/src/index';
import {
  ActionIcon,
  Badge,
  Box,
  Button,
  Divider,
  Drawer,
  Group,
  NumberInput,
  Stack,
  Text,
  Textarea,
  TextInput,
} from '@mantine/core';
import {
  IconBookmark,
  IconBookmarks,
  IconPencil,
  IconPlayerPlay,
  IconPlus,
  IconRefresh,
  IconTrash,
} from '@tabler/icons-react';
import { formatMarkerTime } from '@/components/VideoPlayer.helpers';

const BOOKMARK_TITLE_MAX_LENGTH = 120;
const BOOKMARK_NOTE_MAX_LENGTH = 2000;

type BookmarkDraft = {
  bookmark: MediaBookmark | null;
  note: string;
  positionSeconds: number | string;
  title: string;
};

interface VideoMarkersPanelProps {
  bookmarks: readonly MediaBookmark[];
  chapters: readonly MediaChapter[];
  contextKey: string;
  currentChapter: MediaChapter | null;
  currentTime: number;
  error?: string | null;
  loading?: boolean;
  onCreateBookmark?: (input: MediaBookmarkInput) => Promise<void>;
  onDeleteBookmark?: (bookmarkId: string, revision: number) => Promise<void>;
  onReload?: () => void;
  onSeek: (positionMs: number) => void;
  onUpdateBookmark?: (bookmarkId: string, input: MediaBookmarkUpdate) => Promise<void>;
  stale?: boolean;
}

function emptyDraft(positionMs: number): BookmarkDraft {
  return { bookmark: null, note: '', positionSeconds: positionMs / 1000, title: '' };
}

function draftPositionMs(draft: BookmarkDraft): number | null {
  const seconds = Number(draft.positionSeconds);
  if (!Number.isFinite(seconds) || seconds < 0) return null;
  return Math.round(seconds * 1000);
}

function unicodeLength(value: string): number {
  return Array.from(value).length;
}

export default function VideoMarkersPanel({
  bookmarks,
  chapters,
  contextKey,
  currentChapter,
  currentTime,
  error,
  loading = false,
  onCreateBookmark,
  onDeleteBookmark,
  onReload,
  onSeek,
  onUpdateBookmark,
  stale = false,
}: VideoMarkersPanelProps) {
  const [opened, setOpened] = useState(false);
  const [draft, setDraft] = useState<BookmarkDraft | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<MediaBookmark | null>(null);
  const [saving, setSaving] = useState(false);
  const [positionError, setPositionError] = useState<string | null>(null);
  const [validationError, setValidationError] = useState<string | null>(null);
  const [noteError, setNoteError] = useState<string | null>(null);

  useEffect(() => {
    setOpened(false);
    setDraft(null);
    setDeleteTarget(null);
    setPositionError(null);
    setValidationError(null);
    setNoteError(null);
  }, [contextKey]);

  const startEdit = (item: MediaBookmark) => {
    setDeleteTarget(null);
    setPositionError(null);
    setValidationError(null);
    setNoteError(null);
    setDraft({
      bookmark: item,
      note: item.note ?? '',
      positionSeconds: item.positionMs / 1000,
      title: item.title,
    });
  };

  const submitDraft = async () => {
    if (!draft) return;
    const title = draft.title.trim();
    if (!title) {
      setValidationError('书签标题不能为空');
      return;
    }
    if (unicodeLength(title) > BOOKMARK_TITLE_MAX_LENGTH) {
      setValidationError(`书签标题不能超过 ${BOOKMARK_TITLE_MAX_LENGTH} 个字符`);
      return;
    }
    const positionMs = draftPositionMs(draft);
    if (positionMs === null) {
      setPositionError('书签时间必须是非负数字');
      return;
    }
    const normalizedNote = draft.note.trim();
    if (unicodeLength(normalizedNote) > BOOKMARK_NOTE_MAX_LENGTH) {
      setNoteError(`书签备注不能超过 ${BOOKMARK_NOTE_MAX_LENGTH} 个字符`);
      return;
    }
    const note = normalizedNote || null;
    setSaving(true);
    try {
      if (draft.bookmark && onUpdateBookmark) {
        await onUpdateBookmark(draft.bookmark.id, {
          note,
          positionMs,
          revision: draft.bookmark.revision,
          title,
        });
      } else if (onCreateBookmark) {
        await onCreateBookmark({ note, positionMs, title });
      }
      setDraft(null);
      setPositionError(null);
      setValidationError(null);
      setNoteError(null);
    } catch {
      // 数据层已展示具体错误，保留草稿供用户修正或重试。
    } finally {
      setSaving(false);
    }
  };

  const confirmDelete = async () => {
    if (!deleteTarget || !onDeleteBookmark) return;
    setSaving(true);
    try {
      await onDeleteBookmark(deleteTarget.id, deleteTarget.revision);
      setDeleteTarget(null);
    } catch {
      // 数据层已恢复服务端真源并提示，保留确认区便于用户理解结果。
    } finally {
      setSaving(false);
    }
  };

  const triggerLabel = currentChapter
    ? `章节与书签，当前章节 ${currentChapter.title}`
    : '章节与书签';

  return (
    <>
      <ActionIcon
        variant="subtle"
        color="gray"
        aria-label={triggerLabel}
        onClick={() => setOpened(true)}
      >
        <IconBookmarks size={18} />
      </ActionIcon>
      <Drawer
        opened={opened}
        onClose={() => setOpened(false)}
        position="right"
        size="sm"
        title="章节与书签"
      >
        <Stack gap="md">
          <Group justify="space-between" align="center">
            <Box>
              <Text size="xs" c="dimmed">
                当前章节
              </Text>
              <Text size="sm" fw={600}>
                {currentChapter ? `当前章节：${currentChapter.title}` : '当前章节：无'}
              </Text>
            </Box>
            <Button
              size="xs"
              variant="light"
              leftSection={<IconPlus size={14} />}
              disabled={!onCreateBookmark}
              onClick={() => {
                setDeleteTarget(null);
                setPositionError(null);
                setValidationError(null);
                setNoteError(null);
                setDraft(emptyDraft(Math.round(Math.max(0, currentTime) * 1000)));
              }}
            >
              在当前时间新增书签
            </Button>
          </Group>

          {loading && (
            <Text size="sm" c="dimmed" role="status">
              正在加载章节与书签…
            </Text>
          )}
          {error && (
            <Group gap="xs" justify="space-between" wrap="nowrap">
              <Text size="sm" c="red" role="alert">
                {error}
              </Text>
              {onReload && (
                <Button
                  size="compact-xs"
                  variant="subtle"
                  leftSection={<IconRefresh size={13} />}
                  onClick={onReload}
                >
                  重试
                </Button>
              )}
            </Group>
          )}
          {stale && (
            <Badge color="yellow" variant="light">
              章节数据待刷新
            </Badge>
          )}

          {draft && (
            <Stack
              gap="xs"
              p="sm"
              style={{ border: '1px solid var(--mantine-color-default-border)' }}
            >
              <Text size="xs" c="dimmed">
                {draft.bookmark
                  ? `编辑 ${formatMarkerTime(draftPositionMs(draft) ?? 0)}`
                  : `新建于 ${formatMarkerTime(draftPositionMs(draft) ?? 0)}`}
              </Text>
              <NumberInput
                label="书签时间（秒）"
                value={draft.positionSeconds}
                min={0}
                decimalScale={3}
                error={positionError}
                onChange={(value) => {
                  setPositionError(null);
                  setDraft({ ...draft, positionSeconds: value });
                }}
              />
              <TextInput
                label="书签标题"
                value={draft.title}
                error={validationError}
                onChange={(event) => {
                  setValidationError(null);
                  setDraft({ ...draft, title: event.currentTarget.value });
                }}
              />
              <Textarea
                label="书签备注"
                value={draft.note}
                error={noteError}
                autosize
                minRows={2}
                maxRows={5}
                onChange={(event) => {
                  setNoteError(null);
                  setDraft({ ...draft, note: event.currentTarget.value });
                }}
              />
              <Group justify="flex-end" gap="xs">
                <Button size="xs" variant="subtle" color="gray" onClick={() => setDraft(null)}>
                  取消
                </Button>
                <Button size="xs" loading={saving} onClick={() => void submitDraft()}>
                  {draft.bookmark ? '保存修改' : '保存书签'}
                </Button>
              </Group>
            </Stack>
          )}

          <Divider label={`章节 ${chapters.length}`} labelPosition="left" />
          <Stack gap={4}>
            {chapters.length === 0 && !loading ? (
              <Text size="sm" c="dimmed">
                未检测到内嵌章节
              </Text>
            ) : (
              chapters.map((item) => {
                const active = currentChapter?.id === item.id;
                return (
                  <Button
                    key={item.id}
                    variant={active ? 'light' : 'subtle'}
                    color={active ? 'purple' : 'gray'}
                    justify="space-between"
                    aria-current={active ? 'true' : undefined}
                    aria-label={`跳转到章节 ${item.title}，${formatMarkerTime(item.startMs)}`}
                    onClick={() => onSeek(item.startMs)}
                  >
                    <Text component="span" size="sm" truncate>
                      {item.title}
                    </Text>
                    <Text component="span" size="xs" c="dimmed">
                      {formatMarkerTime(item.startMs)}
                    </Text>
                  </Button>
                );
              })
            )}
          </Stack>

          <Divider label={`书签 ${bookmarks.length}`} labelPosition="left" />
          <Stack gap="xs">
            {bookmarks.length === 0 && !loading ? (
              <Text size="sm" c="dimmed">
                暂无书签
              </Text>
            ) : (
              bookmarks.map((item) => (
                <Box
                  key={item.id}
                  p="xs"
                  style={{ border: '1px solid var(--mantine-color-default-border)' }}
                >
                  <Group justify="space-between" wrap="nowrap" align="flex-start">
                    <Box style={{ minWidth: 0 }}>
                      <Group gap={6} wrap="nowrap">
                        <IconBookmark size={14} />
                        <Text size="sm" fw={600} truncate>
                          {item.title}
                        </Text>
                        <Text size="xs" c="dimmed">
                          {formatMarkerTime(item.positionMs)}
                        </Text>
                      </Group>
                      {item.note && (
                        <Text size="xs" c="dimmed" mt={4}>
                          {item.note}
                        </Text>
                      )}
                    </Box>
                    <Group gap={2} wrap="nowrap">
                      <ActionIcon
                        size="sm"
                        variant="subtle"
                        color="gray"
                        aria-label={`跳转到书签 ${item.title}，${formatMarkerTime(item.positionMs)}`}
                        onClick={() => onSeek(item.positionMs)}
                      >
                        <IconPlayerPlay size={14} />
                      </ActionIcon>
                      <ActionIcon
                        size="sm"
                        variant="subtle"
                        color="gray"
                        aria-label={`编辑书签 ${item.title}`}
                        disabled={!onUpdateBookmark}
                        onClick={() => startEdit(item)}
                      >
                        <IconPencil size={14} />
                      </ActionIcon>
                      <ActionIcon
                        size="sm"
                        variant="subtle"
                        color="red"
                        aria-label={`删除书签 ${item.title}，${formatMarkerTime(item.positionMs)}`}
                        disabled={!onDeleteBookmark}
                        onClick={() => {
                          setDraft(null);
                          setDeleteTarget(item);
                        }}
                      >
                        <IconTrash size={14} />
                      </ActionIcon>
                    </Group>
                  </Group>
                  {deleteTarget?.id === item.id && (
                    <Group justify="space-between" mt="xs" gap="xs" wrap="nowrap">
                      <Text size="xs" c="red">
                        确认删除「{item.title}」{formatMarkerTime(item.positionMs)}？
                      </Text>
                      <Button
                        size="compact-xs"
                        color="red"
                        loading={saving}
                        aria-label={`确认删除书签 ${item.title}`}
                        onClick={() => void confirmDelete()}
                      >
                        确认删除
                      </Button>
                    </Group>
                  )}
                </Box>
              ))
            )}
          </Stack>
        </Stack>
      </Drawer>
    </>
  );
}
