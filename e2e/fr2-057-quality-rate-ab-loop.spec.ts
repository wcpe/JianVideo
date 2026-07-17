import { expect, test, type Locator, type Page } from "@playwright/test";
import { login } from "./helpers";
import {
  createHlsAbr,
  hasFfmpeg,
  withMediaLibrary,
  withSetting,
  writeVideoFixture,
} from "./media-playback-fixtures";

test.describe.configure({ mode: "serial" });
test.use({ serviceWorkers: "block" });

const STANDARD_FILE = "fr2-057-standard.mp4";
const HIGH_ONLY_FILE = "fr2-057-high-only.mp4";
const ABR_LADDER_SETTING = "transcode_abr_ladder";
const PLAYBACK_RATES = [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2] as const;
const DATA_SAVER_OBSERVATION_MS = 10_000;
const WEAK_NETWORK_DOWNLOAD_BYTES_PER_SECOND = 128 * 1024;
const WEAK_NETWORK_UPLOAD_BYTES_PER_SECOND = 128 * 1024;
const WEAK_NETWORK_LATENCY_MS = 150;

interface SegmentRequest {
  at: number;
  url: string;
}

interface WatchStateRequest {
  event_type?: string;
  reason?: string;
}

test("FR2-057 真实 HLS 覆盖清晰度、省流量、倍速与 A-B 循环", async ({
  page,
}) => {
  test.setTimeout(540_000);
  test.skip(!hasFfmpeg, "需要 ffmpeg 与真实后端生成 HLS");
  await login(page);

  await withMediaLibrary(
    page.request,
    {
      files: [
        {
          name: STANDARD_FILE,
          write: (path) =>
            writeVideoFixture(path, {
              duration: 36,
              frameRate: 12,
              height: 720,
              width: 1280,
            }),
        },
        {
          name: HIGH_ONLY_FILE,
          write: (path) =>
            writeVideoFixture(path, {
              duration: 12,
              frameRate: 12,
              height: 720,
              width: 1280,
            }),
        },
      ],
      label: "FR2-057 清晰度倍速循环 E2E",
      prefix: "fr2-057-",
    },
    async ({ mediaByName }) => {
      const standardID = requiredMediaID(mediaByName, STANDARD_FILE);
      const highOnlyID = requiredMediaID(mediaByName, HIGH_ONLY_FILE);
      const masterURL = await createHlsAbr(page.request, standardID);
      await verifyHlsArtifacts(page, standardID, masterURL, ["720p", "480p"]);
      await verifyQualityRateAndLoop(page, standardID);

      await withSetting(
        page.request,
        ABR_LADDER_SETTING,
        '["720p"]',
        async () => {
          const highOnlyMaster = await createHlsAbr(page.request, highOnlyID);
          await verifyHlsArtifacts(page, highOnlyID, highOnlyMaster, ["720p"]);
          await verifyHighOnlyDataSaverBlock(page, highOnlyID);
        },
      );
      await page.goto("/about:blank");
    },
  );
});

