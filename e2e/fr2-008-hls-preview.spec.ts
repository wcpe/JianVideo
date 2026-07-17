import { test, expect, type APIRequestContext } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { login } from "./helpers";

test.use({ serviceWorkers: "block" });

test("真实视频经统一任务生成单档 HLS、登记缓存并在直连失败时回退播放", async ({
  page,
}) => {
  const mediaDir = await mkdtemp(join(tmpdir(), "jianvideo-fr2-008-"));
  const mediaPath = join(mediaDir, "fr2-008-preview.mp4");
  let libraryID = 0;

  try {
    mkdirSync(".tmp/screenshots", { recursive: true });
    writeVideoFixture(mediaPath);
    await login(page);

    const createLibrary = await page.request.post("/api/library/paths", {
      data: {
        path: mediaDir.replace(/\\/g, "/"),
        type: "local",
        label: "FR2-008 HLS E2E",
      },
    });
    expect(createLibrary.ok()).toBeTruthy();
    libraryID = (await createLibrary.json()).id;

    const scan = await page.request.post(`/api/library/scan/${libraryID}`, {
      params: { mode: "full" },
    });
    expect(scan.ok()).toBeTruthy();
    await waitLegacyTask(
      page.request,
      (await scan.json()).task_id,
      "/api/library/scan/tasks",
    );

    const mediaResponse = await page.request.get(
      `/api/library/media?library_id=${libraryID}&page_size=10`,
    );
    expect(mediaResponse.ok()).toBeTruthy();
    const media = (await mediaResponse.json()).items[0] as { id: number };
    expect(media.id).toBeGreaterThan(0);

    const presetResponse = await page.request.post("/api/transcode/presets", {
      data: {
        name: "FR2-008 单档 H.264",
        codec: "h264",
        width: 640,
        height: 480,
      },
    });
    expect(presetResponse.status()).toBe(201);
    const preset = (await presetResponse.json()) as { id: number };

    const enqueue = await page.request.post("/api/transcode/tasks", {
      data: {
        media_id: media.id,
        preset_id: preset.id,
        priority: 7,
        force_rebuild: true,
      },
    });
    expect(enqueue.ok()).toBeTruthy();
    const taskID = ((await enqueue.json()) as { task_id: number }).task_id;
    await waitUnifiedTask(page.request, taskID);

    const task = await page.request.get(`/api/tasks/${taskID}`);
    expect(task.ok()).toBeTruthy();
    const taskBody = (await task.json()) as {
      type: string;
      progress: number;
      priority: number;
    };
    expect(taskBody.type).toBe("transcode.hls.preview");
    expect(taskBody.progress).toBe(1);
    expect(taskBody.priority).toBe(7);

    const statusResponse = await page.request.get(
      `/api/play/${media.id}/hls-status`,
    );
    expect(statusResponse.ok()).toBeTruthy();
    const status = (await statusResponse.json()) as {
      available: boolean;
      url: string;
    };
    expect(status.available).toBe(true);
    expect(status.url).toBe(`/api/play/hls/${media.id}/master.m3u8`);

    const masterResponse = await page.request.get(status.url);
    expect(masterResponse.ok()).toBeTruthy();
    const master = await masterResponse.text();
    expect(master.match(/#EXT-X-STREAM-INF/g)).toHaveLength(1);
    const variant = master
      .split(/\r?\n/)
      .find((line) => line.trim().endsWith(".m3u8"));
    expect(variant).toBeTruthy();
    const variantResponse = await page.request.get(
      `/api/play/hls/${media.id}/${variant}`,
    );
    expect(variantResponse.ok()).toBeTruthy();

    const assets = await page.request.get(
      `/api/storage/cache/assets?kind=hls&media_id=${media.id}`,
    );
    expect(assets.ok()).toBeTruthy();
    const assetItems = (
      (await assets.json()) as { items: Array<{ profile_id: string }> }
    ).items;
    expect(assetItems.some((item) => item.profile_id === "h264")).toBe(true);

    const clean = await page.request.post("/api/storage/cache/clean", {
      data: { dry_run: false, kinds: ["hls"] },
    });
    expect(clean.status()).toBe(202);
    await waitUnifiedTask(
      page.request,
      ((await clean.json()) as { task_id: number }).task_id,
    );
    await expect
      .poll(async () => {
        const response = await page.request.get(
          `/api/play/${media.id}/hls-status`,
        );
        return ((await response.json()) as { available: boolean }).available;
      })
      .toBe(false);

    const rebuild = await page.request.post("/api/transcode/tasks", {
      data: { media_id: media.id, preset_id: preset.id, force_rebuild: true },
    });
    expect(rebuild.ok()).toBeTruthy();
    await waitUnifiedTask(
      page.request,
      ((await rebuild.json()) as { task_id: number }).task_id,
    );

    let requestedHLS = false;
    page.on("request", (request) => {
      if (request.url().includes(`/api/play/hls/${media.id}/master.m3u8`))
        requestedHLS = true;
    });
    await page.route(`**/api/play/${media.id}/stream`, async (route) => {
      await route.fulfill({
        status: 500,
        contentType: "application/json",
        body: '{"code":"DIRECT_FAILED"}',
      });
    });
    await page.goto(`/play/${media.id}`);
    await expect.poll(() => requestedHLS, { timeout: 15000 }).toBe(true);
    await page.screenshot({
      path: ".tmp/screenshots/fr2-008-hls-preview.png",
      fullPage: true,
    });
  } finally {
    if (libraryID) await page.request.delete(`/api/library/paths/${libraryID}`);
    await removeMediaDir(mediaDir);
  }
});

async function removeMediaDir(mediaDir: string): Promise<void> {
  let lastError: unknown;
  for (let attempt = 0; attempt < 40; attempt += 1) {
    try {
      await rm(mediaDir, { recursive: true, force: true });
      return;
    } catch (error) {
      lastError = error;
      await new Promise((resolve) => setTimeout(resolve, 250));
    }
  }
  throw lastError;
}

function writeVideoFixture(path: string) {
  execFileSync(
    "ffmpeg",
    [
      "-y",
      "-f",
      "lavfi",
      "-i",
      "testsrc=duration=2:size=640x480:rate=24",
      "-f",
      "lavfi",
      "-i",
      "sine=frequency=440:duration=2",
      "-c:v",
      "libx264",
      "-pix_fmt",
      "yuv420p",
      "-c:a",
      "aac",
      "-shortest",
      path,
    ],
    { stdio: "ignore" },
  );
}

async function waitLegacyTask(
  request: APIRequestContext,
  taskID: number,
  listURL: string,
) {
  await expect
    .poll(
      async () => {
        const response = await request.get(listURL);
        expect(response.ok()).toBeTruthy();
        const item = ((await response.json()).tasks ?? []).find(
          (task: { id: number }) => task.id === taskID,
        );
        if (!item) return "missing";
        if (item.status === "error") throw new Error(item.error || "任务失败");
        return item.status;
      },
      { timeout: 20000 },
    )
    .toBe("completed");
}

async function waitUnifiedTask(request: APIRequestContext, taskID: number) {
  await expect
    .poll(
      async () => {
        const response = await request.get(`/api/tasks/${taskID}`);
        expect(response.ok()).toBeTruthy();
        const task = (await response.json()) as {
          status: string;
          error?: string;
        };
        if (task.status === "failed")
          throw new Error(task.error || "HLS preview 任务失败");
        return task.status;
      },
      { timeout: 60000 },
    )
    .toBe("succeeded");
}
