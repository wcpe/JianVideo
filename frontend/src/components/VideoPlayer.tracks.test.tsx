import { MantineProvider } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { TrackResponse } from '@/api/subtitle';
import VideoPlayer from './VideoPlayer';

const api = vi.hoisted(() => ({
  deleteSubtitle: vi.fn(),
  getTrackContent: vi.fn(),
  uploadSubtitle: vi.fn(),
}));

const playApi = vi.hoisted(() => ({
  createAudioReload: vi.fn(),
  getHLSStatus: vi.fn(),
}));

const HLS_INSTANCE_WAIT_TIMEOUT_MS = 5_000;

const hlsHarness = vi.hoisted(() => ({
  autoReady: true,
  instances: [] as Array<{
    emit: (event: string, ...args: unknown[]) => void;
    media: HTMLMediaElement | null;
  }>,
  supported: true,
}));

vi.mock('@/api/subtitle', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/subtitle')>();
  return { ...actual, ...api };
});

vi.mock('@/api/play', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/play')>();
  return { ...actual, ...playApi };
});

vi.mock('hls.js', () => {
  type Handler = (...args: unknown[]) => void;

  class FakeHls {
    static Events = { ERROR: 'error', LEVEL_SWITCHED: 'level', MANIFEST_PARSED: 'manifest' };
    static isSupported() {
      return hlsHarness.supported;
    }
    autoLevelCapping = -1;
    currentLevel = -1;
    handlers = new Map<string, Set<Handler>>();
    levels = [];
    loadingEnabled = false;
    loadSource = vi.fn();
    media: HTMLMediaElement | null = null;
    startLoad = vi.fn(() => {
      this.loadingEnabled = true;
    });
    stopLoad = vi.fn(() => {
      this.loadingEnabled = false;
    });
    attachMedia = vi.fn((media: HTMLMediaElement) => {
      this.media = media;
    });
    destroy = vi.fn();

    constructor() {
      hlsHarness.instances.push(this);
    }

    emit(event: string, ...args: unknown[]) {
      for (const handler of this.handlers.get(event) ?? []) {
        handler(...args);
      }
    }

    off(event: string, handler: Handler) {
      this.handlers.get(event)?.delete(handler);
    }

    on(event: string, handler: Handler) {
      const handlers = this.handlers.get(event) ?? new Set<Handler>();
      handlers.add(handler);
      this.handlers.set(event, handlers);
      if (event === FakeHls.Events.MANIFEST_PARSED && hlsHarness.autoReady) {
        queueMicrotask(() => {
          this.emit(event);
          this.media?.dispatchEvent(new Event('canplay'));
        });
      }
    }
  }
  return { default: FakeHls };
});

vi.mock('mpegts.js', () => ({ default: { createPlayer: () => ({}) } }));

const manifest: TrackResponse = {
  tracks: [
    {
      available: true,
      capability: 'seamless',
      id: 'sub-a',
      kind: 'subtitle',
      label: '字幕 A',
      source: 'uploaded',
    },
    {
      available: true,
      capability: 'reload',
      id: 'audio-a',
      kind: 'audio',
      label: '音轨 A',
      source: 'embedded',
    },
    {
      available: true,
      capability: 'reload',
      id: 'audio-b',
      kind: 'audio',
      label: '音轨 B',
      source: 'embedded',
    },
  ],
  selection: {
    audio: { effectiveTrackId: 'audio-a', selectedTrackId: 'audio-a' },
    subtitle: { effectiveTrackId: null, selectedTrackId: null },
  },
  backend: {},
  sources: {},
};

function playerElement(refresh: () => Promise<TrackResponse>, trackResponse: TrackResponse) {
  return (
    <MantineProvider>
      <VideoPlayer
        url="/api/play/9/stream"
        streamType="mp4"
        autoPlay={false}
        mediaId={9}
        trackResponse={trackResponse}
        onTrackManifestRefresh={refresh}
      />
    </MantineProvider>
  );
}

function renderPlayer(
  refresh = vi.fn(async () => manifest),
  trackResponse: TrackResponse = manifest,
) {
  const view = render(playerElement(refresh, trackResponse));
  return {
    refresh,
    ...view,
    rerenderTrackResponse: (next: TrackResponse) => view.rerender(playerElement(refresh, next)),
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, reject, resolve };
}

