import { test, expect, type APIRequestContext } from "@playwright/test";
import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { login } from "./helpers";

test.use({ serviceWorkers: "block" });

const screenshotDir = ".tmp/screenshots/fr2-048";

async function waitForTask(request: APIRequestContext, taskID: number) {
  await expect
    .poll(async () => {
      const response = await request.get(`/api/tasks/${taskID}`);
      expect(response.ok()).toBeTruthy();
      return ((await response.json()) as { status: string }).status;
    })
    .toBe("succeeded");
}

test.describe("存储与缓存管理端到端（FR2-048）", () => {
  test.beforeEach(() => {
    mkdirSync(screenshotDir, { recursive: true });
  });

  test("真实服务支持异步盘点、dry-run 与清理终态闭环", async ({ page }) => {
    const cachePath = join(".tmp", "metadata_temp", "fr2-048-e2e.cache");
    mkdirSync(join(".tmp", "metadata_temp"), { recursive: true });
    writeFileSync(cachePath, "cache-temp");

    await login(page);
    const inventory = await page.request.post("/api/storage/cache/inventory");
    expect(inventory.status()).toBe(202);
    const inventoryBody = (await inventory.json()) as { task_id: number };
    const inventoryTask = await page.request.get(
      `/api/tasks/${inventoryBody.task_id}`,
    );
    expect(inventoryTask.ok()).toBeTruthy();
    await waitForTask(page.request, inventoryBody.task_id);

    await page.goto("/storage-cache");
    await expect(page.getByRole("heading", { name: "缓存管理" })).toBeVisible();
    await expect(page.getByText("元数据临时项").first()).toBeVisible();
    await expect(page.getByText(/\d+ B/).first()).toBeVisible();

    await page.getByRole("button", { name: "预览清理" }).click();
    await expect(page.getByText(/预计影响/)).toBeVisible();

    const clean = await page.request.post("/api/storage/cache/clean", {
      data: { dry_run: false, kinds: ["metadata_temp"] },
    });
    expect(clean.status()).toBe(202);
    const cleanBody = (await clean.json()) as { task_id: number };
    const cleanTask = await page.request.get(`/api/tasks/${cleanBody.task_id}`);
    expect(cleanTask.ok()).toBeTruthy();
    await waitForTask(page.request, cleanBody.task_id);
    expect(existsSync(cachePath)).toBe(false);

    await page.screenshot({
      path: `${screenshotDir}/storage-cache-real.png`,
      fullPage: true,
    });
  });
});
