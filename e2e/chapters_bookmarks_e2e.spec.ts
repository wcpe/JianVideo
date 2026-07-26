// 覆盖 PRD 章节跳转与书签
import { expect, test, type Locator, type Page } from "@playwright/test";
import { copyFileSync } from "node:fs";
import { resolve } from "node:path";
import { login } from "./helpers";
import { hasFfmpeg, withMediaLibrary } from "./media-playback-fixtures";

test.use({ serviceWorkers: "block" });

const VIDEO_FILE = "chapters-bookmarks-chapters-bookmarks.mp4";
const CHAPTER_FIXTURE = resolve(
  "apps/server/internal/library/testdata/chapters/embedded-chapters-three.mp4",
);
const ALLOWED_NOTE = "😀".repeat(2_000);
const OVERSIZED_NOTE = "😀".repeat(2_001);

type BookmarkDTO = {
  id: string;
  note: string | null;
  position_ms: number;
  revision: number;
  title: string;
};

test("真实媒体覆盖章节跳转与书签完整生命周期", async ({ page }) => {
  test.setTimeout(180_000);
  test.skip(!hasFfmpeg, "需要 ffmpeg 与真实后端解析内嵌章节");
  await login(page);

  await withMediaLibrary(
    page.request,
    {
      files: [
        {
          name: VIDEO_FILE,
          write: (path) => copyFileSync(CHAPTER_FIXTURE, path),
        },
      ],
      label: "章节书签 E2E",
      prefix: "chapters-bookmarks-",
    },
    async ({ mediaByName }) => {
      const mediaID = requiredMediaID(mediaByName, VIDEO_FILE);
      await waitForMetadataParse(page, mediaID);
      await verifyChapterAPI(page, mediaID);
      await page.goto(`/play/${mediaID}`);

      const player = page.getByTestId("video-player-root");
      const video = player.locator("video");
      await expect(player).toBeVisible({ timeout: 20_000 });
      await expect
        .poll(
          () => video.evaluate((node) => (node as HTMLVideoElement).duration),
          { timeout: 20_000 },
        )
        .toBeGreaterThan(14);

      await verifyChapterJump(page, video);
      const created = await createBookmarkWhilePlaying(
        page,
        player,
        video,
        mediaID,
      );
      await verifyConflictKeepsDraft(page, mediaID, created);
      await verifySuccessfulEditAndDelete(page, mediaID, created.id);
      await page.goto("/about:blank");
    },
  );
});

async function waitForMetadataParse(
  page: Page,
  mediaID: number,
): Promise<void> {
  await expect
    .poll(
      async () => {
        const response = await page.request.get("/api/tasks", {
          params: {
            page_size: 1,
            resource_id: mediaID,
            resource_type: "media",
            type: "metadata.parse",
          },
        });
        expect(response.ok()).toBeTruthy();
        const task = (
          (await response.json()) as {
            items: Array<{ error?: string | null; status: string }>;
          }
        ).items[0];
        if (task?.status === "failed" || task?.status === "canceled") {
          throw new Error(
            task.error ||
              `元数据解析任务${task.status === "failed" ? "失败" : "已取消"}`,
          );
        }
        return task?.status ?? "missing";
      },
      { timeout: 30_000 },
    )
    .toBe("succeeded");
}

async function verifyChapterAPI(page: Page, mediaID: number): Promise<void> {
  const response = await page.request.get(
    `/api/library/media/${mediaID}/chapters`,
  );
  expect(response.ok()).toBeTruthy();
  const body = (await response.json()) as {
    items: Array<{
      end_ms: number;
      source: string;
      start_ms: number;
      title: string;
    }>;
  };
  expect(
    body.items.map(({ end_ms, source, start_ms, title }) => ({
      end_ms,
      source,
      start_ms,
      title,
    })),
  ).toEqual([
    { end_ms: 5_000, source: "embedded", start_ms: 0, title: "开场" },
    { end_ms: 10_000, source: "embedded", start_ms: 5_000, title: "Main" },
    { end_ms: 15_000, source: "embedded", start_ms: 10_000, title: "结尾" },
  ]);
}

