import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

import { createPreviewFacet } from "./index";
import type {
  PlaybackCommandContext,
  PreparedPreviewCue,
  PreparedPreviewTrack,
  PreviewTrackState,
} from "./types";

const COMMAND_A: PlaybackCommandContext = {
  requestId: 10,
  sourceEpoch: 1,
  sourceId: "source-a",
};

function cue(
  startTime: number,
  endTime: number,
  assetId = `sprite-${String(startTime)}`,
): PreparedPreviewCue {
  return {
    endTime,
    sprite: {
      assetId,
      height: 90,
      width: 160,
      x: startTime + 1,
      y: startTime + 2,
    },
    startTime,
  };
}

function track(
  cues: readonly PreparedPreviewCue[],
  overrides: Partial<PreparedPreviewTrack> = {},
): PreparedPreviewTrack {
  return {
    cues,
    generationId: "generation-a",
    mediaId: "media-a",
    profileId: "profile-a",
    sourceFingerprint: "fingerprint-a",
    ...overrides,
  };
}

function expectReady(state: PreviewTrackState, command = COMMAND_A): void {
  expect(state).toEqual({
    generationId: "generation-a",
    mediaId: "media-a",
    profileId: "profile-a",
    requestId: command.requestId,
    sourceEpoch: command.sourceEpoch,
    sourceId: command.sourceId,
    status: "ready",
  });
}

