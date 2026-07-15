import { describe, expect, it } from 'vitest';
import {
  SEEK_TIERS,
  PlaybackCore,
  type FrameStepResult,
  type PlaybackSource,
  type SeekResult,
  type SeekTier,
} from './index';
import { Deferred, FakePlaybackBackend, createSnapshot } from './test-utils';

const SOURCE: PlaybackSource = { id: 'tier-source', mode: 'stream' };
const FRAME_TIER: SeekTier = { count: 1, kind: 'frame' };

class TrackingBackend extends FakePlaybackBackend {
  override async seek(request: Parameters<FakePlaybackBackend['seek']>[0]): Promise<SeekResult> {
    const result = await super.seek(request);
    if (result.status === 'completed') {
      this.setSnapshot({
        ...this.getSnapshot(),
        currentTime: result.confirmedTime,
        requestId: request.requestId,
        state: this.getSnapshot().state,
      });
    }
    return result;
  }
}

function createTierCore(tier: SeekTier, currentTime = 50, playbackRate = 1) {
  const backend = new TrackingBackend();
  backend.setSnapshot(createSnapshot({ currentTime, playbackRate, seekable: [{ end: 200, start: 0 }] }));
  const core = new PlaybackCore({ backend, initialSeekTier: tier });
  return { backend, core };
}

