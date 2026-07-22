import { renderHook, act, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';

const showMock = vi.fn();
vi.mock('@mantine/notifications', () => ({
  notifications: { show: (...a: unknown[]) => showMock(...a) },
}));

const listAlbumsMock = vi.fn();
const addAlbumItemMock = vi.fn();
vi.mock('@/api/albums', () => ({
  listAlbums: () => listAlbumsMock(),
  addAlbumItem: (...a: unknown[]) => addAlbumItemMock(...a),
}));

const getTagsMock = vi.fn();
const createTagMock = vi.fn();
const addMediaTagMock = vi.fn();
const getLibraryPathsMock = vi.fn();
const batchTranscodeMock = vi.fn();
const batchMoveMock = vi.fn();
vi.mock('@/api/library', () => ({
  getTags: () => getTagsMock(),
  createTag: (...a: unknown[]) => createTagMock(...a),
  addMediaTag: (...a: unknown[]) => addMediaTagMock(...a),
  getLibraryPaths: () => getLibraryPathsMock(),
  batchTranscodeMediaFiles: (...a: unknown[]) => batchTranscodeMock(...a),
  batchMoveMediaFiles: (...a: unknown[]) => batchMoveMock(...a),
}));

const listPresetsMock = vi.fn();
vi.mock('@/api/transcode', () => ({
  listPresets: () => listPresetsMock(),
}));

import { useBatchActions } from './useBatchActions';

beforeEach(() => {
  showMock.mockClear();
  listAlbumsMock.mockReset();
  addAlbumItemMock.mockReset();
  getTagsMock.mockReset();
  addMediaTagMock.mockReset();
  getLibraryPathsMock.mockReset();
  batchTranscodeMock.mockReset();
  batchMoveMock.mockReset();
  listPresetsMock.mockReset();
});

describe('useBatchActions（FR-91）', () => {
  it('加入相册：逐项调 addAlbumItem，toast 显示成功计数', async () => {
    listAlbumsMock.mockResolvedValue([{ id: 7, name: '相册甲' }]);
    addAlbumItemMock.mockResolvedValue(undefined);
    const { result } = renderHook(() => useBatchActions());

    await act(async () => {
      await result.current.openAddToAlbum([1, 2, 3]);
    });
    await waitFor(() => expect(result.current.modalState.albums).toHaveLength(1));

    await act(async () => {
      await result.current.modalState.confirmAlbum(7);
    });
    expect(addAlbumItemMock).toHaveBeenCalledTimes(3);
    expect(addAlbumItemMock).toHaveBeenCalledWith(7, 1);
    expect(showMock).toHaveBeenCalledWith(
      expect.objectContaining({ color: 'green', message: '已加入相册 3 项' }),
    );
  });

  it('打标签：单项失败计入失败计数，toast 黄色', async () => {
    getTagsMock.mockResolvedValue([{ id: 5, name: '标签甲' }]);
    addMediaTagMock.mockResolvedValueOnce(undefined).mockRejectedValueOnce(new Error('boom'));
    const { result } = renderHook(() => useBatchActions());

    await act(async () => {
      await result.current.openAddTag([1, 2]);
    });
    await act(async () => {
      await result.current.modalState.confirmTag({ tag_id: 5 });
    });

    expect(addMediaTagMock).toHaveBeenCalledTimes(2);
    expect(showMock).toHaveBeenCalledWith(
      expect.objectContaining({ color: 'yellow', message: '已打标签 1 项，失败 1 项' }),
    );
  });

  it('打包下载：fetch 成功时触发 blob 下载', async () => {
    const clickMock = vi.fn();
    const createEl = document.createElement.bind(document);
    vi.spyOn(document, 'createElement').mockImplementation((tag: string) => {
      const el = createEl(tag);
      if (tag === 'a') el.click = clickMock;
      return el;
    });
    globalThis.URL.createObjectURL = vi.fn(() => 'blob:x');
    globalThis.URL.revokeObjectURL = vi.fn();
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      blob: () => Promise.resolve(new Blob(['zip'])),
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useBatchActions());
    await act(async () => {
      await result.current.download([1, 2]);
    });

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/library/media/batch-download?ids=1,2',
      expect.anything(),
    );
    expect(clickMock).toHaveBeenCalled();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('打包下载：后端返回错误时弹红色 toast、不触发下载', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      json: () => Promise.resolve({ message: '选中文件总大小超过上限' }),
    });
    vi.stubGlobal('fetch', fetchMock);

    const { result } = renderHook(() => useBatchActions());
    await act(async () => {
      await result.current.download([1]);
    });

    expect(showMock).toHaveBeenCalledWith(
      expect.objectContaining({ color: 'red', message: '选中文件总大小超过上限' }),
    );
    vi.unstubAllGlobals();
  });

  it('批量转码：确认后调 batchTranscode 并 toast 入队/跳过计数（FR2-053）', async () => {
    listPresetsMock.mockResolvedValue([{ id: 3, name: '1080p', codec: 'h264' }]);
    batchTranscodeMock.mockResolvedValue({ queued: 2, skipped: 1, failed: 0, task_ids: [10, 11] });
    const { result } = renderHook(() => useBatchActions());

    await act(async () => {
      await result.current.openTranscode([1, 2, 3]);
    });
    await waitFor(() => expect(result.current.modalState.presets).toHaveLength(1));

    await act(async () => {
      await result.current.modalState.confirmTranscode(3);
    });
    expect(batchTranscodeMock).toHaveBeenCalledWith([1, 2, 3], 3);
    expect(showMock).toHaveBeenCalledWith(
      expect.objectContaining({ color: 'green', message: '已入队 2 项，跳过 1 项' }),
    );
  });

  it('批量移动：确认后调 batchMove 并 toast（FR2-053）', async () => {
    getLibraryPathsMock.mockResolvedValue([{ id: 9, path: 'D:/lib', label: '片库' }]);
    batchMoveMock.mockResolvedValue({ moved: 2, skipped: 0 });
    const { result } = renderHook(() => useBatchActions());

    await act(async () => {
      await result.current.openMove([1, 2]);
    });
    await waitFor(() => expect(result.current.modalState.libraries).toHaveLength(1));

    await act(async () => {
      await result.current.modalState.confirmMove(9);
    });
    expect(batchMoveMock).toHaveBeenCalledWith([1, 2], 9);
    expect(showMock).toHaveBeenCalledWith(
      expect.objectContaining({
        color: 'green',
        message: expect.stringContaining('已移动 2 项'),
      }),
    );
  });
});
