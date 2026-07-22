import type { PlaybackCommandContext } from '@jianvideo/player-core';
import { describe, expect, it, vi } from 'vitest';
import type { TrackResponse } from '@/api/subtitle';
import { WebTrackFacet } from './WebTrackFacet';

const baseResponse: TrackResponse = {
  tracks: [
    {
      available: true,
      capability: 'seamless',
      id: 'sub-a',
      kind: 'subtitle',
      label: '字幕 A',
      source: 'sidecar',
    },
    {
      available: true,
      capability: 'seamless',
      id: 'sub-b',
      kind: 'subtitle',
      label: '字幕 B',
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
    {
      available: true,
      capability: 'reload',
      id: 'audio-c',
      kind: 'audio',
      label: '音轨 C',
      source: 'embedded',
    },
    {
      available: true,
      capability: 'unsupported',
      id: 'audio-x',
      kind: 'audio',
      label: '音轨 X',
      source: 'embedded',
      unsupportedReason: '当前容器不支持该音轨',
    },
  ],
  selection: {
    audio: { effectiveTrackId: 'audio-a', selectedTrackId: 'audio-a' },
    subtitle: { effectiveTrackId: null, selectedTrackId: null },
  },
  backend: {},
  sources: {},
};

function command(requestId = 1, sourceId = 'source-a', sourceEpoch = 1): PlaybackCommandContext {
  return { requestId, sourceEpoch, sourceId };
}

function load(
  facet: WebTrackFacet,
  response = baseResponse,
  sourceId = 'source-a',
  sourceEpoch = 1,
  requestId = 1,
) {
  facet.load({ mediaId: 9, requestId, response, sourceEpoch, sourceId });
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

function audioReload(trackId = 'audio-b') {
  return {
    created: {
      task_id: '81',
      profile_id: `profile-${trackId}`,
      requested_track_id: trackId,
      space_id: 'space-a',
      url: `https://example.test/${trackId}.m3u8`,
    },
    status: {
      available: true,
      profile_id: `profile-${trackId}`,
      url: `https://example.test/${trackId}.m3u8`,
      effective_track_id: trackId,
      task: { id: '81', status: 'succeeded' as const, progress: 100 },
    },
  };
}

describe('WebTrackFacet', () => {
  it('字幕内容成功解析后才确认 effective', async () => {
    let resolveContent!: (value: string) => void;
    const loadContent = vi.fn(() => new Promise<string>((resolve) => (resolveContent = resolve)));
    const facet = new WebTrackFacet(loadContent);
    load(facet);

    const pending = facet.selectTrack('subtitle', 'sub-a', command(2));
    expect(facet.getSelectionState('subtitle')).toMatchObject({
      selectedTrackId: 'sub-a',
      effectiveTrackId: null,
      requestId: 2,
    });
    resolveContent('WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n第一行\n第二行');
    await pending;

    expect(facet.getSelectionState('subtitle')).toMatchObject({
      effectiveTrackId: 'sub-a',
      requestId: 2,
    });
    expect(facet.getCues()).toEqual([{ start: 1, end: 2, text: '第一行\n第二行' }]);
  });

  it('关闭字幕会取消请求并清空 cue', async () => {
    const facet = new WebTrackFacet(async () => 'WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n字幕');
    load(facet);
    await facet.selectTrack('subtitle', 'sub-a', command());

    await facet.selectTrack('subtitle', null, command(2));

    expect(facet.getCues()).toEqual([]);
    expect(facet.getSelectionState('subtitle')).toMatchObject({
      selectedTrackId: null,
      effectiveTrackId: null,
    });
  });

  it('失败时保持旧 effective 并回滚 selected', async () => {
    const loadContent = vi
      .fn<(mediaId: number, trackId: string, signal?: AbortSignal) => Promise<string>>()
      .mockResolvedValueOnce('WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n旧字幕')
      .mockRejectedValueOnce(new Error('内容失败'));
    const facet = new WebTrackFacet(loadContent);
    load(facet);
    await facet.selectTrack('subtitle', 'sub-a', command());

    await expect(facet.selectTrack('subtitle', 'sub-b', command(2))).rejects.toThrow('内容失败');

    expect(facet.getSelectionState('subtitle')).toMatchObject({
      selectedTrackId: 'sub-a',
      effectiveTrackId: 'sub-a',
    });
    expect(facet.getCues()[0]?.text).toBe('旧字幕');
  });

  it('快速切换只接受最后请求，迟到响应不能覆盖', async () => {
    const resolvers = new Map<string, (value: string) => void>();
    const facet = new WebTrackFacet(
      (_mediaId, trackId) => new Promise<string>((resolve) => resolvers.set(trackId, resolve)),
    );
    load(facet);

    const first = facet.selectTrack('subtitle', 'sub-a', command());
    const second = facet.selectTrack('subtitle', 'sub-b', command(2));
    resolvers.get('sub-b')?.('WEBVTT\n\n00:00:02.000 --> 00:00:03.000\n字幕 B');
    await second;
    resolvers.get('sub-a')?.('WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n字幕 A');
    await expect(first).rejects.toBeDefined();

    expect(facet.getSelectionState('subtitle')).toMatchObject({
      effectiveTrackId: 'sub-b',
      requestId: 2,
    });
    expect(facet.getCues()[0]?.text).toBe('字幕 B');
  });

  it('切源后旧响应不能覆盖新源', async () => {
    let resolveOld!: (value: string) => void;
    const facet = new WebTrackFacet(() => new Promise<string>((resolve) => (resolveOld = resolve)));
    load(facet);
    const pending = facet.selectTrack('subtitle', 'sub-a', command());

    load(facet, baseResponse, 'source-b', 2);
    resolveOld('WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n过期字幕');
    await expect(pending).rejects.toBeDefined();

    expect(facet.getCues()).toEqual([]);
    expect(facet.getSelectionState('subtitle').sourceId).toBe('source-b');
  });

  it('恶意标记只作为纯文本 cue 返回', async () => {
    const malicious = '<img src=x onerror=alert(1)>\n<script>alert(1)</script>';
    const facet = new WebTrackFacet(
      async () => `WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n${malicious}`,
    );
    load(facet);

    await facet.selectTrack('subtitle', 'sub-a', command());

    expect(facet.getCues()[0]?.text).toBe(malicious);
  });

  it('刷新清单时稳定 ID 仍存在则保留当前字幕选择和 cue', async () => {
    const facet = new WebTrackFacet(
      async () => 'WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n稳定字幕',
    );
    load(facet);
    await facet.selectTrack('subtitle', 'sub-a', command());

    facet.updateResponse(
      { ...baseResponse, tracks: [...baseResponse.tracks].reverse() },
      command(3),
    );

    expect(facet.getSelectionState('subtitle')).toMatchObject({
      effectiveTrackId: 'sub-a',
      requestId: 3,
    });
    expect(facet.getCues()[0]?.text).toBe('稳定字幕');
  });

  it.each([
    ['移除', baseResponse.tracks.filter((track) => track.id !== 'sub-b')],
    [
      '禁用',
      baseResponse.tracks.map((track) =>
        track.id === 'sub-b' ? { ...track, available: false } : track,
      ),
    ],
  ])('清单刷新%s加载中的字幕时取消请求并阻止迟到内容提交', async (_name, tracks) => {
    const content = deferred<string>();
    const loadContent = vi
      .fn<(_mediaId: number, _trackId: string, signal?: AbortSignal) => Promise<string>>()
      .mockResolvedValueOnce('WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n稳定字幕')
      .mockImplementationOnce((_mediaId, _trackId, signal) => {
        signal?.addEventListener('abort', () =>
          content.reject(new DOMException('已取消', 'AbortError')),
        );
        return content.promise;
      });
    const facet = new WebTrackFacet(loadContent);
    load(facet);
    await facet.selectTrack('subtitle', 'sub-a', command(2));
    const pending = facet.selectTrack('subtitle', 'sub-b', command(3));
    const signal = loadContent.mock.calls[1]?.[2] as AbortSignal;

    facet.updateResponse({ ...baseResponse, tracks }, command(4));
    content.resolve('WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n迟到字幕');

    await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
    expect(signal.aborted).toBe(true);
    expect(facet.getSelectionState('subtitle')).toMatchObject({
      effectiveTrackId: 'sub-a',
      selectedTrackId: 'sub-a',
      requestId: 4,
    });
    expect(facet.getCues()[0]?.text).toBe('稳定字幕');
  });

  it.each([
    ['移除', baseResponse.tracks.filter((track) => track.id !== 'audio-b')],
    [
      '禁用',
      baseResponse.tracks.map((track) =>
        track.id === 'audio-b' ? { ...track, available: false } : track,
      ),
    ],
  ])('清单刷新%s加载中的音轨时取消 HLS 事务并保留稳定已完成选择', async (_name, tracks) => {
    const reload = audioReload();
    const transaction = deferred<void>();
    const switchSource = vi.fn((_url, _spaceId, _command, signal: AbortSignal) => {
      signal.addEventListener('abort', () =>
        transaction.reject(new DOMException('已取消', 'AbortError')),
      );
      return transaction.promise;
    });
    const facet = new WebTrackFacet(
      async () => 'WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n稳定字幕',
      undefined,
      {
        createReload: vi.fn().mockResolvedValue(reload.created),
        getStatus: vi.fn().mockResolvedValue(reload.status),
        switchSource,
      },
    );
    load(facet);
    await facet.selectTrack('subtitle', 'sub-a', command(2));
    const pending = facet.selectTrack('audio', 'audio-b', command(3));
    await vi.waitFor(() => expect(switchSource).toHaveBeenCalledOnce());
    const signal = switchSource.mock.calls[0]?.[3] as AbortSignal;

    facet.updateResponse({ ...baseResponse, tracks }, command(4));
    transaction.resolve();

    await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
    expect(signal.aborted).toBe(true);
    expect(facet.getSelectionState('audio')).toMatchObject({
      effectiveTrackId: 'audio-a',
      selectedTrackId: 'audio-a',
      requestId: 4,
    });
    expect(facet.getSelectionState('subtitle')).toMatchObject({
      effectiveTrackId: 'sub-a',
      selectedTrackId: 'sub-a',
      requestId: 4,
    });
    expect(facet.getCues()[0]?.text).toBe('稳定字幕');
  });

  it('旧清单刷新不能覆盖更新代次的字幕选择', async () => {
    const facet = new WebTrackFacet(
      async () => 'WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n当前字幕',
    );
    load(facet);
    await facet.selectTrack('subtitle', 'sub-a', command(2));

    facet.updateResponse(
      { ...baseResponse, tracks: baseResponse.tracks.filter((track) => track.id !== 'sub-a') },
      command(1),
    );

    expect(facet.getSelectionState('subtitle')).toMatchObject({
      effectiveTrackId: 'sub-a',
      requestId: 2,
    });
    expect(facet.getCues()[0]?.text).toBe('当前字幕');
  });

  it('后端仅返回 selected 音轨时保持 effective 为空', () => {
    const facet = new WebTrackFacet();
    load(facet, {
      ...baseResponse,
      selection: {
        ...baseResponse.selection,
        audio: { effectiveTrackId: null, selectedTrackId: 'audio-a' },
      },
    });

    expect(facet.getSelectionState('audio')).toMatchObject({
      effectiveTrackId: null,
      selectedTrackId: 'audio-a',
    });
  });

  it('音轨成功时先保留旧 effective，源事务完成后才收敛', async () => {
    const reload = audioReload();
    let completeSource!: () => void;
    const switchSource = vi.fn(() => new Promise<void>((resolve) => (completeSource = resolve)));
    const facet = new WebTrackFacet(undefined, undefined, {
      createReload: vi.fn().mockResolvedValue(reload.created),
      getStatus: vi.fn().mockResolvedValue(reload.status),
      switchSource,
    });
    load(facet);

    const pending = facet.selectTrack('audio', 'audio-b', command(2));
    await vi.waitFor(() => expect(switchSource).toHaveBeenCalled());
    expect(facet.getSelectionState('audio')).toMatchObject({
      selectedTrackId: 'audio-b',
      effectiveTrackId: 'audio-a',
    });
    completeSource();
    await pending;

    expect(facet.getSelectionState('audio')).toMatchObject({
      selectedTrackId: 'audio-b',
      effectiveTrackId: 'audio-b',
      requestId: 2,
    });
  });

  it.each([
    ['任务失败', { task: { id: '81', status: 'failed' as const, progress: 20 } }],
    ['目标不匹配', { effective_track_id: 'audio-c' }],
  ])('%s 时完整回滚原 audio 快照', async (_name, patch) => {
    const reload = audioReload();
    const facet = new WebTrackFacet(undefined, undefined, {
      createReload: vi.fn().mockResolvedValue(reload.created),
      getStatus: vi.fn().mockResolvedValue({ ...reload.status, ...patch }),
      switchSource: vi.fn().mockResolvedValue(undefined),
    });
    load(facet);

    await expect(facet.selectTrack('audio', 'audio-b', command(2))).rejects.toThrow();

    expect(facet.getSelectionState('audio')).toMatchObject({
      selectedTrackId: 'audio-a',
      effectiveTrackId: 'audio-a',
      requestId: 2,
    });
  });

  it('轮询超时回滚原 audio 快照', async () => {
    const reload = audioReload();
    const facet = new WebTrackFacet(undefined, undefined, {
      createReload: vi.fn().mockResolvedValue(reload.created),
      getStatus: vi.fn().mockResolvedValue({
        ...reload.status,
        available: false,
        task: { id: '81', status: 'running', progress: 20 },
      }),
      pollIntervalMs: 1,
      pollTimeoutMs: 2,
      switchSource: vi.fn(),
    });
    load(facet);

    await expect(facet.selectTrack('audio', 'audio-b', command(2))).rejects.toThrow('超时');
    expect(facet.getSelectionState('audio')).toMatchObject({
      selectedTrackId: 'audio-a',
      effectiveTrackId: 'audio-a',
    });
  });

  it('B→C 后发胜出，B 迟到不提交且服务端任务不取消', async () => {
    const reloadB = audioReload('audio-b');
    const reloadC = audioReload('audio-c');
    const creates = new Map<string, ReturnType<typeof deferred<typeof reloadB.created>>>();
    const createReload = vi.fn((_mediaId: number, trackId: string, _signal?: AbortSignal) => {
      const request = deferred<typeof reloadB.created>();
      creates.set(trackId, request);
      return request.promise;
    });
    const switchSource = vi.fn().mockResolvedValue(undefined);
    const facet = new WebTrackFacet(undefined, undefined, {
      createReload,
      getStatus: vi.fn((_mediaId, profileId) =>
        Promise.resolve(profileId.endsWith('audio-c') ? reloadC.status : reloadB.status),
      ),
      switchSource,
    });
    load(facet);

    const first = facet.selectTrack('audio', 'audio-b', command(2));
    const firstSignal = createReload.mock.calls[0]?.[2] as AbortSignal;
    const second = facet.selectTrack('audio', 'audio-c', command(3));
    creates.get('audio-c')?.resolve(reloadC.created);
    await second;
    creates.get('audio-b')?.resolve(reloadB.created);
    await expect(first).rejects.toBeDefined();

    expect(firstSignal.aborted).toBe(true);
    expect(facet.getSelectionState('audio')).toMatchObject({
      selectedTrackId: 'audio-c',
      effectiveTrackId: 'audio-c',
    });
    expect(switchSource).toHaveBeenCalledTimes(1);
    expect(switchSource).toHaveBeenCalledWith(
      reloadC.status.url,
      'space-a',
      command(3),
      expect.any(AbortSignal),
    );
  });

  it('音轨切换不取消字幕请求也不清空已有 cue', async () => {
    const subtitle = deferred<string>();
    const reload = audioReload();
    const facet = new WebTrackFacet(() => subtitle.promise, undefined, {
      createReload: vi.fn().mockResolvedValue(reload.created),
      getStatus: vi.fn().mockResolvedValue(reload.status),
      switchSource: vi.fn().mockResolvedValue(undefined),
    });
    load(facet);
    const subtitlePending = facet.selectTrack('subtitle', 'sub-a', command(2));

    await facet.selectTrack('audio', 'audio-b', command(3));
    subtitle.resolve('WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n字幕保留');
    await subtitlePending;

    expect(facet.getCues()[0]?.text).toBe('字幕保留');
    expect(facet.getSelectionState('audio').effectiveTrackId).toBe('audio-b');
  });

  it('保留服务端 reload/unsupported 能力并透传不支持原因', async () => {
    const facet = new WebTrackFacet();
    load(facet);

    expect(facet.getTracks('audio').find((track) => track.id === 'audio-b')).toMatchObject({
      capability: 'reload',
    });
    expect(facet.getTracks('audio').find((track) => track.id === 'audio-x')).toMatchObject({
      capability: 'unsupported',
      unsupportedReason: '当前容器不支持该音轨',
    });
    await expect(facet.selectTrack('audio', 'audio-x', command(2))).rejects.toThrow(
      '当前容器不支持该音轨',
    );
  });
});
