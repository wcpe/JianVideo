import { describe, expect, it, vi } from 'vitest';
import { Deferred } from './test-utils';
import { WatchStateReporter } from './watch-state-reporter';
import type {
  WatchStateReport,
  WatchStateSendResult,
  WatchStateSnapshot,
  WatchStateTransport,
} from './types';

const INITIAL_STATE: WatchStateSnapshot = {
  completed: false,
  positionSeconds: 0,
  revision: 0,
};

function applied(event: WatchStateReport, revision = event.expectedRevision + 1): WatchStateSendResult {
  return {
    applied: true,
    current: {
      completed: false,
      positionSeconds: event.positionSeconds,
      revision,
    },
    kind: 'applied',
  };
}

function conflict(revision: number, positionSeconds: number): WatchStateSendResult {
  return {
    current: { completed: false, positionSeconds, revision },
    kind: 'conflict',
  };
}

type SendMock = ReturnType<typeof vi.fn<WatchStateTransport['send']>>;

function sentCall(send: SendMock, index: number): Parameters<WatchStateTransport['send']> {
  const call = send.mock.calls[index];
  if (!call) throw new Error(`缺少第 ${String(index + 1)} 次观看状态请求`);
  return call;
}

describe('WatchStateReporter', () => {
  it('同一媒体生命周期使用固定 session 且严格递增 event_seq 与 expected_revision', async () => {
    const send = vi.fn<WatchStateTransport['send']>((event) => Promise.resolve(applied(event)));
    const reporter = new WatchStateReporter({
      getPlaybackState: () => ({ durationSeconds: 120, foreground: true, playing: true, positionSeconds: 20 }),
      initialState: { ...INITIAL_STATE, revision: 4 },
      sessionId: 'session-a',
      transport: { send },
    });

    reporter.report({ eventType: 'progress', positionSeconds: 12, reason: 'system' });
    await reporter.idle();
    reporter.report({ eventType: 'pause', positionSeconds: 18, reason: 'user' });
    await reporter.idle();

    expect(send.mock.calls.map(([event]) => event)).toEqual([
      {
        eventSeq: 1,
        eventType: 'progress',
        expectedRevision: 4,
        positionSeconds: 12,
        reason: 'system',
        sessionId: 'session-a',
      },
      {
        eventSeq: 2,
        eventType: 'pause',
        expectedRevision: 5,
        positionSeconds: 18,
        reason: 'user',
        sessionId: 'session-a',
      },
    ]);
  });

  it('同一时刻只有一个在途请求并把周期 progress 合并为最新位置', async () => {
    const first = new Deferred<WatchStateSendResult>();
    const send = vi
      .fn<WatchStateTransport['send']>()
      .mockImplementationOnce(() => first.promise)
      .mockImplementation((event) => Promise.resolve(applied(event)));
    const reporter = new WatchStateReporter({
      getPlaybackState: () => ({ foreground: true, playing: true, positionSeconds: 30 }),
      initialState: INITIAL_STATE,
      sessionId: 'session-a',
      transport: { send },
    });

    reporter.report({ eventType: 'progress', positionSeconds: 10, reason: 'system' });
    reporter.report({ eventType: 'progress', positionSeconds: 20, reason: 'system' });
    reporter.report({ eventType: 'progress', positionSeconds: 30, reason: 'system' });

    expect(send).toHaveBeenCalledTimes(1);
    first.resolve(applied(sentCall(send, 0)[0]));
    await reporter.idle();

    expect(send).toHaveBeenCalledTimes(2);
    expect(sentCall(send, 1)[0]).toMatchObject({ eventSeq: 2, expectedRevision: 1, positionSeconds: 30 });
  });

  it('409 后先采用 current，前台持续播放仅重试一次并使用当前最新进度', async () => {
    let playback = { durationSeconds: 120, foreground: true, playing: true, positionSeconds: 60 };
    const send = vi
      .fn<WatchStateTransport['send']>()
      .mockResolvedValueOnce(conflict(7, 50))
      .mockImplementationOnce((event) => Promise.resolve(applied(event, 8)));
    const reporter = new WatchStateReporter({
      getPlaybackState: () => playback,
      initialState: { ...INITIAL_STATE, revision: 4 },
      sessionId: 'session-a',
      transport: { send },
    });

    reporter.report({ eventType: 'progress', positionSeconds: 40, reason: 'system' });
    playback = { ...playback, positionSeconds: 63 };
    await reporter.idle();

    expect(send).toHaveBeenCalledTimes(2);
    expect(sentCall(send, 1)[0]).toEqual({
      durationSeconds: 120,
      eventSeq: 2,
      eventType: 'progress',
      expectedRevision: 7,
      positionSeconds: 63,
      reason: 'system',
      sessionId: 'session-a',
    });
    expect(reporter.getState()).toEqual({ completed: false, positionSeconds: 63, revision: 8 });
  });

  it('未被取代的主动 seek 可在暂停态重试一次，第二次冲突不再重试', async () => {
    const send = vi
      .fn<WatchStateTransport['send']>()
      .mockResolvedValueOnce(conflict(2, 25))
      .mockResolvedValueOnce(conflict(3, 30));
    const reporter = new WatchStateReporter({
      getPlaybackState: () => ({ foreground: true, playing: false, positionSeconds: 40 }),
      initialState: { ...INITIAL_STATE, revision: 1 },
      sessionId: 'session-a',
      transport: { send },
    });

    reporter.report({ eventType: 'seek', positionSeconds: 40, reason: 'user' });
    await reporter.idle();

    expect(send).toHaveBeenCalledTimes(2);
    expect(sentCall(send, 1)[0]).toMatchObject({
      eventSeq: 2,
      eventType: 'seek',
      expectedRevision: 2,
      positionSeconds: 40,
      reason: 'user',
    });
    expect(reporter.getState()).toEqual({ completed: false, positionSeconds: 30, revision: 3 });
  });

  it('主动 seek 被更新事件取代后不重试旧 seek，只发送最新事件', async () => {
    const first = new Deferred<WatchStateSendResult>();
    const send = vi
      .fn<WatchStateTransport['send']>()
      .mockImplementationOnce(() => first.promise)
      .mockImplementation((event) => Promise.resolve(applied(event, 4)));
    const reporter = new WatchStateReporter({
      getPlaybackState: () => ({ foreground: true, playing: false, positionSeconds: 45 }),
      initialState: { ...INITIAL_STATE, revision: 1 },
      sessionId: 'session-a',
      transport: { send },
    });

    reporter.report({ eventType: 'seek', positionSeconds: 40, reason: 'user' });
    reporter.report({ eventType: 'pause', positionSeconds: 45, reason: 'system' });
    first.resolve(conflict(3, 30));
    await reporter.idle();

    expect(send).toHaveBeenCalledTimes(2);
    expect(sentCall(send, 1)[0]).toMatchObject({ eventSeq: 2, eventType: 'pause', expectedRevision: 3 });
  });

  it('后台冲突、关闭补报和旧 session 均不重试', async () => {
    let playback = { foreground: true, playing: true, positionSeconds: 20 };
    const pending = new Deferred<WatchStateSendResult>();
    const send = vi.fn<WatchStateTransport['send']>(() => pending.promise);
    const reporter = new WatchStateReporter({
      getPlaybackState: () => playback,
      initialState: INITIAL_STATE,
      sessionId: 'session-old',
      transport: { send },
    });

    reporter.report({ eventType: 'progress', positionSeconds: 20, reason: 'system' });
    playback = { ...playback, foreground: false };
    pending.resolve(conflict(2, 50));
    await reporter.idle();
    expect(send).toHaveBeenCalledTimes(1);

    const closingSend = vi.fn<WatchStateTransport['send']>().mockResolvedValue(conflict(3, 60));
    const closing = new WatchStateReporter({
      getPlaybackState: () => ({ foreground: false, playing: false, positionSeconds: 25 }),
      initialState: INITIAL_STATE,
      sessionId: 'session-closing',
      transport: { send: closingSend },
    });
    closing.close({ eventType: 'pause', positionSeconds: 25, reason: 'system' });
    await closing.idle();
    expect(closingSend).toHaveBeenCalledTimes(1);
    expect(sentCall(closingSend, 0)[1]).toEqual({ keepalive: true });
  });

  it('关闭时若已有请求在途则丢弃待发补报，不建立离线队列', async () => {
    const first = new Deferred<WatchStateSendResult>();
    const send = vi.fn<WatchStateTransport['send']>(() => first.promise);
    const reporter = new WatchStateReporter({
      getPlaybackState: () => ({ foreground: false, playing: false, positionSeconds: 30 }),
      initialState: INITIAL_STATE,
      sessionId: 'session-a',
      transport: { send },
    });

    reporter.report({ eventType: 'progress', positionSeconds: 20, reason: 'system' });
    reporter.close({ eventType: 'pause', positionSeconds: 30, reason: 'system' });
    first.resolve(applied(sentCall(send, 0)[0]));
    await reporter.idle();

    expect(send).toHaveBeenCalledTimes(1);
  });

  it('不同 reporter 创建不同 session，旧 reporter 的迟到结果不能污染新媒体状态', async () => {
    const oldResult = new Deferred<WatchStateSendResult>();
    const oldReporter = new WatchStateReporter({
      getPlaybackState: () => ({ foreground: true, playing: true, positionSeconds: 10 }),
      initialState: INITIAL_STATE,
      sessionId: 'session-old',
      transport: { send: () => oldResult.promise },
    });
    const newSend = vi.fn<WatchStateTransport['send']>((event) => Promise.resolve(applied(event, 6)));
    const newReporter = new WatchStateReporter({
      getPlaybackState: () => ({ foreground: true, playing: true, positionSeconds: 70 }),
      initialState: { ...INITIAL_STATE, positionSeconds: 60, revision: 5 },
      sessionId: 'session-new',
      transport: { send: newSend },
    });

    oldReporter.report({ eventType: 'progress', positionSeconds: 10, reason: 'system' });
    oldReporter.dispose();
    newReporter.report({ eventType: 'progress', positionSeconds: 70, reason: 'system' });
    oldResult.resolve(applied({
      eventSeq: 1,
      eventType: 'progress',
      expectedRevision: 0,
      positionSeconds: 10,
      reason: 'system',
      sessionId: 'session-old',
    }, 99));
    await Promise.all([oldReporter.idle(), newReporter.idle()]);

    expect(newReporter.getState()).toEqual({ completed: false, positionSeconds: 70, revision: 6 });
    expect(sentCall(newSend, 0)[0].sessionId).toBe('session-new');
  });
});
