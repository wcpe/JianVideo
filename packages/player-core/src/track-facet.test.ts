import { describe, expect, it } from "vitest";
import {
  PlaybackBackendError,
  PlaybackCore,
  type PlaybackCommandContext,
  type PlaybackEvent,
  type PlaybackSource,
  type PlaybackTrack,
  type TrackFacet,
  type TrackKind,
  type TrackSelectionState,
} from "./index";
import {
  Deferred,
  EMPTY_CAPABILITIES,
  FakePlaybackBackend,
  createSnapshot,
} from "./test-utils";

const SOURCE_A: PlaybackSource = { id: "source-a", mode: "stream" };
const SOURCE_B: PlaybackSource = { id: "source-b", mode: "adaptive" };
const AUDIO_A: PlaybackTrack = {
  available: true,
  capability: "seamless",
  codec: "aac",
  default: true,
  format: "mp4",
  id: "audio-a",
  kind: "audio",
  label: "中文",
  language: "zh-CN",
  source: "embedded",
  streamIndex: 1,
  title: "中文音轨",
};
const AUDIO_B: PlaybackTrack = {
  ...AUDIO_A,
  default: false,
  id: "audio-b",
  label: "English",
  language: "en",
};
const SUBTITLE_A: PlaybackTrack = {
  available: true,
  capability: "seamless",
  codec: "webvtt",
  forced: false,
  format: "vtt",
  id: "subtitle-a",
  kind: "subtitle",
  label: "中文字幕",
  language: "zh-CN",
  source: "uploaded",
  title: "中文字幕",
};

class FakeTrackFacet implements TrackFacet {
  readonly calls: Array<{
    readonly command: PlaybackCommandContext;
    readonly kind: TrackKind;
    readonly trackId: string | null;
  }> = [];
  readonly tracks: Record<TrackKind, readonly PlaybackTrack[]> = {
    audio: [AUDIO_A, AUDIO_B],
    subtitle: [SUBTITLE_A],
  };
  selectHandler:
    | ((
        kind: TrackKind,
        trackId: string | null,
        command: PlaybackCommandContext,
      ) => Promise<void>)
    | undefined;
  stateHandler: ((kind: TrackKind) => TrackSelectionState) | undefined;
  private states: Record<TrackKind, TrackSelectionState> = {
    audio: selectionState("audio", AUDIO_A.id),
    subtitle: selectionState("subtitle", SUBTITLE_A.id),
  };

  getTracks(kind: TrackKind): readonly PlaybackTrack[] {
    return this.tracks[kind];
  }

  getSelectionState(kind: TrackKind): TrackSelectionState {
    return this.stateHandler?.(kind) ?? this.states[kind];
  }

  selectTrack(
    kind: TrackKind,
    trackId: string | null,
    command: PlaybackCommandContext,
  ): Promise<void> {
    this.calls.push({ command, kind, trackId });
    if (this.selectHandler !== undefined) {
      return this.selectHandler(kind, trackId, command);
    }
    this.confirmState(kind, trackId, command);
    return Promise.resolve();
  }

  confirmState(
    kind: TrackKind,
    trackId: string | null,
    command: PlaybackCommandContext,
  ): void {
    this.setState(
      kind,
      trackId,
      command.sourceId,
      command.sourceEpoch,
      command.requestId,
    );
  }

  setState(
    kind: TrackKind,
    trackId: string | null,
    sourceId = SOURCE_A.id,
    sourceEpoch = 1,
    requestId = 0,
  ): void {
    this.states[kind] = {
      effectiveTrackId: trackId,
      kind,
      requestId,
      selectedTrackId: trackId,
      sourceEpoch,
      sourceId,
    };
  }
}

function selectionState(
  kind: TrackKind,
  trackId: string | null,
): TrackSelectionState {
  return {
    effectiveTrackId: trackId,
    kind,
    requestId: 0,
    selectedTrackId: trackId,
    sourceEpoch: 1,
    sourceId: SOURCE_A.id,
  };
}

function omitTrackField(
  track: PlaybackTrack,
  field: "available" | "capability",
): PlaybackTrack {
  if (field === "available") {
    const { available, ...copy } = track;
    void available;
    return copy;
  }
  const { capability, ...copy } = track;
  void capability;
  return copy;
}

