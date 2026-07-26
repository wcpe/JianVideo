// 覆盖 PRD 阶梯定位
import { expect, test, type Locator, type Page } from "@playwright/test";
import { login } from "./helpers";
import {
  createHlsAbr,
  hasFfmpeg,
  withMediaLibrary,
  writeNumberedVideo,
  writeTransportStreamFixture,
  writeVideoFixture,
} from "./media-playback-fixtures";

test.describe.configure({ mode: "serial" });
test.use({ serviceWorkers: "block" });

const EXACT_FILE = "tiered-seek-exact.mp4";
const LONG_FILE = "tiered-seek-long.mp4";
const TS_FILE = "tiered-seek-long.ts";
const LONG_DURATION = 130;
const INITIAL_FRAME_STEP_REQUEST_ID = 0;
const SECOND_TIERS = [0.5, 1, 5, 30, 60] as const;
const MIXED_OPERATIONS = [
  [0.5, "next"],
  [1, "next"],
  [5, "previous"],
  [30, "next"],
  [60, "previous"],
  [5, "next"],
  [1, "previous"],
  [0.5, "previous"],
  [30, "previous"],
  [60, "next"],
  [1, "next"],
  [5, "next"],
  [0.5, "previous"],
  [60, "previous"],
  [30, "next"],
  [0.5, "next"],
  [5, "previous"],
  [1, "previous"],
  [60, "next"],
  [30, "previous"],
  [5, "next"],
  [0.5, "next"],
  [1, "next"],
  [30, "next"],
  [60, "previous"],
  [1, "previous"],
  [0.5, "previous"],
  [5, "previous"],
  [30, "previous"],
  [60, "next"],
  [0.5, "next"],
  [30, "next"],
  [1, "previous"],
  [5, "next"],
  [60, "previous"],
  [5, "previous"],
  [1, "next"],
  [0.5, "previous"],
  [60, "next"],
  [30, "previous"],
  [1, "next"],
  [5, "next"],
  [30, "next"],
  [0.5, "next"],
  [60, "previous"],
  [30, "previous"],
  [5, "previous"],
  [1, "previous"],
  [0.5, "previous"],
  [60, "next"],
] as const;

test("六档定位覆盖 exact 入口、边界与三路径混合操作", async ({
  page,
}) => {
  test.setTimeout(480_000);
  test.skip(!hasFfmpeg, "需要 ffmpeg 生成真实定位测试媒体");
  const pageErrors: string[] = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));

  await login(page);
  await withMediaLibrary(
    page.request,
    {
      files: [
        {
          name: EXACT_FILE,
          write: (path) =>
            writeNumberedVideo(path, { duration: 10, frameRate: 30 }),
        },
        {
          name: LONG_FILE,
          write: (path) =>
            writeVideoFixture(path, {
              duration: LONG_DURATION,
              frameRate: 10,
              height: 180,
              width: 320,
            }),
        },
        {
          name: TS_FILE,
          write: (path) =>
            writeTransportStreamFixture(path, {
              duration: LONG_DURATION,
              frameRate: 30,
              gopSeconds: 0.5,
              height: 180,
              width: 320,
            }),
        },
      ],
      label: "六档定位 E2E",
      prefix: "tiered-seek-",
    },
    async ({ mediaByName }) => {
      const exactID = requiredMediaID(mediaByName, EXACT_FILE);
      const longID = requiredMediaID(mediaByName, LONG_FILE);
      const tsID = requiredMediaID(mediaByName, TS_FILE);

      await verifyExactFrameTier(page, exactID);
      await verifySecondTiers(page, longID, "原文件直出", 0.35);

      const hlsURL = await createHlsAbr(page.request, longID);
      expect(hlsURL).toContain(
        `/api/play/hls/${longID}/profiles/abr-h264/master.m3u8`,
      );
      const hlsRequests: string[] = [];
      page.on("request", (request) => {
        if (request.url().includes(`/api/play/hls/${longID}/`))
          hlsRequests.push(request.url());
      });
      const hlsStreamRoute = `**/api/play/${longID}/stream`;
      await page.route(hlsStreamRoute, async (route) => {
        await route.fulfill({
          body: '{"code":"DIRECT_FAILED"}',
          contentType: "application/json",
          status: 500,
        });
      });
      await verifySecondTiers(page, longID, "真实 HLS/MSE", 0.8);
      expect(hlsRequests.some((url) => url.endsWith("master.m3u8"))).toBe(true);
      expect(
        hlsRequests.some(
          (url) => url.endsWith(".m3u8") && !url.endsWith("master.m3u8"),
        ),
      ).toBe(true);
      expect(hlsRequests.some((url) => url.endsWith(".ts"))).toBe(true);

      await page.goto("about:blank");
      await page.unroute(hlsStreamRoute);
      const tsDirectRequests: string[] = [];
      const tsHlsRequests: string[] = [];
      page.on("request", (request) => {
        const url = request.url();
        if (url.includes(`/api/play/${tsID}/stream`))
          tsDirectRequests.push(url);
        if (url.includes(`/api/play/hls/${tsID}/`)) tsHlsRequests.push(url);
      });
      await verifySecondTiers(page, tsID, "真实 MPEG-TS/mpegts.js/MSE", 0.5);
      expect(tsDirectRequests.length).toBeGreaterThan(0);
      expect(tsHlsRequests).toEqual([]);
      await page.goto("about:blank");
    },
  );

  expect(MIXED_OPERATIONS).toHaveLength(50);
  expect(
    pageErrors.filter((message) => message.includes("Network Error")),
  ).toEqual([]);
  await expect(page.getByText("Network Error", { exact: false })).toHaveCount(
    0,
  );
});