async function verifyChapterJump(page: Page, video: Locator): Promise<void> {
  await openMarkersPanel(page);
  await expect(
    page.getByRole("button", { name: "跳转到章节 开场，0:00" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "跳转到章节 Main，0:05" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "跳转到章节 结尾，0:10" }),
  ).toBeVisible();

  await page.getByRole("button", { name: "跳转到章节 Main，0:05" }).click();
  await expect
    .poll(() => currentTime(video), { timeout: 15_000 })
    .toBeGreaterThanOrEqual(4.8);
  await expect
    .poll(() => currentTime(video), { timeout: 15_000 })
    .toBeLessThan(6);
  await expect(page.getByText("当前章节：Main", { exact: true })).toBeVisible();
}

async function createBookmarkWhilePlaying(
  page: Page,
  player: Locator,
  video: Locator,
  mediaID: number,
): Promise<BookmarkDTO> {
  await setMediaTime(video, 2);
  await play(player);
  await expect
    .poll(() => video.evaluate((node) => (node as HTMLVideoElement).paused))
    .toBe(false);
  await page.getByRole("button", { name: "在当前时间新增书签" }).click();
  await expect
    .poll(() => video.evaluate((node) => (node as HTMLVideoElement).paused))
    .toBe(false);

  const position = page.getByLabel("书签时间（秒）");
  const capturedPosition = Number(await position.inputValue());
  expect(capturedPosition).toBeGreaterThanOrEqual(2);
  expect(capturedPosition).toBeLessThan(5);
  await verifyOversizedNote(page, mediaID);
  await page.getByLabel("书签备注").fill(ALLOWED_NOTE);
  await page.getByRole("button", { name: "保存书签" }).click();
  await expect(page.getByLabel("书签标题")).toHaveCount(0);

  await expect
    .poll(async () => (await listBookmarks(page, mediaID)).length, {
      timeout: 15_000,
    })
    .toBe(1);
  const created = (await listBookmarks(page, mediaID))[0];
  if (!created) throw new Error("真实后端未返回新建书签");
  expect(created.title).toBe("播放中新增书签");
  expect(created.revision).toBe(1);
  expect(Array.from(created.note ?? "")).toHaveLength(2_000);
  expect(created.position_ms).toBe(Math.round(capturedPosition * 1_000));
  await expect(
    page.getByRole("button", { name: "编辑书签 播放中新增书签" }),
  ).toBeVisible();
  return created;
}

