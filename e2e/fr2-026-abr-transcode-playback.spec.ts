import { test, expect, type APIRequestContext } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { existsSync, rmSync } from "node:fs";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { login } from "./helpers";

test.describe.configure({ mode: "serial" });
test.use({ serviceWorkers: "block" });

test("显式任务生成多档 HLS、逐档登记缓存并在直连失败后回退 ABR", async ({
  page,
}) => {
  const mediaDir = await mkdtemp(join(tmpdir(), "jianvideo-fr2-026-"));
  const mediaPath = join(mediaDir, "fr2-026-abr.mp4");
  let libraryID = 0;
  try {
    writeVideoFixture(mediaPath);
    await login(page);
    const library = await page.request.post("/api/library/paths", {
      data: {
        path: mediaDir.replace(/\\/g, "/"),
        type: "local",
        label: "FR2-026 ABR E2E",
      },
    });
    expect(library.ok()).toBeTruthy();
    libraryID = (await library.json()).id;
    const scan = await page.request.post(`/api/library/scan/${libraryID}`, {
      params: { mode: "full" },
    });
    expect(scan.ok()).toBeTruthy();
    await waitLegacyTask(page.request, (await scan.json()).task_id);
    const mediaList = await page.request.get(
      `/api/library/media?library_id=${libraryID}&page_size=10`,
    );
    const media = (await mediaList.json()).items[0] as { id: number };

    const enqueue = await page.request.post(`/api/play/${media.id}/hls-abr`, {
      data: { priority: 8, force_rebuild: true },
    });
    expect(enqueue.status()).toBe(202);
    const taskID = ((await enqueue.json()) as { task_id: number }).task_id;
    await waitUnifiedTask(page.request, taskID);

    const statusResponse = await page.request.get(
      `/api/play/${media.id}/hls-status`,
      {
        params: { profile_id: "abr-h264" },
      },
    );
    const status = (await statusResponse.json()) as {
      available: boolean;
      url: string;
    };
    expect(status.available).toBe(true);
    const masterResponse = await page.request.get(status.url);
    expect(masterResponse.ok()).toBeTruthy();
    const master = await masterResponse.text();
    expect(master.match(/#EXT-X-STREAM-INF/g)).toHaveLength(2);
    expect(master).toContain("720p/index.m3u8");
    expect(master).toContain("480p/index.m3u8");

    for (const variant of ["720p", "480p"]) {
      const playlist = await page.request.get(
        `/api/play/hls/${media.id}/profiles/abr-h264/${variant}/index.m3u8`,
      );
      expect(playlist.ok()).toBeTruthy();
    }
    const assets = await page.request.get(
      `/api/storage/cache/assets?kind=hls&media_id=${media.id}`,
    );
    const variants = (
      (await assets.json()) as {
        items: Array<{ profile_id: string; variant: string }>;
      }
    ).items
      .filter((item) => item.profile_id === "abr-h264")
      .map((item) => item.variant);
    expect(variants).toEqual(
      expect.arrayContaining(["master", "720p", "480p"]),
    );

    let requestedABR = false;
    page.on("request", (request) => {
      if (
        request
          .url()
          .includes(`/api/play/hls/${media.id}/profiles/abr-h264/master.m3u8`)
      )
        requestedABR = true;
    });
    await page.route(`**/api/play/${media.id}/stream`, async (route) => {
      await route.fulfill({
        status: 500,
        contentType: "application/json",
        body: '{"code":"DIRECT_FAILED"}',
      });
    });
    await page.goto(`/play/${media.id}`);
    await expect.poll(() => requestedABR, { timeout: 15000 }).toBe(true);
  } finally {
    if (libraryID) await page.request.delete(`/api/library/paths/${libraryID}`);
    if (existsSync(mediaDir))
      rmSync(mediaDir, { recursive: true, force: true });
  }
});

function writeVideoFixture(path: string) {
  execFileSync(
    "ffmpeg",
    [
      "-y",
      "-f",
      "lavfi",
      "-i",
      "testsrc=duration=3:size=1280x720:rate=24",
      "-f",
      "lavfi",
      "-i",
      "sine=frequency=440:duration=3",
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

async function waitLegacyTask(request: APIRequestContext, taskID: number) {
  await expect
    .poll(
      async () => {
        const response = await request.get("/api/library/scan/tasks");
        const item = ((await response.json()).tasks ?? []).find(
          (task: { id: number }) => task.id === taskID,
        );
        if (!item) return "missing";
        if (item.status === "error") throw new Error(item.error || "扫描失败");
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
        const task = (await response.json()) as {
          status: string;
          error?: string;
        };
        if (task.status === "failed")
          throw new Error(task.error || "ABR 任务失败");
        return task.status;
      },
      { timeout: 120000 },
    )
    .toBe("succeeded");
}
