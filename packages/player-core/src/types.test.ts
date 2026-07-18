import { describe, expectTypeOf, it } from "vitest";
import type {
  AdjacentFrameTarget,
  FramePresentationFacet,
  FrameStepDirection,
  FrameStepResult,
  LoadControlFacet,
  PlaybackBackendBinding,
  PlaybackBackendEvent,
  PlaybackCapabilities,
  PlaybackCommandContext,
  PlaybackEvent,
  PlaybackSnapshot,
  PlaybackTrack,
  PresentedFrame,
  PreviewFacet,
  QualityFacet,
  SeekTier,
  TrackFacet,
  TrackSelectionResult,
  TrackSource,
  TrackSwitchCapability,
} from "./index";

describe("player-core 公共分面类型", () => {
  it("五个可选分面保持独立且可选", () => {
    expectTypeOf<PlaybackBackendBinding["facets"]>().toEqualTypeOf<
      | {
          readonly framePresentation?: FramePresentationFacet;
          readonly loadControl?: LoadControlFacet;
          readonly preview?: PreviewFacet;
          readonly quality?: QualityFacet;
          readonly tracks?: TrackFacet;
        }
      | undefined
    >();
  });

  it("五个分面的命令入口统一携带源代次上下文", () => {
    expectTypeOf<
      Parameters<FramePresentationFacet["waitForPresentedFrame"]>[0]
    >().toEqualTypeOf<PlaybackCommandContext>();
    expectTypeOf<
      Parameters<PreviewFacet["setTrack"]>[1]
    >().toEqualTypeOf<PlaybackCommandContext>();
    expectTypeOf<
      Parameters<TrackFacet["selectTrack"]>[2]
    >().toEqualTypeOf<PlaybackCommandContext>();
    expectTypeOf<
      Parameters<QualityFacet["selectQuality"]>[1]
    >().toEqualTypeOf<PlaybackCommandContext>();
    expectTypeOf<
      Parameters<LoadControlFacet["startLoading"]>[0]
    >().toEqualTypeOf<PlaybackCommandContext>();
  });

  it("轨道模型保持基础字段兼容并扩展端无关元数据", () => {
    expectTypeOf<TrackSource>().toEqualTypeOf<
      "sidecar" | "uploaded" | "embedded" | "derived"
    >();
    expectTypeOf<TrackSwitchCapability>().toEqualTypeOf<
      "seamless" | "reload" | "unsupported"
    >();
    expectTypeOf<PlaybackTrack>().toMatchObjectType<{
      readonly id: string;
      readonly kind: "audio" | "subtitle";
      readonly label: string;
      readonly format?: string;
      readonly codec?: string;
      readonly language?: string;
      readonly title?: string;
      readonly default?: boolean;
      readonly forced?: boolean;
      readonly available?: boolean;
      readonly capability?: TrackSwitchCapability;
      readonly unsupportedReason?: string;
      readonly source?: TrackSource;
      readonly streamIndex?: number;
    }>();
  });

  it("轨道选择结果携带意图、实际轨道和命令状态", () => {
    expectTypeOf<TrackSelectionResult["effectiveTrackId"]>().toEqualTypeOf<
      string | null
    >();
    expectTypeOf<TrackSelectionResult["error"]>().toEqualTypeOf<
      | {
          readonly category:
            "network" | "media" | "decode" | "unsupported" | "unknown";
          readonly code?: string;
          readonly message: string;
        }
      | undefined
    >();
    expectTypeOf<TrackSelectionResult["kind"]>().toEqualTypeOf<
      "audio" | "subtitle"
    >();
    expectTypeOf<TrackSelectionResult["requestId"]>().toBeNumber();
    expectTypeOf<TrackSelectionResult["selectedTrackId"]>().toEqualTypeOf<
      string | null
    >();
    expectTypeOf<TrackSelectionResult["status"]>().toEqualTypeOf<
      "completed" | "superseded" | "canceled" | "unsupported" | "failed"
    >();
  });

  it("后端事件统一携带命令 requestId", () => {
    expectTypeOf<PlaybackBackendEvent["requestId"]>().toBeNumber();
  });

  it("轨道能力字段保持可选以兼容旧后端", () => {
    expectTypeOf<PlaybackCapabilities["tracks"]>().toEqualTypeOf<
      "available" | "unavailable" | undefined
    >();
  });

  it("所有公开事件变体顶层统一携带 requestId", () => {
    expectTypeOf<PlaybackEvent["requestId"]>().toBeNumber();

    type SnapshotChanged = Extract<
      PlaybackEvent,
      { readonly type: "snapshotChanged" }
    >;
    type CapabilitiesChanged = Extract<
      PlaybackEvent,
      { readonly type: "capabilitiesChanged" }
    >;
    type CommandCompleted = Extract<
      PlaybackEvent,
      { readonly type: "commandCompleted" }
    >;
    type FrameStepCompleted = Extract<
      PlaybackEvent,
      { readonly type: "frameStepCompleted" }
    >;
    type TrackSelectionChanged = Extract<
      PlaybackEvent,
      { readonly type: "trackSelectionChanged" }
    >;
    type TrackSelectionCompleted = Extract<
      PlaybackEvent,
      { readonly type: "trackSelectionCompleted" }
    >;
    type SeekTierChanged = Extract<
      PlaybackEvent,
      { readonly type: "seekTierChanged" }
    >;
    type ErrorEvent = Extract<PlaybackEvent, { readonly type: "error" }>;
    expectTypeOf<SnapshotChanged["requestId"]>().toEqualTypeOf<
      SnapshotChanged["snapshot"]["requestId"]
    >();
    expectTypeOf<CapabilitiesChanged["requestId"]>().toBeNumber();
    expectTypeOf<CommandCompleted["requestId"]>().toEqualTypeOf<
      CommandCompleted["result"]["requestId"]
    >();
    expectTypeOf<CommandCompleted["snapshot"]>().toEqualTypeOf<
      PlaybackSnapshot | undefined
    >();
    expectTypeOf<FrameStepCompleted["requestId"]>().toEqualTypeOf<
      FrameStepCompleted["result"]["requestId"]
    >();
    expectTypeOf<FrameStepCompleted["snapshot"]>().toEqualTypeOf<
      PlaybackSnapshot | undefined
    >();
    expectTypeOf<TrackSelectionChanged["requestId"]>().toBeNumber();
    expectTypeOf<TrackSelectionCompleted["requestId"]>().toEqualTypeOf<
      TrackSelectionCompleted["result"]["requestId"]
    >();
    expectTypeOf<SeekTierChanged["requestId"]>().toBeNumber();
    expectTypeOf<ErrorEvent["requestId"]>().toBeNumber();
  });

  it("轨道选择事件显式携带源身份与选择状态", () => {
    type Changed = Extract<
      PlaybackEvent,
      { readonly type: "trackSelectionChanged" }
    >;
    type Completed = Extract<
      PlaybackEvent,
      { readonly type: "trackSelectionCompleted" }
    >;
    expectTypeOf<Changed["sourceId"]>().toEqualTypeOf<string>();
    expectTypeOf<Changed["sourceEpoch"]>().toBeNumber();
    expectTypeOf<Changed["kind"]>().toEqualTypeOf<"audio" | "subtitle">();
    expectTypeOf<Changed["selectedTrackId"]>().toEqualTypeOf<string | null>();
    expectTypeOf<Changed["effectiveTrackId"]>().toEqualTypeOf<string | null>();
    expectTypeOf<Completed["sourceId"]>().toEqualTypeOf<string | null>();
    expectTypeOf<Completed["sourceEpoch"]>().toBeNumber();
    expectTypeOf<Completed["result"]>().toEqualTypeOf<TrackSelectionResult>();
    expectTypeOf<Completed["snapshot"]>().toEqualTypeOf<
      PlaybackSnapshot | undefined
    >();
  });

  it("commandCompleted 事件显式携带源标识与源代次", () => {
    type CommandCompleted = Extract<
      PlaybackEvent,
      { readonly type: "commandCompleted" }
    >;
    expectTypeOf<CommandCompleted["sourceId"]>().toEqualTypeOf<string | null>();
    expectTypeOf<CommandCompleted["sourceEpoch"]>().toBeNumber();
  });

  it("SeekTier 是封闭六档且逐帧方向只有前后", () => {
    expectTypeOf<SeekTier>().toEqualTypeOf<
      | { readonly count: 1; readonly kind: "frame" }
      | { readonly kind: "seconds"; readonly value: 0.5 | 1 | 5 | 30 | 60 }
    >();
    expectTypeOf<FrameStepDirection>().toEqualTypeOf<"next" | "previous">();
  });

  it("FramePresentationFacet 只扩展逐帧所需契约", () => {
    expectTypeOf<
      ReturnType<FramePresentationFacet["getCurrentPresentedFrame"]>
    >().toEqualTypeOf<PresentedFrame | null>();
    expectTypeOf<
      ReturnType<FramePresentationFacet["getAdjacentFrameTarget"]>
    >().toEqualTypeOf<AdjacentFrameTarget | null>();
    expectTypeOf<
      ReturnType<FramePresentationFacet["getNominalFrameDuration"]>
    >().toEqualTypeOf<number | null>();
    expectTypeOf<
      ReturnType<FramePresentationFacet["waitForPresentedFrame"]>
    >().toEqualTypeOf<Promise<PresentedFrame>>();
  });

  it("逐帧完成事件携带完整 FrameStepResult", () => {
    type FrameStepCompleted = Extract<
      PlaybackEvent,
      { readonly type: "frameStepCompleted" }
    >;
    expectTypeOf<
      FrameStepCompleted["result"]
    >().toEqualTypeOf<FrameStepResult>();
  });

  it("档位变化事件携带前后档位与命令代次", () => {
    type TierChanged = Extract<
      PlaybackEvent,
      { readonly type: "seekTierChanged" }
    >;
    expectTypeOf<
      TierChanged["previousTier"]
    >().toEqualTypeOf<SeekTier | null>();
    expectTypeOf<TierChanged["tier"]>().toEqualTypeOf<SeekTier>();
    expectTypeOf<TierChanged["requestId"]>().toBeNumber();
  });
});