describe('PlaybackCore 阶梯 Seek', () => {
  it('只公开六个固定档位', () => {
    expect(SEEK_TIERS).toEqual([
      { count: 1, kind: 'frame' },
      { kind: 'seconds', value: 0.5 },
      { kind: 'seconds', value: 1 },
      { kind: 'seconds', value: 5 },
      { kind: 'seconds', value: 30 },
      { kind: 'seconds', value: 60 },
    ]);
  });

  it('不自行决定默认档位，未设置时 seekByTier 返回 unsupported', async () => {
    const backend = new TrackingBackend();
    const core = new PlaybackCore({ backend });
    await core.load(SOURCE);

    await expect(core.seekByTier('next')).resolves.toMatchObject({ status: 'unsupported' });
    expect(core.getSeekTier()).toBeNull();
    expect(backend.calls.filter(({ method }) => method === 'seek')).toHaveLength(0);
  });

  it('显式初始档位可读，切档不位移且非法档位受控拒绝', async () => {
    const { backend, core } = createTierCore({ kind: 'seconds', value: 5 });
    await core.load(SOURCE);
    backend.setSnapshot(createSnapshot({ currentTime: 42, seekable: [{ end: 200, start: 0 }], sourceId: SOURCE.id }));
    const events: Array<{ readonly requestId: number; readonly tier: SeekTier }> = [];
    core.subscribe((event) => {
      if (event.type === 'seekTierChanged') {
        events.push({ requestId: event.requestId, tier: event.tier });
      }
    });

    expect(core.getSeekTier()).toEqual({ kind: 'seconds', value: 5 });
    const currentTime = core.getSnapshot().currentTime;
    const result = await core.setSeekTier({ kind: 'seconds', value: 30 });
    expect(result).toMatchObject({ status: 'completed' });
    expect(core.getSeekTier()).toEqual({ kind: 'seconds', value: 30 });
    expect(core.getSnapshot().currentTime).toBe(currentTime);
    expect(backend.calls.filter(({ method }) => method === 'seek')).toHaveLength(0);

    const invalid = { kind: 'seconds', value: 2 } as unknown as SeekTier;
    await expect(core.setSeekTier(invalid)).resolves.toMatchObject({ status: 'unsupported' });
    expect(core.getSeekTier()).toEqual({ kind: 'seconds', value: 30 });
    expect(events).toEqual([{ requestId: result.requestId, tier: { kind: 'seconds', value: 30 } }]);
  });

  it.each([0.5, 1, 5, 30, 60] as const)('秒级 %s 档前后完全对称', async (value) => {
    const { backend, core } = createTierCore({ kind: 'seconds', value });
    await core.load(SOURCE);
    backend.setSnapshot(createSnapshot({ currentTime: 80, seekable: [{ end: 200, start: 0 }], sourceId: SOURCE.id }));

    await expect(core.seekByTier('next')).resolves.toMatchObject({ confirmedTime: 80 + value, status: 'completed' });
    await expect(core.seekByTier('previous')).resolves.toMatchObject({ confirmedTime: 80, status: 'completed' });
    expect(core.getSnapshot().currentTime).toBeCloseTo(80, 10);
  });

  it('每次使用后端实际 currentTime 与实时滑动 seekable 夹取', async () => {
    const { backend, core } = createTierCore({ kind: 'seconds', value: 5 });
    await core.load(SOURCE);
    backend.setSnapshot(createSnapshot({ currentTime: 119, seekable: [{ end: 120, start: 100 }], sourceId: SOURCE.id }));

    await expect(core.seekByTier('next')).resolves.toMatchObject({ clamped: true, confirmedTime: 120, targetTime: 120 });
    backend.setSnapshot(createSnapshot({ currentTime: 101, seekable: [{ end: 125, start: 100 }], sourceId: SOURCE.id }));
    await expect(core.seekByTier('previous')).resolves.toMatchObject({ clamped: true, confirmedTime: 100, targetTime: 100 });
  });

  it('已在方向边界时直接完成且不调用后端 seek', async () => {
    const { backend, core } = createTierCore({ kind: 'seconds', value: 60 }, 200);
    await core.load(SOURCE);
    backend.setSnapshot(createSnapshot({ currentTime: 200, seekable: [{ end: 200, start: 10 }], sourceId: SOURCE.id }));
    const callCount = backend.calls.length;

    await expect(core.seekByTier('next')).resolves.toMatchObject({
      clamped: true,
      confirmedTime: 200,
      status: 'completed',
      targetTime: 200,
    });
    expect(backend.calls).toHaveLength(callCount);
  });

  it('阶梯距离只按媒体时间计算，与播放速率无关', async () => {
    const { backend, core } = createTierCore({ kind: 'seconds', value: 5 }, 20, 2);
    await core.load(SOURCE);
    backend.setSnapshot(
      createSnapshot({ currentTime: 20, playbackRate: 2, seekable: [{ end: 200, start: 0 }], sourceId: SOURCE.id }),
    );

    await expect(core.seekByTier('next')).resolves.toMatchObject({ confirmedTime: 25, targetTime: 25 });
  });

  it.each(['playing', 'paused'] as const)('秒级阶梯完成后恢复原 %s 意图', async (state) => {
    const { backend, core } = createTierCore({ kind: 'seconds', value: 1 });
    await core.load(SOURCE);
    if (state === 'playing') {
      await core.play();
    } else {
      await core.pause();
    }
    backend.setSnapshot({ ...backend.getSnapshot(), currentTime: 20, seekable: [{ end: 200, start: 0 }] });

    await core.seekByTier('next');

    expect(core.getSnapshot().state).toBe(state);
  });

  it('快速 50 次秒级意图只允许最终结果回写', async () => {
    const { backend, core } = createTierCore({ kind: 'seconds', value: 5 }, 10);
    const pending = new Deferred<SeekResult>();
    backend.seekHandler = () => pending.promise;
    await core.load(SOURCE);
    backend.setSnapshot(createSnapshot({ currentTime: 10, seekable: [{ end: 200, start: 0 }], sourceId: SOURCE.id }));

    const requests = Array.from({ length: 50 }, (_, index) => core.seekByTier(index === 49 ? 'next' : 'previous'));
    await Promise.resolve();
    await Promise.resolve();
    pending.resolve({
      clamped: false,
      confirmedTime: 15,
      requestId: 51,
      requestedTime: 15,
      status: 'completed',
      targetTime: 15,
    });
    const results = await Promise.all(requests);

    expect(results.slice(0, -1).every(({ status }) => status === 'superseded')).toBe(true);
    expect(results.at(-1)).toMatchObject({ confirmedTime: 15, status: 'completed' });
    expect(core.getSnapshot()).toMatchObject({ currentTime: 15, error: null });
  });

  it('1 frame 档严格复用同一 stepFrame 入口', async () => {
    const backend = new TrackingBackend();
    const core = new PlaybackCore({ backend, initialSeekTier: FRAME_TIER });
    const expected = { direction: 'next', precision: 'unsupported', requestId: 1, status: 'unsupported' } as FrameStepResult;
    let receivedDirection: string | null = null;
    core.stepFrame = (direction) => {
      receivedDirection = direction;
      return Promise.resolve(expected);
    };

    await expect(core.seekByTier('next')).resolves.toBe(expected);
    expect(receivedDirection).toBe('next');
  });
});
