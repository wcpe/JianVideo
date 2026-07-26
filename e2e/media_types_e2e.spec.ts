import { test, expect } from "@playwright/test";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { ensureSetup, login } from "./helpers";

test.use({ serviceWorkers: "block" });

const screenshotDir = ".tmp/screenshots/media-types";

test.describe("媒体类型规则端到端", () => {
  test.describe.configure({ timeout: 60000 });

  test.beforeEach(async ({ page }) => {
    await ensureSetup(page.request);
    mkdirSync(screenshotDir, { recursive: true });
  });

  test("mock 接口下展示中文说明、能力标签并禁用内置规则", async ({ page }) => {
    let updatePayload: { library_id?: number; enabled?: boolean } | null = null;
    await login(page);
    await page.route("**/api/library/paths", async (route) => {
      await route.fulfill({
        json: {
          items: [
            {
              id: 101,
              path: "D:/Mock/Media",
              type: "local",
              library_kind: "mixed",
              label: "Mock 媒体库",
              enabled: true,
              created_at: "2026-07-09T00:00:00Z",
              media_count: 0,
            },
          ],
        },
      });
    });
    await page.route("**/api/media-types**", async (route) => {
      await route.fulfill({
        json: {
          types: [
            {
              type: "video",
              name: "视频",
              description: "可播放、可转码的视频文件。",
              default_extensions: ["mp4"],
              capabilities: ["scan", "transcode", "thumbnail", "metadata"],
            },
          ],
          rules: [
            {
              id: "builtin-video-mp4",
              space_id: "space-default",
              library_id: 101,
              type: "video",
              extension: "mp4",
              label: "MP4 视频",
              description: "常见视频容器。",
              enabled: true,
              builtin: true,
              capabilities: ["scan", "transcode", "thumbnail", "metadata"],
            },
          ],
        },
      });
    });
    await page.route("**/api/media-types/rules/**", async (route) => {
      updatePayload = (await route.request().postDataJSON()) as {
        library_id?: number;
        enabled?: boolean;
      };
      await route.fulfill({
        json: {
          id: "builtin-video-mp4",
          space_id: "space-default",
          library_id: 101,
          type: "video",
          extension: "mp4",
          label: "MP4 视频",
          description: "常见视频容器。",
          enabled: updatePayload.enabled,
          builtin: true,
          capabilities: ["scan", "transcode", "thumbnail", "metadata"],
        },
      });
    });

    await page.goto("/library-manager");
    const card = page
      .locator(".mantine-Card-root")
      .filter({ hasText: "Mock 媒体库" })
      .first();
    await card.getByLabel("Mock 媒体库 后缀列表").click();
    await expect(card.getByText("MP4 视频")).toBeVisible();
    await expect(card.getByText("常见视频容器。")).toBeVisible();
    await expect(
      card.locator(".mantine-Badge-root").filter({ hasText: /^可转码$/ }),
    ).toBeVisible();

    await card.getByLabel("禁用后缀 mp4").click();
    await expect.poll(() => updatePayload).toEqual({ library_id: 101, enabled: false });
    await card.screenshot({
      path: `${screenshotDir}/media-types-mock.png`,
    });
  });

  test("真实服务中新增自定义规则、禁用内置规则并刷新保持生效", async ({
    page,
  }) => {
    const dir = mkdtempSync(join(tmpdir(), "jianvideo-media-types-e2e-"));
    const label = `媒体类型 E2E ${dir.split(/[\\/]/).pop()}`;
    let libraryID = 0;
    try {
      await login(page);
      const createLib = await page.evaluate(
        async (input: { path: string; label: string }) => {
          const res = await fetch("/api/library/paths", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
              path: input.path,
              type: "local",
              label: input.label,
            }),
          });
          return { ok: res.ok, status: res.status, body: await res.text() };
        },
        { path: dir.replace(/\\/g, "/"), label },
      );
      expect(createLib.ok, createLib.body).toBeTruthy();
      libraryID = JSON.parse(createLib.body).id;

      await page.goto("/library-manager");
      let card = page
        .locator(".mantine-Card-root")
        .filter({ hasText: label })
        .first();
      await card.getByLabel(`${label} 后缀列表`).click();
      await card.getByLabel(`${label} 自定义后缀`).fill(".rawx");
      await card.getByRole("button", { name: "图片" }).click();
      await card.getByRole("button", { name: "添加后缀" }).click();
      await expect(
        card.locator(".mantine-Badge-root").filter({ hasText: /^rawx$/ }),
      ).toBeVisible({
        timeout: 10000,
      });

      const disableMp4 = card.getByLabel("禁用后缀 mp4");
      if ((await disableMp4.count()) > 0) {
        await disableMp4.click();
      }
      await page.reload();
      card = page
        .locator(".mantine-Card-root")
        .filter({ hasText: label })
        .first();
      await card.getByLabel(`${label} 后缀列表`).click();

      await expect(
        card.locator(".mantine-Badge-root").filter({ hasText: /^rawx$/ }),
      ).toBeVisible({
        timeout: 10000,
      });
      await expect(card.getByText(/mp4（内置）（已禁用）/)).toBeVisible();
      await card.screenshot({
        path: `${screenshotDir}/media-types-real.png`,
      });
    } finally {
      if (libraryID && !page.isClosed()) {
        await page.evaluate(async (id) => {
          await fetch(`/api/library/paths/${id}`, { method: "DELETE" });
        }, libraryID);
      }
      rmSync(dir, { recursive: true, force: true });
    }
  });
});