async function verifyExactFrameTier(
  page: Page,
  mediaID: number,
): Promise<void> {
  await page.goto(`/play/${mediaID}`);
  const player = page.getByTestId("video-player-root");
  const video = page.locator("video");
  await waitForPlayable(player, video, 9.5);
  await pause(player);
  await expect(player).toHaveAttribute("data-frame-presentation", "exact", {
    timeout: 15_000,
  });
  await setMediaTime(video, 4);

  const before = await currentTime(video);
  await openTierMenu(player);
  for (const label of ["1 帧", "0.5 秒", "1 秒", "5 秒", "30 秒", "60 秒"]) {
    await expect(
      page.getByRole("menuitem", { name: label, exact: true }),
    ).toBeVisible();
  }
  await page.getByRole("menuitem", { name: "1 帧", exact: true }).click();
  expect(Math.abs((await currentTime(video)) - before)).toBeLessThan(0.35);

  const firstMarker = await readFrameIndex(video);
  expect(firstMarker).toBeGreaterThanOrEqual(0);
  const firstBaseline = await clearFrameStepResult(player);
  await clickPlayerButton(player, "前进 1 帧");
  const firstStep = await waitForExactFrameStep(
    player,
    firstBaseline,
    firstMarker + 1,
  );

  const boundaryBaseline = await clearFrameStepResult(player);
  expect(boundaryBaseline).toBe(firstStep.requestId);
  await setMediaTime(video, 0);
  await clickPlayerButton(player, "后退 1 帧");
  const boundaryStep = await waitForFrameStepResult(player, boundaryBaseline);
  expect(boundaryStep.status).toBe("completed");
  await expect.poll(() => currentTime(video)).toBeLessThan(0.04);

  await setMediaTime(video, 4);
  const markerBeforeRace = await readFrameIndex(video);
  expect(markerBeforeRace).toBeGreaterThanOrEqual(0);
  const raceBaseline = await clearFrameStepResult(player);
  expect(raceBaseline).toBe(boundaryStep.requestId);
  await verifyExactFrameRace(player, raceBaseline, markerBeforeRace);
  await expect(page.getByText("Network Error", { exact: false })).toHaveCount(
    0,
  );
}

type FrameStepStatus =
  "canceled" | "completed" | "failed" | "superseded" | "unsupported";

type FrameStepMode = "approximate" | "exact-verified" | "unsupported";