async function verifyHlsArtifacts(
  page: Page,
  mediaID: number,
  masterURL: string,
  expectedVariants: readonly string[],
): Promise<void> {
  const masterResponse = await page.request.get(masterURL);
  expect(masterResponse.ok()).toBeTruthy();
  expect(masterResponse.headers()["content-type"]).toContain("mpegurl");
  const master = await masterResponse.text();
  expect(master.match(/#EXT-X-STREAM-INF/g)).toHaveLength(
    expectedVariants.length,
  );

  for (const variant of expectedVariants) {
    const variantPath = `${variant}/index.m3u8`;
    expect(master).toContain(variantPath);
    const playlistURL = `/api/play/hls/${mediaID}/profiles/abr-h264/${variantPath}`;
    const playlistResponse = await page.request.get(playlistURL);
    expect(playlistResponse.ok()).toBeTruthy();
    const playlist = await playlistResponse.text();
    const segment = playlist
      .split(/\r?\n/)
      .find((line) => line.trim().endsWith(".ts"));
    expect(segment).toBeTruthy();
    const segmentResponse = await page.request.get(
      `/api/play/hls/${mediaID}/profiles/abr-h264/${variant}/${segment}`,
    );
    expect(segmentResponse.ok()).toBeTruthy();
    expect(
      Number(segmentResponse.headers()["content-length"] ?? 0),
    ).toBeGreaterThan(0);
  }
}

async function verifyQualityRateAndLoop(
  page: Page,
  mediaID: number,
): Promise<void> {
  const segmentRequests: SegmentRequest[] = [];
  const watchStateRequests: WatchStateRequest[] = [];
  page.on("request", (request) => {
    const url = request.url();
    if (url.includes(`/api/play/hls/${mediaID}/`) && url.endsWith(".ts")) {
      segmentRequests.push({ at: Date.now(), url });
    }
    if (
      request.method() === "PUT" &&
      url.includes(`/api/play/${mediaID}/watch-state`)
    ) {
      const body = request.postDataJSON() as WatchStateRequest | null;
      if (body) watchStateRequests.push(body);
    }
  });
  await page.route(`**/api/play/${mediaID}/stream`, async (route) => {
    await route.fulfill({
      body: '{"code":"DIRECT_FAILED"}',
      contentType: "application/json",
      status: 500,
    });
  });
  await page.goto(`/play/${mediaID}`);
  const player = page.getByTestId("video-player-root");
  const video = page.locator("video");
  await expect(player).toBeVisible({ timeout: 20_000 });
  await expect(page.getByRole("button", { name: "清晰度" })).toBeVisible({
    timeout: 30_000,
  });
  await expect
    .poll(() => video.evaluate((node) => (node as HTMLVideoElement).duration), {
      timeout: 30_000,
    })
    .toBeGreaterThan(35);
  await expect
    .poll(() => segmentRequests.length, { timeout: 30_000 })
    .toBeGreaterThan(0);

  await pause(player);
  await selectQuality(page, "720p");
  await expect(page.getByText("720p（手动）", { exact: true })).toBeVisible();
  await selectQuality(page, "自动");
  await expect(page.getByRole("button", { name: "清晰度" })).toContainText(
    "自动",
  );

  await openRateMenu(player);
  for (const rate of PLAYBACK_RATES) {
    await expect(
      page.getByRole("menuitem", { name: `${rate}×`, exact: true }),
    ).toBeVisible();
  }
  await page.keyboard.press("Escape");
  for (const rate of PLAYBACK_RATES) {
    await selectRate(page, player, rate);
    expect(
      await video.evaluate((node) => (node as HTMLVideoElement).playbackRate),
    ).toBe(rate);
  }

  await setMediaTime(video, 6);
  await setAbPoint(page, player, "设置 A 点");
  await setMediaTime(video, 6.3);
  await setAbPoint(page, player, "设置 B 点");
  await expect(page.getByText(/B 点必须晚于 A 点至少 0.5 秒/)).toBeVisible();
  const loopState = player.getByText(/^A \d/);
  await expect(loopState).toHaveText(/A 0:06\.\d{3} · B 未设置/);
  await setMediaTime(video, 7.5);
  await setAbPoint(page, player, "设置 B 点");
  await expect(loopState).toHaveText(/A 0:06\.\d{3} · B 0:07\.\d{3}/);

  await selectRate(page, player, 1.5);
  await setMediaTime(video, 6.5);
  const retainedTime = await currentTime(video);
  const loopLabel = await loopState.textContent();
  await selectQuality(page, "480p");
  await expect(page.getByText("480p（手动）", { exact: true })).toBeVisible();
  expect(Math.abs((await currentTime(video)) - retainedTime)).toBeLessThan(1);
  expect(
    await video.evaluate((node) => (node as HTMLVideoElement).playbackRate),
  ).toBe(1.5);
  expect(await loopState.textContent()).toBe(loopLabel);
  await selectQuality(page, "自动");
  await setAbPoint(page, player, "清除 A-B");

  await verifyDataSaverUnderWeakNetwork(
    page,
    player,
    video,
    mediaID,
    segmentRequests,
  );
  await setMediaTime(video, 6);
  await setAbPoint(page, player, "设置 A 点");
  await setMediaTime(video, 7.5);
  await setAbPoint(page, player, "设置 B 点");

  for (const rate of [0.5, 1, 2] as const) {
    await selectRate(page, player, rate);
    await setMediaTime(video, 6);
    await installLoopResetObserver(video);
    try {
      await play(player);
      await observeLoopResets(video, 2, 12_000);
      await pause(player);
    } finally {
      await removeLoopResetObserver(video);
    }
  }
  await expect
    .poll(
      () =>
        watchStateRequests.some(
          (request) =>
            request.event_type === "seek" && request.reason === "ab_loop",
        ),
      { message: "A-B 循环定位必须以上报 reason=ab_loop", timeout: 15_000 },
    )
    .toBe(true);

  const mediaResponse = await page.request.get(`/api/library/media/${mediaID}`);
  expect(mediaResponse.ok()).toBeTruthy();
  const media = (await mediaResponse.json()) as { watched?: boolean };
  expect(media.watched).not.toBe(true);
  await expect(page.getByText("Network Error", { exact: false })).toHaveCount(
    0,
  );
}

async function verifyDataSaverUnderWeakNetwork(
  page: Page,
  player: Locator,
  video: Locator,
  mediaID: number,
  segmentRequests: SegmentRequest[],
): Promise<void> {
  const cdp = await page.context().newCDPSession(page);
  await cdp.send("Network.enable");
  try {
    await cdp.send("Network.emulateNetworkConditions", {
      connectionType: "cellular3g",
      downloadThroughput: WEAK_NETWORK_DOWNLOAD_BYTES_PER_SECOND,
      latency: WEAK_NETWORK_LATENCY_MS,
      offline: false,
      uploadThroughput: WEAK_NETWORK_UPLOAD_BYTES_PER_SECOND,
    });
    await verifyDataSaverCap(page, player, video, segmentRequests);
  } finally {
    try {
      await cdp.send("Network.emulateNetworkConditions", {
        downloadThroughput: -1,
        latency: 0,
        offline: false,
        uploadThroughput: -1,
      });
    } finally {
      await cdp.detach();
    }
  }
  await verifyNormalLoadingRestored(
    page,
    player,
    video,
    mediaID,
    segmentRequests,
  );
}

async function verifyDataSaverCap(
  page: Page,
  player: Locator,
  video: Locator,
  segmentRequests: SegmentRequest[],
): Promise<void> {
  const saver = page.getByRole("switch", { name: "省流量" });
  await selectRate(page, player, 0.5);
  await setDataSaver(page, player, true);
  await expect(saver).toBeChecked();
  const capAppliedAt = Date.now();
  await setMediaTime(video, 28, 30_000);
  await play(player);
  await expect
    .poll(
      () =>
        segmentRequests.some(
          (request) =>
            request.at >= capAppliedAt && request.url.includes("/480p/"),
        ),
      { message: "受控弱网下省流量必须加载 480p 分片", timeout: 30_000 },
    )
    .toBe(true);
  const qualityButton = page.getByRole("button", { name: "清晰度" });
  await expect(qualityButton).toContainText("480p", { timeout: 30_000 });
  await page.waitForTimeout(750);
  await observeDataSaverWindow(
    page,
    qualityButton,
    segmentRequests,
    Date.now(),
  );

  await pause(player);
  await page.waitForTimeout(750);
  const stoppedCount = segmentRequests.length;
  await page.waitForTimeout(DATA_SAVER_OBSERVATION_MS);
  expect(segmentRequests.length, "省流量暂停后持续 10 秒不得额外请求分片").toBe(
    stoppedCount,
  );
  await setDataSaver(page, player, false);
  await expect(saver).not.toBeChecked();
}

async function observeDataSaverWindow(
  page: Page,
  qualityButton: Locator,
  segmentRequests: SegmentRequest[],
  capAppliedAt: number,
): Promise<void> {
  const observationStartedAt = Date.now();
  do {
    const cappedRequests = segmentRequests.filter(
      (request) => request.at >= capAppliedAt,
    );
    expect(
      cappedRequests.every((request) => request.url.includes("/480p/")),
      "省流量观察期若继续请求分片，只能请求 480p 变体",
    ).toBe(true);
    const heights = [
      ...(await qualityButton.innerText()).matchAll(/(\d+)p/g),
    ].map((match) => Number(match[1]));
    expect(
      heights.length,
      "hls.js 省流量观察期必须持续报告实际档位",
    ).toBeGreaterThan(0);
    expect(
      Math.max(...heights),
      "hls.js 省流量观察期不得上探到 480p 以上",
    ).toBeLessThanOrEqual(480);
    await page.waitForTimeout(500);
  } while (Date.now() - observationStartedAt < DATA_SAVER_OBSERVATION_MS);
}

async function verifyNormalLoadingRestored(
  page: Page,
  player: Locator,
  video: Locator,
  mediaID: number,
  segmentRequests: SegmentRequest[],
): Promise<void> {
  const restoredAt = Date.now();
  await selectQuality(page, "720p");
  await expect(page.getByText("720p（手动）", { exact: true })).toBeVisible();
  await play(player);
  await setMediaTime(video, 28);
  await expect
    .poll(
      () =>
        segmentRequests.some(
          (request) =>
            request.at >= restoredAt &&
            request.url.includes(`/api/play/hls/${mediaID}/`) &&
            request.url.includes("/720p/"),
        ),
      {
        message: "关闭省流量并恢复正常网络后必须重新加载 720p 分片",
        timeout: 30_000,
      },
    )
    .toBe(true);
  await pause(player);
  await selectQuality(page, "自动");
}

async function verifyHighOnlyDataSaverBlock(
  page: Page,
  mediaID: number,
): Promise<void> {
  const segmentRequests: string[] = [];
  page.on("request", (request) => {
    if (
      request.url().includes(`/api/play/hls/${mediaID}/`) &&
      request.url().endsWith(".ts")
    ) {
      segmentRequests.push(request.url());
    }
  });
  await page.route(`**/api/play/${mediaID}/stream`, async (route) => {
    await route.fulfill({
      body: '{"code":"DIRECT_FAILED"}',
      contentType: "application/json",
      status: 500,
    });
  });
  await page.goto(`/play/${mediaID}`);
  const player = page.getByTestId("video-player-root");
  await expect(page.getByRole("button", { name: "清晰度" })).toBeVisible({
    timeout: 30_000,
  });
  const saver = page.getByRole("switch", { name: "省流量" });
  await setDataSaver(page, player, true);
  await expect(page.getByRole("alert")).toContainText(
    "当前视频无 480p 或更低档位",
  );
  await player.hover();
  await expect(
    player.getByRole("button", { name: "播放", exact: true }),
  ).toBeDisabled();
  await page.waitForTimeout(750);
  const blockedCount = segmentRequests.length;
  await page.waitForTimeout(DATA_SAVER_OBSERVATION_MS);
  expect(
    segmentRequests.length,
    "仅高码率视频启用省流量后持续 10 秒不得额外请求分片",
  ).toBe(blockedCount);
  await setDataSaver(page, player, false);
  await expect(page.getByRole("alert")).toHaveCount(0);
  await expect(
    player.getByRole("button", { name: /^(播放|暂停)$/ }),
  ).toBeEnabled();
}

async function setDataSaver(
  page: Page,
  player: Locator,
  checked: boolean,
): Promise<void> {
  const saver = page.getByRole("switch", { name: "省流量" });
  await player.hover();
  if ((await saver.isChecked()) !== checked) {
    await page
      .getByText("省流量", { exact: true })
      .evaluate((node: HTMLElement) => node.click());
  }
  if (checked) await expect(saver).toBeChecked();
  else await expect(saver).not.toBeChecked();
}

async function selectQuality(page: Page, label: string): Promise<void> {
  await page
    .getByRole("button", { name: "清晰度" })
    .evaluate((node: HTMLButtonElement) => node.click());
  await page.getByRole("menuitem", { name: label, exact: true }).click();
}

async function selectRate(
  page: Page,
  player: Locator,
  rate: number,
): Promise<void> {
  await openRateMenu(player);
  await page.getByRole("menuitem", { name: `${rate}×`, exact: true }).click();
  await expect(player.getByRole("button", { name: "播放速度" })).toContainText(
    `${rate}×`,
  );
}

async function openRateMenu(player: Locator): Promise<void> {
  await player
    .getByRole("button", { name: "播放速度" })
    .evaluate((node: HTMLButtonElement) => node.click());
}

async function setAbPoint(
  page: Page,
  player: Locator,
  label: string,
): Promise<void> {
  await player
    .getByRole("button", { name: "A-B 循环" })
    .evaluate((node: HTMLButtonElement) => node.click());
  await page.getByRole("menuitem", { name: label, exact: true }).click();
}

async function pause(player: Locator): Promise<void> {
  const video = player.locator("video");
  if (!(await video.evaluate((node) => (node as HTMLVideoElement).paused))) {
    const pauseButton = player.getByRole("button", { name: "暂停" });
    await expect(pauseButton).toHaveCount(1);
    await pauseButton.evaluate((node: HTMLButtonElement) => node.click());
  }
  await expect
    .poll(() => video.evaluate((node) => (node as HTMLVideoElement).paused))
    .toBe(true);
  await expect
    .poll(() => video.evaluate((node) => !(node as HTMLVideoElement).seeking))
    .toBe(true);
}

async function play(player: Locator): Promise<void> {
  const video = player.locator("video");
  if (await video.evaluate((node) => (node as HTMLVideoElement).paused)) {
    const playButton = player.getByRole("button", {
      name: "播放",
      exact: true,
    });
    await expect(playButton).toHaveCount(1);
    await playButton.evaluate((node: HTMLButtonElement) => node.click());
  }
  await expect
    .poll(() => video.evaluate((node) => !(node as HTMLVideoElement).paused), {
      timeout: 15_000,
    })
    .toBe(true);
}

async function setMediaTime(
  video: Locator,
  target: number,
  timeout = 15_000,
): Promise<void> {
  await video.evaluate((node, value) => {
    (node as HTMLVideoElement).currentTime = value;
  }, target);
  await expect
    .poll(() => video.evaluate((node) => !(node as HTMLVideoElement).seeking), {
      timeout,
    })
    .toBe(true);
  await expect
    .poll(() => currentTime(video), { timeout })
    .toBeGreaterThanOrEqual(target - 0.8);
}

async function installLoopResetObserver(video: Locator): Promise<void> {
  await video.evaluate((node) => {
    type LoopResetObserver = {
      cleanup: () => void;
      count: number;
      highWater: number;
    };
    type ObservedVideo = HTMLVideoElement & {
      __jianvideoLoopResetObserver?: LoopResetObserver;
    };
    const media = node as ObservedVideo;
    const observer: LoopResetObserver = {
      cleanup: () => undefined,
      count: 0,
      highWater: media.currentTime,
    };
    const record = () => {
      const current = media.currentTime;
      if (observer.highWater - current >= 0.5) {
        observer.count += 1;
        observer.highWater = current;
        return;
      }
      observer.highWater = Math.max(observer.highWater, current);
    };
    const eventTypes = [
      "canplay",
      "playing",
      "seeking",
      "seeked",
      "timeupdate",
      "waiting",
    ];
    for (const eventType of eventTypes)
      media.addEventListener(eventType, record, true);
    observer.cleanup = () => {
      for (const eventType of eventTypes)
        media.removeEventListener(eventType, record, true);
    };
    media.__jianvideoLoopResetObserver = observer;
  });
}

async function observeLoopResets(
  video: Locator,
  rounds: number,
  timeout: number,
): Promise<void> {
  await expect
    .poll(
      () =>
        video.evaluate(
          (node) =>
            (
              node as HTMLVideoElement & {
                __jianvideoLoopResetObserver?: { count: number };
              }
            ).__jianvideoLoopResetObserver?.count ?? 0,
        ),
      { message: "必须观测到媒体时钟从 B 点回到 A 点", timeout },
    )
    .toBeGreaterThanOrEqual(rounds);
}

async function removeLoopResetObserver(video: Locator): Promise<void> {
  await video.evaluate((node) => {
    const media = node as HTMLVideoElement & {
      __jianvideoLoopResetObserver?: { cleanup: () => void };
    };
    media.__jianvideoLoopResetObserver?.cleanup();
    delete media.__jianvideoLoopResetObserver;
  });
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