async function verifyOversizedNote(page: Page, mediaID: number): Promise<void> {
  await page.getByLabel("书签标题").fill("播放中新增书签");
  await page.getByLabel("书签备注").fill(OVERSIZED_NOTE);
  await page.getByRole("button", { name: "保存书签" }).click();
  await expect(
    page.getByText("书签备注不能超过 2000 个字符", { exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("书签备注")).toHaveValue(OVERSIZED_NOTE);
  expect(
    Array.from(await page.getByLabel("书签备注").inputValue()),
  ).toHaveLength(2_001);
  expect(await listBookmarks(page, mediaID)).toHaveLength(0);
}

async function verifyConflictKeepsDraft(
  page: Page,
  mediaID: number,
  created: BookmarkDTO,
): Promise<void> {
  await page.getByRole("button", { name: "编辑书签 播放中新增书签" }).click();
  await page.getByLabel("书签标题").fill("本地草稿标题");
  await page.getByLabel("书签备注").fill("本地草稿备注 😀");

  const competing = await page.request.put(
    `/api/library/media/${mediaID}/bookmarks/${created.id}`,
    {
      data: {
        note: "服务端版本",
        position_ms: created.position_ms,
        revision: created.revision,
        title: "服务端抢先修改",
      },
    },
  );
  expect(competing.status()).toBe(200);
  expect((await competing.json()) as BookmarkDTO).toMatchObject({
    id: created.id,
    revision: 2,
    title: "服务端抢先修改",
  });

  await page.getByRole("button", { name: "保存修改" }).click();
  await expect(
    page.getByText("书签已在其他设备更新", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("已重新加载服务端最新书签，未覆盖其他设备的修改", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(page.getByText("服务端抢先修改", { exact: true })).toBeVisible();
  await expect(page.getByLabel("书签标题")).toHaveValue("本地草稿标题");
  await expect(page.getByLabel("书签备注")).toHaveValue("本地草稿备注 😀");
  await expect(page.getByRole("button", { name: "保存修改" })).toBeEnabled();
}

async function verifySuccessfulEditAndDelete(
  page: Page,
  mediaID: number,
  bookmarkID: string,
): Promise<void> {
  await page.reload();
  await expect(page.getByTestId("video-player-root")).toBeVisible({
    timeout: 20_000,
  });
  await openMarkersPanel(page);
  await editBookmarkSuccessfully(page, mediaID, bookmarkID);
  await deleteBookmark(page, mediaID);
}

async function editBookmarkSuccessfully(
  page: Page,
  mediaID: number,
  bookmarkID: string,
): Promise<void> {
  await expect(
    page.getByRole("button", { name: "编辑书签 服务端抢先修改" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "编辑书签 服务端抢先修改" }).click();
  await page.getByLabel("书签标题").fill("最终编辑书签");
  await page.getByLabel("书签备注").fill("成功编辑备注 😀");
  await page.getByRole("button", { name: "保存修改" }).click();
  await expect
    .poll(async () => (await listBookmarks(page, mediaID))[0]?.revision, {
      timeout: 15_000,
    })
    .toBe(3);
  expect((await listBookmarks(page, mediaID))[0]).toMatchObject({
    id: bookmarkID,
    note: "成功编辑备注 😀",
    revision: 3,
    title: "最终编辑书签",
  });
  await expect(
    page.getByRole("button", { name: "编辑书签 最终编辑书签" }),
  ).toBeVisible();
}

async function deleteBookmark(page: Page, mediaID: number): Promise<void> {
  await page.getByRole("button", { name: /^删除书签 最终编辑书签，/ }).click();
  await expect(page.getByText(/^确认删除「最终编辑书签」/)).toBeVisible();
  await page.getByRole("button", { name: "确认删除书签 最终编辑书签" }).click();
  await expect
    .poll(async () => (await listBookmarks(page, mediaID)).length, {
      timeout: 15_000,
    })
    .toBe(0);
  await expect(page.getByText("暂无书签", { exact: true })).toBeVisible();
}

async function openMarkersPanel(page: Page): Promise<void> {
  const addButton = page.getByRole("button", { name: "在当前时间新增书签" });
  if (!(await addButton.isVisible())) {
    await page.getByRole("button", { name: /^章节与书签/ }).click();
  }
  await expect(addButton).toBeVisible();
}

async function listBookmarks(
  page: Page,
  mediaID: number,
): Promise<BookmarkDTO[]> {
  const response = await page.request.get(
    `/api/library/media/${mediaID}/bookmarks`,
  );
  expect(response.ok()).toBeTruthy();
  return ((await response.json()) as { items: BookmarkDTO[] }).items;
}

async function play(player: Locator): Promise<void> {
  const video = player.locator("video");
  if (await video.evaluate((node) => (node as HTMLVideoElement).paused)) {
    await player.hover();
    const button = player.getByRole("button", { name: "播放", exact: true });
    await expect(button).toHaveCount(1);
    await button.evaluate((node: HTMLButtonElement) => node.click());
  }
  await expect
    .poll(() => video.evaluate((node) => (node as HTMLVideoElement).paused), {
      timeout: 15_000,
    })
    .toBe(false);
}

async function setMediaTime(video: Locator, target: number): Promise<void> {
  await video.evaluate((node, value) => {
    (node as HTMLVideoElement).currentTime = value;
  }, target);
  await expect
    .poll(() => video.evaluate((node) => !(node as HTMLVideoElement).seeking), {
      timeout: 15_000,
    })
    .toBe(true);
  await expect
    .poll(() => currentTime(video), { timeout: 15_000 })
    .toBeGreaterThanOrEqual(target - 0.2);
}

async function currentTime(video: Locator): Promise<number> {
  return video.evaluate((node) => (node as HTMLVideoElement).currentTime);
}

function requiredMediaID(
  mediaByName: ReadonlyMap<string, number>,
  name: string,
): number {
  const mediaID = mediaByName.get(name);
  if (!mediaID) throw new Error(`未扫描到测试媒体：${name}`);
  return mediaID;
}