interface FrameStepObservation {
  confirmedFrame: number | null;
  errorCode: string;
  mode: FrameStepMode;
  requestId: number;
  result: string;
  status: FrameStepStatus;
}

interface ExactFrameStepObservation extends FrameStepObservation {
  confirmedFrame: number;
  marker: number;
  mode: "exact-verified";
  status: "completed";
}

interface FrameStepSnapshot {
  changes: string[];
  marker: number;
  result: string | null;
}

async function clearFrameStepResult(player: Locator): Promise<number> {
  const result = await player.getAttribute("data-frame-step-result");
  const requestId =
    result === "pending"
      ? INITIAL_FRAME_STEP_REQUEST_ID
      : requireFrameStepResult(result).requestId;
  await player.evaluate((node) =>
    node.removeAttribute("data-frame-step-result"),
  );
  expect(await player.getAttribute("data-frame-step-result")).toBeNull();
  return requestId;
}

async function waitForFrameStepResult(
  player: Locator,
  previousRequestId: number,
): Promise<FrameStepObservation> {
  let observation: FrameStepObservation | undefined;
  await expect
    .poll(
      async () => {
        const value = await player.getAttribute("data-frame-step-result");
        if (value === null) return previousRequestId;
        const parsed = requireFrameStepResult(value);
        if (parsed.requestId > previousRequestId) observation = parsed;
        return parsed.requestId;
      },
      { timeout: 15_000 },
    )
    .toBeGreaterThan(previousRequestId);
  if (!observation) throw new Error("未取得本次逐帧命令结果");
  return observation;
}

async function waitForExactFrameStep(
  player: Locator,
  previousRequestId: number,
  expectedFrame: number,
): Promise<ExactFrameStepObservation> {
  let observation: ExactFrameStepObservation | undefined;
  await expect
    .poll(
      async () => {
        const snapshot = await readFrameStepSnapshot(player);
        const parsed = parseFrameStepResult(snapshot.result);
        if (!parsed || parsed.requestId <= previousRequestId) return null;
        observation = {
          ...parsed,
          marker: snapshot.marker,
        } as ExactFrameStepObservation;
        return observation;
      },
      { timeout: 15_000 },
    )
    .toEqual({
      confirmedFrame: expectedFrame,
      errorCode: "ok",
      marker: expectedFrame,
      mode: "exact-verified",
      requestId: expect.any(Number),
      result: expect.stringMatching(/^\d+:completed:exact-verified:\d+:ok$/u),
      status: "completed",
    });
  if (!observation) throw new Error("未取得原子逐帧快照");
  expect(observation.requestId).toBeGreaterThan(previousRequestId);
  return observation;
}

async function verifyExactFrameRace(
  player: Locator,
  previousRequestId: number,
  markerBeforeRace: number,
): Promise<void> {
  await armFrameStepResultObserver(player);
  try {
    await clickPlayerButton(player, "前进 1 帧");
    await clickPlayerButton(player, "后退 1 帧");
    let terminalSnapshot:
      | {
          marker: number;
          result: FrameStepObservation;
          results: FrameStepObservation[];
        }
      | undefined;
    await expect
      .poll(
        async () => {
          const snapshot = await readFrameStepSnapshot(player);
          const results = collectNewFrameStepResults(
            snapshot.changes,
            previousRequestId,
          );
          const result = parseFrameStepResult(snapshot.result);
          if (results.length !== 2 || !result) return false;
          try {
            validateExactFrameRace(
              results,
              result,
              previousRequestId,
              markerBeforeRace,
              snapshot.marker,
            );
          } catch {
            return false;
          }
          terminalSnapshot = { marker: snapshot.marker, result, results };
          return true;
        },
        { timeout: 15_000 },
      )
      .toBe(true);
    if (!terminalSnapshot) throw new Error("未取得竞态原子终态快照");
    validateExactFrameRace(
      terminalSnapshot.results,
      terminalSnapshot.result,
      previousRequestId,
      markerBeforeRace,
      terminalSnapshot.marker,
    );
  } finally {
    await removeFrameStepResultObserver(player);
  }
}

