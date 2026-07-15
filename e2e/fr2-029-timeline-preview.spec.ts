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
const hasFfmpeg = (() => {
  try {
    execFileSync("ffmpeg", ["-version"], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
})();

test("真实视频生成多页时间轴预览并验证桌面与触摸仿真交互", async ({ page }) => {
  test.skip(!hasFfmpeg, "需要 ffmpeg 生成真实长视频素材");
  const mediaDir = await mkdtemp(join(tmpdir(), "jianvideo-fr2-029-"));
  const mediaPath = join(mediaDir, "fr2-029-timeline-preview.mp4");
  let libraryID = 0;

  try {
    writeVideoFixture(mediaPath);
    await login(page);
    libraryID = await createLibrary(page.request, mediaDir);
    const mediaID = await scanAndLoadMedia(page.request, libraryID);
    const pending = await requestTimelinePreview(page.request, mediaID);
    const player = await openPlayer(page, mediaID);
    await waitForPreviewWithoutRefresh(player.progress, player.video);
    const status = await getAvailableTimelinePreview(
      page.request,
      mediaID,
      pending.profile_id,
    );
    await assertPreviewResources(page.request, status);
    await verifyPlayerInteractions(player.progress, player.video);
  } finally {
    if (libraryID) await page.request.delete(`/api/library/paths/${libraryID}`);
    if (existsSync(mediaDir))
      rmSync(mediaDir, { recursive: true, force: true });
  }
});

interface PlayerElements {
  progress: Locator;
  video: Locator;
}

interface TimelineStatus {
  profile_id: string;
  sprite_urls?: Record<string, string>;
  status: "available" | "pending";
  task_id?: number;
  vtt_url?: string;
}

function writeVideoFixture(path: string): void {
  execFileSync(
    "ffmpeg",
    [
      "-y",
      "-f",
      "lavfi",
      "-i",
      `testsrc2=duration=${VIDEO_DURATION}:size=320x180:rate=1`,
      "-c:v",
      "libx264",
      "-preset",
      "ultrafast",
      "-crf",
      "35",
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
      label: "FR2-029 时间轴预览 E2E",
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
  await waitForScan(
    request,
    ((await scan.json()) as { task_id: number }).task_id,
  );
  const response = await request.get("/api/library/media", {
    params: { library_id: libraryID, page_size: 10 },
  });
  expect(response.ok()).toBeTruthy();
  const items = (await response.json()) as { items: Array<{ id: number }> };
  expect(items.items).toHaveLength(1);
  return items.items[0]!.id;
}

async function waitForScan(
  request: APIRequestContext,
  taskID: number,
): Promise<void> {
  await expect
    .poll(
      async () => {
        const response = await request.get("/api/library/scan/tasks");
        expect(response.ok()).toBeTruthy();
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
}

async function requestTimelinePreview(
  request: APIRequestContext,
  mediaID: number,
): Promise<TimelineStatus> {
  const first = await request.get(`/api/play/${mediaID}/timeline-preview`);
  expect(first.status()).toBe(202);
  const pending = (await first.json()) as TimelineStatus;
  expect(pending.status).toBe("pending");
  expect(pending.task_id).toBeGreaterThan(0);
  return pending;
}

async function getAvailableTimelinePreview(
  request: APIRequestContext,
  mediaID: number,
  profileID: string,
): Promise<TimelineStatus> {
  const ready = await request.get(`/api/play/${mediaID}/timeline-preview`, {
    params: { profile: profileID },
  });
  expect(ready.status()).toBe(200);
  const status = (await ready.json()) as TimelineStatus;
  expect(status.status).toBe("available");
  return status;
}

async function assertPreviewResources(
  request: APIRequestContext,
  status: TimelineStatus,
): Promise<void> {
  expect(status.vtt_url).toBeTruthy();
  const vttResponse = await request.get(status.vtt_url!);
  expect(vttResponse.status()).toBe(200);
  expect(vttResponse.headers()["content-type"]).toContain("text/vtt");
  const vtt = await vttResponse.text();
  const references = [
    ...vtt.matchAll(/(sprite-\d{3}\.jpg)#xywh=(\d+),(\d+),(\d+),(\d+)/gu),
  ];
  const spriteNames = [...new Set(references.map((match) => match[1]!))];
  expect(references.length).toBeGreaterThanOrEqual(26);
  expect(spriteNames.length).toBeGreaterThanOrEqual(2);
  expect(Object.keys(status.sprite_urls ?? {})).toEqual(
    expect.arrayContaining(spriteNames),
  );
  for (const name of spriteNames)
    await expectSpriteReady(request, status.sprite_urls![name]!);
}

async function expectSpriteReady(
  request: APIRequestContext,
  url: string,
): Promise<void> {
  const response = await request.get(url);
  expect(response.status()).toBe(200);
  expect(response.headers()["content-type"]).toContain("image/jpeg");
  expect((await response.body()).byteLength).toBeGreaterThan(100);
}

async function openPlayer(
  page: Page,
  mediaID: number,
): Promise<PlayerElements> {
  const pendingResponse = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return (
      url.pathname === `/api/play/${mediaID}/timeline-preview` &&
      response.status() === 202
    );
  });
  await page.goto(`/play/${mediaID}`);
  await pendingResponse;
  const progress = page.getByTestId("video-progress-preview");
  const video = page.locator("video");
  await expect(progress).toBeVisible({ timeout: 15_000 });
  await expect
    .poll(() => video.evaluate((node) => node.duration), { timeout: 15_000 })
    .toBeGreaterThan(125);
  await video.evaluate((node) => node.pause());
  return { progress, video };
}

async function waitForPreviewWithoutRefresh(
  progress: Locator,
  video: Locator,
): Promise<void> {
  const duration = await video.evaluate((node) => node.duration);
  await expect(async () => {
    await moveToFraction(progress, 0.2);
    await expect(progress.getByTestId("timeline-preview-sprite")).toBeVisible();
    await expectPreviewTime(
      progress.getByTestId("timeline-preview-overlay"),
      duration * 0.2,
    );
  }).toPass({ timeout: 120_000, intervals: [1000] });
}

async function verifyPlayerInteractions(
  progress: Locator,
  video: Locator,
): Promise<void> {
  await verifyDesktopPreview(progress, video);
  await installSeekCounter(video);
  await verifyTouchSimulation(progress, video);
}

async function verifyDesktopPreview(
  progress: Locator,
  video: Locator,
): Promise<void> {
  const duration = await video.evaluate((node) => node.duration);
  await moveToFraction(progress, 0.2);
  const overlay = progress.getByTestId("timeline-preview-overlay");
  await expectPreviewTime(overlay, duration * 0.2);
  await expect(progress.getByTestId("timeline-preview-sprite")).toBeVisible();
  await moveToFraction(progress, 0.35);
  await moveToFraction(progress, 0.6);
  await moveToFraction(progress, 0.8);
  await expectPreviewTime(overlay, duration * 0.8);
  await progress.page().mouse.move(0, 0);
  await expect(overlay).toBeHidden();
}

async function moveToFraction(
  progress: Locator,
  fraction: number,
): Promise<void> {
  const box = await progress.boundingBox();
  if (!box) throw new Error("播放进度条不可见");
  await progress
    .page()
    .mouse.move(box.x + box.width * fraction, box.y + box.height / 2);
}

async function installSeekCounter(video: Locator): Promise<void> {
  await video.evaluate((node) => {
    const target = node as HTMLVideoElement & { __seekCount?: number };
    target.__seekCount = 0;
    target.addEventListener("seeking", () => {
      target.__seekCount = (target.__seekCount ?? 0) + 1;
    });
  });
}

async function verifyTouchSimulation(
  progress: Locator,
  video: Locator,
): Promise<void> {
  await resetSeekState(video, 10);
  await dispatchPointer(progress, "pointerdown", 0.2, 0, 1);
  await dispatchPointer(progress, "pointerup", 0.2, 0, 1);
  await expectSeekState(video, 0, 10);

  await resetSeekState(video, 10);
  await dispatchPointer(progress, "pointerdown", 0.25, 0, 2);
  await progress.page().waitForTimeout(450);
  const overlay = progress.getByTestId("timeline-preview-overlay");
  await expect(overlay).toBeVisible();
  await dispatchPointer(progress, "pointermove", 0.7, 1, 2);
  const duration = await video.evaluate((node) => node.duration);
  await expectPreviewTime(overlay, duration * 0.7);
  await dispatchPointer(progress, "pointerup", 0.7, 1, 2);
  await expectSeekState(video, 1, 0.7 * duration);

  await verifyCanceledTouches(progress, video);
}

async function verifyCanceledTouches(
  progress: Locator,
  video: Locator,
): Promise<void> {
  await resetSeekState(video, 10);
  await dispatchPointer(progress, "pointerdown", 0.3, 0, 3);
  await progress.page().waitForTimeout(450);
  await dispatchPointer(progress, "pointercancel", 0.3, 0, 3);
  await expectSeekState(video, 0, 10);

  await resetSeekState(video, 10);
  await dispatchPointer(progress, "pointerdown", 0.3, 0, 4);
  await dispatchPointer(progress, "pointermove", 0.31, 30, 4);
  await progress.page().waitForTimeout(450);
  await dispatchPointer(progress, "pointerup", 0.31, 30, 4);
  await expectSeekState(video, 0, 10);
}

async function dispatchPointer(
  progress: Locator,
  type: "pointerdown" | "pointermove" | "pointerup" | "pointercancel",
  fraction: number,
  deltaY: number,
  pointerId: number,
): Promise<void> {
  await progress.evaluate(
    (node, input) => {
      const rect = node.getBoundingClientRect();
      const sliders = node.querySelectorAll('[role="slider"]');
      const target = sliders.item(1) || node;
      target.dispatchEvent(
        new PointerEvent(input.type, {
          bubbles: true,
          buttons:
            input.type === "pointerup" || input.type === "pointercancel"
              ? 0
              : 1,
          clientX: rect.left + rect.width * input.fraction,
          clientY: rect.top + rect.height / 2 + input.deltaY,
          isPrimary: true,
          pointerId: input.pointerId,
          pointerType: "touch",
        }),
      );
    },
    { type, fraction, deltaY, pointerId },
  );
}

async function resetSeekState(video: Locator, time: number): Promise<void> {
  await video.evaluate((node, target) => {
    const media = node as HTMLVideoElement & { __seekCount?: number };
    media.currentTime = target;
    media.__seekCount = 0;
  }, time);
  await video.page().waitForTimeout(100);
  await video.evaluate((node) => {
    (node as HTMLVideoElement & { __seekCount?: number }).__seekCount = 0;
  });
}

async function expectSeekState(
  video: Locator,
  count: number,
  time: number,
): Promise<void> {
  await expect
    .poll(() =>
      video.evaluate((node) => {
        return (
          (node as HTMLVideoElement & { __seekCount?: number }).__seekCount ?? 0
        );
      }),
    )
    .toBe(count);
  const currentTime = await video.evaluate((node) => node.currentTime);
  expect(currentTime).toBeCloseTo(time, 0);
}

async function expectPreviewTime(
  overlay: Locator,
  expected: number,
): Promise<void> {
  await expect
    .poll(async () =>
      Math.abs(parseDisplayedTime(await overlay.textContent()) - expected),
    )
    .toBeLessThanOrEqual(1.5);
}

function parseDisplayedTime(value: string | null): number {
  const [minutes = "0", seconds = "0"] = (value ?? "").trim().split(":");
  return Number(minutes) * 60 + Number(seconds);
}
