import {
  expect,
  test,
  type APIRequestContext,
  type Locator,
  type Page,
} from "@playwright/test";
import { execFileSync } from "node:child_process";
import { existsSync, rmSync } from "node:fs";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { login } from "./helpers";

test.use({ serviceWorkers: "block" });

const VIDEO_DURATION = 130;
const FRAME_RATE = 2;
const FRAME_COUNT = VIDEO_DURATION * FRAME_RATE;
const MARKER = { bits: 9, cellSize: 8, x: 16, y: 16 } as const;
const hasFfmpeg = (() => {
  try {
    execFileSync("ffmpeg", ["-version"], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
})();

test("真实长视频从编号画面 marker 取得稳定身份", async ({ page }) => {
  test.setTimeout(90_000);
  test.skip(!hasFfmpeg, "需要 ffmpeg 生成真实编号帧长视频");
  const mediaDir = await mkdtemp(join(tmpdir(), "jianvideo-p3-frame-"));
  const mediaPath = join(mediaDir, "p3-numbered-long-video.mp4");
  let libraryID = 0;

  try {
    writeNumberedVideo(mediaPath);
    await login(page);
    libraryID = await createLibrary(page.request, mediaDir);
    const mediaID = await scanAndLoadMedia(page.request, libraryID);
    await installFramePresentationDescriptor(page, mediaID);
    await page.goto(`/play/${mediaID}`);

    const player = page.getByTestId("video-player-root");
    const video = page.locator("video");
    await expect(player).toBeVisible({ timeout: 15_000 });
    await expect
      .poll(() => video.evaluate((node) => node.duration), { timeout: 15_000 })
      .toBeGreaterThan(125);
    await presentFrameAt(video, 64);
    await expect(player).toHaveAttribute("data-frame-presentation", "exact", {
      timeout: 15_000,
    });

    expect(await readMarker(video)).toBe(128);
    await presentFrameAt(video, 96);
    await expect(player).toHaveAttribute("data-frame-presentation", "exact");
    expect(await readMarker(video)).toBe(192);
  } finally {
    if (libraryID) await page.request.delete(`/api/library/paths/${libraryID}`);
    if (existsSync(mediaDir))
      rmSync(mediaDir, { recursive: true, force: true });
  }
});

function writeNumberedVideo(path: string): void {
  const markerWidth = (MARKER.bits + 2) * MARKER.cellSize;
  const filters = [
    `drawbox=x=${MARKER.x}:y=${MARKER.y}:w=${markerWidth}:h=${MARKER.cellSize}:color=black:t=fill`,
    `drawbox=x=${MARKER.x}:y=${MARKER.y}:w=${MARKER.cellSize}:h=${MARKER.cellSize}:color=white:t=fill`,
    `drawbox=x=${MARKER.x + (MARKER.bits + 1) * MARKER.cellSize}:y=${MARKER.y}:w=${MARKER.cellSize}:h=${MARKER.cellSize}:color=white:t=fill`,
  ];
  for (let bit = 0; bit < MARKER.bits; bit += 1) {
    const x = MARKER.x + (bit + 1) * MARKER.cellSize;
    filters.push(
      `drawbox=x=${x}:y=${MARKER.y}:w=${MARKER.cellSize}:h=${MARKER.cellSize}:color=white:t=fill:enable='eq(mod(floor(n/${2 ** bit}),2),1)'`,
    );
  }
  execFileSync(
    "ffmpeg",
    [
      "-y",
      "-f",
      "lavfi",
      "-i",
      `testsrc2=duration=${VIDEO_DURATION}:size=320x180:rate=${FRAME_RATE}`,
      "-vf",
      filters.join(","),
      "-c:v",
      "libx264",
      "-profile:v",
      "baseline",
      "-preset",
      "ultrafast",
      "-crf",
      "18",
      "-g",
      String(FRAME_RATE),
      "-pix_fmt",
      "yuv420p",
      "-movflags",
      "+faststart",
      path,
    ],
    { stdio: "ignore" },
  );
}

async function createLibrary(
  request: APIRequestContext,
  mediaDir: string,
): Promise<number> {
  const response = await request.post("/api/library/paths", {
    data: {
      path: mediaDir.replace(/\\/g, "/"),
      type: "local",
      label: "P3 编号帧长视频 E2E",
    },
  });
  expect(response.ok()).toBeTruthy();
  return ((await response.json()) as { id: number }).id;
}

async function scanAndLoadMedia(
  request: APIRequestContext,
  libraryID: number,
): Promise<number> {
  const scan = await request.post(`/api/library/scan/${libraryID}`, {
    params: { mode: "full" },
  });
  expect(scan.ok()).toBeTruthy();
  const taskID = ((await scan.json()) as { task_id: number }).task_id;
  await expect
    .poll(
      async () => {
        const response = await request.get("/api/library/scan/tasks");
        const body = (await response.json()) as {
          tasks: Array<{ id: number; status: string; error?: string }>;
        };
        const task = body.tasks.find((item) => item.id === taskID);
        if (task?.status === "error")
          throw new Error(task.error || "扫描任务失败");
        return task?.status ?? "missing";
      },
      { timeout: 60_000 },
    )
    .toBe("completed");
  const response = await request.get("/api/library/media", {
    params: { library_id: libraryID, page_size: 10 },
  });
  const items = (await response.json()) as { items: Array<{ id: number }> };
  expect(items.items).toHaveLength(1);
  return items.items[0]!.id;
}

async function installFramePresentationDescriptor(
  page: Page,
  mediaID: number,
): Promise<void> {
  await page.route(`**/api/play/${mediaID}/negotiate`, async (route) => {
    await route.fulfill({
      json: {
        codec: "h264",
        path: "mp4",
        url: `/api/play/${mediaID}/stream`,
        frame_presentation: {
          marker: {
            bits: MARKER.bits,
            cell_size: MARKER.cellSize,
            x: MARKER.x,
            y: MARKER.y,
          },
          nominal_frame_rate: FRAME_RATE,
          timeline: Array.from({ length: FRAME_COUNT }, (_, index) => ({
            media_time: index / FRAME_RATE,
            source_frame_index: index,
            stable_frame_id: `binary-marker:${index}`,
          })),
        },
      },
    });
  });
}

async function presentFrameAt(
  video: Locator,
  mediaTime: number,
): Promise<void> {
  await video.evaluate(async (node, targetTime) => {
    node.pause();
    node.currentTime = targetTime;
    await new Promise<void>((resolve) =>
      node.addEventListener("seeked", () => resolve(), { once: true }),
    );
    await new Promise<void>((resolve) =>
      node.requestVideoFrameCallback(() => resolve()),
    );
  }, mediaTime);
}

async function readMarker(video: Locator): Promise<number> {
  return video.evaluate((node, marker) => {
    const canvas = document.createElement("canvas");
    const width = (marker.bits + 2) * marker.cellSize;
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
    for (let bit = 0; bit < marker.bits; bit += 1) {
      if (white(bit + 1)) index += 2 ** bit;
    }
    return index;
  }, MARKER);
}