function validateExactFrameRace(
  results: readonly FrameStepObservation[],
  terminalResult: FrameStepObservation,
  previousRequestId: number,
  markerBeforeRace: number,
  markerAfterRace: number,
): void {
  expect(results).toHaveLength(2);
  const [next, previous] = results as [
    FrameStepObservation,
    FrameStepObservation,
  ];
  expect(next.requestId).toBeGreaterThan(previousRequestId);
  expect(previous.requestId).toBeGreaterThan(next.requestId);
  for (const result of results) {
    expect(result.status).toBe("completed");
    expect(result.mode).toBe("exact-verified");
    expect(result.errorCode).toBe("ok");
  }
  expect(next.confirmedFrame).toBe(markerBeforeRace + 1);
  expect(markerAfterRace).toBe(markerBeforeRace);
  expect(previous.confirmedFrame).toBe(markerAfterRace);
  expect(terminalResult.result).toBe(previous.result);
  expect(terminalResult.requestId).toBe(previous.requestId);
  expect(terminalResult.status).toBe("completed");
  expect(terminalResult.mode).toBe("exact-verified");
  expect(terminalResult.confirmedFrame).toBe(markerAfterRace);
  expect(terminalResult.errorCode).toBe("ok");
}

function collectNewFrameStepResults(
  changes: readonly string[],
  previousRequestId: number,
): FrameStepObservation[] {
  return changes
    .map(requireFrameStepResult)
    .filter(({ requestId }) => requestId > previousRequestId);
}

function requireFrameStepResult(value: string | null): FrameStepObservation {
  const parsed = parseFrameStepResult(value);
  if (!parsed) throw new Error(`逐帧结果协议无效：${value ?? "缺失"}`);
  return parsed;
}

function parseFrameStepResult(
  value: string | null,
): FrameStepObservation | null {
  const result = value ?? "";
  const match =
    /^(\d+):(canceled|completed|failed|superseded|unsupported):(approximate|exact-verified|unsupported):(\d+|unknown):([^:]+)$/u.exec(
      result,
    );
  if (!match) return null;
  const requestId = parseNonNegativeSafeInteger(match[1]);
  const confirmedFrame =
    match[4] === "unknown" ? null : parseNonNegativeSafeInteger(match[4]);
  if (requestId === null || (match[4] !== "unknown" && confirmedFrame === null))
    return null;
  return {
    confirmedFrame,
    errorCode: match[5]!,
    mode: match[3] as FrameStepMode,
    requestId,
    result,
    status: match[2] as FrameStepStatus,
  };
}

function parseNonNegativeSafeInteger(value: string | undefined): number | null {
  if (!value || !/^\d+$/u.test(value)) return null;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed >= 0 ? parsed : null;
}

async function armFrameStepResultObserver(player: Locator): Promise<void> {
  await player.evaluate((node) => {
    type ObservedPlayer = HTMLElement & {
      __fr2035FrameStepObserver?: {
        changes: string[];
        observer: MutationObserver;
      };
    };
    const root = node as ObservedPlayer;
    root.__fr2035FrameStepObserver?.observer.disconnect();
    const changes: string[] = [];
    const observer = new MutationObserver((records) => {
      const current = root.getAttribute("data-frame-step-result");
      records.forEach((record, index) => {
        const value = records[index + 1]?.oldValue ?? current;
        if (record.attributeName === "data-frame-step-result" && value !== null)
          changes.push(value);
      });
    });
    observer.observe(root, {
      attributeFilter: ["data-frame-step-result"],
      attributeOldValue: true,
      attributes: true,
    });
    root.__fr2035FrameStepObserver = { changes, observer };
  });
}

