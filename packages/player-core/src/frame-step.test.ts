import { describe, expect, it } from "vitest";
import {
  PlaybackCore,
  type AdjacentFrameTarget,
  type FramePresentationFacet,
  type FrameStepDirection,
  type PlaybackSource,
  type PresentedFrame,
  type SeekResult,
} from "./index";
import {
  Deferred,
  EMPTY_CAPABILITIES,
  FakePlaybackBackend,
  createSnapshot,
} from "./test-utils";

const SOURCE_A: PlaybackSource = { id: "frame-source-a", mode: "stream" };
const SOURCE_B: PlaybackSource = { id: "frame-source-b", mode: "adaptive" };
const FRAME_DURATION = 1 / 30;

function frame(
  index: number,
  overrides: Partial<PresentedFrame> = {},
): PresentedFrame {
  return {
    mediaTime: index * FRAME_DURATION,
    presentationSequence: index,
    sampleSource: "video-frame-callback",
    sourceEpoch: 1,
    sourceFrameIndex: index,
    sourceId: SOURCE_A.id,
    stableFrameId: `frame-${String(index)}`,
    ...overrides,
  };
}

function target(
  index: number,
  overrides: Partial<AdjacentFrameTarget> = {},
): AdjacentFrameTarget {
  return {
    frameDuration: FRAME_DURATION,
    mediaTime: index * FRAME_DURATION,
    sourceFrameIndex: index,
    stableFrameId: `frame-${String(index)}`,
    ...overrides,
  };
}

function stableFrame(index: number, stableFrameId: string): PresentedFrame {
  return {
    mediaTime: index * FRAME_DURATION,
    presentationSequence: index,
    sampleSource: "video-frame-callback",
    sourceEpoch: 1,
    sourceId: SOURCE_A.id,
    stableFrameId,
  };
}

function stableTarget(
  index: number,
  stableFrameId: string,
): AdjacentFrameTarget {
  return {
    frameDuration: FRAME_DURATION,
    mediaTime: index * FRAME_DURATION,
    stableFrameId,
  };
}

function unidentifiedFrame(index: number): PresentedFrame {
  return {
    mediaTime: index * FRAME_DURATION,
    presentationSequence: index,
    sampleSource: "video-frame-callback",
    sourceEpoch: 1,
    sourceId: SOURCE_A.id,
  };
}

class TrackingBackend extends FakePlaybackBackend {
  override async seek(
    request: Parameters<FakePlaybackBackend["seek"]>[0],
  ): Promise<SeekResult> {
    const result = await super.seek(request);
    if (result.status === "completed") {
      this.setSnapshot({
        ...this.getSnapshot(),
        currentTime: result.confirmedTime,
        requestId: request.requestId,
      });
    }
    return result;
  }
}

class ScriptedFrameFacet implements FramePresentationFacet {
  current: PresentedFrame | null;
  nominalFrameDuration: number | null = FRAME_DURATION;
  target: AdjacentFrameTarget | null;
  waitCount = 0;
  private readonly presentedFrames: PresentedFrame[];

  constructor(
    current: PresentedFrame | null,
    adjacent: AdjacentFrameTarget | null,
    presentedFrames: PresentedFrame[],
  ) {
    this.current = current;
    this.target = adjacent;
    this.presentedFrames = [...presentedFrames];
  }

  getCurrentPresentedFrame(): PresentedFrame | null {
    return this.current;
  }

  getAdjacentFrameTarget(): AdjacentFrameTarget | null {
    return this.target;
  }

  getNominalFrameDuration(): number | null {
    return this.nominalFrameDuration;
  }

  waitForPresentedFrame(): Promise<PresentedFrame> {
    this.waitCount += 1;
    const presented = this.presentedFrames.shift() ?? this.current;
    if (presented === null) {
      return Promise.reject(new Error("缺少脚本呈现帧"));
    }
    this.current = presented;
    return Promise.resolve(presented);
  }
}

