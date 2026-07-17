import {
  expect,
  test,
  type APIRequestContext,
  type Locator,
  type Page,
  type Response,
} from "@playwright/test";
import { execFileSync } from "node:child_process";
import { rmSync } from "node:fs";
import { login } from "./helpers";
import {
  FRAME_MARKER,
  hasFfmpeg,
  withMediaLibrary,
  withSetting,
  writeNumberedVideo,
} from "./media-playback-fixtures";

test.describe.configure({ mode: "serial" });
test.use({ serviceWorkers: "block" });

const FRAME_RATE = 30;
const VIDEO_DURATION = 8;
const CONTINUOUS_STEPS = 4;
const FMP4_FILE = "fr2-034-fmp4-numbered.webm";
const TS_FILE = "fr2-034-mpegts-numbered.ts";
const CODEC_PRIORITY_SETTING = "transcode_codec_priority";
const HW_ACCEL_SETTING = "transcode_hwaccel_mode";

test("FR2-034 真实 VP9 fMP4 HLS 连续前后逐帧", async ({ page }) => {
  test.setTimeout(300_000);
  test.skip(!hasFfmpeg, "需要 ffmpeg 生成编号帧并产出真实 fMP4 HLS");
  await login(page);

  await withMediaLibrary(
    page.request,
    {
      files: [
        {
          name: FMP4_FILE,
          write: (path) => writeNumberedVp9Video(path),
        },
      ],
      label: "FR2-034 真实 fMP4 HLS 逐帧 E2E",
      prefix: "fr2-034-fmp4-",
    },
    async ({ mediaByName }) => {
      const mediaID = requiredMediaID(mediaByName, FMP4_FILE);
      await withSetting(page.request, HW_ACCEL_SETTING, "software", () =>
        withSetting(page.request, CODEC_PRIORITY_SETTING, '["vp9","h264"]', async () => {
          const requestedMedia = observeMediaRequests(page, mediaID);
          const negotiation = waitForNegotiation(page, mediaID);

          try {
            await page.goto(`/play/${mediaID}`);
            const descriptor = await readNegotiation(negotiation);
            expect(descriptor).toMatchObject({
              codec: "vp9",
              path: "fmp4",
              url: `/api/play/hls/${mediaID}/index.m3u8`,
            });
            expect(descriptor.frame_presentation).toBeUndefined();

            await verifyMsePath(page, mediaID, descriptor.url, requestedMedia, ".m4s");
            await verifyApproximateDirectionalSteps(page);
          } finally {
            await releasePlayerAndCleanHls(page);
          }
        }),
      );
    },
  );
});

test("FR2-034 真实 MPEG-TS 直连经 mpegts.js 连续前后逐帧", async ({ page }) => {
  test.setTimeout(300_000);
  test.skip(!hasFfmpeg, "需要 ffmpeg 生成编号帧 MPEG-TS");
  await login(page);

  await withMediaLibrary(
    page.request,
    {
      files: [
        {
          name: TS_FILE,
          write: (path) => writeNumberedTransportStream(path),
        },
      ],
      label: "FR2-034 真实 MPEG-TS 逐帧 E2E",
      prefix: "fr2-034-ts-",
    },
    async ({ mediaByName }) => {
      const mediaID = requiredMediaID(mediaByName, TS_FILE);
      const apiDescriptor = await negotiateH264(page.request, mediaID);
      expect(apiDescriptor).toMatchObject({
        codec: "h264",
        path: "ts",
        url: `/api/play/hls/${mediaID}/master`,
      });

      const requestedMedia = observeMediaRequests(page, mediaID);
      const negotiation = waitForNegotiation(page, mediaID);
      try {
        await page.goto(`/play/${mediaID}`);
        const descriptor = await readNegotiation(negotiation);
        expect(descriptor).toMatchObject({ codec: "h264", path: "ts" });
        await verifyMpegtsPath(page, mediaID, requestedMedia);
        await verifyApproximateDirectionalSteps(page);
      } finally {
        await page.goto("about:blank").catch(() => undefined);
      }
    },
  );
});

interface NegotiationDescriptor {
  codec: string;
  frame_presentation?: unknown;
  path: string;
  url: string;
}

interface MediaRequests {
  directStreams: string[];
  hlsRequests: string[];
  manifests: string[];
  segments: string[];
}

function requiredMediaID(mediaByName: ReadonlyMap<string, number>, name: string): number {
  const mediaID = mediaByName.get(name);
  if (!mediaID) throw new Error(`未找到测试媒体：${name}`);
  return mediaID;
}

function waitForNegotiation(page: Page, mediaID: number): Promise<Response> {
  return page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      response.url().endsWith(`/api/play/${mediaID}/negotiate`),
  );
}

async function readNegotiation(
  responsePromise: Promise<Response>,
): Promise<NegotiationDescriptor> {
  const response = await responsePromise;
  expect(response.ok()).toBeTruthy();
  return (await response.json()) as NegotiationDescriptor;
}

