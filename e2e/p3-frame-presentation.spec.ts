import {
  expect,
  test,
  type APIRequestContext,
  type Locator,
} from "@playwright/test";
import { execFileSync } from "node:child_process";
import { existsSync, rmSync } from "node:fs";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { login } from "./helpers";

test.use({ serviceWorkers: "block" });

const VIDEO_DURATION = 10;
const FRAME_RATE = 30;
const CONTINUOUS_STEPS = 60;
const MARKER = { bits: 9, cellSize: 8, x: 16, y: 16 } as const;
const hasFfmpeg = (() => {
  try {
    execFileSync("ffmpeg", ["-version"], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
})();

test("真实协商契约驱动 UI 连续精确逐帧", async ({ page }) => {
  test.setTimeout(240_000);
  test.skip(!hasFfmpeg, "需要 ffmpeg 生成真实编号帧长视频");
  const mediaDir = await mkdtemp(join(tmpdir(), "jianvideo-p3-frame-"));
  const mediaPath = join(mediaDir, "p3-numbered-long-video.mp4");
  let libraryID = 0;

  try {
    writeNumberedVideo(mediaPath);
    await login(page);
    libraryID = await createLibrary(page.request, mediaDir);
    const mediaID = await scanAndLoadMedia(page.request, libraryID);
    const negotiation = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        response.url().endsWith(`/api/play/${mediaID}/negotiate`),
    );
    await page.goto(`/play/${mediaID}`);

    const descriptor = (await (await negotiation).json()) as {
      codec: string;
      path: string;
      url: string;
      frame_presentation?: {
        marker: { bits: number; cell_size: number; x: number; y: number };
        nominal_frame_rate: number;
        timeline: Array<{
          source_frame_index: number;
          stable_frame_id: string;
        }>;
      };
    };
    expect(descriptor).toMatchObject({
      codec: "h264",
      path: "mp4",
      url: `/api/play/${mediaID}/stream`,
    });
    expect(descriptor.frame_presentation).toMatchObject({
      marker: {
        bits: MARKER.bits,
        cell_size: MARKER.cellSize,
        x: MARKER.x,
        y: MARKER.y,
      },
      nominal_frame_rate: FRAME_RATE,
    });
    expect(descriptor.frame_presentation?.timeline).toHaveLength(
      VIDEO_DURATION * FRAME_RATE,
    );
    expect(descriptor.frame_presentation?.timeline.at(-1)).toMatchObject({
      source_frame_index: VIDEO_DURATION * FRAME_RATE - 1,
      stable_frame_id: `binary-marker:${VIDEO_DURATION * FRAME_RATE - 1}`,
    });

    const player = page.getByTestId("video-player-root");
    const video = page.locator("video");
    await expect(player).toBeVisible({ timeout: 15_000 });
    await expect(page.getByRole("button", { name: "暂停" })).toBeVisible({
      timeout: 15_000,
    });
    await player
      .getByRole("button", { name: "暂停" })
      .evaluate((button: HTMLButtonElement) => button.click());
    await expect(
      player.getByRole("button", { name: "播放", exact: true }),
    ).toBeVisible();
    await expect
      .poll(() => video.evaluate((node) => node.currentTime), {
        timeout: 15_000,
      })
      .toBeLessThan(5.5);
    await expect
      .poll(() => video.evaluate((node) => node.duration), { timeout: 15_000 })
      .toBeGreaterThan(9.5);
    await expect
      .poll(() => video.evaluate((node) => node.currentSrc), { timeout: 15_000 })
      .toContain(`/api/play/${mediaID}/stream`);
    await expect(player).toHaveAttribute("data-frame-presentation", "exact", {
      timeout: 15_000,
    });
    await seekWithProgress(player, 40);
    await expect
      .poll(
        () => video.evaluate((node) => !node.seeking && node.paused),
        { timeout: 15_000 },
      )
      .toBe(true);

    await expect
      .poll(() => readMarker(video), { timeout: 15_000 })
      .toBeGreaterThanOrEqual(100);
    const startMarker = await readMarker(video);
    expect(startMarker).toBeGreaterThanOrEqual(100);
    expect(startMarker).toBeLessThan(
      VIDEO_DURATION * FRAME_RATE - CONTINUOUS_STEPS,
    );

    for (let step = 1; step <= CONTINUOUS_STEPS; step += 1) {
      await clickPlayerButton(player, "后一帧");
      await expectFrameStepResult(player, startMarker + step);
      await expectMarker(video, startMarker + step);
    }
    for (let step = 1; step <= CONTINUOUS_STEPS; step += 1) {
      await clickPlayerButton(player, "前一帧");
      await expectFrameStepResult(
        player,
        startMarker + CONTINUOUS_STEPS - step,
      );
      await expectMarker(video, startMarker + CONTINUOUS_STEPS - step);
    }
    await expect(player).toHaveAttribute("data-frame-presentation", "exact");
  } finally {
    if (libraryID) {
      await page.request
        .delete(`/api/library/paths/${libraryID}`, { timeout: 5_000 })
        .catch(() => undefined);
    }
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

async function seekWithProgress(player: Locator, percent: number): Promise<void> {
  const progress = player.getByTestId("video-progress-preview");
  const box = await progress.boundingBox();
  if (box === null) throw new Error("无法定位播放进度控件");
  await progress.click({
    position: { x: (box.width * percent) / 100, y: box.height / 2 },
  });
}

async function clickPlayerButton(player: Locator, name: string): Promise<void> {
  await player
    .getByRole("button", { name })
    .evaluate((button: HTMLButtonElement) => button.click());
}

async function expectFrameStepResult(
  player: Locator,
  expectedFrame: number,
): Promise<void> {
  await expect(player).toHaveAttribute(
    "data-frame-step-result",
    `completed:exact-verified:${expectedFrame}:ok`,
    { timeout: 15_000 },
  );
}

async function expectMarker(video: Locator, expected: number): Promise<void> {
  await expect
    .poll(() => readMarker(video), { timeout: 15_000 })
    .toBe(expected);
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