class SequentialFrameFacet implements FramePresentationFacet {
  current: PresentedFrame;
  private pendingTarget: AdjacentFrameTarget | null = null;

  constructor(index: number) {
    this.current = frame(index);
  }

  getCurrentPresentedFrame(): PresentedFrame {
    return this.current;
  }

  getAdjacentFrameTarget(
    _current: PresentedFrame,
    direction: FrameStepDirection,
  ): AdjacentFrameTarget {
    const delta = direction === "next" ? 1 : -1;
    this.pendingTarget = target((this.current.sourceFrameIndex ?? 0) + delta);
    return this.pendingTarget;
  }

  getNominalFrameDuration(): number {
    return FRAME_DURATION;
  }

  waitForPresentedFrame(): Promise<PresentedFrame> {
    const nextTarget = this.pendingTarget;
    if (nextTarget?.sourceFrameIndex === undefined) {
      return Promise.reject(new Error("缺少顺序目标"));
    }
    this.current = frame(nextTarget.sourceFrameIndex);
    return Promise.resolve(this.current);
  }
}

function exactSnapshot(currentTime = 10 * FRAME_DURATION) {
  return createSnapshot({
    capabilities: {
      ...EMPTY_CAPABILITIES,
      framePresentation: "exact",
      seek: "available",
    },
    currentTime,
    duration: 20,
    seekable: [{ end: 20, start: 0 }],
  });
}

async function createFrameCore(
  facet: FramePresentationFacet,
  snapshot = exactSnapshot(),
) {
  const backend = new TrackingBackend();
  backend.setSnapshot(snapshot);
  const core = new PlaybackCore({
    backend,
    facets: { framePresentation: facet },
  });
  await core.load(SOURCE_A);
  return { backend, core };
}

