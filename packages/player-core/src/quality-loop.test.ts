import { describe, expect, it } from 'vitest';
import {
  PlaybackBackendError,
  PlaybackCore,
  type LoadControlFacet,
  type PlaybackCommandContext,
  type PlaybackEvent,
  type PlaybackQuality,
  type PlaybackSource,
  type QualityFacet,
  type QualityFacetListener,
  type QualityFacetState,
  type QualitySelection,
  type SeekRequest,
} from './index';
import { Deferred, EMPTY_CAPABILITIES, FakePlaybackBackend, createSnapshot } from './test-utils';

const SOURCE_A: PlaybackSource = { id: 'source-a', mode: 'adaptive' };
const SOURCE_B: PlaybackSource = { id: 'source-b', mode: 'adaptive' };
const QUALITY_1080: PlaybackQuality = { bandwidth: 5_000_000, height: 1080, id: '1080-high', label: '1080p' };
const QUALITY_720: PlaybackQuality = { bandwidth: 2_500_000, height: 720, id: '720-high', label: '720p' };
const QUALITY_480_LOW: PlaybackQuality = { bandwidth: 800_000, height: 480, id: '480-low', label: '480p · 800 kbps' };
const QUALITY_480_HIGH: PlaybackQuality = { bandwidth: 1_200_000, height: 480, id: '480-high', label: '480p · 1200 kbps' };
const QUALITY_360: PlaybackQuality = { bandwidth: 500_000, height: 360, id: '360', label: '360p' };
const QUALITIES = [QUALITY_1080, QUALITY_720, QUALITY_480_LOW, QUALITY_480_HIGH, QUALITY_360] as const;

class FakeQualityFacet implements QualityFacet {
  readonly calls: Array<
    | { readonly command: PlaybackCommandContext; readonly maxHeight: number | null; readonly type: 'cap' }
    | { readonly command: PlaybackCommandContext; readonly rate: number; readonly type: 'rate' }
    | { readonly command: PlaybackCommandContext; readonly selection: QualitySelection; readonly type: 'select' }
  > = [];
  capFailure: Error | undefined;
  rateFailure: Error | undefined;
  selectFailure: Error | undefined;
  private readonly listeners = new Set<QualityFacetListener>();
  private state: QualityFacetState = {
    actualQualityId: QUALITY_720.id,
    playbackRate: 1,
    qualities: QUALITIES,
    selection: { mode: 'auto' },
  };

  getState(): QualityFacetState {
    return this.state;
  }

  selectQuality(selection: QualitySelection, command: PlaybackCommandContext): Promise<void> {
    this.calls.push({ command, selection, type: 'select' });
    if (this.selectFailure !== undefined) return Promise.reject(this.selectFailure);
    this.state = { ...this.state, selection };
    this.emit(command);
    return Promise.resolve();
  }

  setAutoQualityCap(maxHeight: number | null, command: PlaybackCommandContext): Promise<void> {
    this.calls.push({ command, maxHeight, type: 'cap' });
    if (this.capFailure !== undefined) return Promise.reject(this.capFailure);
    return Promise.resolve();
  }

  setPlaybackRate(rate: number, command: PlaybackCommandContext): Promise<void> {
    this.calls.push({ command, rate, type: 'rate' });
    if (this.rateFailure !== undefined) return Promise.reject(this.rateFailure);
    this.state = { ...this.state, playbackRate: rate };
    this.emit(command);
    return Promise.resolve();
  }

  subscribe(listener: QualityFacetListener): () => void {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  replaceState(state: Partial<QualityFacetState>, command: PlaybackCommandContext): void {
    this.state = { ...this.state, ...state };
    this.emit(command);
  }

  private emit(command: PlaybackCommandContext): void {
    this.listeners.forEach((listener) => {
      listener(this.state, command);
    });
  }
}

class FakeLoadControlFacet implements LoadControlFacet {
  readonly calls: Array<{ readonly command: PlaybackCommandContext; readonly type: 'start' | 'stop' }> = [];
  startHandler: (() => Promise<void>) | undefined;
  private loading = true;

  getLoadingState(): 'loading' | 'stopped' {
    return this.loading ? 'loading' : 'stopped';
  }

