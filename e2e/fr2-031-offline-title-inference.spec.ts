import { test, expect, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { ensureSetup, login } from "./helpers";

test.use({ serviceWorkers: "block" });

const screenshotDir = ".tmp/screenshots/fr2-031";
const hasFfmpeg = (() => {
  try {
    execFileSync("ffmpeg", ["-version"], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
})();

test("本地离线推断可人工纠正且 backfill 不覆盖人工值", async ({ page }) => {
  test.setTimeout(90000);
  test.skip(!hasFfmpeg, "未检测到 ffmpeg，跳过真实服务播放页推断 E2E");

  const dir = mkdtempSync(join(tmpdir(), "jianvideo-fr2-031-e2e-"));
  const fileName = "Show.Name.S01E02.Pilot.mp4";
  let libraryID = 0;
  let mediaID = 0;

  try {
    mkdirSync(screenshotDir, { recursive: true });
    writeFileSync(join(dir, ".keep"), "");
    execFileSync(
      "ffmpeg",
      [
        "-y",
        "-f",
        "lavfi",
        "-i",
        "testsrc=duration=1:size=320x240:rate=15",
        "-c:v",
        "libx264",
        "-profile:v",
        "baseline",
        "-pix_fmt",
        "yuv420p",
        "-movflags",
        "+faststart",
        join(dir, fileName),
      ],
      { stdio: "ignore" },
    );

    await ensureSetup(page.request);
    await login(page);

    const createLib = await page.request.post("/api/library/paths", {
      data: {
        path: dir.replace(/\\/g, "/"),
        type: "local",
        label: "FR2-031 E2E 剧集库",
        library_kind: "series",
      },
    });
    expect(createLib.ok()).toBeTruthy();
    libraryID = (await createLib.json()).id;

    const scan = await page.request.post(`/api/library/scan/${libraryID}`);
    expect(scan.ok()).toBeTruthy();
    mediaID = await pollMediaID(page, libraryID, fileName);

    await expect
      .poll(async () => {
        const res = await page.request.get(
          `/api/library/media/${mediaID}/inference`,
        );
        const body = await res.json();
        return body.inference?.title;
      })
      .toBe("Show Name");

    await page.goto(`/play/${mediaID}`);
    await expect(page.getByRole("heading", { name: "Show Name" })).toBeVisible({
      timeout: 10000,
    });
    const manual = await page.request.put(
      `/api/library/media/${mediaID}/inference`,
      {
        data: {
          title: "人工剧名",
          season: 1,
          episode: 2,
          episode_title: "人工集标题",
        },
      },
    );
    expect(manual.ok()).toBeTruthy();

    const backfill = await page.request.post(
      "/api/library/inference/backfill",
      {
        data: { library_id: libraryID },
      },
    );
    expect(backfill.status()).toBe(202);
    const backfillBody = (await backfill.json()) as {
      status: string;
      task_id: number;
    };
    expect(backfillBody.status).toBe("pending");
    expect(backfillBody.task_id).toBeGreaterThan(0);
    await pollTaskTerminal(page, backfillBody.task_id, "succeeded");
    const inference = await (
      await page.request.get(`/api/library/media/${mediaID}/inference`)
    ).json();
    expect(inference.inference.title).toBe("人工剧名");
    expect(inference.inference.manual).toBe(true);
    await page.reload();
    await expect(page.getByRole("heading", { name: "人工剧名" })).toBeVisible({
      timeout: 10000,
    });

    await page.screenshot({
      path: `${screenshotDir}/offline-title-inference-real.png`,
      fullPage: true,
    });
  } finally {
    if (libraryID) await page.request.delete(`/api/library/paths/${libraryID}`);
    rmSync(dir, { recursive: true, force: true });
  }
});

async function pollTaskTerminal(
  page: Page,
  taskID: number,
  expected: "succeeded",
): Promise<void> {
  await expect
    .poll(
      async () => {
        const res = await page.request.get(`/api/tasks/${taskID}`);
        expect(res.ok()).toBeTruthy();
        const task = (await res.json()) as {
          status: string;
          error?: string | null;
        };
        if (task.status === "failed" || task.status === "canceled") {
          throw new Error(
            `回填任务异常终止: ${task.status} ${task.error ?? ""}`,
          );
        }
        return task.status;
      },
      { timeout: 15000 },
    )
    .toBe(expected);
}

async function pollMediaID(
  page: Page,
  libraryID: number,
  fileName: string,
): Promise<number> {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    const res = await page.request.get(
      `/api/library/media?library_id=${libraryID}&page_size=100`,
    );
    const items: Array<{ id: number; file_name: string }> =
      (await res.json()).items ?? [];
    const hit = items.find((item) => item.file_name === fileName);
    if (hit) return hit.id;
    await new Promise((resolve) => setTimeout(resolve, 300));
  }
  throw new Error("等待媒体入库超时");
}
