import { describe, expect, it } from 'vitest';
import { PlaybackCore } from './playback-core';
import { FakePlaybackBackend, createSnapshot } from './test-utils';
import type { PlaybackEvent } from './types';

describe('PlaybackCore watch-state 事件语义', () => {
  it('seek 完成事件保留 user、restore 与 ab_loop 原因', async () => {
    const backend = new FakePlaybackBackend();
    backend.setSnapshot(
      createSnapshot({
        capabilities: {
          framePresentation: 'unavailable',
          loadControl: 'unavailable',
          preview: 'unavailable',
          quality: 'unavailable',
          seek: 'available',
          tracks: 'unavailable',
        },
        duration: 120,
        seekable: [{ end: 120, start: 0 }],
      }),
    );
    const core = new PlaybackCore({ backend });
    const events: PlaybackEvent[] = [];
    core.subscribe((event) => events.push(event));

    await core.load({ id: 'media-a', mode: 'direct' });
    await core.seek(10, 'restore');
    await core.seek(20, 'user');
    await core.seek(5, 'ab_loop');

    expect(
      events
        .filter((event) => event.type === 'seekCompleted')
        .map((event) => ({ reason: event.reason, time: event.result.confirmedTime })),
    ).toEqual([
      { reason: 'restore', time: 10 },
      { reason: 'user', time: 20 },
      { reason: 'ab_loop', time: 5 },
    ]);
  });
});
