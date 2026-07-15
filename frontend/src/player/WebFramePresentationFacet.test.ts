import type { PlaybackCommandContext } from '@jianvideo/player-core';
import { describe, expect, it, vi } from 'vitest';
import {
  createBinaryFrameMarkerResolver,
  WebFramePresentationFacet,
  type WebBinaryFrameMarker,
  type WebFramePresentationSource,
} from './WebFramePresentationFacet';

interface VideoFrameHarness {
  readonly callbacks: Map<number, VideoFrameRequestCallback>;
  readonly cancel: ReturnType<typeof vi.fn>;
  readonly request: ReturnType<typeof vi.fn>;
  present(mediaTime: number, id?: number): void;
}

function command(sourceId: string, sourceEpoch: number): PlaybackCommandContext {
  return { requestId: sourceEpoch, sourceEpoch, sourceId };
}

function source(
  frameTimeline?: WebFramePresentationSource['frameTimeline'],
  nominalFrameRate?: number,
  resolvePresentedFrameIdentity?: WebFramePresentationSource['resolvePresentedFrameIdentity'],
): WebFramePresentationSource {
  return { frameTimeline, nominalFrameRate, resolvePresentedFrameIdentity, seekAvailable: true };
}

function installVideoFrameCallback(video: HTMLVideoElement): VideoFrameHarness {
  let nextId = 1;
  const callbacks = new Map<number, VideoFrameRequestCallback>();
  const request = vi.fn((callback: VideoFrameRequestCallback) => {
    const id = nextId++;
    callbacks.set(id, callback);
    return id;
  });
  const cancel = vi.fn((id: number) => callbacks.delete(id));
  Object.assign(video, { cancelVideoFrameCallback: cancel, requestVideoFrameCallback: request });
  return {
    callbacks,
    cancel,
    request,
    present(mediaTime, id = Math.min(...callbacks.keys())) {
      const callback = callbacks.get(id);
      callbacks.delete(id);
      callback?.(performance.now(), { mediaTime } as VideoFrameCallbackMetadata);
    },
  };
}

function createVideo(currentTime = 0): HTMLVideoElement {
  const video = document.createElement('video');
  Object.defineProperty(video, 'currentTime', { configurable: true, get: () => currentTime });
  return video;
}

function markerPixels(
  index: number,
  marker: WebBinaryFrameMarker,
  validSentinels = true,
): ImageData {
  const cells = marker.bits + 2;
  const width = cells * marker.cellSize;
  const data = new Uint8ClampedArray(width * marker.cellSize * 4);
  for (let cell = 0; cell < cells; cell += 1) {
    const sentinel = cell === 0 || cell === cells - 1;
    const bit = sentinel ? validSentinels : ((index >> (cell - 1)) & 1) === 1;
    const value = bit ? 255 : 0;
    for (let y = 0; y < marker.cellSize; y += 1) {
      for (let x = cell * marker.cellSize; x < (cell + 1) * marker.cellSize; x += 1) {
        const offset = (y * width + x) * 4;
        data[offset] = value;
        data[offset + 1] = value;
        data[offset + 2] = value;
        data[offset + 3] = 255;
      }
    }
  }
  return { colorSpace: 'srgb', data, height: marker.cellSize, width } as ImageData;
}

