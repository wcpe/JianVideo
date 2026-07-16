import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import {
  PlaybackBackendError,
  PlaybackCore,
  type PlaybackCapabilities,
  createPreviewFacet,
  type PlaybackCommandContext,
  type PlaybackEvent,
  type PlaybackSource,
  type PreparedPreviewTrack,
  type SeekResult,
} from './index';
import { Deferred, EMPTY_CAPABILITIES, FakePlaybackBackend, createSnapshot } from './test-utils';

const SOURCE_A: PlaybackSource = { id: 'source-a', mode: 'stream' };
const SOURCE_B: PlaybackSource = { id: 'source-b', mode: 'adaptive' };
const PREVIEW_TRACK: PreparedPreviewTrack = {
  cues: [
    {
      endTime: 5,
      sprite: { assetId: 'sheet-a', height: 90, width: 160, x: 0, y: 0 },
      startTime: 0,
    },
  ],
  generationId: 'generation-a',
  mediaId: 'media-a',
  profileId: 'profile-a',
  sourceFingerprint: 'fingerprint-a',
};

class ControllableSnapshotBackend extends FakePlaybackBackend {
  snapshotError: Error | null = null;

  override getSnapshot() {
    if (this.snapshotError !== null) {
      throw this.snapshotError;
    }
    return super.getSnapshot();
  }
}

function emitSnapshot(
  backend: FakePlaybackBackend,
  eventId: number,
  currentTime: number,
  sourceEpoch = 1,
  sourceId = SOURCE_A.id,
  requestId = sourceEpoch,
): void {
  backend.emit({
    eventId,
    requestId,
    snapshot: createSnapshot({ currentTime, requestId, sourceEpoch, sourceId, state: 'playing' }),
    sourceEpoch,
    sourceId,
    type: 'snapshotChanged',
  });
}

function completedSeek(requestId: number, targetTime: number): SeekResult {
  return seekResult(requestId, targetTime, { status: 'completed' });
}

function seekResult(
  requestId: number,
  requestedTime: number,
  overrides: Partial<SeekResult>,
): SeekResult {
  return {
    clamped: false,
    confirmedTime: requestedTime,
    requestId,
    requestedTime,
    status: 'completed',
    targetTime: requestedTime,
    ...overrides,
  };
}