function readFrameStepSnapshot(player: Locator): Promise<FrameStepSnapshot> {
  return player.evaluate((node) => {
    const root = node as HTMLElement & {
      __fr2035FrameStepObserver?: { changes: string[] };
    };
    const video = root.querySelector("video");
    if (!(video instanceof HTMLVideoElement)) {
      return {
        changes: [...(root.__fr2035FrameStepObserver?.changes ?? [])],
        marker: -1,
        result: root.getAttribute("data-frame-step-result"),
      };
    }
    const marker = { bits: 9, cellSize: 8, x: 16, y: 16 };
    const width = (marker.bits + 2) * marker.cellSize;
    const canvas = document.createElement("canvas");
    canvas.width = width;
    canvas.height = marker.cellSize;
    const context = canvas.getContext("2d", { willReadFrequently: true });
    let frame = -1;
    if (context) {
      context.drawImage(
        video,
        marker.x,
        marker.y,
        width,
        marker.cellSize,
        0,
        0,
        width,
        marker.cellSize,
      );
      const pixels = context.getImageData(0, 0, width, marker.cellSize);
      const white = (cell: number) => {
        const x = cell * marker.cellSize + Math.floor(marker.cellSize / 2);
        const y = Math.floor(marker.cellSize / 2);
        const offset = (y * pixels.width + x) * 4;
        return (
          ((pixels.data[offset] ?? 0) +
            (pixels.data[offset + 1] ?? 0) +
            (pixels.data[offset + 2] ?? 0)) /
            3 >=
          160
        );
      };
      if (white(0) && white(marker.bits + 1)) {
        frame = 0;
        for (let bit = 0; bit < marker.bits; bit += 1) {
          if (white(bit + 1)) frame += 2 ** bit;
        }
      }
    }
    return {
      changes: [...(root.__fr2035FrameStepObserver?.changes ?? [])],
      marker: frame,
      result: root.getAttribute("data-frame-step-result"),
    };
  });
}

async function removeFrameStepResultObserver(player: Locator): Promise<void> {
  await player.evaluate((node) => {
    const observed = node as HTMLElement & {
      __fr2035FrameStepObserver?: { observer: MutationObserver };
    };
    observed.__fr2035FrameStepObserver?.observer.disconnect();
    delete observed.__fr2035FrameStepObserver;
  });
}

type PlaybackMode =
  "原文件直出" | "真实 HLS/MSE" | "真实 MPEG-TS/mpegts.js/MSE";

async function verifySecondTiers(
  page: Page,
  mediaID: number,
  mode: PlaybackMode,
  tolerance: number,
): Promise<void> {
  const negotiation =
    mode === "真实 MPEG-TS/mpegts.js/MSE"
      ? page.waitForResponse(
          (response) =>
            response.request().method() === "POST" &&
            response.url().endsWith(`/api/play/${mediaID}/negotiate`),
        )
      : null;
  await page.goto(`/play/${mediaID}`);
  if (negotiation) {
    const response = await negotiation;
    expect(response.ok()).toBeTruthy();
    expect(await response.json()).toMatchObject({ codec: "h264", path: "ts" });
  }
  const player = page.getByTestId("video-player-root");
  const video = page.locator("video");
  await waitForPlayable(player, video, LONG_DURATION - 1);
  if (mode === "真实 MPEG-TS/mpegts.js/MSE") {
    await video.evaluate((node) => {
      (node as HTMLVideoElement).currentTime = 65;
    });
    await expect
      .poll(() => currentTime(video), { timeout: 15_000 })
      .toBeGreaterThanOrEqual(64.5);
  }
  await pause(player);
  await verifyPlaybackPath(page, player, video, mediaID, mode);

  await setMediaTime(video, 65);
  for (const tier of SECOND_TIERS) {
    const beforeSwitch = await currentTime(video);
    await selectTier(page, player, secondsLabel(tier));
    expect(Math.abs((await currentTime(video)) - beforeSwitch)).toBeLessThan(
      tolerance,
    );
    await clickPlayerButton(player, `前进 ${secondsLabel(tier)}`);
    await expectTime(video, 65 + tier, tolerance);
    const forwardTime = await currentTime(video);
    expect(forwardTime).toBeGreaterThan(beforeSwitch);
    await clickPlayerButton(player, `后退 ${secondsLabel(tier)}`);
    await expectTime(video, 65, tolerance);
    expect(await currentTime(video)).toBeLessThan(forwardTime);
  }

  await selectTier(page, player, "60 秒");
  await setMediaTime(video, 5);
  await clickPlayerButton(player, "后退 60 秒");
  await expectTime(video, 0, tolerance);
  await setMediaTime(video, LONG_DURATION - 5);
  await clickPlayerButton(player, "前进 60 秒");
  await expect
    .poll(() => currentTime(video), { timeout: 15_000 })
    .toBeGreaterThan(LONG_DURATION - 1);

  await setMediaTime(video, 65);
  let expected = 65;
  for (const [tier, direction] of MIXED_OPERATIONS) {
    await selectTier(page, player, secondsLabel(tier));
    const offset = direction === "next" ? tier : -tier;
    expected = Math.min(LONG_DURATION, Math.max(0, expected + offset));
    await clickPlayerButton(
      player,
      `${direction === "next" ? "前进" : "后退"} ${secondsLabel(tier)}`,
    );
    await expectTime(video, expected, tolerance);
  }
  await page.waitForTimeout(300);
  await expectTime(video, expected, tolerance);
  await expect(page.getByText("Network Error", { exact: false })).toHaveCount(
    0,
  );
}