function manifestWithSubtitles(...subtitles: Array<{ id: string; label: string }>): TrackResponse {
  return {
    ...manifest,
    tracks: [
      ...subtitles.map((subtitle) => ({ ...manifest.tracks[0], ...subtitle })),
      ...manifest.tracks.filter((track) => track.kind === 'audio'),
    ],
  };
}

describe('VideoPlayer 统一轨道接入', () => {
  beforeEach(() => {
    vi.stubGlobal('MediaSource', { isTypeSupported: () => true });
    hlsHarness.autoReady = true;
    hlsHarness.instances.length = 0;
    hlsHarness.supported = true;
    api.deleteSubtitle.mockReset().mockResolvedValue(undefined);
    api.getTrackContent
      .mockReset()
      .mockResolvedValue('WEBVTT\n\n00:00:01.000 --> 00:00:03.000\n安全字幕');
    api.uploadSubtitle.mockReset().mockResolvedValue(manifest.tracks[0]);
    playApi.createAudioReload.mockReset().mockResolvedValue({
      task_id: '81',
      profile_id: 'audio-b',
      requested_track_id: 'audio-b',
      space_id: 'space-a',
      url: 'https://example.test/audio-b.m3u8',
    });
    playApi.getHLSStatus.mockReset().mockResolvedValue({
      available: true,
      profile_id: 'audio-b',
      url: 'https://example.test/audio-b.m3u8',
      effective_track_id: 'audio-b',
      task: { id: '81', status: 'succeeded', progress: 100 },
    });
    const mediaPrototype = Object.getPrototypeOf(
      Object.getPrototypeOf(document.createElement('video')),
    ) as HTMLMediaElement;
    Object.defineProperty(mediaPrototype, 'play', {
      configurable: true,
      value: vi.fn(() => Promise.resolve()),
    });
    Object.defineProperty(mediaPrototype, 'pause', {
      configurable: true,
      value: vi.fn(),
    });
  });

  it('字幕选择经 core/facet 请求内容并显示纯文本 overlay', async () => {
    renderPlayer();
    await waitFor(() =>
      expect(screen.getByRole('button', { name: '字幕轨道' })).toBeInTheDocument(),
    );
    await userEvent.click(screen.getByRole('button', { name: '字幕轨道' }));
    await userEvent.click(screen.getByText('字幕 A'));

    expect(api.getTrackContent).toHaveBeenCalledWith(9, 'sub-a', expect.any(AbortSignal));
    const video = document.querySelector('video') as HTMLVideoElement;
    video.currentTime = 2;
    fireEvent.timeUpdate(video);
    expect(await screen.findByTestId('subtitle-overlay')).toHaveTextContent('安全字幕');
  });

  it('上传后刷新并选择返回的稳定轨道 ID', async () => {
    const { refresh } = renderPlayer();
    await waitFor(() => expect(screen.getByLabelText('上传字幕文件')).toBeInTheDocument());
    const file = new File(['WEBVTT'], 'sample.vtt', { type: 'text/vtt' });
    fireEvent.change(screen.getByLabelText('上传字幕文件'), { target: { files: [file] } });

    await waitFor(() => expect(api.uploadSubtitle).toHaveBeenCalledWith(9, file));
    await waitFor(() => expect(refresh).toHaveBeenCalled());
    await waitFor(() =>
      expect(api.getTrackContent).toHaveBeenCalledWith(9, 'sub-a', expect.any(AbortSignal)),
    );
  });

  it('两个刷新后发先至时只提交最新清单', async () => {
    const first = deferred<TrackResponse>();
    const second = deferred<TrackResponse>();
    const staleResponse = manifestWithSubtitles({ id: 'sub-old', label: '旧字幕' });
    const latestResponse = manifestWithSubtitles({ id: 'sub-new', label: '新字幕' });
    const refresh = vi
      .fn<() => Promise<TrackResponse>>()
      .mockImplementationOnce(() => first.promise)
      .mockImplementationOnce(() => second.promise);
    api.uploadSubtitle
      .mockResolvedValueOnce(staleResponse.tracks[0])
      .mockResolvedValueOnce(latestResponse.tracks[0]);
    renderPlayer(refresh);
    const input = await screen.findByLabelText('上传字幕文件');

    fireEvent.change(input, {
      target: { files: [new File(['WEBVTT'], 'old.vtt', { type: 'text/vtt' })] },
    });
    await waitFor(() => expect(refresh).toHaveBeenCalledTimes(1));
    fireEvent.change(input, {
      target: { files: [new File(['WEBVTT'], 'new.vtt', { type: 'text/vtt' })] },
    });
    await waitFor(() => expect(refresh).toHaveBeenCalledTimes(2));

    await act(async () => {
      second.resolve(latestResponse);
      await second.promise;
    });
    await waitFor(() =>
      expect(api.getTrackContent).toHaveBeenCalledWith(9, 'sub-new', expect.any(AbortSignal)),
    );
    await act(async () => {
      first.resolve(staleResponse);
      await first.promise;
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    await userEvent.click(screen.getByRole('button', { name: '字幕轨道' }));
    expect(screen.getByText('新字幕')).toBeInTheDocument();
    expect(screen.queryByText('旧字幕')).not.toBeInTheDocument();
  });

  it('旧 prop 清单不覆盖交互刷新结果与新选择', async () => {
    const refreshedResponse = manifestWithSubtitles({ id: 'sub-new', label: '新字幕' });
    const refresh = vi.fn(async () => refreshedResponse);
    api.uploadSubtitle.mockResolvedValueOnce(refreshedResponse.tracks[0]);
    const { rerenderTrackResponse } = renderPlayer(refresh);
    const input = await screen.findByLabelText('上传字幕文件');

    fireEvent.change(input, {
      target: { files: [new File(['WEBVTT'], 'new.vtt', { type: 'text/vtt' })] },
    });
    await waitFor(() =>
      expect(api.getTrackContent).toHaveBeenCalledWith(9, 'sub-new', expect.any(AbortSignal)),
    );

    rerenderTrackResponse({ ...manifest, tracks: [...manifest.tracks] });
    await userEvent.click(screen.getByRole('button', { name: '字幕轨道' }));
    expect(screen.getByRole('menuitem', { name: /新字幕.*当前播放/ })).toBeInTheDocument();
    expect(screen.queryByText('字幕 A')).not.toBeInTheDocument();
  });

  it('上传与删除并发刷新时最终保留最新清单', async () => {
    const uploadRefresh = deferred<TrackResponse>();
    const deleteRefresh = deferred<TrackResponse>();
    const uploadedResponse = manifestWithSubtitles(
      { id: 'sub-a', label: '字幕 A' },
      { id: 'sub-new', label: '新字幕' },
    );
    const deletedResponse = manifestWithSubtitles({ id: 'sub-new', label: '新字幕' });
    const refresh = vi
      .fn<() => Promise<TrackResponse>>()
      .mockImplementationOnce(() => uploadRefresh.promise)
      .mockImplementationOnce(() => deleteRefresh.promise);
    api.uploadSubtitle.mockResolvedValueOnce(uploadedResponse.tracks[1]);
    renderPlayer(refresh);
    const input = await screen.findByLabelText('上传字幕文件');

    fireEvent.change(input, {
      target: { files: [new File(['WEBVTT'], 'new.vtt', { type: 'text/vtt' })] },
    });
    await waitFor(() => expect(refresh).toHaveBeenCalledTimes(1));
    await userEvent.click(screen.getByRole('button', { name: '字幕轨道' }));
    await userEvent.click(screen.getByText('删除 字幕 A'));
    await waitFor(() => expect(api.deleteSubtitle).toHaveBeenCalledWith(9, 'sub-a'));
    await waitFor(() => expect(refresh).toHaveBeenCalledTimes(2));

    await act(async () => {
      deleteRefresh.resolve(deletedResponse);
      await deleteRefresh.promise;
    });
    await act(async () => {
      uploadRefresh.resolve(uploadedResponse);
      await uploadRefresh.promise;
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    await userEvent.click(screen.getByRole('button', { name: '字幕轨道' }));
    expect(screen.getByText('新字幕')).toBeInTheDocument();
    expect(screen.queryByText('字幕 A')).not.toBeInTheDocument();
  });

  it('上传失败显示中文错误通知', async () => {
    api.uploadSubtitle.mockRejectedValueOnce(new Error('网络不可用'));
    const notification = vi.spyOn(notifications, 'show');
    renderPlayer();
    const file = new File(['WEBVTT'], 'sample.vtt', { type: 'text/vtt' });
    fireEvent.change(await screen.findByLabelText('上传字幕文件'), { target: { files: [file] } });

    await waitFor(() =>
      expect(notification).toHaveBeenCalledWith(
        expect.objectContaining({ title: '上传字幕失败', message: '网络不可用', color: 'red' }),
      ),
    );
  });

  it('上传成功但刷新失败时明确提示', async () => {
    const notification = vi.spyOn(notifications, 'show');
    const refresh = vi.fn().mockRejectedValue(new Error('刷新失败'));
    renderPlayer(refresh);
    const file = new File(['WEBVTT'], 'sample.vtt', { type: 'text/vtt' });
    fireEvent.change(await screen.findByLabelText('上传字幕文件'), { target: { files: [file] } });

    await waitFor(() =>
      expect(notification).toHaveBeenCalledWith(
        expect.objectContaining({ title: '已上传但刷新失败', color: 'red' }),
      ),
    );
    expect(api.getTrackContent).not.toHaveBeenCalled();
  });

  it('选择字幕失败时提示并保留可见轨道状态', async () => {
    api.getTrackContent.mockRejectedValueOnce(new Error('字幕内容损坏'));
    const notification = vi.spyOn(notifications, 'show');
    renderPlayer();
    await userEvent.click(await screen.findByRole('button', { name: '字幕轨道' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '字幕 A' }));

    await waitFor(() =>
      expect(notification).toHaveBeenCalledWith(
        expect.objectContaining({ title: '切换字幕失败', message: '字幕内容损坏', color: 'red' }),
      ),
    );
    await userEvent.click(screen.getByRole('button', { name: '字幕轨道' }));
    expect(screen.getByRole('menuitem', { name: /关闭字幕.*当前播放/ })).toBeInTheDocument();
  });

  it('删除 uploaded 轨道前先关闭且删除后刷新', async () => {
    const { refresh } = renderPlayer();
    await waitFor(() =>
      expect(screen.getByRole('button', { name: '字幕轨道' })).toBeInTheDocument(),
    );
    await userEvent.click(screen.getByRole('button', { name: '字幕轨道' }));
    await userEvent.click(screen.getByText('删除 字幕 A'));

    await waitFor(() => expect(api.deleteSubtitle).toHaveBeenCalledWith(9, 'sub-a'));
    expect(refresh).toHaveBeenCalled();
  });

  it('DELETE 失败时恢复先前字幕选择与 cues', async () => {
    api.deleteSubtitle.mockRejectedValueOnce(new Error('删除被拒绝'));
    const notification = vi.spyOn(notifications, 'show');
    renderPlayer();
    await userEvent.click(await screen.findByRole('button', { name: '字幕轨道' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '字幕 A' }));
    await waitFor(() => expect(api.getTrackContent).toHaveBeenCalledTimes(1));
    await userEvent.click(screen.getByRole('button', { name: '字幕轨道' }));
    await userEvent.click(screen.getByText('删除 字幕 A'));

    await waitFor(() => expect(api.getTrackContent).toHaveBeenCalledTimes(2));
    expect(notification).toHaveBeenCalledWith(
      expect.objectContaining({ title: '删除字幕失败', message: '删除被拒绝', color: 'red' }),
    );
    await userEvent.click(screen.getByRole('button', { name: '字幕轨道' }));
    expect(screen.getByRole('menuitem', { name: /字幕 A.*当前播放/ })).toBeInTheDocument();
  });

  it('删除非当前 uploaded 轨道时不关闭当前字幕', async () => {
    const twoTracks: TrackResponse = {
      ...manifest,
      tracks: [
        manifest.tracks[0],
        {
          available: true,
          capability: 'seamless',
          id: 'sub-b',
          kind: 'subtitle',
          label: '字幕 B',
          source: 'uploaded',
        },
        manifest.tracks[1],
      ],
    };
    renderPlayer(
      vi.fn(async () => twoTracks),
      twoTracks,
    );
    await userEvent.click(await screen.findByRole('button', { name: '字幕轨道' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '字幕 A' }));
    await waitFor(() => expect(api.getTrackContent).toHaveBeenCalledTimes(1));
    await userEvent.click(screen.getByRole('button', { name: '字幕轨道' }));
    await userEvent.click(screen.getByText('删除 字幕 B'));
    await waitFor(() => expect(api.deleteSubtitle).toHaveBeenCalledWith(9, 'sub-b'));

    expect(api.getTrackContent).toHaveBeenCalledTimes(1);
    await userEvent.click(screen.getByRole('button', { name: '字幕轨道' }));
    expect(screen.getByRole('menuitem', { name: /字幕 A.*当前播放/ })).toBeInTheDocument();
  });

  it('音轨 HLS 清单与 canplay 均就绪后才收敛 effective，并按创建时 task 查询', async () => {
    hlsHarness.autoReady = false;
    renderPlayer();
    await userEvent.click(await screen.findByRole('button', { name: '音轨' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '音轨 B' }));
    await waitFor(() => expect(hlsHarness.instances).toHaveLength(1), {
      timeout: HLS_INSTANCE_WAIT_TIMEOUT_MS,
    });

    await userEvent.click(screen.getByRole('button', { name: '音轨' }));
    expect(screen.getByRole('menuitem', { name: /音轨 A.*当前播放/ })).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByRole('menuitem', { name: /音轨 B · 切换中/ })).toBeInTheDocument(),
    );

    const hls = hlsHarness.instances[0];
    act(() => hls.emit('manifest'));
    expect(screen.getByRole('menuitem', { name: /音轨 A.*当前播放/ })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: /音轨 B · 切换中/ })).toBeInTheDocument();

    act(() => hls.media?.dispatchEvent(new Event('canplay')));
    await waitFor(() =>
      expect(screen.getByRole('menuitem', { name: /音轨 B.*当前播放/ })).toBeInTheDocument(),
    );
    expect(playApi.createAudioReload).toHaveBeenCalledWith(9, 'audio-b', expect.any(AbortSignal));
    expect(playApi.getHLSStatus).toHaveBeenCalledWith(9, 'audio-b', '81', expect.any(AbortSignal));
    expect(api.getTrackContent).not.toHaveBeenCalled();
  });

  it('音轨任务失败提示中文错误并恢复旧 current', async () => {
    playApi.getHLSStatus.mockResolvedValueOnce({
      available: false,
      profile_id: 'audio-b',
      url: 'https://example.test/audio-b.m3u8',
      effective_track_id: 'audio-b',
      task: { id: '81', status: 'failed', progress: 30 },
    });
    const notification = vi.spyOn(notifications, 'show');
    renderPlayer();
    await userEvent.click(await screen.findByRole('button', { name: '音轨' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '音轨 B' }));

    await waitFor(() =>
      expect(notification).toHaveBeenCalledWith(
        expect.objectContaining({
          title: '切换音轨失败',
          message: '音轨版本生成失败',
          color: 'red',
        }),
      ),
    );
    await userEvent.click(screen.getByRole('button', { name: '音轨' }));
    expect(screen.getByRole('menuitem', { name: /音轨 A.*当前播放/ })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: '音轨 B' })).toBeInTheDocument();
  });

  it('HLS manifest 后 canplay 前 fatal 会提示并恢复旧 current', async () => {
    hlsHarness.autoReady = false;
    const notification = vi.spyOn(notifications, 'show');
    renderPlayer();
    await userEvent.click(await screen.findByRole('button', { name: '音轨' }));
    await userEvent.click(screen.getByRole('menuitem', { name: '音轨 B' }));
    await waitFor(() => expect(hlsHarness.instances).toHaveLength(1), {
      timeout: HLS_INSTANCE_WAIT_TIMEOUT_MS,
    });

    act(() => {
      hlsHarness.instances[0].emit('manifest');
      hlsHarness.instances[0].emit('error', 'error', { fatal: true });
    });

    await waitFor(() =>
      expect(notification).toHaveBeenCalledWith(
        expect.objectContaining({ title: '切换音轨失败', color: 'red' }),
      ),
    );
    await userEvent.click(screen.getByRole('button', { name: '音轨' }));
    expect(screen.getByRole('menuitem', { name: /音轨 A.*当前播放/ })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: '音轨 B' })).toBeInTheDocument();
  });

  it('浏览器无 MediaSource 时显式降级为 unsupported 且不请求 reload', async () => {
    vi.stubGlobal('MediaSource', undefined);
    renderPlayer();
    await userEvent.click(await screen.findByRole('button', { name: '音轨' }));

    expect(
      screen.getByRole('menuitem', { name: /音轨 B · 当前浏览器不支持 HLS 音轨重载/ }),
    ).toBeDisabled();
    expect(playApi.createAudioReload).not.toHaveBeenCalled();
    expect(playApi.getHLSStatus).not.toHaveBeenCalled();
  });
});