async function negotiateH264(
  request: APIRequestContext,
  mediaID: number,
): Promise<NegotiationDescriptor> {
  const response = await request.post(`/api/play/${mediaID}/negotiate`, {
    data: { client_caps: { av1: false, h265: false, vp9: false } },
  });
  expect(response.ok()).toBeTruthy();
  return (await response.json()) as NegotiationDescriptor;
}

function observeMediaRequests(page: Page, mediaID: number): MediaRequests {
  const observed: MediaRequests = {
    directStreams: [],
    hlsRequests: [],
    manifests: [],
    segments: [],
  };
  page.on("request", (request) => {
    const url = request.url();
    if (url.includes(`/api/play/${mediaID}/stream`)) observed.directStreams.push(url);
    if (!url.includes(`/api/play/hls/${mediaID}/`)) return;
    observed.hlsRequests.push(url);
    if (url.includes(".m3u8")) observed.manifests.push(url);
    if (url.endsWith(".m4s") || url.endsWith(".ts")) observed.segments.push(url);
  });
  return observed;
}

async function verifyMpegtsPath(
  page: Page,
  mediaID: number,
  requested: MediaRequests,
): Promise<void> {
  const player = page.getByTestId("video-player-root");
  const video = page.locator("video");
  await expect(player).toBeVisible({ timeout: 30_000 });
  await expect(player.getByRole("button", { name: /^(播放|暂停)$/ })).toBeVisible({ timeout: 30_000 });
  await expect
    .poll(() => requested.directStreams.some((url) => url.includes(`/api/play/${mediaID}/stream`)), {
      timeout: 30_000,
    })
    .toBe(true);
  await expect.poll(() => video.evaluate((node) => node.currentSrc), { timeout: 30_000 }).toMatch(/^blob:http:\/\//);
  await expect(video).toHaveAttribute("src", /^blob:http:\/\//, { timeout: 30_000 });
  await expect(player).toHaveAttribute("data-frame-presentation", "approximate", { timeout: 30_000 });
  expect(requested.hlsRequests).toHaveLength(0);
}

async function verifyMsePath(
  page: Page,
  mediaID: number,
  expectedManifest: string,
  requested: MediaRequests,
  segmentSuffix: ".m4s" | ".ts",
): Promise<void> {
  const player = page.getByTestId("video-player-root");
  const video = page.locator("video");
  await expect(player).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole("button", { name: "暂停" })).toBeVisible({
    timeout: 30_000,
  });
  await expect
    .poll(() => requested.manifests.some((url) => url.endsWith(expectedManifest)), {
      timeout: 30_000,
    })
    .toBe(true);
  await expect
    .poll(() => requested.segments.some((url) => url.endsWith(segmentSuffix)), {
      timeout: 30_000,
    })
    .toBe(true);
  await expect
    .poll(() => video.evaluate((node) => node.currentSrc), { timeout: 30_000 })
    .toMatch(/^blob:http:\/\//);
  await expect(video).toHaveAttribute("src", /^blob:http:\/\//, { timeout: 30_000 });
  await expect(player).toHaveAttribute("data-frame-presentation", "approximate", {
    timeout: 30_000,
  });
  expect(requested.manifests.join("\n")).toContain(`/api/play/hls/${mediaID}/`);
}

async function verifyApproximateDirectionalSteps(page: Page): Promise<void> {
  const player = page.getByTestId("video-player-root");
  const video = page.locator("video");
  await pause(player);
  await seekToMarkerWindow(video);

  const startMarker = await readMarker(video);
  expect(startMarker).toBeGreaterThanOrEqual(FRAME_RATE);
  expect(startMarker).toBeLessThan(VIDEO_DURATION * FRAME_RATE - CONTINUOUS_STEPS);

  let currentTime = await readCurrentTime(video);
  for (let step = 0; step < CONTINUOUS_STEPS; step += 1) {
    currentTime = await verifyApproximateStep(player, video, "后一帧", currentTime);
  }
  await expect.poll(() => readMarker(video), { timeout: 30_000 }).toBeGreaterThan(startMarker);
  const forwardMarker = await readMarker(video);

  for (let step = 0; step < CONTINUOUS_STEPS; step += 1) {
    currentTime = await verifyApproximateStep(player, video, "前一帧", currentTime);
  }
  await expect.poll(() => readMarker(video), { timeout: 30_000 }).toBeLessThan(forwardMarker);
}

async function pause(player: Locator): Promise<void> {
  const pauseButton = player.getByRole("button", { name: "暂停" });
  if (await pauseButton.isVisible()) {
    await pauseButton.evaluate((button: HTMLButtonElement) => button.click());
  }
  await expect(player.getByRole("button", { name: "播放", exact: true })).toBeVisible();
}

async function seekToMarkerWindow(video: Locator): Promise<void> {
  await video.evaluate((node) => {
    node.currentTime = 2;
  });
  await expect
    .poll(() => video.evaluate((node) => !node.seeking && node.paused), {
      timeout: 30_000,
    })
    .toBe(true);
  await expect.poll(() => readMarker(video), { timeout: 30_000 }).toBeGreaterThanOrEqual(FRAME_RATE);
}

async function clickPlayerButton(player: Locator, name: "前一帧" | "后一帧"): Promise<void> {
  await player
    .getByRole("button", { name })
    .evaluate((button: HTMLButtonElement) => button.click());
}

async function verifyApproximateStep(
  player: Locator,
  video: Locator,
  name: "前一帧" | "后一帧",
  previousTime: number,
): Promise<number> {
  const previousRequestId = await readFrameStepRequestId(player);
  await clickPlayerButton(player, name);
  let requestId = previousRequestId;
  await expect
    .poll(async () => {
      requestId = await readFrameStepRequestId(player);
      return requestId;
    }, { timeout: 15_000 })
    .toBeGreaterThan(previousRequestId);
  await expect(player).toHaveAttribute(
    "data-frame-step-result",
    new RegExp(`^${requestId}:completed:approximate:unknown:ok$`),
    { timeout: 15_000 },
  );
  const mediaTime = expect.poll(() => readCurrentTime(video), { timeout: 15_000 });
  if (name === "后一帧") await mediaTime.toBeGreaterThan(previousTime);
  else await mediaTime.toBeLessThan(previousTime);
  await expect(player.getByRole("button", { name: "播放", exact: true })).toBeVisible();
  return readCurrentTime(video);
}

async function readFrameStepRequestId(player: Locator): Promise<number> {
  const value = await player.getAttribute("data-frame-step-result");
  const requestId = /^(\d+):/.exec(value ?? "")?.[1];
  return requestId ? Number(requestId) : -1;
}

async function readCurrentTime(video: Locator): Promise<number> {
  return video.evaluate((node) => node.currentTime);
}

async function readMarker(video: Locator): Promise<number> {
  return video.evaluate((node, marker) => {
    const width = (marker.bits + 2) * marker.cellSize;
    const canvas = document.createElement("canvas");
    canvas.width = width;
    canvas.height = marker.cellSize;
    const context = canvas.getContext("2d", { willReadFrequently: true });
    if (!context) return -1;
    context.drawImage(
      node,
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
      const centerX = cell * marker.cellSize + Math.floor(marker.cellSize / 2);
      const centerY = Math.floor(marker.cellSize / 2);
      let luma = 0;
      let samples = 0;
      for (let y = centerY - 1; y <= centerY + 1; y += 1) {
        for (let x = centerX - 1; x <= centerX + 1; x += 1) {
          const offset = (y * pixels.width + x) * 4;
          luma +=
            ((pixels.data[offset] ?? 0) +
              (pixels.data[offset + 1] ?? 0) +
              (pixels.data[offset + 2] ?? 0)) /
            3;
          samples += 1;
        }
      }
      return luma / samples >= 160;
    };
    if (!white(0) || !white(marker.bits + 1)) return -1;
    let frame = 0;
    for (let bit = 0; bit < marker.bits; bit += 1) {
      if (white(bit + 1)) frame += 2 ** bit;
    }
    return frame;
  }, FRAME_MARKER);
}

function writeNumberedVp9Video(path: string): void {
  const intermediate = `${path}.source.mp4`;
  try {
    writeNumberedVideo(intermediate, {
      duration: VIDEO_DURATION,
      frameRate: FRAME_RATE,
      height: 180,
      width: 320,
    });
    execFileSync(
      "ffmpeg",
      [
        "-y",
        "-i",
        intermediate,
        "-map",
        "0:v:0",
        "-c:v",
        "libvpx-vp9",
        "-lossless",
        "1",
        "-threads",
        "2",
        "-an",
        path,
      ],
      { stdio: "ignore" },
    );
  } finally {
    rmSync(intermediate, { force: true });
  }
}

function writeNumberedTransportStream(path: string): void {
  const intermediate = `${path}.source.mp4`;
  try {
    writeNumberedVideo(intermediate, {
      duration: VIDEO_DURATION,
      frameRate: FRAME_RATE,
      height: 720,
      width: 1280,
    });
    execFileSync(
      "ffmpeg",
      ["-y", "-i", intermediate, "-map", "0:v:0", "-c:v", "copy", "-f", "mpegts", path],
      { stdio: "ignore" },
    );
  } finally {
    rmSync(intermediate, { force: true });
  }
}

async function releasePlayerAndCleanHls(page: Page): Promise<void> {
  await page.goto("about:blank").catch(() => undefined);
  const response = await page.request.post("/api/storage/cache/clean", {
    data: { dry_run: false, kinds: ["hls"] },
  });
  expect(response.status()).toBe(202);
  const taskID = ((await response.json()) as { task_id: number }).task_id;
  await expect
    .poll(
      async () => {
        const taskResponse = await page.request.get(`/api/tasks/${taskID}`);
        expect(taskResponse.ok()).toBeTruthy();
        const task = (await taskResponse.json()) as { error?: string; status: string };
        if (task.status === "failed") throw new Error(task.error || "HLS 缓存清理失败");
        return task.status;
      },
      { timeout: 120_000 },
    )
    .toBe("succeeded");
}
