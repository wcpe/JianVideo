import { useState, useCallback } from 'react';
import { notifications } from '@mantine/notifications';
import { listAlbums, addAlbumItem } from '@/api/albums';
import { getTags, addMediaTag } from '@/api/library';
import { useAuthStore } from '@/stores/auth';
import { extractErrorMessage } from '@/utils/error';
import type { Album, Tag } from '@/types';

/** 批量操作的触发与弹窗状态（FR-91）。三类动作均以选中集为对象（无选中由调用方退化为右键项）。 */
export interface BatchActions {
  /** 打开「加入相册」弹窗，对给定 id 集生效 */
  openAddToAlbum: (ids: number[]) => void;
  /** 打开「打标签」弹窗，对给定 id 集生效 */
  openAddTag: (ids: number[]) => void;
  /** 触发批量打包下载（直接下载，无弹窗） */
  download: (ids: number[]) => void;
  /** 弹窗渲染所需的内部状态与回调，交给 BatchActionsModals */
  modalState: BatchModalState;
}

/** 供 BatchActionsModals 消费的弹窗内部状态 */
export interface BatchModalState {
  albumOpened: boolean;
  albums: Album[];
  loadingAlbums: boolean;
  confirmAlbum: (albumID: number) => Promise<void>;
  closeAlbum: () => void;
  tagOpened: boolean;
  tags: Tag[];
  loadingTags: boolean;
  confirmTag: (tag: { tag_id?: number; name?: string }) => Promise<void>;
  closeTag: () => void;
}

/** 逐项调用单项端点，统计成功/失败计数；幂等去重由后端单项端点保证。 */
async function runEach(
  ids: number[],
  fn: (id: number) => Promise<unknown>,
): Promise<{ ok: number; fail: number }> {
  let ok = 0;
  let fail = 0;
  for (const id of ids) {
    try {
      await fn(id);
      ok++;
    } catch {
      fail++;
    }
  }
  return { ok, fail };
}

/**
 * 批量操作编排（FR-91）：封装加相册 / 打标签弹窗与逐项端点调用、打包下载触发与 toast 计数。
 * 加相册、打标签为纯前端循环复用 FR-40/FR-41 单项端点（零新后端端点）；
 * 打包下载经 fetch 带 Bearer token 拉取后端 zip 流，blob 触发浏览器附件下载（不用 axios，规避其整体超时）。
 */
export function useBatchActions(): BatchActions {
  const [albumOpened, setAlbumOpened] = useState(false);
  const [albums, setAlbums] = useState<Album[]>([]);
  const [loadingAlbums, setLoadingAlbums] = useState(false);
  const [albumTargets, setAlbumTargets] = useState<number[]>([]);

  const [tagOpened, setTagOpened] = useState(false);
  const [tags, setTags] = useState<Tag[]>([]);
  const [loadingTags, setLoadingTags] = useState(false);
  const [tagTargets, setTagTargets] = useState<number[]>([]);

  const openAddToAlbum = useCallback(async (ids: number[]) => {
    if (ids.length === 0) return;
    setAlbumTargets(ids);
    setAlbumOpened(true);
    setLoadingAlbums(true);
    try {
      setAlbums(await listAlbums());
    } catch (err) {
      notifications.show({ color: 'red', message: extractErrorMessage(err, '加载相册失败') });
    } finally {
      setLoadingAlbums(false);
    }
  }, []);

  const confirmAlbum = useCallback(
    async (albumID: number) => {
      setAlbumOpened(false);
      const { ok, fail } = await runEach(albumTargets, (id) => addAlbumItem(albumID, id));
      notifications.show({
        color: fail > 0 ? 'yellow' : 'green',
        message: `已加入相册 ${ok} 项${fail > 0 ? `，失败 ${fail} 项` : ''}`,
      });
    },
    [albumTargets],
  );

  const openAddTag = useCallback(async (ids: number[]) => {
    if (ids.length === 0) return;
    setTagTargets(ids);
    setTagOpened(true);
    setLoadingTags(true);
    try {
      setTags(await getTags());
    } catch (err) {
      notifications.show({ color: 'red', message: extractErrorMessage(err, '加载标签失败') });
    } finally {
      setLoadingTags(false);
    }
  }, []);

  const confirmTag = useCallback(
    async (tag: { tag_id?: number; name?: string }) => {
      setTagOpened(false);
      const { ok, fail } = await runEach(tagTargets, (id) => addMediaTag(id, tag));
      notifications.show({
        color: fail > 0 ? 'yellow' : 'green',
        message: `已打标签 ${ok} 项${fail > 0 ? `，失败 ${fail} 项` : ''}`,
      });
    },
    [tagTargets],
  );

  const download = useCallback(async (ids: number[]) => {
    if (ids.length === 0) return;
    const token = useAuthStore.getState().token;
    try {
      const res = await fetch(`/api/library/media/batch-download?ids=${ids.join(',')}`, {
        headers: token ? { Authorization: `Bearer ${token}` } : undefined,
      });
      if (!res.ok) {
        let msg = '打包下载失败';
        try {
          const body = await res.json();
          if (body?.message) msg = body.message;
        } catch {
          /* 非 JSON 响应，沿用默认文案 */
        }
        notifications.show({ color: 'red', message: msg });
        return;
      }
      const blob = await res.blob();
      const objectUrl = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = objectUrl;
      a.download = '媒体打包.zip';
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(objectUrl);
    } catch (err) {
      notifications.show({ color: 'red', message: extractErrorMessage(err, '打包下载失败') });
    }
  }, []);

  return {
    openAddToAlbum,
    openAddTag,
    download,
    modalState: {
      albumOpened,
      albums,
      loadingAlbums,
      confirmAlbum,
      closeAlbum: () => setAlbumOpened(false),
      tagOpened,
      tags,
      loadingTags,
      confirmTag,
      closeTag: () => setTagOpened(false),
    },
  };
}