describe("PlaybackCore 逐帧控制", () => {
  it.each([
    { direction: "next" as const, end: 11, start: 10 },
    { direction: "previous" as const, end: 9, start: 10 },
  ])(
    "用 sourceFrameIndex 精确验证 $direction 恰好相邻",
    async ({ direction, end, start }) => {
      const facet = new ScriptedFrameFacet(frame(start), target(end), [
        frame(end),
      ]);
      const { core } = await createFrameCore(facet);

      await expect(core.stepFrame(direction)).resolves.toMatchObject({
        confirmedMediaTime: end * FRAME_DURATION,
        confirmedSourceFrameIndex: end,
        correctionCount: 0,
        direction,
        precision: "exact-verified",
        startSourceFrameIndex: start,
        status: "completed",
        targetSourceFrameIndex: end,
      });
      expect(core.getSnapshot()).toMatchObject({
        currentTime: end * FRAME_DURATION,
        state: "paused",
      });
    },
  );

  it("用 stableFrameId 精确验证最终帧等于方向性相邻目标", async () => {
    const start = stableFrame(10, "stable-a");
    const adjacent = stableTarget(11, "stable-b");
    const presented = stableFrame(11, "stable-b");
    const { core } = await createFrameCore(
      new ScriptedFrameFacet(start, adjacent, [presented]),
    );

    await expect(core.stepFrame("next")).resolves.toMatchObject({
      confirmedStableFrameId: "stable-b",
      precision: "exact-verified",
      startStableFrameId: "stable-a",
      status: "completed",
      targetStableFrameId: "stable-b",
    });
  });

  it.each([
    { actual: frame(10, { mediaTime: 11 * FRAME_DURATION }), label: "同帧" },
    { actual: frame(9), label: "反向" },
    { actual: frame(12), label: "跳帧" },
    {
      actual: frame(11, { mediaTime: 14 * FRAME_DURATION }),
      label: "超出时间容差",
    },
  ])("$label 最多校正 2 次后受控失败", async ({ actual }) => {
    const facet = new ScriptedFrameFacet(frame(10), target(11), [
      actual,
      actual,
      actual,
    ]);
    const { backend, core } = await createFrameCore(facet);

    const result = await core.stepFrame("next");

    expect(result).toMatchObject({
      correctionCount: 2,
      precision: "exact-verified",
      status: "failed",
    });
    expect(result.error?.category).not.toBe("network");
    expect(
      backend.calls.filter(({ method }) => method === "seek"),
    ).toHaveLength(3);
    expect(facet.waitCount).toBe(3);
    expect(core.getSnapshot().state).toBe("paused");
  });

  it("第二次有限校正后精确到达目标", async () => {
    const facet = new ScriptedFrameFacet(frame(10), target(11), [
      frame(10),
      frame(12),
      frame(11),
    ]);
    const { backend, core } = await createFrameCore(facet);

    await expect(core.stepFrame("next")).resolves.toMatchObject({
      confirmedSourceFrameIndex: 11,
      correctionCount: 2,
      precision: "exact-verified",
      status: "completed",
    });
    expect(
      backend.calls.filter(({ method }) => method === "seek"),
    ).toHaveLength(3);
  });

  it("校正按实际落点相对目标使用半帧偏置且最多两次", async () => {
    const facet = new ScriptedFrameFacet(frame(10), target(11), [
      frame(10),
      frame(12),
      frame(11),
    ]);
    const { backend, core } = await createFrameCore(facet);

    await expect(core.stepFrame("next")).resolves.toMatchObject({
      correctionCount: 2,
      status: "completed",
    });

    const targets = backend.calls
      .filter(({ method }) => method === "seek")
      .map(({ targetTime }) => targetTime);
    expect(targets).toHaveLength(3);
    expect(targets[0]).toBeCloseTo(11 * FRAME_DURATION);
    expect(targets[1]).toBeCloseTo(11.5 * FRAME_DURATION);
    expect(targets[2]).toBeCloseTo(10.5 * FRAME_DURATION);
  });

  it("最终呈现帧身份缺失时降级为 approximate，不用 mediaTime 冒充精确", async () => {
    const actual = unidentifiedFrame(11);
    const facet = new ScriptedFrameFacet(frame(10), target(11), [actual]);
    const { core } = await createFrameCore(facet);

    await expect(core.stepFrame("next")).resolves.toMatchObject({
      precision: "approximate",
      status: "completed",
    });
    expect(facet.waitCount).toBe(1);
  });

  it("相邻目标身份缺失时在执行前降级为 approximate", async () => {
    const adjacent: AdjacentFrameTarget = {
      frameDuration: FRAME_DURATION,
      mediaTime: 11 * FRAME_DURATION,
    };
    const facet = new ScriptedFrameFacet(frame(10), adjacent, [frame(11)]);
    const { core } = await createFrameCore(facet);

    await expect(core.stepFrame("next")).resolves.toMatchObject({
      precision: "approximate",
      status: "completed",
    });
    expect(facet.waitCount).toBe(0);
  });

  it("实际呈现不是 rVFC 样本时降级为 approximate", async () => {
    const actual = frame(11, { sampleSource: "backend" });
    const facet = new ScriptedFrameFacet(frame(10), target(11), [actual]);
    const { core } = await createFrameCore(facet);

    await expect(core.stepFrame("next")).resolves.toMatchObject({
      precision: "approximate",
      status: "completed",
    });
    expect(facet.waitCount).toBe(1);
  });

  it.each([
    { actual: unidentifiedFrame(10), label: "身份缺失同帧" },
    { actual: unidentifiedFrame(9), label: "身份缺失反向" },
    { actual: unidentifiedFrame(14), label: "身份缺失明显跳错" },
    {
      actual: frame(10, { sampleSource: "backend" as const }),
      label: "非 rVFC 同帧",
    },
    {
      actual: frame(9, { sampleSource: "backend" as const }),
      label: "非 rVFC 反向",
    },
    {
      actual: frame(14, { sampleSource: "backend" as const }),
      label: "非 rVFC 明显跳错",
    },
  ])("$label 降级后最多校正 2 次并失败", async ({ actual }) => {
    const facet = new ScriptedFrameFacet(frame(10), target(11), [
      actual,
      actual,
      actual,
    ]);
    const { backend, core } = await createFrameCore(facet);

    await expect(core.stepFrame("next")).resolves.toMatchObject({
      correctionCount: 2,
      precision: "approximate",
      status: "failed",
    });
    expect(
      backend.calls.filter(({ method }) => method === "seek"),
    ).toHaveLength(3);
  });

  it("身份缺失时允许最终 mediaTime 在目标一帧容差内", async () => {
    const actual = unidentifiedFrame(12);
    const facet = new ScriptedFrameFacet(frame(10), target(11), [actual]);
    const { core } = await createFrameCore(facet);

    await expect(core.stepFrame("next")).resolves.toMatchObject({
      confirmedMediaTime: 12 * FRAME_DURATION,
      correctionCount: 0,
      precision: "approximate",
      status: "completed",
    });
  });

  it("缺少 rVFC 能力时按名义帧时长近似且不等待呈现帧", async () => {
    const facet = new ScriptedFrameFacet(frame(10), target(11), [frame(11)]);
    const snapshot = createSnapshot({
      capabilities: {
        ...EMPTY_CAPABILITIES,
        framePresentation: "approximate",
        seek: "available",
      },
      currentTime: 10 * FRAME_DURATION,
      seekable: [{ end: 20, start: 0 }],
    });
    const { core } = await createFrameCore(facet, snapshot);

    await expect(core.stepFrame("next")).resolves.toMatchObject({
      confirmedMediaTime: 11 * FRAME_DURATION,
      precision: "approximate",
      status: "completed",
    });
    expect(facet.waitCount).toBe(0);
  });

  it("近似逐帧连续命令基于上一确认位置累积，不复用旧呈现帧时间", async () => {
    const facet = new ScriptedFrameFacet(frame(10), null, []);
    const snapshot = createSnapshot({
      capabilities: {
        ...EMPTY_CAPABILITIES,
        framePresentation: "approximate",
        seek: "available",
      },
      currentTime: 10 * FRAME_DURATION,
      seekable: [{ end: 20, start: 0 }],
    });
    const { backend, core } = await createFrameCore(facet, snapshot);

    const results = await Promise.all(
      Array.from({ length: 4 }, () => core.stepFrame("next")),
    );

    const expectedTimes = [11, 12, 13, 14].map(
      (index) => index * FRAME_DURATION,
    );
    results.forEach(({ confirmedMediaTime }, index) => {
      expect(confirmedMediaTime).toBeCloseTo(expectedTimes[index] ?? 0);
    });
    backend.calls
      .filter(({ method }) => method === "seek")
      .forEach(({ targetTime }, index) => {
        expect(targetTime).toBeCloseTo(expectedTimes[index] ?? 0);
      });
  });

  it("缺少相邻目标但有名义帧时长时降级为 approximate", async () => {
    const facet = new ScriptedFrameFacet(frame(10), null, []);
    const { core } = await createFrameCore(facet);

    await expect(core.stepFrame("previous")).resolves.toMatchObject({
      precision: "approximate",
      status: "completed",
      targetMediaTime: 9 * FRAME_DURATION,
    });
  });

  it("缺少基本 seek 时仍先暂停并返回 unsupported", async () => {
    const facet = new ScriptedFrameFacet(frame(10), target(11), [frame(11)]);
    const snapshot = createSnapshot({
      capabilities: {
        ...EMPTY_CAPABILITIES,
        framePresentation: "exact",
        seek: "unavailable",
      },
      currentTime: 10 * FRAME_DURATION,
      seekable: [{ end: 20, start: 0 }],
    });
    const { backend, core } = await createFrameCore(facet, snapshot);

    await expect(core.stepFrame("next")).resolves.toMatchObject({
      precision: "unsupported",
      status: "unsupported",
    });
    expect(backend.calls.at(-1)?.method).toBe("pause");
    expect(
      backend.calls.filter(({ method }) => method === "seek"),
    ).toHaveLength(0);
  });

  it("帧边界夹取不发起 seek", async () => {
    const facet = new ScriptedFrameFacet(frame(0), target(0), []);
    const { backend, core } = await createFrameCore(facet, exactSnapshot(0));

    await expect(core.stepFrame("previous")).resolves.toMatchObject({
      clamped: true,
      confirmedMediaTime: 0,
      status: "completed",
    });
    expect(backend.calls.at(-1)?.method).toBe("pause");
    expect(
      backend.calls.filter(({ method }) => method === "seek"),
    ).toHaveLength(0);
  });

  it("连续 60 次前进再 60 次后退严格串行并基于上一确认帧", async () => {
    const facet = new SequentialFrameFacet(100);
    const { backend, core } = await createFrameCore(
      facet,
      exactSnapshot(100 * FRAME_DURATION),
    );

    const forward = await Promise.all(
      Array.from({ length: 60 }, () => core.stepFrame("next")),
    );
    const backward = await Promise.all(
      Array.from({ length: 60 }, () => core.stepFrame("previous")),
    );

    expect(
      forward.map(({ confirmedSourceFrameIndex }) => confirmedSourceFrameIndex),
    ).toEqual(Array.from({ length: 60 }, (_, index) => 101 + index));
    expect(
      backward.map(
        ({ confirmedSourceFrameIndex }) => confirmedSourceFrameIndex,
      ),
    ).toEqual(Array.from({ length: 60 }, (_, index) => 159 - index));
    expect(facet.current.sourceFrameIndex).toBe(100);
    expect(
      backend.calls.filter(({ method }) => method === "seek"),
    ).toHaveLength(120);
  });

  it("ready 态先等待 pause 完成，再发起逐帧 seek", async () => {
    const facet = new ScriptedFrameFacet(frame(10), target(11), [frame(11)]);
    const { backend, core } = await createFrameCore(facet);
    const pendingPause = new Deferred<void>();
    backend.pauseHandler = () => pendingPause.promise;

    const stepping = core.stepFrame("next");
    await Promise.resolve();
    await Promise.resolve();
    expect(backend.calls.at(-1)?.method).toBe("pause");
    expect(
      backend.calls.filter(({ method }) => method === "seek"),
    ).toHaveLength(0);

    pendingPause.resolve();
    await expect(stepping).resolves.toMatchObject({ status: "completed" });
    expect(backend.calls.map(({ method }) => method).slice(-2)).toEqual([
      "pause",
      "seek",
    ]);
    expect(core.getSnapshot().state).toBe("paused");
  });

  it("paused 态直接逐帧，不重复调用 pause", async () => {
    const facet = new ScriptedFrameFacet(frame(10), target(11), [frame(11)]);
    const { backend, core } = await createFrameCore(facet);
    await core.pause();
    const pauseCount = backend.calls.filter(
      ({ method }) => method === "pause",
    ).length;

    await expect(core.stepFrame("next")).resolves.toMatchObject({
      status: "completed",
    });

    expect(
      backend.calls.filter(({ method }) => method === "pause"),
    ).toHaveLength(pauseCount);
    expect(core.getSnapshot().state).toBe("paused");
  });

  it.each([
    {
      direction: "previous" as const,
      expectedTime: 599 * FRAME_DURATION,
      seekCount: 1,
    },
    { direction: "next" as const, expectedTime: 20, seekCount: 0 },
  ])(
    "ended 态 $direction 先暂停确认并正确处理片尾边界",
    async ({ direction, expectedTime, seekCount }) => {
      const endFrame = frame(600);
      const adjacent = target(direction === "next" ? 601 : 599);
      const presented = direction === "next" ? [] : [frame(599)];
      const facet = new ScriptedFrameFacet(endFrame, adjacent, presented);
      const { backend, core } = await createFrameCore(facet, exactSnapshot(20));
      const pendingPause = new Deferred<void>();
      backend.pauseHandler = () => pendingPause.promise;
      backend.emit({
        eventId: 0,
        requestId: 1,
        sourceEpoch: 1,
        sourceId: SOURCE_A.id,
        type: "ended",
      });

      const stepping = core.stepFrame(direction);
      await Promise.resolve();
      await Promise.resolve();
      expect(backend.calls.at(-1)?.method).toBe("pause");
      expect(
        backend.calls.filter(({ method }) => method === "seek"),
      ).toHaveLength(0);

      pendingPause.resolve();
      await expect(stepping).resolves.toMatchObject({
        confirmedMediaTime: expectedTime,
        status: "completed",
      });
      expect(
        backend.calls.filter(({ method }) => method === "seek"),
      ).toHaveLength(seekCount);
      expect(core.getSnapshot()).toMatchObject({
        currentTime: expectedTime,
        state: "paused",
      });
    },
  );

  it("播放态先等待 pause 完成，再发起逐帧 seek，最终保持 paused", async () => {
    const facet = new ScriptedFrameFacet(frame(10), target(11), [frame(11)]);
    const { backend, core } = await createFrameCore(facet);
    const pendingPause = new Deferred<void>();
    backend.pauseHandler = () => pendingPause.promise;
    await core.play();

    const stepping = core.stepFrame("next");
    await Promise.resolve();
    await Promise.resolve();
    expect(backend.calls.at(-1)?.method).toBe("pause");
    expect(
      backend.calls.filter(({ method }) => method === "seek"),
    ).toHaveLength(0);

    pendingPause.resolve();
    await expect(stepping).resolves.toMatchObject({ status: "completed" });
    expect(backend.calls.map(({ method }) => method).slice(-2)).toEqual([
      "pause",
      "seek",
    ]);
    expect(core.getSnapshot().state).toBe("paused");
  });

  it("loading 态受控返回 unsupported 且不发起 seek", async () => {
    const backend = new TrackingBackend();
    backend.setSnapshot(exactSnapshot());
    const pendingLoad = new Deferred<void>();
    backend.loadHandler = () => pendingLoad.promise;
    const facet = new ScriptedFrameFacet(frame(10), target(11), [frame(11)]);
    const core = new PlaybackCore({
      backend,
      facets: { framePresentation: facet },
    });
    const loading = core.load(SOURCE_A);
    await Promise.resolve();

    await expect(core.stepFrame("next")).resolves.toMatchObject({
      status: "unsupported",
    });
    expect(
      backend.calls.filter(({ method }) => method === "seek"),
    ).toHaveLength(0);

    pendingLoad.resolve();
    await loading;
  });

  it("seeking 态受控返回 unsupported 且不发起第二次 seek", async () => {
    const facet = new ScriptedFrameFacet(frame(10), target(11), [frame(11)]);
    const { backend, core } = await createFrameCore(facet);
    const pendingSeek = new Deferred<SeekResult>();
    let seekCount = 0;
    backend.seekHandler = (request) => {
      seekCount += 1;
      return seekCount === 1
        ? pendingSeek.promise
        : Promise.resolve({
            clamped: false,
            confirmedTime: request.targetTime,
            requestId: request.requestId,
            requestedTime: request.requestedTime,
            status: "completed",
            targetTime: request.targetTime,
          });
    };
    const seeking = core.seek(8);
    await Promise.resolve();

    await expect(core.stepFrame("next")).resolves.toMatchObject({
      status: "unsupported",
    });
    expect(
      backend.calls.filter(({ method }) => method === "seek"),
    ).toHaveLength(1);

    pendingSeek.resolve({
      clamped: false,
      confirmedTime: 8,
      requestId: 2,
      requestedTime: 8,
      status: "completed",
      targetTime: 8,
    });
    await seeking;
  });

  it("error 态受控返回 unsupported 且不发起 seek", async () => {
    const facet = new ScriptedFrameFacet(frame(10), target(11), [frame(11)]);
    const { backend, core } = await createFrameCore(facet);
    backend.emit({
      error: { category: "decode", message: "解码失败" },
      eventId: 0,
      requestId: 1,
      sourceEpoch: 1,
      sourceId: SOURCE_A.id,
      type: "error",
    });

    await expect(core.stepFrame("next")).resolves.toMatchObject({
      status: "unsupported",
    });
    expect(
      backend.calls.filter(({ method }) => method === "seek"),
    ).toHaveLength(0);
  });

  it("idle 与 disposed 态分别受控返回 unsupported 和 canceled", async () => {
    const facet = new ScriptedFrameFacet(frame(10), target(11), [frame(11)]);
    const idleBackend = new TrackingBackend();
    idleBackend.setSnapshot(exactSnapshot());
    const idleCore = new PlaybackCore({
      backend: idleBackend,
      facets: { framePresentation: facet },
    });
    await expect(idleCore.stepFrame("next")).resolves.toMatchObject({
      status: "unsupported",
    });
    expect(idleBackend.calls).toHaveLength(0);

    const { backend, core } = await createFrameCore(facet);
    core.dispose();
    const callCount = backend.calls.length;
    await expect(core.stepFrame("next")).resolves.toMatchObject({
      status: "canceled",
    });
    expect(backend.calls).toHaveLength(callCount);
  });

  it("切源安全取代等待中的逐帧，旧呈现结果不回写", async () => {
    const pendingFrame = new Deferred<PresentedFrame>();
    const facet = new ScriptedFrameFacet(frame(10), target(11), []);
    facet.waitForPresentedFrame = () => pendingFrame.promise;
    const { core } = await createFrameCore(facet);

    const stepping = core.stepFrame("next");
    await Promise.resolve();
    await Promise.resolve();
    await core.load(SOURCE_B);

    await expect(stepping).resolves.toMatchObject({ status: "superseded" });
    pendingFrame.resolve(frame(11));
    await Promise.resolve();
    expect(core.getSnapshot()).toMatchObject({
      error: null,
      sourceId: SOURCE_B.id,
    });
  });

  it("dispose 安全取消等待中的逐帧且不伪装网络错误", async () => {
    const pendingFrame = new Deferred<PresentedFrame>();
    const facet = new ScriptedFrameFacet(frame(10), target(11), []);
    facet.waitForPresentedFrame = () => pendingFrame.promise;
    const { core } = await createFrameCore(facet);

    const stepping = core.stepFrame("next");
    await Promise.resolve();
    await Promise.resolve();
    core.dispose();

    const result = await stepping;
    expect(result).toMatchObject({ status: "canceled" });
    expect(result.error?.category).not.toBe("network");
    expect(core.getSnapshot()).toMatchObject({
      error: null,
      state: "disposed",
    });
    pendingFrame.resolve(frame(11));
  });

  it("逐帧事件顶层 requestId 来自完整结果，getter 持续暴露上次逐帧精度", async () => {
    const facet = new ScriptedFrameFacet(frame(10), target(11), [frame(11)]);
    const { core } = await createFrameCore(facet);
    const events: Array<{
      readonly requestId: number;
      readonly resultRequestId: number;
      readonly type: string;
    }> = [];
    core.subscribe((event) => {
      if (
        event.type === "frameStepCompleted" ||
        event.type === "commandCompleted"
      ) {
        events.push({
          requestId: event.requestId,
          resultRequestId: event.result.requestId,
          type: event.type,
        });
      }
    });

    const result = await core.stepFrame("next");

    const expected = {
      requestId: result.requestId,
      resultRequestId: result.requestId,
    };
    expect(events).toEqual([
      { ...expected, type: "frameStepCompleted" },
      { ...expected, type: "commandCompleted" },
    ]);
    expect(core.getLastFrameStepResult()).toBe(result);
    expect(core.getLastFrameStepResult()?.precision).toBe("exact-verified");
  });
});
