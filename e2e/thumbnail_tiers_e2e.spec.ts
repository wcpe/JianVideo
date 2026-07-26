// 覆盖 PRD 分档缩略图生成
import { test, expect, type APIRequestContext } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, rmSync } from "node:fs";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { login } from "./helpers";

test.use({ serviceWorkers: "block" });

const screenshotDir = ".tmp/screenshots/thumbnail-tiers";
const hasFfmpeg = (() => {
  try {
    execFileSync("ffmpeg", ["-version"], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
})();

test("真实图片与视频完成三档任务生成、缓存登记和页面刷新", async ({ page }) => {
  test.skip(!hasFfmpeg, "需要 ffmpeg 生成真实图片与视频素材");
  const mediaDir = await mkdtemp(join(tmpdir(), "jianvideo-thumbnail-tiers-"));
  const imageName = "thumbnail-tiers-image.jpg";
  const videoName = "thumbnail-tiers-video.mp4";
  let libraryID = 0;

  try {
    mkdirSync(screenshotDir, { recursive: true });
    generateFixtures(mediaDir, imageName, videoName);
    await login(page);

    const created = await page.request.post("/api/library/paths", {
      data: {
        path: mediaDir.replace(/\\/g, "/"),
        type: "local",
        label: "缩略图 E2E",
      },
    });
    expect(created.ok()).toBeTruthy();
    libraryID = ((await created.json()) as { id: number }).id;

    const scan = await page.request.post(`/api/library/scan/${libraryID}`, {
      params: { mode: "full" },
    });
    expect(scan.ok()).toBeTruthy();
    await waitForScan(
      page.request,
      ((await scan.json()) as { task_id: number }).task_id,
    );

    const media = await loadMedia(page.request, libraryID);
    expect(media.map((item) => item.file_name).sort()).toEqual(
      [imageName, videoName].sort(),
    );
    for (const item of media) {
      for (const size of [160, 320, 640]) {
        await expectThumbnailReady(page.request, item.id, size);
      }
    }

    const assetsResponse = await page.request.get("/api/storage/cache/assets", {
      params: { kind: "thumbnail", page_size: 100 },
    });
    expect(assetsResponse.ok()).toBeTruthy();
    const assets = (
      (await assetsResponse.json()) as { items: ThumbnailAsset[] }
    ).items.filter((asset) => media.some((item) => item.id === asset.media_id));
    expect(assets).toHaveLength(6);
    expect(assets.map((asset) => asset.variant).sort()).toEqual([
      "160",
      "160",
      "320",
      "320",
      "640",
      "640",
    ]);
    for (const asset of assets) {
      expect(asset.relative_path).toContain(
        `thumbnails/space-default/${asset.media_id}/`,
      );
    }

    await page.goto("/timeline");
    await expect(page.getByAltText(imageName).first()).toBeVisible({
      timeout: 15000,
    });
    await page.reload();
    await expect(page.getByAltText(videoName).first()).toBeVisible({
      timeout: 15000,
    });
    await page.screenshot({
      path: `${screenshotDir}/thumbnail-tiers-loaded.png`,
      fullPage: true,
    });
  } finally {
    if (libraryID) await page.request.delete(`/api/library/paths/${libraryID}`);
    if (existsSync(mediaDir))
      rmSync(mediaDir, { recursive: true, force: true });
  }
});

interface MediaItem {
  id: number;
  file_name: string;
}

interface ThumbnailAsset {
  media_id: number;
  variant: string;
  relative_path: string;
}

function generateFixtures(dir: string, imageName: string, videoName: string) {
  execFileSync(
    "ffmpeg",
    [
      "-y",
      "-f",
      "lavfi",
      "-i",
      "testsrc=size=800x450:rate=1",
      "-frames:v",
      "1",
      join(dir, imageName),
    ],
    { stdio: "ignore" },
  );
  execFileSync(
    "ffmpeg",
    [
      "-y",
      "-f",
      "lavfi",
      "-i",
      "testsrc=duration=3:size=640x360:rate=15",
      "-c:v",
      "libx264",
      "-profile:v",
      "baseline",
      "-pix_fmt",
      "yuv420p",
      "-movflags",
      "+faststart",
      join(dir, videoName),
    ],
    { stdio: "ignore" },
  );
}

async function waitForScan(request: APIRequestContext, taskID: number) {
  await expect
    .poll(
      async () => {
        const response = await request.get("/api/library/scan/tasks");
        expect(response.ok()).toBeTruthy();
        const tasks = (
          (await response.json()) as {
            tasks: Array<{ id: number; status: string; error?: string }>;
          }
        ).tasks;
        const task = tasks.find((item) => item.id === taskID);
        if (!task) return "missing";
        if (task.status === "error")
          throw new Error(task.error || "扫描任务失败");
        return task.status;
      },
      { timeout: 20000 },
    )
    .toBe("completed");
}

async function loadMedia(
  request: APIRequestContext,
  libraryID: number,
): Promise<MediaItem[]> {
  const response = await request.get("/api/library/media", {
    params: { library_id: libraryID, page_size: 100 },
  });
  expect(response.ok()).toBeTruthy();
  return ((await response.json()) as { items: MediaItem[] }).items;
}

async function expectThumbnailReady(
  request: APIRequestContext,
  mediaID: number,
  size: number,
) {
  const first = await request.get(`/api/library/thumbnail/${mediaID}`, {
    params: { size, probe: 1 },
  });
  expect([200, 202]).toContain(first.status());
  await expect
    .poll(
      async () => {
        const response = await request.get(
          `/api/library/thumbnail/${mediaID}`,
          {
            params: { size, probe: 1 },
          },
        );
        if (response.status() === 202) return 202;
        expect(response.status()).toBe(200);
        expect(response.headers()["content-type"]).toContain("image/jpeg");
        expect((await response.body()).byteLength).toBeGreaterThan(100);
        return 200;
      },
      { timeout: 30000 },
    )
    .toBe(200);
}