async function verifyPlaybackPath(
  page: Page,
  player: Locator,
  video: Locator,
  mediaID: number,
  mode: PlaybackMode,
): Promise<void> {
  if (mode === "真实 HLS/MSE") {
    await expect(page.getByRole("button", { name: "清晰度" })).toBeVisible();
    return;
  }
  if (mode === "真实 MPEG-TS/mpegts.js/MSE") {
    await expect
      .poll(() =>
        video.evaluate((node) => (node as HTMLVideoElement).currentSrc),
      )
      .toMatch(/^blob:http:\/\//u);
    await expect(video).toHaveAttribute("src", /^blob:http:\/\//u);
    await expect(player).toHaveAttribute(
      "data-frame-presentation",
      "approximate",
    );
    return;
  }
  await expect
    .poll(() => video.evaluate((node) => (node as HTMLVideoElement).currentSrc))
    .toContain(`/api/play/${mediaID}/stream`);
}

async function waitForPlayable(
  player: Locator,
  video: Locator,
  minimumDuration: number,
): Promise<void> {
  await expect(player).toBeVisible({ timeout: 20_000 });
  await expect
    .poll(() => video.evaluate((node) => (node as HTMLVideoElement).duration), {
      timeout: 30_000,
    })
    .toBeGreaterThan(minimumDuration);
  await expect
    .poll(
      () =>
        video.evaluate(
          (node) => (node as HTMLVideoElement).seekable.length > 0,
        ),
      { timeout: 30_000 },
    )
    .toBe(true);
}

async function pause(player: Locator): Promise<void> {
  const video = player.locator("video");
  const pauseButton = player.getByRole("button", { name: "暂停" });
  const playButton = player.getByRole("button", { name: "播放", exact: true });
  let lastPlayAttempt = 0;
  // 进入可暂停态：UI「暂停」按钮出现，或 video 已在播放（HLS/MSE 路径偶发按钮晚于实际起播）。
  await expect
    .poll(
      async () => {
        if ((await pauseButton.count()) > 0) return true;
        const playing = await video.evaluate((node) => {
          const el = node as HTMLVideoElement;
          return !el.paused && el.readyState >= 2;
        });
        if (playing) return true;
        const now = Date.now();
        if (now - lastPlayAttempt < 500) return false;
        lastPlayAttempt = now;
        await video.evaluate((node) => {
          const el = node as HTMLVideoElement;
          el.muted = true;
          // 浏览器自动播放策略下，直接 play 作为按钮点击的回退
          void el.play().catch(() => undefined);
        });
        if ((await playButton.count()) > 0) {
          await playButton.evaluate((node: HTMLButtonElement) => node.click());
        }
        return false;
      },
      { timeout: 45_000 },
    )
    .toBe(true);
  // 优先点「暂停」；按钮未同步时直接 pause 元素，避免全量 e2e 串行负载下 30s 空等
  if ((await pauseButton.count()) > 0) {
    await pauseButton.evaluate((node: HTMLButtonElement) => node.click());
  } else {
    await video.evaluate((node) => {
      (node as HTMLVideoElement).pause();
    });
  }
  await expect
    .poll(
      async () =>
        video.evaluate((node) => (node as HTMLVideoElement).paused),
      { timeout: 30_000 },
    )
    .toBe(true);
}

async function selectTier(
  page: Page,
  player: Locator,
  label: string,
): Promise<void> {
  const current = player.getByRole("button", { name: `定位档位：${label}` });
  if (await current.isVisible()) return;
  await openTierMenu(player);
  await page.getByRole("menuitem", { name: label, exact: true }).click();
  await expect(
    player.getByRole("button", { name: `定位档位：${label}` }),
  ).toBeVisible();
}

async function openTierMenu(player: Locator): Promise<void> {
  await player
    .getByRole("button", { name: /^定位档位：/ })
    .evaluate((node: HTMLButtonElement) => node.click());
}

async function clickPlayerButton(player: Locator, name: string): Promise<void> {
  const button = player.getByRole("button", { name, exact: true });
  await expect(button).toBeEnabled();
  await button.evaluate((node: HTMLButtonElement, buttonName) => {
    if (node.disabled)
      throw new Error(`播放器按钮不可用：${node.ariaLabel ?? buttonName}`);
    node.click();
  }, name);
}

async function setMediaTime(video: Locator, target: number): Promise<void> {
  await video.evaluate((node, value) => {
    (node as HTMLVideoElement).currentTime = value;
  }, target);
  await expectTime(video, target, 0.8);
}

async function expectTime(
  video: Locator,
  expected: number,
  tolerance: number,
): Promise<void> {
  await expect
    .poll(() => currentTime(video), { timeout: 15_000 })
    .toBeGreaterThanOrEqual(Math.max(0, expected - tolerance));
  expect(await currentTime(video)).toBeLessThanOrEqual(
    expected + tolerance + 0.1,
  );
  await expect
    .poll(() => video.evaluate((node) => (node as HTMLVideoElement).paused))
    .toBe(true);
}

async function currentTime(video: Locator): Promise<number> {
  return video.evaluate((node) => (node as HTMLVideoElement).currentTime);
}

function secondsLabel(value: number): string {
  return `${value} 秒`;
}

function requiredMediaID(
  mediaByName: ReadonlyMap<string, number>,
  name: string,
): number {
  const mediaID = mediaByName.get(name);
  if (!mediaID) throw new Error(`未扫描到测试媒体：${name}`);
  return mediaID;
}

async function readFrameIndex(video: Locator): Promise<number> {
  return video.evaluate((node) => {
    const marker = { bits: 9, cellSize: 8, x: 16, y: 16 };
    const width = (marker.bits + 2) * marker.cellSize;
    const canvas = document.createElement("canvas");
    canvas.width = width;
    canvas.height = marker.cellSize;
    const context = canvas.getContext("2d", { willReadFrequently: true });
    if (!context) return -1;
    context.drawImage(
      node as HTMLVideoElement,
      marker.x,
      marker.y,
      width,
      marker.cellSize,
      0,
      0,
      width,
      marker.cellSize,
    );
    const pixels = context.getImageData(0, 0, width, marker.cellSize);
    const white = (cell: number) => {
      const x = cell * marker.cellSize + Math.floor(marker.cellSize / 2);
      const y = Math.floor(marker.cellSize / 2);
      const offset = (y * pixels.width + x) * 4;
      return (
        ((pixels.data[offset] ?? 0) +
          (pixels.data[offset + 1] ?? 0) +
          (pixels.data[offset + 2] ?? 0)) /
          3 >=
        160
      );
    };
    if (!white(0) || !white(marker.bits + 1)) return -1;
    let index = 0;
    for (let bit = 0; bit < marker.bits; bit += 1)
      if (white(bit + 1)) index += 2 ** bit;
    return index;
  });
}