  async startLoading(command: PlaybackCommandContext): Promise<void> {
    this.loading = true;
    this.calls.push({ command, type: 'start' });
    await this.startHandler?.();
  }

  stopLoading(command: PlaybackCommandContext): Promise<void> {
    this.loading = false;
    this.calls.push({ command, type: 'stop' });
    return Promise.resolve();
  }
}

class TrackingBackend extends FakePlaybackBackend {
  readonly seekRequests: SeekRequest[] = [];

  constructor() {
    super();
    this.seekHandler = (request) => {
      this.seekRequests.push(request);
      this.setSnapshot(
        createSnapshot({
          capabilities: availableCapabilities(),
          currentTime: request.targetTime,
          duration: 60,
          playbackRate: this.getSnapshot().playbackRate,
          requestId: request.requestId,
          seekable: [{ end: 60, start: 0 }],
          sourceEpoch: request.sourceEpoch,
          sourceId: request.sourceId,
          state: this.getSnapshot().state,
        }),
      );
      return Promise.resolve({
        clamped: request.requestedTime !== request.targetTime,
        confirmedTime: request.targetTime,
        requestId: request.requestId,
        requestedTime: request.requestedTime,
        status: 'completed' as const,
        targetTime: request.targetTime,
      });
    };
  }
}

function availableCapabilities() {
  return { ...EMPTY_CAPABILITIES, loadControl: 'available' as const, quality: 'available' as const };
}

async function createLoadedCore(options: { qualities?: readonly PlaybackQuality[]; state?: 'paused' | 'playing' } = {}) {
  const backend = new TrackingBackend();
  const quality = new FakeQualityFacet();
  const loadControl = new FakeLoadControlFacet();
  const state = options.state ?? 'paused';
  backend.setSnapshot(
    createSnapshot({
      capabilities: availableCapabilities(),
      currentTime: 10,
      duration: 60,
      seekable: [{ end: 60, start: 0 }],
      sourceId: SOURCE_A.id,
      state,
    }),
  );
  if (options.qualities !== undefined) {
    quality.replaceState({ qualities: options.qualities }, { requestId: 0, sourceEpoch: 0, sourceId: SOURCE_A.id });
  }
  const core = new PlaybackCore({ backend, facets: { loadControl, quality } });
  await core.load(SOURCE_A);
  if (state === 'playing') await core.play();
  else await core.pause();
  return { backend, core, loadControl, quality };
}

function currentCommand(core: PlaybackCore): PlaybackCommandContext {
  const snapshot = core.getSnapshot();
  return {
    requestId: snapshot.requestId,
    sourceEpoch: snapshot.sourceEpoch,
    sourceId: requireSourceId(snapshot.sourceId),
  };
}

function manualQuality(quality: PlaybackQuality): QualitySelection {
  if (quality.height === undefined) throw new Error('测试清晰度缺少高度');
  return {
    mode: 'manual',
    quality: {
      ...(quality.bandwidth === undefined ? {} : { bandwidth: quality.bandwidth }),
      height: quality.height,
    },
  };
}

function requireSourceId(sourceId: string | null): string {
  if (sourceId === null) throw new Error('测试播放源缺少标识');
  return sourceId;
}

async function flushTasks(): Promise<void> {
  await new Promise<void>((resolve) => setTimeout(resolve, 0));
}

describe('PlaybackCore 清晰度与省流量', () => {
  it('默认自动模式枚举清单真实档位并保留实际档位观测', async () => {
    const { core } = await createLoadedCore();

    expect(core.getQualities()).toEqual(QUALITIES);
    expect(core.getQualityState()).toMatchObject({
      actualQuality: QUALITY_720,
      dataSaver: false,
      dataSaverBlocked: false,
      manualQuality: null,
      qualityMode: 'auto',
    });
  });

  it('手动选择使用高度和带宽语义，切回自动不会把实际档位误标为手动', async () => {
    const { core, quality } = await createLoadedCore();

    await expect(core.selectQuality(manualQuality(QUALITY_480_LOW))).resolves.toMatchObject({
      status: 'completed',
    });
    expect(quality.calls.find((call) => call.type === 'select')).toMatchObject({
      selection: { mode: 'manual', quality: { bandwidth: 800_000, height: 480 } },
      type: 'select',
    });
    expect(core.getQualityState()).toMatchObject({ manualQuality: QUALITY_480_LOW, qualityMode: 'manual' });

    await core.selectQuality({ mode: 'auto' });
    expect(core.getQualityState()).toMatchObject({
      actualQuality: QUALITY_720,
      manualQuality: null,
      qualityMode: 'auto',
    });
  });

  it('自动省流量把 ABR 上限锁到 480p 最高可用变体', async () => {
    const { core, quality } = await createLoadedCore();

    await expect(core.setDataSaver(true)).resolves.toMatchObject({ status: 'completed' });

    expect(quality.calls).toContainEqual(expect.objectContaining({ maxHeight: 480, type: 'cap' }));
    expect(core.getQualityState()).toMatchObject({ dataSaver: true, dataSaverBlocked: false });
  });

  it('手动低档开启省流量保持原档，手动高档开启时降到 480p 最高带宽变体', async () => {
    const low = await createLoadedCore();
    await low.core.selectQuality(manualQuality(QUALITY_360));
    await low.core.setDataSaver(true);
    expect(low.core.getQualityState()).toMatchObject({ manualQuality: QUALITY_360, qualityMode: 'manual' });

    const high = await createLoadedCore();
    await high.core.selectQuality(manualQuality(QUALITY_1080));
    await high.core.setDataSaver(true);
    expect(high.core.getQualityState()).toMatchObject({ manualQuality: QUALITY_480_HIGH, qualityMode: 'manual' });
  });

  it('省流量中手动选择高档先清除上限并关闭省流量', async () => {
    const { core, quality } = await createLoadedCore();
    await core.setDataSaver(true);
    quality.calls.length = 0;

    await core.selectQuality(manualQuality(QUALITY_720));

    expect(quality.calls.slice(0, 2).map((call) => call.type)).toEqual(['cap', 'select']);
    expect(quality.calls[0]).toMatchObject({ maxHeight: null });
    expect(core.getQualityState()).toMatchObject({ dataSaver: false, manualQuality: QUALITY_720 });
  });

  it('无 480p 或更低档位时暂停并停止加载，关闭省流量后按原播放意图恢复', async () => {
    const { backend, core, loadControl } = await createLoadedCore({
      qualities: [QUALITY_1080, QUALITY_720],
      state: 'playing',
    });

    await core.setDataSaver(true);
    expect(core.getQualityState()).toMatchObject({ dataSaver: true, dataSaverBlocked: true });
    expect(backend.calls.some((call) => call.method === 'pause')).toBe(true);
    expect(loadControl.calls.at(-1)?.type).toBe('stop');

    await core.setDataSaver(false);
    expect(core.getQualityState()).toMatchObject({ dataSaver: false, dataSaverBlocked: false });
    expect(loadControl.calls.at(-1)?.type).toBe('start');
    expect(backend.calls.at(-1)?.method).toBe('play');

    const resumed = { ...backend.getSnapshot(), currentTime: 12 };
    backend.setSnapshot(resumed);
    backend.emit({
      eventId: 200,
      requestId: resumed.requestId,
      snapshot: resumed,
      sourceEpoch: resumed.sourceEpoch,
      sourceId: requireSourceId(resumed.sourceId),
      type: 'snapshotChanged',
    });
    expect(core.getSnapshot().currentTime).toBe(12);
  });

  it('省流量切换新源时空清单不阻断，后续兼容档位解除已确认阻断', async () => {
    const { backend, core, quality } = await createLoadedCore();
    await core.setDataSaver(true);
    backend.loadHandler = (_source, command) => {
      quality.replaceState({ actualQualityId: null, qualities: [] }, command);
      return Promise.resolve();
    };

    await core.load(SOURCE_B);
    expect(core.getQualityState()).toMatchObject({ dataSaver: true, dataSaverBlocked: false, qualities: [] });

    quality.replaceState({ qualities: [QUALITY_1080, QUALITY_720] }, currentCommand(core));
    await flushTasks();
    expect(core.getQualityState()).toMatchObject({ dataSaver: true, dataSaverBlocked: true });

    quality.replaceState(
      { actualQualityId: QUALITY_480_LOW.id, qualities: [QUALITY_1080, QUALITY_480_LOW] },
      currentCommand(core),
    );
    await flushTasks();
    expect(core.getQualityState()).toMatchObject({
      actualQuality: QUALITY_480_LOW,
      dataSaver: true,
      dataSaverBlocked: false,
    });
    expect(quality.calls.filter((call) => call.type === 'cap').at(-1)).toMatchObject({ maxHeight: 480 });
  });

  it('省流量 stop 停止 HLS 加载且重复停止不重复触发', async () => {
    const { core, loadControl } = await createLoadedCore({ state: 'playing' });
    await core.setDataSaver(true);
    loadControl.calls.length = 0;

    await expect(core.stop()).resolves.toMatchObject({ status: 'completed' });
    expect(loadControl.calls).toEqual([expect.objectContaining({ type: 'stop' })]);

    await expect(core.stop()).resolves.toMatchObject({ status: 'completed' });
    expect(loadControl.calls).toHaveLength(1);
  });

  it('省流量 stop 被同源 play 取代后不停止新播放的加载', async () => {
    const { backend, core, loadControl } = await createLoadedCore({ state: 'playing' });
    await core.setDataSaver(true);
    const pauseGate = new Deferred<void>();
    backend.pauseHandler = () => pauseGate.promise;
    loadControl.calls.length = 0;

    const stop = core.stop();
    await Promise.resolve();
    const play = core.play();

    await expect(stop).resolves.toMatchObject({ status: 'superseded' });
    await expect(play).resolves.toMatchObject({ status: 'completed' });
    pauseGate.resolve();
    await flushTasks();

    expect(loadControl.calls.map((call) => call.type)).toEqual(['start']);
  });

  it('省流量 play 等待启动加载时，后发 pause 取代旧播放意图', async () => {
    const { backend, core, loadControl } = await createLoadedCore();
    await core.setDataSaver(true);
    const startGate = new Deferred<void>();
    loadControl.startHandler = () => startGate.promise;
    backend.calls.length = 0;

    const play = core.play();
    await Promise.resolve();
    const pause = core.pause();
    await expect(pause).resolves.toMatchObject({ status: 'completed' });
    startGate.resolve();

    await expect(play).resolves.toMatchObject({ status: 'superseded' });
    expect(backend.calls.some((call) => call.method === 'play')).toBe(false);
    expect(core.getSnapshot().state).toBe('paused');
  });

  it('清单刷新时按语义重匹配手动档，省流量下无合规档位立即阻断', async () => {
    const { core, quality, loadControl } = await createLoadedCore();
    await core.selectQuality(manualQuality(QUALITY_480_LOW));
    await core.setDataSaver(true);

    quality.replaceState({ qualities: [QUALITY_1080, QUALITY_720] }, currentCommand(core));
    await flushTasks();

    expect(core.getQualityState()).toMatchObject({ dataSaver: true, dataSaverBlocked: true });
    expect(loadControl.calls.at(-1)?.type).toBe('stop');
  });

  it('切换失败保持原核心状态且不破坏播放', async () => {
    const { core, quality } = await createLoadedCore({ state: 'playing' });
    quality.selectFailure = new PlaybackBackendError('media', '目标清晰度切换失败');

    await expect(core.selectQuality(manualQuality(QUALITY_480_HIGH))).resolves.toMatchObject({
      error: { message: '目标清晰度切换失败' },
      status: 'failed',
    });
    expect(core.getQualityState()).toMatchObject({ manualQuality: null, qualityMode: 'auto' });
    expect(core.getSnapshot().state).toBe('playing');
  });
});

describe('PlaybackCore 倍速', () => {
  it('只接受固定七档并仅调用 QualityFacet.setPlaybackRate', async () => {
    const { core, quality } = await createLoadedCore();
    quality.calls.length = 0;

    for (const rate of [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2] as const) {
      await expect(core.setPlaybackRate(rate)).resolves.toMatchObject({ status: 'completed' });
    }
    expect(quality.calls.filter((call) => call.type === 'rate').map((call) => call.rate)).toEqual([
      0.5,
      0.75,
      1,
      1.25,
      1.5,
      1.75,
      2,
    ]);
    await expect(core.setPlaybackRate(2.5)).resolves.toMatchObject({ status: 'unsupported' });
  });

  it('后端拒绝倍速时回退 1x，新媒体也重置 1x', async () => {
    const { core, quality } = await createLoadedCore();
    await core.setPlaybackRate(1.5);
    quality.rateFailure = new PlaybackBackendError('unsupported', '不支持该倍速');

    await expect(core.setPlaybackRate(2)).resolves.toMatchObject({ status: 'unsupported' });
    expect(core.getQualityState().playbackRate).toBe(1);

    quality.rateFailure = undefined;
    await core.setPlaybackRate(1.75);
    await core.load(SOURCE_B);
    expect(core.getQualityState().playbackRate).toBe(1);
  });

  it('同 sourceId 重新加载后恢复原倍速而不是保留后端重置的 1x', async () => {
    const { backend, core, quality } = await createLoadedCore();
    await core.setPlaybackRate(1.5);
    backend.loadHandler = (_source, command) => {
      quality.replaceState({ playbackRate: 1 }, command);
      return Promise.resolve();
    };

    await core.load(SOURCE_A);

    expect(quality.calls.filter((call) => call.type === 'rate').at(-1)).toMatchObject({ rate: 1.5 });
    expect(core.getQualityState().playbackRate).toBe(1.5);
  });
});

describe('PlaybackCore A-B 循环', () => {
  it('A 点单独不启用，合法 B 点启用且最小区间为 0.5 秒', async () => {
    const { backend, core } = await createLoadedCore();
    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 10, duration: 60 });

    await core.setAbLoopA();
    expect(core.getAbLoopState()).toEqual({ a: 10, b: null, enabled: false });

    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 10.49 });
    await expect(core.setAbLoopB()).resolves.toMatchObject({ status: 'unsupported' });
    expect(core.getAbLoopState()).toEqual({ a: 10, b: null, enabled: false });

    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 10.5 });
    await expect(core.setAbLoopB()).resolves.toMatchObject({ status: 'completed' });
    expect(core.getAbLoopState()).toEqual({ a: 10, b: 10.5, enabled: true });
  });

  it('B 非法、时长未知和位置非法时拒绝且不改已有合法区间', async () => {
    const { backend, core } = await createLoadedCore();
    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 5, duration: 60 });
    await core.setAbLoopA();
    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 8 });
    await core.setAbLoopB();

    for (const currentTime of [5, Number.NaN, 61]) {
      backend.setSnapshot({ ...backend.getSnapshot(), currentTime });
      await expect(core.setAbLoopB()).resolves.toMatchObject({ status: 'unsupported' });
      expect(core.getAbLoopState()).toEqual({ a: 5, b: 8, enabled: true });
    }
    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 7, duration: 0 });
    await expect(core.setAbLoopB()).resolves.toMatchObject({ status: 'unsupported' });
    expect(core.getAbLoopState()).toEqual({ a: 5, b: 8, enabled: true });
  });

  it('仅在实际 currentTime 大于等于 B 时以 ab_loop 原因回跳 A', async () => {
    const { backend, core } = await createLoadedCore({ state: 'playing' });
    const backendRequestId = core.getSnapshot().requestId;
    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 5, duration: 60, state: 'playing' });
    await core.setAbLoopA();
    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 8, state: 'playing' });
    await core.setAbLoopB();
    backend.seekRequests.length = 0;
    const completions: Array<Extract<PlaybackEvent, { readonly type: 'seekCompleted' }>> = [];
    core.subscribe((event) => {
      if (event.type === 'seekCompleted') completions.push(event);
    });

    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 7.999, requestId: backendRequestId });
    backend.emit({
      eventId: 100,
      requestId: backendRequestId,
      snapshot: backend.getSnapshot(),
      sourceEpoch: core.getSnapshot().sourceEpoch,
      sourceId: SOURCE_A.id,
      type: 'snapshotChanged',
    });
    await flushTasks();
    expect(backend.seekRequests).toHaveLength(0);

    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 8, requestId: backendRequestId });
    backend.emit({
      eventId: 101,
      requestId: backendRequestId,
      snapshot: backend.getSnapshot(),
      sourceEpoch: core.getSnapshot().sourceEpoch,
      sourceId: SOURCE_A.id,
      type: 'snapshotChanged',
    });
    await flushTasks();
    expect(backend.seekRequests.at(-1)).toMatchObject({ reason: 'ab_loop', targetTime: 5 });
    expect(completions).toHaveLength(1);
    expect(completions[0]).toMatchObject({
      reason: 'ab_loop',
      result: { status: 'completed', targetTime: 5 },
    });
    expect(core.getSnapshot().state).toBe('playing');
  });

  it('B 等于 duration 时原生 ended 不终止循环且回跳后保持播放', async () => {
    const { backend, core } = await createLoadedCore({ state: 'playing' });
    const backendRequestId = core.getSnapshot().requestId;
    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 5, duration: 10, state: 'playing' });
    await core.setAbLoopA();
    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 10, state: 'playing' });
    await core.setAbLoopB();

    const ended = { ...backend.getSnapshot(), currentTime: 10, requestId: backendRequestId, state: 'ended' as const };
    backend.setSnapshot(ended);
    backend.emit({
      eventId: 110,
      requestId: backendRequestId,
      snapshot: ended,
      sourceEpoch: ended.sourceEpoch,
      sourceId: requireSourceId(ended.sourceId),
      type: 'snapshotChanged',
    });
    backend.emit({
      eventId: 111,
      requestId: backendRequestId,
      sourceEpoch: ended.sourceEpoch,
      sourceId: requireSourceId(ended.sourceId),
      type: 'ended',
    });
    await flushTasks();

    expect(backend.seekRequests.at(-1)).toMatchObject({ reason: 'ab_loop', targetTime: 5 });
    expect(core.getSnapshot()).toMatchObject({ currentTime: 5, state: 'playing' });
  });

  it('用户 seek 到 B 后立即回 A，seek 到 A 前不清除区间', async () => {
    const { backend, core } = await createLoadedCore();
    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 10, duration: 60 });
    await core.setAbLoopA();
    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 20 });
    await core.setAbLoopB();

    await core.seek(5, 'user');
    expect(core.getAbLoopState()).toEqual({ a: 10, b: 20, enabled: true });

    await core.seek(25, 'user');
    expect(backend.seekRequests.slice(-2).map((request) => request.reason)).toEqual(['user', 'ab_loop']);
    expect(backend.seekRequests.at(-1)?.targetTime).toBe(10);
  });

  it('媒体解码错误会清除 A-B 区间', async () => {
    const { backend, core } = await createLoadedCore();
    const backendRequestId = core.getSnapshot().requestId;
    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 10, duration: 60 });
    await core.setAbLoopA();
    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 20 });
    await core.setAbLoopB();

    backend.emit({
      error: { category: 'decode', message: '媒体解码失败' },
      eventId: 120,
      requestId: backendRequestId,
      sourceEpoch: core.getSnapshot().sourceEpoch,
      sourceId: SOURCE_A.id,
      type: 'error',
    });

    expect(core.getAbLoopState()).toEqual({ a: null, b: null, enabled: false });
  });

  it('清晰度切换保持区间，清除和切换媒体重置区间', async () => {
    const { backend, core } = await createLoadedCore();
    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 10, duration: 60 });
    await core.setAbLoopA();
    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 20 });
    await core.setAbLoopB();

    await core.selectQuality(manualQuality(QUALITY_480_HIGH));
    expect(core.getAbLoopState()).toEqual({ a: 10, b: 20, enabled: true });

    await core.clearAbLoop();
    expect(core.getAbLoopState()).toEqual({ a: null, b: null, enabled: false });

    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 10, duration: 60 });
    await core.setAbLoopA();
    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 20 });
    await core.setAbLoopB();
    await core.load(SOURCE_B);
    expect(core.getAbLoopState()).toEqual({ a: null, b: null, enabled: false });
  });
});