describe('PlaybackCore', () => {
  it('完成 load→ready→play→pause 并分配单调 requestId', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    const states: string[] = [];
    core.subscribe((event) => {
      if (event.type === 'snapshotChanged') {
        states.push(event.snapshot.state);
      }
    });

    expect(core.getSnapshot().state).toBe('idle');
    await expect(core.load(SOURCE_A)).resolves.toMatchObject({ requestId: 1, status: 'completed' });
    expect(core.getSnapshot().state).toBe('ready');
    await expect(core.play()).resolves.toMatchObject({ requestId: 2, status: 'completed' });
    expect(core.getSnapshot().state).toBe('playing');
    await expect(core.pause()).resolves.toMatchObject({ requestId: 3, status: 'completed' });
    expect(core.getSnapshot().state).toBe('paused');
    expect(states).toEqual(['loading', 'ready', 'playing', 'paused']);
    expect(backend.calls.map(({ requestId }) => requestId)).toEqual([1, 2, 3]);
  });

  it('持有 PreviewFacet 并通过核心绑定、命中当前源预览轨', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend, facets: { preview: createPreviewFacet() } });
    const load = await core.load(SOURCE_A);
    const command: PlaybackCommandContext = { requestId: load.requestId, sourceEpoch: 1, sourceId: SOURCE_A.id };

    expect(core.setPreviewTrack(PREVIEW_TRACK, command)).toMatchObject({
      generationId: 'generation-a',
      requestId: 1,
      sourceEpoch: 1,
      sourceId: SOURCE_A.id,
      status: 'ready',
    });
    expect(core.getPreviewState()).toMatchObject({ generationId: 'generation-a', status: 'ready' });
    expect(core.hitTestPreview(2, command)).toMatchObject({
      generationId: 'generation-a',
      sprite: { assetId: 'sheet-a' },
    });
  });

  it('切源立即清空 PreviewFacet，旧 source epoch/requestId 不得回写', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend, facets: { preview: createPreviewFacet() } });
    const first = await core.load(SOURCE_A);
    const staleCommand: PlaybackCommandContext = {
      requestId: first.requestId,
      sourceEpoch: 1,
      sourceId: SOURCE_A.id,
    };
    core.setPreviewTrack(PREVIEW_TRACK, staleCommand);

    const second = await core.load(SOURCE_B);
    const currentCommand: PlaybackCommandContext = {
      requestId: second.requestId,
      sourceEpoch: 2,
      sourceId: SOURCE_B.id,
    };

    expect(core.getPreviewState()).toMatchObject({
      generationId: null,
      requestId: second.requestId,
      sourceEpoch: 2,
      sourceId: SOURCE_B.id,
      status: 'empty',
    });
    expect(core.setPreviewTrack(PREVIEW_TRACK, staleCommand)).toMatchObject({
      generationId: null,
      sourceEpoch: 2,
      sourceId: SOURCE_B.id,
    });
    expect(core.hitTestPreview(2, staleCommand)).toBeNull();
    expect(core.hitTestPreview(2, currentCommand)).toBeNull();
  });

  it('未绑定 PreviewFacet 时预览 API 返回 null', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    const load = await core.load(SOURCE_A);
    const command: PlaybackCommandContext = { requestId: load.requestId, sourceEpoch: 1, sourceId: SOURCE_A.id };

    expect(core.setPreviewTrack(PREVIEW_TRACK, command)).toBeNull();
    expect(core.getPreviewState()).toBeNull();
    expect(core.hitTestPreview(2, command)).toBeNull();
  });

  it('将 seek 目标夹取到可 Seek 区间边界', async () => {
    const backend = new FakePlaybackBackend();
    backend.setSnapshot(createSnapshot({ seekable: [{ end: 8, start: 2 }], sourceId: SOURCE_A.id, state: 'ready' }));
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);

    await expect(core.seek(-10)).resolves.toMatchObject({ clamped: true, confirmedTime: 2, targetTime: 2 });
    await expect(core.seek(99)).resolves.toMatchObject({ clamped: true, confirmedTime: 8, targetTime: 8 });
    expect(backend.calls.filter(({ method }) => method === 'seek').map(({ targetTime }) => targetTime)).toEqual([2, 8]);
  });

  it('快速双 seek 以最后意图为准且旧结果不覆盖快照', async () => {
    const backend = new FakePlaybackBackend();
    const firstBackendResult = new Deferred<SeekResult>();
    const secondBackendResult = new Deferred<SeekResult>();
    let seekCount = 0;
    backend.seekHandler = () => {
      seekCount += 1;
      return seekCount === 1 ? firstBackendResult.promise : secondBackendResult.promise;
    };
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);

    const first = core.seek(4);
    await Promise.resolve();
    const second = core.seek(12);
    await expect(first).resolves.toMatchObject({ status: 'superseded' });
    secondBackendResult.resolve(completedSeek(3, 12));
    await expect(second).resolves.toMatchObject({ confirmedTime: 12, status: 'completed' });
    firstBackendResult.resolve(completedSeek(2, 4));
    await Promise.resolve();

    expect(core.getSnapshot()).toMatchObject({ currentTime: 12, requestId: 3 });
  });

  it('同源旧 seek 的高 eventId 快照不得覆盖新命令，当前 requestId 的自发事件仍生效', async () => {
    const backend = new FakePlaybackBackend();
    const pendingSeek = new Deferred<SeekResult>();
    backend.seekHandler = () => pendingSeek.promise;
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);

    const seek = core.seek(8);
    await Promise.resolve();
    await core.pause();
    await expect(seek).resolves.toMatchObject({ requestId: 2, status: 'superseded' });

    backend.emit({ eventId: 49, requestId: 2, sourceEpoch: 1, sourceId: SOURCE_A.id, type: 'ended' });
    expect(core.getSnapshot()).toMatchObject({ currentTime: 0, requestId: 3, state: 'paused' });

    backend.emit({
      eventId: 50,
      requestId: 2,
      snapshot: createSnapshot({ currentTime: 8, requestId: 3, sourceEpoch: 1, sourceId: SOURCE_A.id, state: 'playing' }),
      sourceEpoch: 1,
      sourceId: SOURCE_A.id,
      type: 'snapshotChanged',
    });
    expect(core.getSnapshot()).toMatchObject({ currentTime: 0, requestId: 3, state: 'paused' });

    backend.emit({
      eventId: 51,
      requestId: 3,
      snapshot: createSnapshot({ currentTime: 8, requestId: 2, sourceEpoch: 1, sourceId: SOURCE_A.id, state: 'playing' }),
      sourceEpoch: 1,
      sourceId: SOURCE_A.id,
      type: 'snapshotChanged',
    });
    expect(core.getSnapshot()).toMatchObject({ currentTime: 0, requestId: 3, state: 'paused' });

    emitSnapshot(backend, 52, 1, 1, SOURCE_A.id, 3);
    expect(core.getSnapshot()).toMatchObject({ currentTime: 1, requestId: 3, state: 'playing' });
    pendingSeek.resolve(completedSeek(2, 8));
  });

  it('切源后丢弃旧源状态事件', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);
    await core.load(SOURCE_B);

    backend.emit({
      eventId: 1,
      requestId: 1,
      snapshot: createSnapshot({ currentTime: 50, requestId: 1, sourceEpoch: 1, sourceId: SOURCE_A.id, state: 'playing' }),
      sourceEpoch: 1,
      sourceId: SOURCE_A.id,
      type: 'snapshotChanged',
    });

    expect(core.getSnapshot()).toMatchObject({ currentTime: 0, sourceId: SOURCE_B.id, state: 'ready' });
  });

  it('stop 暂停并回到可定位起点，重复调用不重复触发后端', async () => {
    const backend = new FakePlaybackBackend();
    backend.setSnapshot(
      createSnapshot({ currentTime: 20, sourceId: SOURCE_A.id, state: 'playing' }),
    );
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);

    await expect(core.stop()).resolves.toMatchObject({ requestId: 2, status: 'completed' });
    expect(core.getSnapshot()).toMatchObject({ currentTime: 0, requestId: 2, state: 'ready' });
    expect(backend.calls.slice(1)).toEqual([
      expect.objectContaining({ method: 'pause', requestId: 2, sourceId: SOURCE_A.id }),
      expect.objectContaining({ method: 'seek', requestId: 2, sourceId: SOURCE_A.id, targetTime: 0 }),
    ]);

    await expect(core.stop()).resolves.toMatchObject({ requestId: 3, status: 'completed' });
    expect(core.getSnapshot()).toMatchObject({ currentTime: 0, requestId: 3, state: 'ready' });
    expect(backend.calls).toHaveLength(3);
  });

  it('stop 取代待处理播放且不把受控取消归类为错误', async () => {
    const backend = new FakePlaybackBackend();
    const pendingPlay = new Deferred<void>();
    backend.playHandler = () => pendingPlay.promise;
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);

    const play = core.play();
    await Promise.resolve();
    const stop = core.stop();

    await expect(play).resolves.toMatchObject({ status: 'superseded' });
    await expect(stop).resolves.toMatchObject({ status: 'completed' });
    expect(core.getSnapshot()).toMatchObject({ error: null, state: 'ready' });
    pendingPlay.resolve();
  });

  it('切源会受控取代进行中的 stop，旧源不得继续 seek', async () => {
    const backend = new FakePlaybackBackend();
    const pendingPause = new Deferred<void>();
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);
    await core.play();
    backend.pauseHandler = () => pendingPause.promise;

    const stop = core.stop();
    await Promise.resolve();
    const load = core.load(SOURCE_B);

    await expect(stop).resolves.toMatchObject({ status: 'superseded' });
    await expect(load).resolves.toMatchObject({ status: 'completed' });
    expect(core.getSnapshot()).toMatchObject({ sourceId: SOURCE_B.id, state: 'ready' });
    expect(backend.calls.filter((call) => call.method === 'seek')).toHaveLength(0);
    pendingPause.resolve();
  });

  it('dispose 幂等并让待处理 stop 受控取消', async () => {
    const backend = new FakePlaybackBackend();
    const pendingPause = new Deferred<void>();
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);
    await core.play();
    backend.pauseHandler = () => pendingPause.promise;

    const result = core.stop();
    await Promise.resolve();
    core.dispose();
    core.dispose();

    await expect(result).resolves.toMatchObject({ status: 'canceled' });
    expect(core.getSnapshot().state).toBe('disposed');
    expect(core.getSnapshot().error).toBeNull();
    expect(backend.disposeCount).toBe(1);
    pendingPause.resolve();
  });

  it('stop 在不可定位媒体上只暂停并保留当前位置', async () => {
    const backend = new FakePlaybackBackend();
    backend.setSnapshot(
      createSnapshot({
        capabilities: { ...EMPTY_CAPABILITIES, seek: 'unavailable' },
        currentTime: 20,
        sourceId: SOURCE_A.id,
        state: 'playing',
      }),
    );
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);

    await expect(core.stop()).resolves.toMatchObject({ status: 'completed' });
    expect(core.getSnapshot()).toMatchObject({ currentTime: 20, state: 'ready' });
    expect(backend.calls.filter((call) => call.method === 'seek')).toHaveLength(0);
  });

  it('seekBy 使用后端实时位置并复用核心 seek 的夹取语义', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);
    const current = core.getSnapshot();
    backend.setSnapshot(
      createSnapshot({
        currentTime: 55,
        duration: 60,
        requestId: current.requestId,
        seekable: [{ end: 60, start: 0 }],
        sourceEpoch: current.sourceEpoch,
        sourceId: current.sourceId,
        state: 'playing',
      }),
    );

    await expect(core.seekBy(10)).resolves.toMatchObject({
      confirmedTime: 60,
      requestedTime: 65,
      status: 'completed',
    });
    expect(backend.calls.at(-1)).toMatchObject({ method: 'seek', targetTime: 60 });
  });

  it('seekBy 拒绝不属于当前源的后端实时快照', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);
    const current = core.getSnapshot();
    backend.setSnapshot(
      createSnapshot({
        currentTime: 30,
        requestId: current.requestId,
        sourceEpoch: current.sourceEpoch + 1,
        sourceId: SOURCE_B.id,
        state: 'playing',
      }),
    );
    const seekCalls = backend.calls.filter((call) => call.method === 'seek').length;

    await expect(core.seekBy(10)).resolves.toMatchObject({ status: 'superseded' });
    expect(backend.calls.filter((call) => call.method === 'seek')).toHaveLength(seekCalls);
  });

  it('保留后端错误类别并把未分类异常归为 unknown', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);
    backend.playHandler = () => Promise.reject(new PlaybackBackendError('network', '网络中断'));

    await expect(core.play()).resolves.toMatchObject({ status: 'failed' });
    expect(core.getSnapshot().error).toMatchObject({ category: 'network', message: '网络中断' });

    await core.load(SOURCE_A);
    backend.playHandler = () => Promise.reject(new Error('意外失败'));
    await expect(core.play()).resolves.toMatchObject({ status: 'failed' });
    expect(core.getSnapshot().error).toMatchObject({ category: 'unknown', message: '意外失败' });
  });

  it('受控取代不会被归类为 network 错误', async () => {
    const backend = new FakePlaybackBackend();
    const pending = new Deferred<SeekResult>();
    backend.seekHandler = () => pending.promise;
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);

    const first = core.seek(3);
    const second = core.seek(9);

    await expect(first).resolves.toMatchObject({ status: 'superseded' });
    expect(core.getSnapshot().error).toBeNull();
    core.dispose();
    await expect(second).resolves.toMatchObject({ status: 'canceled' });
    expect(core.getSnapshot().error).toBeNull();
  });

  it('只接受当前源的 capabilitiesChanged 事件', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    const currentCapabilities: PlaybackCapabilities = { ...EMPTY_CAPABILITIES, preview: 'available' };
    const staleCapabilities: PlaybackCapabilities = { ...EMPTY_CAPABILITIES, quality: 'available' };
    await core.load(SOURCE_A);
    await core.load(SOURCE_B);

    backend.emit({
      capabilities: currentCapabilities,
      eventId: 1,
      requestId: 2,
      sourceEpoch: 2,
      sourceId: SOURCE_B.id,
      type: 'capabilitiesChanged',
    });
    expect(core.getSnapshot().capabilities).toEqual(currentCapabilities);

    backend.emit({
      capabilities: staleCapabilities,
      eventId: 99,
      requestId: 1,
      sourceEpoch: 1,
      sourceId: SOURCE_A.id,
      type: 'capabilitiesChanged',
    });
    expect(core.getSnapshot().capabilities).toEqual(currentCapabilities);
  });

  it('将结束和错误事件收敛到状态机', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);

    backend.emit({ eventId: 1, requestId: 1, sourceEpoch: 1, sourceId: SOURCE_A.id, type: 'ended' });
    expect(core.getSnapshot().state).toBe('ended');
    backend.emit({
      error: { category: 'decode', message: '解码失败' },
      eventId: 2,
      requestId: 1,
      sourceEpoch: 1,
      sourceId: SOURCE_A.id,
      type: 'error',
    });
    expect(core.getSnapshot()).toMatchObject({ error: { category: 'decode' }, state: 'error' });
  });

  it('load 期间的 error 事件优先于迟到成功', async () => {
    const backend = new FakePlaybackBackend();
    const pendingLoad = new Deferred<void>();
    const error = { category: 'network' as const, message: '加载中断' };
    backend.loadHandler = () => pendingLoad.promise;
    const core = new PlaybackCore({ backend });

    const load = core.load(SOURCE_A);
    await Promise.resolve();
    backend.emit({ error, eventId: 0, requestId: 1, sourceEpoch: 1, sourceId: SOURCE_A.id, type: 'error' });
    pendingLoad.resolve();

    await expect(load).resolves.toMatchObject({ error, status: 'failed' });
    expect(core.getSnapshot()).toMatchObject({ error, state: 'error' });
  });

  it.each([
    {
      error: { category: 'decode' as const, message: '播放中解码失败' },
      expectedStatus: 'failed' as const,
      expectedState: 'error' as const,
      type: 'error' as const,
    },
    {
      error: null,
      expectedStatus: 'superseded' as const,
      expectedState: 'ended' as const,
      type: 'ended' as const,
    },
  ])('pending play(r2) 期间的 $type(r2) 使用终态事件代次并拒绝旧事件', async (scenario) => {
    const backend = new FakePlaybackBackend();
    const pendingPlay = new Deferred<void>();
    backend.playHandler = () => pendingPlay.promise;
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);
    const terminalSnapshots: Array<{ readonly eventRequestId: number; readonly snapshotRequestId: number }> = [];
    const completions: Array<{ readonly eventRequestId: number; readonly resultRequestId: number }> = [];
    core.subscribe((event) => {
      if (event.type === 'snapshotChanged' && event.snapshot.state === scenario.expectedState) {
        terminalSnapshots.push({ eventRequestId: event.requestId, snapshotRequestId: event.snapshot.requestId });
      }
      if (event.type === 'commandCompleted') {
        completions.push({ eventRequestId: event.requestId, resultRequestId: event.result.requestId });
      }
    });

    const play = core.play();
    await Promise.resolve();
    if (scenario.type === 'error') {
      backend.emit({
        error: scenario.error,
        eventId: 0,
        requestId: 1,
        sourceEpoch: 1,
        sourceId: SOURCE_A.id,
        type: 'error',
      });
    } else {
      backend.emit({ eventId: 0, requestId: 1, sourceEpoch: 1, sourceId: SOURCE_A.id, type: 'ended' });
    }
    expect(core.getSnapshot()).toMatchObject({ requestId: 1, state: 'ready' });
    expect(terminalSnapshots).toEqual([]);

    if (scenario.type === 'error') {
      backend.emit({
        error: scenario.error,
        eventId: 1,
        requestId: 2,
        sourceEpoch: 1,
        sourceId: SOURCE_A.id,
        type: 'error',
      });
    } else {
      backend.emit({ eventId: 1, requestId: 2, sourceEpoch: 1, sourceId: SOURCE_A.id, type: 'ended' });
    }
    pendingPlay.resolve();

    await expect(play).resolves.toMatchObject({ requestId: 2, status: scenario.expectedStatus });
    expect(core.getSnapshot()).toMatchObject({ error: scenario.error, requestId: 2, state: scenario.expectedState });
    expect(terminalSnapshots).toEqual([{ eventRequestId: 2, snapshotRequestId: 2 }]);
    expect(completions).toEqual([{ eventRequestId: 2, resultRequestId: 2 }]);
  });

  it('seek 期间的 error 事件优先于迟到成功', async () => {
    const backend = new FakePlaybackBackend();
    const pendingSeek = new Deferred<SeekResult>();
    const error = { category: 'decode' as const, message: '定位时解码失败' };
    backend.seekHandler = () => pendingSeek.promise;
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);

    const seek = core.seek(10);
    await Promise.resolve();
    backend.emit({ error, eventId: 0, requestId: 2, sourceEpoch: 1, sourceId: SOURCE_A.id, type: 'error' });
    backend.emit({ eventId: 1, requestId: 2, sourceEpoch: 1, sourceId: SOURCE_A.id, type: 'ended' });
    pendingSeek.resolve(completedSeek(2, 10));

    await expect(seek).resolves.toMatchObject({ error, status: 'failed' });
    expect(core.getSnapshot()).toMatchObject({ error, state: 'error' });
  });

  it('ended 后 seek 离开片尾恢复 paused 且不自动播放', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);
    backend.emit({ eventId: 0, requestId: 1, sourceEpoch: 1, sourceId: SOURCE_A.id, type: 'ended' });

    await expect(core.seek(10)).resolves.toMatchObject({ confirmedTime: 10, status: 'completed' });

    expect(core.getSnapshot()).toMatchObject({ currentTime: 10, state: 'paused' });
    expect(backend.calls.map(({ method }) => method)).toEqual(['load', 'seek']);
  });

  it('ended 后 seek 仍在片尾时保持 ended', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);
    backend.emit({ eventId: 0, requestId: 1, sourceEpoch: 1, sourceId: SOURCE_A.id, type: 'ended' });

    await expect(core.seek(60)).resolves.toMatchObject({ confirmedTime: 60, status: 'completed' });

    expect(core.getSnapshot()).toMatchObject({ currentTime: 60, state: 'ended' });
  });

  it('seek 期间 ended 后成功离开片尾恢复原播放意图', async () => {
    const backend = new FakePlaybackBackend();
    const pendingSeek = new Deferred<SeekResult>();
    backend.seekHandler = () => pendingSeek.promise;
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);
    await core.play();

    const seek = core.seek(10);
    await Promise.resolve();
    backend.emit({ eventId: 0, requestId: 3, sourceEpoch: 1, sourceId: SOURCE_A.id, type: 'ended' });
    pendingSeek.resolve(completedSeek(3, 10));

    await expect(seek).resolves.toMatchObject({ confirmedTime: 10, status: 'completed' });
    expect(core.getSnapshot()).toMatchObject({ currentTime: 10, state: 'playing' });
  });

  it('seek 期间 ended 后成功定位片尾仍保持 ended', async () => {
    const backend = new FakePlaybackBackend();
    const pendingSeek = new Deferred<SeekResult>();
    backend.seekHandler = () => pendingSeek.promise;
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);
    await core.play();

    const seek = core.seek(60);
    await Promise.resolve();
    backend.emit({ eventId: 0, requestId: 3, sourceEpoch: 1, sourceId: SOURCE_A.id, type: 'ended' });
    pendingSeek.resolve(completedSeek(3, 60));

    await expect(seek).resolves.toMatchObject({ confirmedTime: 60, status: 'completed' });
    expect(core.getSnapshot()).toMatchObject({ currentTime: 60, state: 'ended' });
  });

  it('同源重载后丢弃旧 sourceEpoch 的生命周期事件', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);
    await core.load(SOURCE_A);

    backend.emit({
      eventId: 99,
      requestId: 1,
      snapshot: createSnapshot({ currentTime: 30, requestId: 1, sourceEpoch: 1, sourceId: SOURCE_A.id, state: 'playing' }),
      sourceEpoch: 1,
      sourceId: SOURCE_A.id,
      type: 'snapshotChanged',
    });

    expect(core.getSnapshot()).toMatchObject({ currentTime: 0, sourceEpoch: 2, state: 'ready' });
  });

  it('丢弃当前 sourceEpoch 内倒退的 eventId', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);

    backend.emit({
      eventId: 2,
      requestId: 1,
      snapshot: createSnapshot({ currentTime: 20, requestId: 1, sourceEpoch: 1, sourceId: SOURCE_A.id, state: 'playing' }),
      sourceEpoch: 1,
      sourceId: SOURCE_A.id,
      type: 'snapshotChanged',
    });
    backend.emit({
      eventId: 1,
      requestId: 1,
      snapshot: createSnapshot({ currentTime: 10, requestId: 1, sourceEpoch: 1, sourceId: SOURCE_A.id, state: 'paused' }),
      sourceEpoch: 1,
      sourceId: SOURCE_A.id,
      type: 'snapshotChanged',
    });

    expect(core.getSnapshot()).toMatchObject({ currentTime: 20, state: 'playing' });
  });

  it('每个 sourceEpoch 都允许首个 eventId 从 0 开始', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);

    emitSnapshot(backend, 0, 10);
    expect(core.getSnapshot().currentTime).toBe(10);

    await core.load(SOURCE_B);
    emitSnapshot(backend, 0, 20, 2, SOURCE_B.id);
    expect(core.getSnapshot()).toMatchObject({ currentTime: 20, sourceEpoch: 2, sourceId: SOURCE_B.id });
  });

  it.each([Number.NaN, Number.POSITIVE_INFINITY, -1, 1.5])(
    '丢弃非法 eventId %s 且不污染后续递增事件',
    async (eventId) => {
      const backend = new FakePlaybackBackend();
      const core = new PlaybackCore({ backend });
      await core.load(SOURCE_A);
      emitSnapshot(backend, 1, 10);

      emitSnapshot(backend, eventId, 99);
      expect(core.getSnapshot().currentTime).toBe(10);
      emitSnapshot(backend, 2, 20);
      expect(core.getSnapshot().currentTime).toBe(20);
      emitSnapshot(backend, 2, 99);
      expect(core.getSnapshot().currentTime).toBe(20);
    },
  );

  it('当前 epoch 的 capabilities、ended、error 携带当前 requestId 时生效', async () => {
    const backend = new FakePlaybackBackend();
    const capabilities: PlaybackCapabilities = { ...EMPTY_CAPABILITIES, tracks: 'available' };
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);
    await core.play();
    await core.pause();

    backend.emit({
      capabilities,
      eventId: 1,
      requestId: 3,
      sourceEpoch: 1,
      sourceId: SOURCE_A.id,
      type: 'capabilitiesChanged',
    });
    expect(core.getSnapshot().capabilities).toEqual(capabilities);
    backend.emit({ eventId: 2, requestId: 3, sourceEpoch: 1, sourceId: SOURCE_A.id, type: 'ended' });
    expect(core.getSnapshot().state).toBe('ended');
    backend.emit({
      error: { category: 'decode', message: '生命周期错误' },
      eventId: 3,
      requestId: 3,
      sourceEpoch: 1,
      sourceId: SOURCE_A.id,
      type: 'error',
    });
    expect(core.getSnapshot()).toMatchObject({ error: { category: 'decode' }, state: 'error' });
  });

  it('快照与命令完成事件顶层 requestId 来自对应嵌套字段', async () => {
    const core = new PlaybackCore({ backend: new FakePlaybackBackend() });
    const events: PlaybackEvent[] = [];
    core.subscribe((event) => {
      events.push(event);
    });

    await core.load(SOURCE_A);

    for (const event of events) {
      expect(Number.isInteger(event.requestId) && event.requestId >= 0).toBe(true);
      if (event.type === 'snapshotChanged') {
        expect(event.requestId).toBe(event.snapshot.requestId);
      } else if (event.type === 'commandCompleted') {
        expect(event.requestId).toBe(event.result.requestId);
      }
    }
  });

  it('能力事件顶层 requestId 来自后端命令上下文', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    const loadResult = await core.load(SOURCE_A);
    let eventRequestId: number | null = null;
    core.subscribe((event) => {
      if (event.type === 'capabilitiesChanged') {
        eventRequestId = event.requestId;
      }
    });

    backend.emit({
      capabilities: { ...EMPTY_CAPABILITIES, preview: 'available' },
      eventId: 0,
      requestId: loadResult.requestId,
      sourceEpoch: 1,
      sourceId: SOURCE_A.id,
      type: 'capabilitiesChanged',
    });

    expect(eventRequestId).toBe(loadResult.requestId);
  });

  it('错误事件顶层 requestId 来自后端命令上下文', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    const loadResult = await core.load(SOURCE_A);
    let eventRequestId: number | null = null;
    core.subscribe((event) => {
      if (event.type === 'error') {
        eventRequestId = event.requestId;
      }
    });

    backend.emit({
      error: { category: 'decode', message: '契约校验错误' },
      eventId: 0,
      requestId: loadResult.requestId,
      sourceEpoch: 1,
      sourceId: SOURCE_A.id,
      type: 'error',
    });

    expect(eventRequestId).toBe(loadResult.requestId);
  });

  it('并发命令的完成事件各自携带对应 requestId 快照', async () => {
    const backend = new FakePlaybackBackend();
    const pendingSeek = new Deferred<SeekResult>();
    backend.seekHandler = () => pendingSeek.promise;
    const core = new PlaybackCore({ backend });
    const completions: Array<Extract<PlaybackEvent, { readonly type: 'commandCompleted' }>> = [];
    core.subscribe((event) => {
      if (event.type === 'commandCompleted') {
        completions.push(event);
      }
    });
    await core.load(SOURCE_A);

    const seek = core.seek(8);
    await Promise.resolve();
    await core.pause();
    await expect(seek).resolves.toMatchObject({ requestId: 2, status: 'superseded' });

    const seekCompletion = completions.find((event) => event.requestId === 2);
    const pauseCompletion = completions.find((event) => event.requestId === 3);
    expect(seekCompletion?.snapshot).toMatchObject({ requestId: 2, state: 'seeking' });
    expect(pauseCompletion?.snapshot).toMatchObject({ requestId: 3, state: 'paused' });
    pendingSeek.resolve(completedSeek(2, 8));
  });

  it('commandCompleted 携带命令发起时的 sourceId 与 sourceEpoch', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    const completions: Array<{ readonly sourceEpoch: number; readonly sourceId: string | null }> = [];
    core.subscribe((event) => {
      if (event.type === 'commandCompleted') {
        completions.push({ sourceEpoch: event.sourceEpoch, sourceId: event.sourceId });
      }
    });

    await core.load(SOURCE_A);
    await core.play();

    expect(completions).toEqual([
      { sourceEpoch: 1, sourceId: SOURCE_A.id },
      { sourceEpoch: 1, sourceId: SOURCE_A.id },
    ]);
  });

  it('idle 状态拒绝 play、pause、seek 且不触碰后端和快照', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    const snapshot = core.getSnapshot();

    await expect(core.play()).resolves.toMatchObject({ status: 'unsupported' });
    await expect(core.pause()).resolves.toMatchObject({ status: 'unsupported' });
    await expect(core.seek(1)).resolves.toMatchObject({ status: 'unsupported' });

    expect(backend.calls).toEqual([]);
    expect(core.getSnapshot()).toBe(snapshot);
  });

  it('loading 与 error 状态拒绝状态命令且不污染进行中的快照', async () => {
    const backend = new FakePlaybackBackend();
    const pendingLoad = new Deferred<void>();
    backend.loadHandler = () => pendingLoad.promise;
    const core = new PlaybackCore({ backend });

    const loading = core.load(SOURCE_A);
    const loadingSnapshot = core.getSnapshot();
    await expect(core.play()).resolves.toMatchObject({ status: 'unsupported' });
    await expect(core.pause()).resolves.toMatchObject({ status: 'unsupported' });
    await expect(core.seek(1)).resolves.toMatchObject({ status: 'unsupported' });
    expect(core.getSnapshot()).toBe(loadingSnapshot);
    expect(backend.calls.map(({ method }) => method)).toEqual(['load']);
    pendingLoad.resolve();
    await loading;

    backend.playHandler = () => Promise.reject(new Error('播放失败'));
    await core.play();
    const errorSnapshot = core.getSnapshot();
    const callCount = backend.calls.length;
    await expect(core.play()).resolves.toMatchObject({ status: 'unsupported' });
    await expect(core.pause()).resolves.toMatchObject({ status: 'unsupported' });
    await expect(core.seek(1)).resolves.toMatchObject({ status: 'unsupported' });
    expect(core.getSnapshot()).toBe(errorSnapshot);
    expect(backend.calls).toHaveLength(callCount);
  });

  it('disposed 状态取消新命令且不再修改终态快照', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    core.dispose();
    const disposedSnapshot = core.getSnapshot();

    await expect(core.play()).resolves.toMatchObject({ status: 'canceled' });
    await expect(core.pause()).resolves.toMatchObject({ status: 'canceled' });
    await expect(core.seek(1)).resolves.toMatchObject({ status: 'canceled' });

    expect(core.getSnapshot()).toBe(disposedSnapshot);
    expect(backend.calls).toEqual([]);
  });

  it('pending seek 后的 pause 成为新意图并阻止旧 seek 回写', async () => {
    const backend = new FakePlaybackBackend();
    const pendingSeek = new Deferred<SeekResult>();
    const pendingPause = new Deferred<void>();
    backend.seekHandler = () => pendingSeek.promise;
    backend.pauseHandler = () => pendingPause.promise;
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);

    const seek = core.seek(8);
    await Promise.resolve();
    const pause = core.pause();
    await expect(seek).resolves.toMatchObject({ requestId: 2, status: 'superseded' });
    expect(core.getSnapshot()).toMatchObject({ requestId: 3, state: 'ready' });
    pendingPause.resolve();
    await expect(pause).resolves.toMatchObject({ requestId: 3, status: 'completed' });
    pendingSeek.resolve(completedSeek(2, 8));
    await Promise.resolve();

    expect(core.getSnapshot()).toMatchObject({ requestId: 3, state: 'paused' });
    expect(backend.calls.map(({ method }) => method)).toEqual(['load', 'seek', 'pause']);
  });

  it('旧 load 晚成功不得覆盖新 load', async () => {
    const backend = new FakePlaybackBackend();
    const pendingLoad = new Deferred<void>();
    backend.loadHandler = (source) => (source.id === SOURCE_A.id ? pendingLoad.promise : Promise.resolve());
    const core = new PlaybackCore({ backend });

    const first = core.load(SOURCE_A);
    await Promise.resolve();
    await core.load(SOURCE_B);
    pendingLoad.resolve();

    await expect(first).resolves.toMatchObject({ status: 'superseded' });
    expect(core.getSnapshot()).toMatchObject({ error: null, sourceEpoch: 2, sourceId: SOURCE_B.id, state: 'ready' });
  });

  it('旧 load 晚失败不得覆盖新 load', async () => {
    const backend = new FakePlaybackBackend();
    const pendingLoad = new Deferred<void>();
    backend.loadHandler = (source) => (source.id === SOURCE_A.id ? pendingLoad.promise : Promise.resolve());
    const core = new PlaybackCore({ backend });

    const first = core.load(SOURCE_A);
    await Promise.resolve();
    await core.load(SOURCE_B);
    pendingLoad.reject(new PlaybackBackendError('network', '旧加载失败'));

    await expect(first).resolves.toMatchObject({ status: 'superseded' });
    expect(core.getSnapshot()).toMatchObject({ error: null, sourceEpoch: 2, sourceId: SOURCE_B.id, state: 'ready' });
  });

  it('play→pause 只允许最新成功结果更新快照', async () => {
    const backend = new FakePlaybackBackend();
    const pendingPlay = new Deferred<void>();
    backend.playHandler = () => pendingPlay.promise;
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);

    const play = core.play();
    await Promise.resolve();
    const pause = core.pause();
    await expect(play).resolves.toMatchObject({ requestId: 2, status: 'superseded' });
    await expect(pause).resolves.toMatchObject({ requestId: 3, status: 'completed' });
    pendingPlay.resolve();
    await Promise.resolve();

    expect(core.getSnapshot()).toMatchObject({ requestId: 3, state: 'paused' });
  });

  it('旧命令晚到的 reject 不得覆盖新命令', async () => {
    const backend = new FakePlaybackBackend();
    const pendingPlay = new Deferred<void>();
    backend.playHandler = () => pendingPlay.promise;
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);

    const play = core.play();
    await Promise.resolve();
    await core.pause();
    pendingPlay.reject(new PlaybackBackendError('network', '旧网络失败'));

    await expect(play).resolves.toMatchObject({ status: 'superseded' });
    await Promise.resolve();
    expect(core.getSnapshot()).toMatchObject({ error: null, requestId: 3, state: 'paused' });
  });

  it('先登记 pending 再惰性启动后端，重入 load 不会晚启动旧 load', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    let reentrantLoad: Promise<unknown> | undefined;
    core.subscribe((event) => {
      if (event.type === 'snapshotChanged' && event.snapshot.sourceId === SOURCE_A.id) {
        reentrantLoad = core.load(SOURCE_B);
      }
    });

    const firstLoad = core.load(SOURCE_A);
    await expect(firstLoad).resolves.toMatchObject({ status: 'superseded' });
    await expect(reentrantLoad).resolves.toMatchObject({ status: 'completed' });

    expect(backend.calls.map(({ method }) => method)).toEqual(['load']);
    expect(core.getSnapshot()).toMatchObject({ sourceId: SOURCE_B.id, state: 'ready' });
  });

  it('发布重入后中止向后续 listener 传播过期快照', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    const laterListenerSources: string[] = [];
    let reentrantLoad: Promise<unknown> | undefined;
    core.subscribe((event) => {
      if (event.type === 'snapshotChanged' && event.snapshot.sourceId === SOURCE_A.id) {
        reentrantLoad = core.load(SOURCE_B);
      }
    });
    core.subscribe((event) => {
      if (event.type === 'snapshotChanged') {
        laterListenerSources.push(`${String(event.snapshot.sourceId)}:${event.snapshot.state}`);
      }
    });

    const firstLoad = core.load(SOURCE_A);
    await expect(firstLoad).resolves.toMatchObject({ status: 'superseded' });
    await expect(reentrantLoad).resolves.toMatchObject({ status: 'completed' });

    expect(laterListenerSources).not.toContain(`${SOURCE_A.id}:loading`);
    expect(laterListenerSources).toContain(`${SOURCE_B.id}:loading`);
  });

  it('捕获后端 operation callback 的同步 throw', async () => {
    class SynchronousThrowBackend extends FakePlaybackBackend {
      override play(): Promise<void> {
        throw new PlaybackBackendError('decode', '同步失败');
      }
    }
    const backend = new SynchronousThrowBackend();
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);

    await expect(core.play()).resolves.toMatchObject({ status: 'failed' });
    expect(core.getSnapshot()).toMatchObject({ error: { category: 'decode' }, state: 'error' });
  });

  it('load 完成后 getSnapshot 同步异常收敛为 failed 并发布完成事件', async () => {
    const backend = new ControllableSnapshotBackend();
    const core = new PlaybackCore({ backend });
    const completions: string[] = [];
    core.subscribe((event) => {
      if (event.type === 'commandCompleted') {
        completions.push(event.result.status);
      }
    });
    backend.snapshotError = new PlaybackBackendError('decode', '快照读取失败');

    await expect(core.load(SOURCE_A)).resolves.toMatchObject({
      error: { category: 'decode', message: '快照读取失败' },
      status: 'failed',
    });
    expect(core.getSnapshot()).toMatchObject({ error: { category: 'decode' }, state: 'error' });
    expect(completions).toEqual(['failed']);
  });

  it('seek 读取实时快照同步异常时不调用后端并进入 error', async () => {
    const backend = new ControllableSnapshotBackend();
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);
    const callCount = backend.calls.length;
    backend.snapshotError = new Error('实时快照失败');

    await expect(core.seek(8)).resolves.toMatchObject({
      error: { category: 'unknown', message: '实时快照失败' },
      status: 'failed',
    });
    expect(backend.calls).toHaveLength(callCount);
    expect(core.getSnapshot()).toMatchObject({ error: { category: 'unknown' }, requestId: 2, state: 'error' });
  });

  it.each(['superseded', 'canceled', 'unsupported'] as const)(
    'seek 后端返回 %s 时保留结果并退出 seeking',
    async (status) => {
      const backend = new FakePlaybackBackend();
      const error = status === 'unsupported' ? { category: 'unsupported' as const, message: '不支持定位' } : undefined;
      backend.seekHandler = (request) =>
        Promise.resolve(
          seekResult(request.requestId, request.requestedTime, {
            clamped: true,
            confirmedTime: 6,
            status,
            targetTime: 6,
            ...(error === undefined ? {} : { error }),
          }),
        );
      const core = new PlaybackCore({ backend });
      await core.load(SOURCE_A);
      await core.pause();

      await expect(core.seek(9)).resolves.toMatchObject({
        clamped: true,
        status,
        targetTime: 6,
        ...(error === undefined ? {} : { error }),
      });
      expect(core.getSnapshot()).toMatchObject({ state: 'paused' });
    },
  );

  it('seek 后端返回 failed 时保留错误并进入 error', async () => {
    const backend = new FakePlaybackBackend();
    const error = { category: 'media' as const, message: '定位失败' };
    backend.seekHandler = (request) =>
      Promise.resolve(
        seekResult(request.requestId, request.requestedTime, {
          clamped: true,
          confirmedTime: 5,
          error,
          status: 'failed',
          targetTime: 5,
        }),
      );
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);

    await expect(core.seek(9)).resolves.toMatchObject({ clamped: true, error, status: 'failed', targetTime: 5 });
    expect(core.getSnapshot()).toMatchObject({ error, state: 'error' });
  });

  it('seek 后端二次夹取结果作为最终真值', async () => {
    const backend = new FakePlaybackBackend();
    backend.seekHandler = (request) =>
      Promise.resolve(
        seekResult(request.requestId, request.requestedTime, {
          clamped: true,
          confirmedTime: 7,
          targetTime: 7,
        }),
      );
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);

    await expect(core.seek(9)).resolves.toMatchObject({ clamped: true, confirmedTime: 7, targetTime: 7 });
    expect(core.getSnapshot().currentTime).toBe(7);
  });

  it('unsupported rejection 让 load 与 seek 退出中间态并保留可见错误', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    backend.loadHandler = () => Promise.reject(new PlaybackBackendError('unsupported', '源不受支持'));

    await expect(core.load(SOURCE_A)).resolves.toMatchObject({ error: { category: 'unsupported' }, status: 'unsupported' });
    expect(core.getSnapshot()).toMatchObject({ error: { category: 'unsupported' }, state: 'error' });

    backend.loadHandler = undefined;
    await core.load(SOURCE_A);
    await core.pause();
    backend.seekHandler = () => Promise.reject(new PlaybackBackendError('unsupported', '无法定位'));
    await expect(core.seek(4)).resolves.toMatchObject({ error: { category: 'unsupported' }, status: 'unsupported' });
    expect(core.getSnapshot()).toMatchObject({ error: { category: 'unsupported' }, state: 'paused' });
  });

  it('seek 使用后端实时 seekable 并规范化区间', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);
    backend.setSnapshot(
      createSnapshot({
        seekable: [
          { end: 12, start: 10 },
          { end: 4, start: 2 },
          { end: 8, start: 4 },
          { end: 1, start: 3 },
          { end: Number.NaN, start: 0 },
        ],
        sourceId: SOURCE_A.id,
        state: 'ready',
      }),
    );

    await expect(core.seek(9)).resolves.toMatchObject({ clamped: true, targetTime: 8 });
    expect(backend.calls.at(-1)).toMatchObject({ method: 'seek', targetTime: 8 });
  });

  it('空 seekable 区间不调用后端并返回 unsupported', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE_A);
    backend.setSnapshot(
      createSnapshot({
        seekable: [
          { end: 1, start: 2 },
          { end: Number.POSITIVE_INFINITY, start: 0 },
        ],
        sourceId: SOURCE_A.id,
        state: 'ready',
      }),
    );
    const callCount = backend.calls.length;

    await expect(core.seek(3)).resolves.toMatchObject({ status: 'unsupported' });
    expect(backend.calls).toHaveLength(callCount);
    expect(core.getSnapshot().state).toBe('ready');
  });

  it.each([Number.NaN, Number.POSITIVE_INFINITY, Number.NEGATIVE_INFINITY])(
    '拒绝非有限 seek 输入 %s 且不调用后端',
    async (requestedTime) => {
      const backend = new FakePlaybackBackend();
      const core = new PlaybackCore({ backend });
      await core.load(SOURCE_A);
      const callCount = backend.calls.length;

      const result = await core.seek(requestedTime);

      expect(result.status).toBe('unsupported');
      expect(Number.isFinite(result.targetTime)).toBe(true);
      expect(backend.calls).toHaveLength(callCount);
    },
  );

  it('listener 异常不阻断其他 listener 与命令结果', async () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    const eventTypes: string[] = [];
    core.subscribe(() => {
      throw new Error('监听器失败');
    });
    core.subscribe((event) => {
      eventTypes.push(event.type);
    });

    await expect(core.load(SOURCE_A)).resolves.toMatchObject({ status: 'completed' });
    expect(eventTypes).toEqual(['snapshotChanged', 'snapshotChanged', 'commandCompleted']);
  });

  it('listener 异常不阻断 dispose 清理且后端只销毁一次', () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    const states: string[] = [];
    core.subscribe(() => {
      throw new Error('监听器失败');
    });
    core.subscribe((event) => {
      if (event.type === 'snapshotChanged') {
        states.push(event.snapshot.state);
      }
    });

    expect(() => {
      core.dispose();
    }).not.toThrow();
    expect(() => {
      core.dispose();
    }).not.toThrow();
    expect(states).toEqual(['disposed']);
    expect(backend.disposeCount).toBe(1);
  });

  it('disposed 后立即回放快照时 listener 异常不外泄', () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    core.dispose();

    expect(() => {
      core.subscribe(() => {
        throw new Error('监听器失败');
      });
    }).not.toThrow();
    expect(backend.disposeCount).toBe(1);
  });

  it('dispose 先发布终态再清理监听器，后订阅者立即得到 disposed', () => {
    const backend = new FakePlaybackBackend();
    const core = new PlaybackCore({ backend });
    const states: string[] = [];
    core.subscribe((event) => {
      if (event.type === 'snapshotChanged') {
        states.push(event.snapshot.state);
      }
    });

    core.dispose();
    backend.emit({ eventId: 99, requestId: 1, sourceEpoch: 1, sourceId: SOURCE_A.id, type: 'ended' });
    core.subscribe((event) => {
      if (event.type === 'snapshotChanged') {
        states.push(`late:${event.snapshot.state}`);
      }
    });

    expect(states).toEqual(['disposed', 'late:disposed']);
  });

  it('backend.dispose 同步异常不能破坏 disposed 终态', () => {
    const backend = new FakePlaybackBackend();
    backend.disposeHandler = () => {
      throw new Error('销毁失败');
    };
    const core = new PlaybackCore({ backend });

    expect(() => {
      core.dispose();
    }).not.toThrow();
    expect(core.getSnapshot()).toMatchObject({ error: null, state: 'disposed' });
    expect(backend.disposeCount).toBe(1);
  });

  it('生产代码不引用端平台、网络或具体播放内核', async () => {
    const directory = new URL('.', import.meta.url);
    const productionFiles = ['types.ts', 'playback-core.ts', 'index.ts'];
    const contents = await Promise.all(
      productionFiles.map((file) => readFile(fileURLToPath(new URL(file, directory)), 'utf8')),
    );

    expect(contents.join('\n')).not.toMatch(/\b(?:DOM|React|fetch|URL|HTMLVideoElement|mpegts|hls)\b/iu);
  });
});