function capabilitiesWithoutTracks(): typeof EMPTY_CAPABILITIES {
  const { tracks, ...copy } = EMPTY_CAPABILITIES;
  void tracks;
  return copy;
}

async function createLoadedCore(
  facet?: TrackFacet,
): Promise<{ backend: FakePlaybackBackend; core: PlaybackCore }> {
  const backend = new FakePlaybackBackend();
  backend.setSnapshot(
    createSnapshot({
      capabilities: { ...EMPTY_CAPABILITIES, tracks: "available" },
      sourceId: SOURCE_A.id,
      state: "ready",
    }),
  );
  const binding =
    facet === undefined ? { backend } : { backend, facets: { tracks: facet } };
  const core = new PlaybackCore(binding);
  await core.load(SOURCE_A);
  return { backend, core };
}

function trackEvents(events: readonly PlaybackEvent[]): PlaybackEvent[] {
  return events.filter(
    (event) =>
      event.type === "trackSelectionChanged" ||
      event.type === "trackSelectionCompleted",
  );
}

describe("PlaybackCore TrackFacet", () => {
  it("枚举两类轨道并保留扩展元数据与当前选择", async () => {
    const facet = new FakeTrackFacet();
    const { core } = await createLoadedCore(facet);

    expect(core.getTracks("audio")).toEqual([AUDIO_A, AUDIO_B]);
    expect(core.getTracks("subtitle")).toEqual([SUBTITLE_A]);
    expect(core.getTrackSelection("audio")).toMatchObject({
      effectiveTrackId: AUDIO_A.id,
      requestId: 0,
      selectedTrackId: AUDIO_A.id,
    });
    expect(core.getTrackSelection("subtitle")).toMatchObject({
      effectiveTrackId: SUBTITLE_A.id,
      requestId: 0,
      selectedTrackId: SUBTITLE_A.id,
    });
  });

  it("能力快照保持后端真值，不根据轨道列表数量推断", async () => {
    const facet = new FakeTrackFacet();
    facet.tracks.audio = [];
    facet.tracks.subtitle = [];
    const available = await createLoadedCore(facet);
    expect(available.core.getSnapshot().capabilities.tracks).toBe("available");

    const backend = new FakePlaybackBackend();
    backend.setSnapshot(
      createSnapshot({
        capabilities: EMPTY_CAPABILITIES,
        sourceId: SOURCE_A.id,
        state: "ready",
      }),
    );
    const unavailable = new PlaybackCore({
      backend,
      facets: { tracks: new FakeTrackFacet() },
    });
    await unavailable.load(SOURCE_A);
    expect(unavailable.getTracks("audio")).toHaveLength(2);
    expect(unavailable.getSnapshot().capabilities.tracks).toBe("unavailable");
  });

  it("能力快照缺少 tracks 时按 unavailable 处理", async () => {
    const facet = new FakeTrackFacet();
    const backend = new FakePlaybackBackend();
    backend.setSnapshot(
      createSnapshot({
        capabilities: capabilitiesWithoutTracks(),
        sourceId: SOURCE_A.id,
        state: "ready",
      }),
    );
    const core = new PlaybackCore({ backend, facets: { tracks: facet } });
    await core.load(SOURCE_A);

    expect(core.getSnapshot().capabilities.tracks).toBe("unavailable");
    await expect(core.selectTrack("audio", AUDIO_B.id)).resolves.toMatchObject({
      status: "unsupported",
    });
    expect(facet.calls).toEqual([]);
  });

  it("选择期间先更新 selected 事件，effective 等待后端确认后收敛", async () => {
    const facet = new FakeTrackFacet();
    const pending = new Deferred<void>();
    facet.selectHandler = async (kind, trackId, command) => {
      await pending.promise;
      facet.confirmState(kind, trackId, command);
    };
    const { core } = await createLoadedCore(facet);
    const events: PlaybackEvent[] = [];
    core.subscribe((event) => events.push(event));

    const selection = core.selectTrack("audio", AUDIO_B.id);
    await Promise.resolve();

    expect(core.getTrackSelection("audio")).toMatchObject({
      effectiveTrackId: AUDIO_A.id,
      requestId: 2,
      selectedTrackId: AUDIO_B.id,
    });
    expect(trackEvents(events)[0]).toMatchObject({
      effectiveTrackId: AUDIO_A.id,
      kind: "audio",
      requestId: 2,
      selectedTrackId: AUDIO_B.id,
      sourceEpoch: 1,
      sourceId: SOURCE_A.id,
      type: "trackSelectionChanged",
    });

    pending.resolve();
    await expect(selection).resolves.toMatchObject({
      effectiveTrackId: AUDIO_B.id,
      kind: "audio",
      requestId: 2,
      selectedTrackId: AUDIO_B.id,
      status: "completed",
    });
    expect(core.getTrackSelection("audio")).toMatchObject({
      effectiveTrackId: AUDIO_B.id,
      requestId: 2,
      selectedTrackId: AUDIO_B.id,
    });
  });

  it("字幕关闭沿用统一选择路径并以 null 确认生效", async () => {
    const facet = new FakeTrackFacet();
    const { core } = await createLoadedCore(facet);

    await expect(core.selectTrack("subtitle", null)).resolves.toMatchObject({
      effectiveTrackId: null,
      kind: "subtitle",
      selectedTrackId: null,
      status: "completed",
    });
    expect(facet.calls).toHaveLength(1);
    expect(facet.calls[0]).toMatchObject({ kind: "subtitle", trackId: null });
    expect(core.getTrackSelection("subtitle")).toMatchObject({
      effectiveTrackId: null,
      requestId: 2,
      selectedTrackId: null,
    });
  });

  it("同步 throw 前不发布临时 selected 状态", async () => {
    const facet = new FakeTrackFacet();
    facet.selectHandler = () => {
      throw new PlaybackBackendError("media", "同步切换失败");
    };
    const { core } = await createLoadedCore(facet);
    const events: PlaybackEvent[] = [];
    core.subscribe((event) => events.push(event));

    await expect(core.selectTrack("audio", AUDIO_B.id)).resolves.toMatchObject({
      effectiveTrackId: AUDIO_A.id,
      selectedTrackId: AUDIO_A.id,
      status: "failed",
    });
    expect(trackEvents(events)).not.toContainEqual(
      expect.objectContaining({
        selectedTrackId: AUDIO_B.id,
        type: "trackSelectionChanged",
      }),
    );
  });

  it("同步 throw 后按 facet 已写入的当前代次实际状态回滚", async () => {
    const facet = new FakeTrackFacet();
    facet.selectHandler = (kind, trackId, command) => {
      facet.confirmState(kind, trackId, command);
      throw new PlaybackBackendError("media", "确认后同步失败");
    };
    const { core } = await createLoadedCore(facet);

    await expect(core.selectTrack("audio", AUDIO_B.id)).resolves.toMatchObject({
      effectiveTrackId: AUDIO_B.id,
      requestId: 2,
      selectedTrackId: AUDIO_B.id,
      status: "failed",
    });
    expect(core.getTrackSelection("audio")).toMatchObject({
      effectiveTrackId: AUDIO_B.id,
      requestId: 2,
      selectedTrackId: AUDIO_B.id,
    });
  });

  it("新请求同步 throw 仍立即取代旧请求，旧确认迟到不污染缓存", async () => {
    const facet = new FakeTrackFacet();
    const first = new Deferred<void>();
    let callCount = 0;
    facet.selectHandler = (kind, trackId, command) => {
      callCount += 1;
      if (callCount === 2) {
        throw new PlaybackBackendError("media", "同步切换失败");
      }
      return first.promise.then(() => {
        facet.confirmState(kind, trackId, command);
      });
    };
    const { core } = await createLoadedCore(facet);
    const events: PlaybackEvent[] = [];
    core.subscribe((event) => events.push(event));

    const firstSelection = core.selectTrack("audio", AUDIO_B.id);
    await Promise.resolve();
    const secondSelection = core.selectTrack("audio", AUDIO_B.id);

    await expect(firstSelection).resolves.toMatchObject({
      requestId: 2,
      status: "superseded",
    });
    await expect(secondSelection).resolves.toMatchObject({
      effectiveTrackId: AUDIO_A.id,
      requestId: 3,
      selectedTrackId: AUDIO_A.id,
      status: "failed",
    });
    expect(events).not.toContainEqual(
      expect.objectContaining({
        requestId: 3,
        selectedTrackId: AUDIO_B.id,
        type: "trackSelectionChanged",
      }),
    );

    first.resolve();
    await Promise.resolve();
    expect(core.getTrackSelection("audio")).toMatchObject({
      effectiveTrackId: AUDIO_A.id,
      selectedTrackId: AUDIO_A.id,
    });
  });

  it.each([
    [
      "同步 throw",
      () => {
        throw new PlaybackBackendError("media", "同步切换失败");
      },
    ],
    [
      "Promise reject",
      () => Promise.reject(new PlaybackBackendError("media", "异步切换失败")),
    ],
  ])("%s 时读取后端实际状态并回滚 selected/effective", async (_, fail) => {
    const facet = new FakeTrackFacet();
    facet.selectHandler = fail;
    const { core } = await createLoadedCore(facet);

    await expect(core.selectTrack("audio", AUDIO_B.id)).resolves.toMatchObject({
      effectiveTrackId: AUDIO_A.id,
      error: { category: "media" },
      selectedTrackId: AUDIO_A.id,
      status: "failed",
    });
    expect(core.getTrackSelection("audio")).toMatchObject({
      effectiveTrackId: AUDIO_A.id,
      selectedTrackId: AUDIO_A.id,
    });
  });

  it("后请求完成后忽略旧确认迟到，并为每个请求保留准确完成快照", async () => {
    const facet = new FakeTrackFacet();
    const first = new Deferred<void>();
    const second = new Deferred<void>();
    let count = 0;
    facet.selectHandler = async (kind, trackId, command) => {
      count += 1;
      await (count === 1 ? first.promise : second.promise);
      facet.confirmState(kind, trackId, command);
    };
    const { core } = await createLoadedCore(facet);
    const events: PlaybackEvent[] = [];
    core.subscribe((event) => events.push(event));

    const firstSelection = core.selectTrack("audio", AUDIO_B.id);
    await Promise.resolve();
    const secondSelection = core.selectTrack("audio", AUDIO_A.id);

    await expect(firstSelection).resolves.toMatchObject({
      requestId: 2,
      status: "superseded",
    });
    second.resolve();
    await expect(secondSelection).resolves.toMatchObject({
      effectiveTrackId: AUDIO_A.id,
      requestId: 3,
      selectedTrackId: AUDIO_A.id,
      status: "completed",
    });
    first.resolve();
    await Promise.resolve();
    expect(core.getTrackSelection("audio")).toMatchObject({
      effectiveTrackId: AUDIO_A.id,
      requestId: 3,
      selectedTrackId: AUDIO_A.id,
    });

    const completed = events.filter(
      (
        event,
      ): event is Extract<
        PlaybackEvent,
        { readonly type: "trackSelectionCompleted" }
      > => event.type === "trackSelectionCompleted",
    );
    const firstCompleted = completed.find((event) => event.requestId === 2);
    expect(firstCompleted).toMatchObject({
      result: {
        requestId: 2,
        selectedTrackId: AUDIO_B.id,
        status: "superseded",
      },
      snapshot: { requestId: 2, sourceId: SOURCE_A.id, state: "ready" },
    });
    const secondCompleted = completed.find((event) => event.requestId === 3);
    expect(secondCompleted).toMatchObject({
      result: {
        requestId: 3,
        selectedTrackId: AUDIO_A.id,
        status: "completed",
      },
      snapshot: { requestId: 3, sourceId: SOURCE_A.id, state: "ready" },
    });
  });

  it("getter 每次同步读取 facet 外部变化", async () => {
    const facet = new FakeTrackFacet();
    const { core } = await createLoadedCore(facet);

    expect(core.getTrackSelection("audio")).toMatchObject({
      effectiveTrackId: AUDIO_A.id,
    });
    facet.setState("audio", AUDIO_B.id, SOURCE_A.id, 1, 1);
    expect(core.getTrackSelection("audio")).toMatchObject({
      effectiveTrackId: AUDIO_B.id,
      requestId: 1,
      selectedTrackId: AUDIO_B.id,
    });
  });

  it("无代次 state 仅作初始值，选择历史后需更高代次才接受外部变化", async () => {
    const facet = new FakeTrackFacet();
    const { requestId, ...initial } = selectionState("audio", AUDIO_A.id);
    void requestId;
    let state: TrackSelectionState = initial;
    facet.stateHandler = () => state;
    facet.selectHandler = (kind, trackId, command) => {
      state = {
        ...selectionState(kind, trackId),
        requestId: command.requestId,
      };
      return Promise.resolve();
    };
    const { core } = await createLoadedCore(facet);

    expect(core.getTrackSelection("audio")).toMatchObject({
      effectiveTrackId: AUDIO_A.id,
      requestId: 0,
    });
    await expect(core.selectTrack("audio", AUDIO_B.id)).resolves.toMatchObject({
      requestId: 2,
      status: "completed",
    });

    state = initial;
    expect(core.getTrackSelection("audio")).toMatchObject({
      effectiveTrackId: AUDIO_B.id,
      requestId: 2,
    });
    state = { ...selectionState("audio", AUDIO_A.id), requestId: 3 };
    expect(core.getTrackSelection("audio")).toMatchObject({
      effectiveTrackId: AUDIO_A.id,
      requestId: 3,
    });
  });

  it.each([
    {
      name: "非对象",
      state: 42,
    },
    {
      name: "kind 不一致",
      state: { ...selectionState("audio", AUDIO_A.id), kind: "subtitle" },
    },
    {
      name: "selectedTrackId 类型非法",
      state: { ...selectionState("audio", AUDIO_A.id), selectedTrackId: 1 },
    },
    {
      name: "effectiveTrackId 类型非法",
      state: {
        ...selectionState("audio", AUDIO_A.id),
        effectiveTrackId: false,
      },
    },
    {
      name: "requestId 非有限整数",
      state: { ...selectionState("audio", AUDIO_A.id), requestId: Number.NaN },
    },
    {
      name: "requestId 为负数",
      state: { ...selectionState("audio", AUDIO_A.id), requestId: -1 },
    },
    {
      name: "sourceEpoch 非有限整数",
      state: {
        ...selectionState("audio", AUDIO_A.id),
        sourceEpoch: Number.NaN,
      },
    },
    {
      name: "sourceId 类型非法",
      state: { ...selectionState("audio", AUDIO_A.id), sourceId: 1 },
    },
    {
      name: "selectedTrackId 不存在",
      state: {
        ...selectionState("audio", AUDIO_A.id),
        selectedTrackId: "missing",
      },
    },
    {
      name: "effectiveTrackId 不存在",
      state: {
        ...selectionState("audio", AUDIO_A.id),
        effectiveTrackId: "missing",
      },
    },
  ])("严格拒绝 $name 的选择 state", async ({ state }) => {
    const facet = new FakeTrackFacet();
    facet.stateHandler = () => state as TrackSelectionState;
    const { core } = await createLoadedCore(facet);

    expect(core.getTrackSelection("audio")).toBeNull();
    await expect(core.selectTrack("audio", AUDIO_B.id)).resolves.toMatchObject({
      status: "failed",
    });
    expect(facet.calls).toEqual([]);
  });

  it.each([
    { effectiveTrackId: AUDIO_A.id, selectedTrackId: null },
    { effectiveTrackId: null, selectedTrackId: AUDIO_A.id },
    { effectiveTrackId: null, selectedTrackId: null },
  ])(
    "允许音轨 selected/effective 独立为空且 getter 保持后端真值",
    async (state) => {
      const facet = new FakeTrackFacet();
      facet.stateHandler = () => ({
        ...selectionState("audio", AUDIO_A.id),
        ...state,
      });
      const { core } = await createLoadedCore(facet);

      expect(core.getTrackSelection("audio")).toMatchObject(state);
    },
  );

  it("允许切换期间 selected 指向可用目标且 effective 保持实际轨道", async () => {
    const facet = new FakeTrackFacet();
    facet.stateHandler = () => ({
      ...selectionState("audio", AUDIO_A.id),
      selectedTrackId: AUDIO_B.id,
    });
    const { core } = await createLoadedCore(facet);

    expect(core.getTrackSelection("audio")).toMatchObject({
      effectiveTrackId: AUDIO_A.id,
      selectedTrackId: AUDIO_B.id,
    });
  });

  it("当前 effective 音轨不可切仍是合法 state，选择另一不可切目标受控 unsupported", async () => {
    const facet = new FakeTrackFacet();
    const current = {
      ...AUDIO_A,
      capability: "unsupported" as const,
      unsupportedReason: "当前音轨不可切换",
    };
    const target = {
      ...AUDIO_B,
      capability: "unsupported" as const,
      unsupportedReason: "目标音轨不可切换",
    };
    facet.tracks.audio = [current, target];
    const { core } = await createLoadedCore(facet);

    expect(core.getTrackSelection("audio")).toMatchObject({
      effectiveTrackId: AUDIO_A.id,
      selectedTrackId: AUDIO_A.id,
    });
    await expect(core.selectTrack("audio", AUDIO_B.id)).resolves.toMatchObject({
      effectiveTrackId: AUDIO_A.id,
      error: { category: "unsupported", message: "目标音轨不可切换" },
      selectedTrackId: AUDIO_A.id,
      status: "unsupported",
    });
    expect(facet.calls).toEqual([]);
  });

  it("严格拒绝不可用轨道出现在选择 state", async () => {
    const facet = new FakeTrackFacet();
    facet.tracks.audio = [AUDIO_A, { ...AUDIO_B, available: false }];
    facet.stateHandler = () => selectionState("audio", AUDIO_B.id);
    const { core } = await createLoadedCore(facet);

    expect(core.getTrackSelection("audio")).toBeNull();
    await expect(core.selectTrack("audio", AUDIO_A.id)).resolves.toMatchObject({
      status: "failed",
    });
    expect(facet.calls).toEqual([]);
  });

  it("选择完成但 effective 未确认目标时返回 unsupported", async () => {
    const facet = new FakeTrackFacet();
    facet.selectHandler = () => Promise.resolve();
    const { core } = await createLoadedCore(facet);

    await expect(core.selectTrack("audio", AUDIO_B.id)).resolves.toMatchObject({
      effectiveTrackId: AUDIO_A.id,
      selectedTrackId: AUDIO_A.id,
      status: "unsupported",
    });
  });

  it("选择完成但 effective 仍为空时不能判定成功", async () => {
    const facet = new FakeTrackFacet();
    let state = selectionState("audio", AUDIO_A.id);
    facet.stateHandler = () => state;
    facet.selectHandler = (kind, trackId, command) => {
      state = {
        ...selectionState(kind, trackId),
        effectiveTrackId: null,
        requestId: command.requestId,
      };
      return Promise.resolve();
    };
    const { core } = await createLoadedCore(facet);

    await expect(core.selectTrack("audio", AUDIO_B.id)).resolves.toMatchObject({
      effectiveTrackId: null,
      status: "unsupported",
    });
  });

  it("getSelectionState 抛出 unsupported 时保留错误类别", async () => {
    const facet = new FakeTrackFacet();
    const { core } = await createLoadedCore(facet);
    facet.stateHandler = () => {
      throw new PlaybackBackendError("unsupported", "后端无法读取轨道状态");
    };

    await expect(core.selectTrack("audio", AUDIO_B.id)).resolves.toMatchObject({
      error: { category: "unsupported", message: "后端无法读取轨道状态" },
      status: "unsupported",
    });
  });

  it("非法 kind 不发布轨道公共事件", async () => {
    const facet = new FakeTrackFacet();
    const { core } = await createLoadedCore(facet);
    const events: PlaybackEvent[] = [];
    core.subscribe((event) => events.push(event));

    await expect(
      core.selectTrack("video" as TrackKind, "video-a"),
    ).resolves.toMatchObject({ status: "unsupported" });
    expect(trackEvents(events)).toEqual([]);
  });

  it.each([
    { name: "缺 available", track: omitTrackField(AUDIO_B, "available") },
    { name: "缺 capability", track: omitTrackField(AUDIO_B, "capability") },
  ])("$name 时保守返回 unsupported", async ({ track }) => {
    const facet = new FakeTrackFacet();
    facet.tracks.audio = [AUDIO_A, track];
    const { core } = await createLoadedCore(facet);

    await expect(core.selectTrack("audio", AUDIO_B.id)).resolves.toMatchObject({
      status: "unsupported",
    });
    expect(facet.calls).toEqual([]);
  });

  it("切源隔离旧选择结果并按新源重新读取选择状态", async () => {
    const facet = new FakeTrackFacet();
    const pending = new Deferred<void>();
    facet.selectHandler = async () => pending.promise;
    const { core } = await createLoadedCore(facet);

    const selection = core.selectTrack("audio", AUDIO_B.id);
    await Promise.resolve();
    facet.setState("audio", AUDIO_B.id, SOURCE_B.id, 2);
    await core.load(SOURCE_B);

    await expect(selection).resolves.toMatchObject({
      requestId: 2,
      status: "superseded",
    });
    expect(core.getTrackSelection("audio")).toMatchObject({
      effectiveTrackId: AUDIO_B.id,
      selectedTrackId: AUDIO_B.id,
      sourceEpoch: 2,
      sourceId: SOURCE_B.id,
    });
    pending.resolve();
  });

  it("dispose 取消待处理选择且旧结果不得覆盖终态", async () => {
    const facet = new FakeTrackFacet();
    const pending = new Deferred<void>();
    facet.selectHandler = async (kind, trackId, command) => {
      await pending.promise;
      facet.confirmState(kind, trackId, command);
    };
    const { core } = await createLoadedCore(facet);

    const selection = core.selectTrack("audio", AUDIO_B.id);
    await Promise.resolve();
    core.dispose();

    await expect(selection).resolves.toMatchObject({
      requestId: 2,
      status: "canceled",
    });
    pending.resolve();
    await Promise.resolve();
    expect(core.getSnapshot().state).toBe("disposed");
  });

  it("无 facet、能力不可用、非法 kind/track 和轨道不支持均受控 unsupported 且不调用后端", async () => {
    const withoutFacet = await createLoadedCore();
    await expect(
      withoutFacet.core.selectTrack("audio", AUDIO_A.id),
    ).resolves.toMatchObject({ status: "unsupported" });

    const facet = new FakeTrackFacet();
    const backend = new FakePlaybackBackend();
    backend.setSnapshot(
      createSnapshot({
        capabilities: EMPTY_CAPABILITIES,
        sourceId: SOURCE_A.id,
        state: "ready",
      }),
    );
    const unavailable = new PlaybackCore({
      backend,
      facets: { tracks: facet },
    });
    await unavailable.load(SOURCE_A);
    await expect(
      unavailable.selectTrack("audio", AUDIO_A.id),
    ).resolves.toMatchObject({ status: "unsupported" });

    const available = await createLoadedCore(facet);
    await expect(
      available.core.selectTrack("video" as TrackKind, "video-a"),
    ).resolves.toMatchObject({ status: "unsupported" });
    await expect(
      available.core.selectTrack("audio", "missing"),
    ).resolves.toMatchObject({ status: "unsupported" });
    await expect(
      available.core.selectTrack("audio", null),
    ).resolves.toMatchObject({ status: "unsupported" });

    const unsupportedTrack = {
      ...AUDIO_B,
      capability: "unsupported" as const,
      unsupportedReason: "当前路径不可切换",
    };
    facet.tracks.audio = [AUDIO_A, unsupportedTrack];
    await expect(
      available.core.selectTrack("audio", AUDIO_B.id),
    ).resolves.toMatchObject({
      error: { category: "unsupported", message: "当前路径不可切换" },
      status: "unsupported",
    });
    expect(facet.calls).toEqual([]);
    expect(available.core.getSnapshot().error).toBeNull();
  });

  it("选择事件顶层 requestId 与源身份一致，完成事件携带状态和错误", async () => {
    const facet = new FakeTrackFacet();
    facet.selectHandler = () =>
      Promise.reject(new PlaybackBackendError("unsupported", "后端不支持切换"));
    const { core } = await createLoadedCore(facet);
    const events: PlaybackEvent[] = [];
    core.subscribe((event) => events.push(event));

    const result = await core.selectTrack("audio", AUDIO_B.id);
    const selectionEvents = trackEvents(events) as Array<
      Extract<
        PlaybackEvent,
        { readonly type: "trackSelectionChanged" | "trackSelectionCompleted" }
      >
    >;

    expect(selectionEvents).toHaveLength(3);
    for (const event of selectionEvents) {
      expect(event.requestId).toBe(result.requestId);
      expect(event.sourceEpoch).toBe(1);
      expect(event.sourceId).toBe(SOURCE_A.id);
    }
    expect(selectionEvents.at(-1)).toMatchObject({
      result: {
        effectiveTrackId: AUDIO_A.id,
        error: { category: "unsupported", message: "后端不支持切换" },
        kind: "audio",
        requestId: result.requestId,
        selectedTrackId: AUDIO_A.id,
        status: "unsupported",
      },
      type: "trackSelectionCompleted",
    });
  });
});