describe("PreviewFacet", () => {
  it("以不可变空快照启动并返回独立快照", () => {
    const facet = createPreviewFacet();
    const first = facet.getState();

    expect(first).toEqual({
      generationId: null,
      mediaId: null,
      profileId: null,
      requestId: 0,
      sourceEpoch: 0,
      sourceId: null,
      status: "empty",
    });
    expect(Object.isFrozen(first)).toBe(true);
    expect(facet.getState()).not.toBe(first);
  });

  it("命中首尾 cue、缝隙与半开区间边界", () => {
    const facet = createPreviewFacet();
    facet.setTrack(track([cue(0, 2, "first"), cue(3, 5, "last")]), COMMAND_A);

    expect(facet.hitTest(0, COMMAND_A)).toMatchObject({
      generationId: "generation-a",
      startTime: 0,
    });
    expect(facet.hitTest(1.999, COMMAND_A)?.sprite.assetId).toBe("first");
    expect(facet.hitTest(2, COMMAND_A)).toBeNull();
    expect(facet.hitTest(2.5, COMMAND_A)).toBeNull();
    expect(facet.hitTest(3, COMMAND_A)?.sprite.assetId).toBe("last");
    expect(facet.hitTest(4.999, COMMAND_A)).toMatchObject({
      endTime: 5,
      profileId: "profile-a",
    });
    expect(facet.hitTest(5, COMMAND_A)).toBeNull();
  });

  it("首格精灵坐标可从原点开始", () => {
    const facet = createPreviewFacet();
    const firstCell = {
      ...cue(0, 2),
      sprite: { ...cue(0, 2).sprite, x: 0, y: 0 },
    };

    facet.setTrack(track([firstCell]), COMMAND_A);

    expect(facet.hitTest(1, COMMAND_A)?.sprite).toMatchObject({ x: 0, y: 0 });
  });

  it("相邻 cue 的接缝归属后一个 cue", () => {
    const facet = createPreviewFacet();
    facet.setTrack(track([cue(0, 2, "first"), cue(2, 4, "second")]), COMMAND_A);

    expect(facet.hitTest(2, COMMAND_A)?.sprite.assetId).toBe("second");
    expect(facet.hitTest(4, COMMAND_A)).toBeNull();
  });

  it("generation 与 profile 切换后只返回当前轨道元数据", () => {
    const facet = createPreviewFacet();
    facet.setTrack(track([cue(0, 2)]), COMMAND_A);
    facet.setTrack(
      track([cue(0, 2)], {
        generationId: "generation-b",
        profileId: "profile-b",
      }),
      { ...COMMAND_A, requestId: 11 },
    );

    expect(facet.hitTest(1, { ...COMMAND_A, requestId: 11 })).toMatchObject({
      generationId: "generation-b",
      profileId: "profile-b",
    });
  });

  it("hitTest 仅接受当前源代次且请求不得早于轨道上下文", () => {
    const facet = createPreviewFacet();
    facet.setTrack(track([cue(0, 2)]), COMMAND_A);

    expect(facet.hitTest(1, { ...COMMAND_A, requestId: 9 })).toBeNull();
    expect(facet.hitTest(1, { ...COMMAND_A, sourceId: "source-b" })).toBeNull();
    expect(facet.hitTest(1, { ...COMMAND_A, sourceEpoch: 2 })).toBeNull();
    expect(facet.hitTest(1, { ...COMMAND_A, requestId: 10 })).not.toBeNull();
    expect(facet.hitTest(1, { ...COMMAND_A, requestId: 99 })).not.toBeNull();
  });

  it.each([Number.NaN, Number.POSITIVE_INFINITY, Number.NEGATIVE_INFINITY])(
    "非有限时间 %s 不命中",
    (mediaTime) => {
      const facet = createPreviewFacet();
      facet.setTrack(track([cue(0, 2)]), COMMAND_A);

      expect(facet.hitTest(mediaTime, COMMAND_A)).toBeNull();
    },
  );

  it("旧 sourceEpoch 与同代次旧 requestId 不得覆盖当前轨道", () => {
    const facet = createPreviewFacet();
    const currentCommand = {
      requestId: 20,
      sourceEpoch: 2,
      sourceId: "source-b",
    };
    facet.setTrack(track([cue(0, 2, "current")]), currentCommand);

    const staleEpochState = facet.setTrack(
      track([cue(0, 2, "stale-epoch")], { generationId: "stale-epoch" }),
      { requestId: 99, sourceEpoch: 1, sourceId: "source-a" },
    );
    const staleRequestState = facet.setTrack(
      track([cue(0, 2, "stale-request")], { generationId: "stale-request" }),
      { ...currentCommand, requestId: 19 },
    );

    expectReady(staleEpochState, currentCommand);
    expectReady(staleRequestState, currentCommand);
    expect(facet.hitTest(1, currentCommand)?.sprite.assetId).toBe("current");
  });

  it("旧命令在轨道校验前忽略，非法迟到响应也不抛出", () => {
    const facet = createPreviewFacet();
    const currentCommand = {
      requestId: 20,
      sourceEpoch: 2,
      sourceId: "source-b",
    };
    facet.setTrack(track([cue(0, 2, "current")]), currentCommand);
    const invalidTrack = track([]);

    expect(() =>
      facet.setTrack(invalidTrack, {
        requestId: 99,
        sourceEpoch: 1,
        sourceId: "source-a",
      }),
    ).not.toThrow();
    expect(() =>
      facet.setTrack(invalidTrack, { ...currentCommand, requestId: 19 }),
    ).not.toThrow();
    expect(() =>
      facet.setTrack(invalidTrack, {
        ...currentCommand,
        requestId: 99,
        sourceId: "source-c",
      }),
    ).not.toThrow();
    expect(facet.hitTest(1, currentCommand)?.sprite.assetId).toBe("current");
  });

  it("同代次不同 sourceId 即使请求更新也不得覆盖当前轨道", () => {
    const facet = createPreviewFacet();
    facet.setTrack(track([cue(0, 2, "current")]), COMMAND_A);

    const rejectedState = facet.setTrack(
      track([cue(0, 2, "other-source")], { generationId: "other-source" }),
      { ...COMMAND_A, requestId: 99, sourceId: "source-b" },
    );

    expectReady(rejectedState);
    expect(facet.hitTest(1, COMMAND_A)?.sprite.assetId).toBe("current");
  });

  it("更高 sourceEpoch 可切换 sourceId 并覆盖轨道", () => {
    const facet = createPreviewFacet();
    facet.setTrack(track([cue(0, 2, "current")]), COMMAND_A);
    const nextCommand = { requestId: 1, sourceEpoch: 2, sourceId: "source-b" };

    const nextState = facet.setTrack(
      track([cue(0, 2, "next")], { generationId: "generation-b" }),
      nextCommand,
    );

    expect(nextState).toMatchObject({
      generationId: "generation-b",
      requestId: 1,
      sourceEpoch: 2,
      sourceId: "source-b",
    });
    expect(facet.hitTest(1, nextCommand)?.sprite.assetId).toBe("next");
  });

  it("null 清空当前轨道且旧代次清空无效", () => {
    const facet = createPreviewFacet();
    facet.setTrack(track([cue(0, 2)]), COMMAND_A);

    facet.setTrack(null, { ...COMMAND_A, requestId: 9 });
    expect(facet.hitTest(1, COMMAND_A)).not.toBeNull();

    const clearCommand = { ...COMMAND_A, requestId: 11 };
    expect(facet.setTrack(null, clearCommand)).toEqual({
      generationId: null,
      mediaId: null,
      profileId: null,
      requestId: 11,
      sourceEpoch: 1,
      sourceId: "source-a",
      status: "empty",
    });
    expect(facet.hitTest(1, clearCommand)).toBeNull();
  });

  it.each([
    ["cue 列表为空", track([])],
    ["cue 起点非有限", track([{ ...cue(0, 2), startTime: Number.NaN }])],
    [
      "cue 终点非有限",
      track([{ ...cue(0, 2), endTime: Number.POSITIVE_INFINITY }]),
    ],
    ["cue 起点为负", track([cue(-1, 2)])],
    ["cue 终点不大于起点", track([cue(2, 2)])],
    ["cue 顺序倒退", track([cue(3, 4), cue(1, 2)])],
    ["cue 相互重叠", track([cue(0, 3), cue(2, 4)])],
    [
      "sprite 横坐标为负",
      track([{ ...cue(0, 2), sprite: { ...cue(0, 2).sprite, x: -1 } }]),
    ],
    [
      "sprite 纵坐标为负",
      track([{ ...cue(0, 2), sprite: { ...cue(0, 2).sprite, y: -1 } }]),
    ],
    [
      "sprite 宽度非正",
      track([{ ...cue(0, 2), sprite: { ...cue(0, 2).sprite, width: 0 } }]),
    ],
    [
      "sprite 高度非正",
      track([{ ...cue(0, 2), sprite: { ...cue(0, 2).sprite, height: 0 } }]),
    ],
    [
      "sprite 高度非有限",
      track([
        { ...cue(0, 2), sprite: { ...cue(0, 2).sprite, height: Number.NaN } },
      ]),
    ],
    ["track generationId 为空", track([cue(0, 2)], { generationId: " " })],
    ["track mediaId 为空", track([cue(0, 2)], { mediaId: "" })],
    ["track profileId 为空", track([cue(0, 2)], { profileId: "\t" })],
    [
      "track sourceFingerprint 为空",
      track([cue(0, 2)], { sourceFingerprint: "" }),
    ],
    [
      "sprite assetId 为空",
      track([{ ...cue(0, 2), sprite: { ...cue(0, 2).sprite, assetId: "" } }]),
    ],
  ])("拒绝%s且不污染现有状态", (_label, invalidTrack) => {
    const facet = createPreviewFacet();
    facet.setTrack(track([cue(0, 2, "valid")]), COMMAND_A);
    const before = facet.getState();

    expect(() =>
      facet.setTrack(invalidTrack, { ...COMMAND_A, requestId: 11 }),
    ).toThrow(Error);
    expect(facet.getState()).toEqual(before);
    expect(facet.hitTest(1, COMMAND_A)?.sprite.assetId).toBe("valid");
  });

  it("复制已准备轨道，调用方后续修改不会污染命中结果", () => {
    const facet = createPreviewFacet();
    const mutableCue = cue(0, 2, "original") as {
      endTime: number;
      sprite: {
        assetId: string;
        height: number;
        width: number;
        x: number;
        y: number;
      };
      startTime: number;
    };
    facet.setTrack(track([mutableCue]), COMMAND_A);

    mutableCue.endTime = 99;
    mutableCue.sprite.assetId = "changed";

    expect(facet.hitTest(1, COMMAND_A)).toMatchObject({
      endTime: 2,
      sprite: { assetId: "original" },
    });
    expect(facet.hitTest(50, COMMAND_A)).toBeNull();
  });

  it("生产实现不引用网络、DOM、地址或图片 API", async () => {
    const source = await readFile(
      fileURLToPath(new URL("preview-facet.ts", import.meta.url)),
      "utf8",
    );

    expect(source).not.toMatch(
      /\b(?:fetch|XMLHttpRequest|document|window|Image|URL|HTMLImageElement)\b/u,
    );
  });
});