describe('WebFramePresentationFacet', () => {
  it('从实际视频像素 marker 读取稳定帧身份且不依赖 mediaTime/currentTime', () => {
    const video = createVideo(99);
    const marker: WebBinaryFrameMarker = { bits: 9, cellSize: 8, x: 16, y: 16 };
    const context = {
      drawImage: vi.fn(),
      getImageData: vi.fn(() => markerPixels(173, marker)),
    };
    const canvas = {
      getContext: vi.fn(() => context),
      height: 0,
      width: 0,
    } as unknown as HTMLCanvasElement;
    const createElement = vi.spyOn(document, 'createElement').mockReturnValue(canvas);
    const resolver = createBinaryFrameMarkerResolver(video, marker);

    expect(resolver({ mediaTime: 0.1 } as VideoFrameCallbackMetadata)).toEqual({
      sourceFrameIndex: 173,
      stableFrameId: 'binary-marker:173',
    });
    expect(resolver({ mediaTime: 88 } as VideoFrameCallbackMetadata)).toEqual({
      sourceFrameIndex: 173,
      stableFrameId: 'binary-marker:173',
    });
    expect(context.drawImage).toHaveBeenCalledWith(video, 16, 16, 88, 8, 0, 0, 88, 8);
    createElement.mockRestore();
  });

  it('二进制 marker 哨兵缺失时拒绝伪造稳定身份', () => {
    const video = createVideo();
    const marker: WebBinaryFrameMarker = { bits: 9, cellSize: 8, x: 0, y: 0 };
    const context = {
      drawImage: vi.fn(),
      getImageData: vi.fn(() => markerPixels(5, marker, false)),
    };
    const canvas = {
      getContext: vi.fn(() => context),
      height: 0,
      width: 0,
    } as unknown as HTMLCanvasElement;
    const createElement = vi.spyOn(document, 'createElement').mockReturnValue(canvas);
    const resolver = createBinaryFrameMarkerResolver(video, marker);

    expect(resolver({ mediaTime: 5 / 30 } as VideoFrameCallbackMetadata)).toBeNull();
    createElement.mockRestore();
  });

  it('mediaTime 恰好命中稳定时间线也不反推身份且保持近似', () => {
    const video = createVideo();
    const harness = installVideoFrameCallback(video);
    const facet = new WebFramePresentationFacet(video);
    const active = command('timeline-only', 1);
    facet.load(
      source([
        { mediaTime: 1, sourceFrameIndex: 40 },
        { mediaTime: 1.04, sourceFrameIndex: 41 },
      ]),
      active,
    );

    harness.present(1.04);

    expect(facet.getCapability()).toBe('approximate');
    expect(facet.getCurrentPresentedFrame(active)).toMatchObject({
      mediaTime: 1.04,
      sampleSource: 'video-frame-callback',
    });
    expect(facet.getCurrentPresentedFrame(active)).not.toHaveProperty('sourceFrameIndex');
    expect(facet.getCurrentPresentedFrame(active)).not.toHaveProperty('stableFrameId');
  });

  it.each([
    ['sourceFrameIndex', { sourceFrameIndex: 41 }],
    ['stableFrameId', { stableFrameId: 'frame-b' }],
  ] as const)('独立 provider 返回可唯一匹配的 %s 样本后才升级 exact', (_kind, identity) => {
    const video = createVideo();
    const harness = installVideoFrameCallback(video);
    const resolvePresentedFrameIdentity = vi.fn(() => identity);
    const capabilityChanged = vi.fn();
    const facet = new WebFramePresentationFacet(video, capabilityChanged);
    const active = command('provider', 2);
    facet.load(
      source(
        [
          { mediaTime: 1, sourceFrameIndex: 40, stableFrameId: 'frame-a' },
          { mediaTime: 1.04, sourceFrameIndex: 41, stableFrameId: 'frame-b' },
        ],
        undefined,
        resolvePresentedFrameIdentity,
      ),
      active,
    );

    expect(facet.getCapability()).toBe('approximate');
    harness.present(1.04);

    expect(facet.getCurrentPresentedFrame(active)).toMatchObject({
      ...identity,
      mediaTime: 1.04,
      sampleSource: 'video-frame-callback',
    });
    expect(facet.getCapability()).toBe('exact');
    expect(capabilityChanged).toHaveBeenCalledWith('exact');
    expect(resolvePresentedFrameIdentity).toHaveBeenCalledOnce();
  });

  it.each([
    ['null', null],
    ['空身份', {}],
    ['类型非法', { sourceFrameIndex: '41' }],
    ['身份不匹配', { sourceFrameIndex: 99 }],
    ['双字段冲突', { sourceFrameIndex: 40, stableFrameId: 'frame-b' }],
  ])('provider 返回%s时从 exact 降级并拒绝继续相邻计算', (_case, invalidIdentity) => {
    const video = createVideo();
    const harness = installVideoFrameCallback(video);
    const provider = vi
      .fn()
      .mockReturnValueOnce({ sourceFrameIndex: 40, stableFrameId: 'frame-a' })
      .mockReturnValue(invalidIdentity);
    const capabilityChanged = vi.fn();
    const facet = new WebFramePresentationFacet(video, capabilityChanged);
    const active = command('invalid-provider', 3);
    facet.load(
      source(
        [
          { mediaTime: 1, sourceFrameIndex: 40, stableFrameId: 'frame-a' },
          { mediaTime: 1.04, sourceFrameIndex: 41, stableFrameId: 'frame-b' },
        ],
        undefined,
        provider,
      ),
      active,
    );

    harness.present(1);
    expect(facet.getCapability()).toBe('exact');
    harness.present(1.04);

    expect(facet.getCapability()).toBe('approximate');
    expect(capabilityChanged.mock.calls.map(([capability]) => capability)).toEqual([
      'exact',
      'approximate',
    ]);
    const current = facet.getCurrentPresentedFrame(active)!;
    expect(current).not.toHaveProperty('sourceFrameIndex');
    expect(current).not.toHaveProperty('stableFrameId');
    expect(facet.getAdjacentFrameTarget(current, 'next', active)).toBeNull();
  });

  it('exact 能力下当前帧无身份或双字段冲突时不计算相邻目标', () => {
    const video = createVideo();
    const harness = installVideoFrameCallback(video);
    const facet = new WebFramePresentationFacet(video);
    const active = command('missing-current-identity', 4);
    facet.load(
      source(
        [
          { mediaTime: 1, sourceFrameIndex: 40, stableFrameId: 'frame-a' },
          { mediaTime: 1.04, sourceFrameIndex: 41, stableFrameId: 'frame-b' },
        ],
        undefined,
        () => ({ sourceFrameIndex: 40, stableFrameId: 'frame-a' }),
      ),
      active,
    );
    harness.present(1);
    const current = facet.getCurrentPresentedFrame(active)!;

    expect(facet.getCapability()).toBe('exact');
    expect(
      facet.getAdjacentFrameTarget(
        { ...current, sourceFrameIndex: undefined, stableFrameId: undefined },
        'next',
        active,
      ),
    ).toBeNull();
    expect(
      facet.getAdjacentFrameTarget(
        { ...current, sourceFrameIndex: 40, stableFrameId: 'frame-b' },
        'next',
        active,
      ),
    ).toBeNull();
  });

  it('支持 stableFrameId 时间线并返回双向相邻目标', () => {
    const video = createVideo();
    const harness = installVideoFrameCallback(video);
    const facet = new WebFramePresentationFacet(video);
    const active = command('stable', 2);
    facet.load(
      source(
        [
          { mediaTime: 2, stableFrameId: 'frame-a' },
          { mediaTime: 2.05, stableFrameId: 'frame-b' },
          { mediaTime: 2.1, stableFrameId: 'frame-c' },
        ],
        undefined,
        (metadata) => {
          if (metadata.mediaTime < 2.025) return { stableFrameId: 'frame-a' };
          if (metadata.mediaTime < 2.075) return { stableFrameId: 'frame-b' };
          return { stableFrameId: 'frame-c' };
        },
      ),
      active,
    );
    harness.present(2.051);
    const current = facet.getCurrentPresentedFrame(active)!;

    expect(current.stableFrameId).toBe('frame-b');
    const previous = facet.getAdjacentFrameTarget(current, 'previous', active);
    const next = facet.getAdjacentFrameTarget(current, 'next', active);
    expect(previous).toMatchObject({ mediaTime: 2, stableFrameId: 'frame-a' });
    expect(previous?.frameDuration).toBeCloseTo(0.05);
    expect(next).toMatchObject({ mediaTime: 2.1, stableFrameId: 'frame-c' });
    expect(next?.frameDuration).toBeCloseTo(0.05);
  });

  it('无 rVFC 或时间线身份不稳定时降级，并提供 1/30 默认帧时长', () => {
    const withoutCallback = new WebFramePresentationFacet(createVideo(3));
    const active = command('fallback', 3);
    withoutCallback.load(source(undefined), active);

    expect(withoutCallback.getCapability()).toBe('approximate');
    expect(withoutCallback.getNominalFrameDuration(active)).toBeCloseTo(1 / 30);
    expect(withoutCallback.getCurrentPresentedFrame(active)).toMatchObject({
      mediaTime: 3,
      sampleSource: 'backend',
    });

    const video = createVideo();
    installVideoFrameCallback(video);
    const invalidTimeline = new WebFramePresentationFacet(video);
    invalidTimeline.load(source([{ mediaTime: 1 }]), active);
    expect(invalidTimeline.getCapability()).toBe('approximate');
  });

  it('基础 seek 不可用时能力不可用', () => {
    const video = createVideo();
    installVideoFrameCallback(video);
    const facet = new WebFramePresentationFacet(video);
    facet.load(
      { frameTimeline: [{ mediaTime: 0, sourceFrameIndex: 0 }], seekAvailable: false },
      command('blocked', 4),
    );

    expect(facet.getCapability()).toBe('unavailable');
  });

  it('切源取消旧 callback、拒绝旧 waiter，且旧回调不污染新源', async () => {
    const video = createVideo();
    const harness = installVideoFrameCallback(video);
    const facet = new WebFramePresentationFacet(video);
    const oldIdentityProvider = vi.fn(() => ({ sourceFrameIndex: 1 }));
    const newIdentityProvider = vi.fn(() => ({ stableFrameId: 'new-frame' }));
    const oldCommand = command('old', 5);
    facet.load(
      source([{ mediaTime: 1, sourceFrameIndex: 1 }], undefined, oldIdentityProvider),
      oldCommand,
    );
    const oldCallbackId = Math.min(...harness.callbacks.keys());
    const oldCallback = harness.callbacks.get(oldCallbackId)!;
    const oldWaiter = facet.waitForPresentedFrame(oldCommand);

    const nextCommand = command('new', 6);
    facet.load(
      source([{ mediaTime: 5, stableFrameId: 'new-frame' }], undefined, newIdentityProvider),
      nextCommand,
    );
    await expect(oldWaiter).rejects.toThrow('帧呈现等待已取消');
    expect(harness.cancel).toHaveBeenCalledWith(oldCallbackId);

    oldCallback(performance.now(), { mediaTime: 1 } as VideoFrameCallbackMetadata);
    expect(oldIdentityProvider).not.toHaveBeenCalled();
    expect(facet.getCurrentPresentedFrame(nextCommand)).toMatchObject({
      sampleSource: 'backend',
      sourceEpoch: nextCommand.sourceEpoch,
      sourceId: nextCommand.sourceId,
    });
    harness.present(5);
    expect(newIdentityProvider).toHaveBeenCalledOnce();
    expect(facet.getCurrentPresentedFrame(nextCommand)?.stableFrameId).toBe('new-frame');
  });

  it('dispose 取消 callback 并拒绝 waiter', async () => {
    const video = createVideo();
    const harness = installVideoFrameCallback(video);
    const facet = new WebFramePresentationFacet(video);
    const active = command('dispose', 7);
    facet.load(
      source([{ mediaTime: 0, sourceFrameIndex: 0 }], undefined, () => ({ sourceFrameIndex: 0 })),
      active,
    );
    const waiter = facet.waitForPresentedFrame(active);

    facet.dispose();

    await expect(waiter).rejects.toThrow('帧呈现等待已取消');
    expect(harness.cancel).toHaveBeenCalledOnce();
  });
});
