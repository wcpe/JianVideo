import { copyFileSync, rmSync, writeFileSync } from "node:fs";
import { mkdir, mkdtemp, readFile, rm } from "node:fs/promises";
import { resolve } from "node:path";
import {
  chromium,
  expect,
  test,
  type APIRequestContext,
  type BrowserContext,
  type CDPSession,
  type Locator,
  type Page,
  type TestInfo,
} from "@playwright/test";
import { ensureSetup, TEST_PASS, TEST_USER } from "./helpers";
import {
  cleanupMediaLibraryFixture,
  createHlsAbr,
  createMediaLibraryFixture,
  FRAME_MARKER,
  hasFfmpeg,
  type MediaLibraryFixture,
  writeNumberedVideo,
  writeSubtitleAudioVideoFixture,
  writeVideoFixture,
} from "./media-playback-fixtures";

const BASE_URL = process.env.TEST_BASE_URL || "http://localhost:8080";
const APP_START_URL = new URL("/", BASE_URL).href;
const ACCEPTANCE_ENABLED = process.env.JIANVIDEO_WINDOWS_ACCEPTANCE === "1";
const PREVIEW_FILE = "p3-windows-preview-long.mp4";
const EXACT_FILE = "p3-windows-exact-numbered.mp4";
const WATCH_FILE = "p3-windows-watch-state.mp4";
const CHAPTER_FILE = "p3-windows-chapters-bookmarks.mp4";
const TRACK_FILE = "p3-windows-subtitle-audio.mp4";
const CHAPTER_SOURCE = resolve(
  "apps/server/internal/library/testdata/chapters/embedded-chapters-three.mp4",
);
const EXACT_FRAME_RATE = 30;
const INITIAL_FRAME_STEP_REQUEST_ID = 0;
const LONG_DURATION = 130;
const TRACK_SUBTITLE_TEXT = "P3 安装态 PWA 内嵌字幕可见";
const MANUAL_ACCEPTANCE_ENV = "JIAN_VIDEO_P3_MANUAL_ACCEPTANCE";
const MANUAL_ACCEPTANCE_EVIDENCE_ENV =
  "JIAN_VIDEO_P3_MANUAL_ACCEPTANCE_EVIDENCE";
const UTC_ISO_TIME_PATTERN =
  /^(\d{4})-(\d{2})-(\d{2})T([01]\d|2[0-3]):([0-5]\d):([0-5]\d)(?:\.(\d{3}))?Z$/u;
const MANUAL_ACCEPTANCE_CHECKLIST = [
  {
    evidence: "在安装态 PWA 中人工听辨 440Hz 与 880Hz 双音轨差异",
    requirement: "FR044",
  },
  {
    evidence: "在安装态 PWA 中人工确认清晰度切换与省流量表现",
    requirement: "FR057",
  },
  {
    evidence: "人工验证 Windows 系统媒体键、锁屏卡片与画中画",
    requirement: "FR058",
  },
  {
    evidence: "独立人工验证安装态 PWA 在后台或窗口失焦后持续播放至少 180 秒",
    minimumDurationSeconds: 180,
    requiredEvidence: [
      "后台或失焦开始时间",
      "后台或失焦结束时间",
      "开始时播放状态与系统媒体状态",
      "结束时播放状态与系统媒体状态",
    ],
    requirement: "FR058",
  },
] as const;

test.beforeAll(async ({}, testInfo) => {
  if (!ACCEPTANCE_ENABLED || process.platform !== "win32") return;
  await enforceManualAcceptance(testInfo);
});

test("P3 必需人工验收确认门", () => {
  test.skip(!ACCEPTANCE_ENABLED, "仅在显式启用 Windows headed 验收时运行");
  test.skip(process.platform !== "win32", "仅支持 Windows 原生 Chrome 验收");
});

interface ManualAcceptanceSummary {
  backgroundPlayback: {
    durationSeconds: number;
    endPlaybackState: "playing";
    endSystemMediaState: "playing";
    startPlaybackState: "playing";
    startSystemMediaState: "playing";
  };
  fr044: {
    dualAudioListening: "passed";
  };
  fr057: {
    dataSaving: "passed";
    qualitySelection: "passed";
  };
  fr058: {
    lockScreen: "passed";
    pictureInPicture: "passed";
    systemMediaKeys: "passed";
  };
  version: 1;
}

async function enforceManualAcceptance(testInfo: TestInfo): Promise<void> {
  const suppliedValue = process.env[MANUAL_ACCEPTANCE_ENV] ?? null;
  const evidencePath = process.env[MANUAL_ACCEPTANCE_EVIDENCE_ENV]?.trim();
  let validationError: Error | undefined;
  let summary: ManualAcceptanceSummary | undefined;

  if (suppliedValue !== "1") {
    validationError = new Error(
      `必须设置 ${MANUAL_ACCEPTANCE_ENV}=1 请求核验；环境变量本身不代表验收通过`,
    );
  } else if (!evidencePath) {
    validationError = new Error(
      `缺少 ${MANUAL_ACCEPTANCE_EVIDENCE_ENV} 指向的结构化 JSON 证据文件`,
    );
  } else {
    try {
      summary = await readManualAcceptanceEvidence(evidencePath);
    } catch (error) {
      validationError = asError(error);
    }
  }

  const attachment = {
    checklist: MANUAL_ACCEPTANCE_CHECKLIST.map((item) => ({
      ...item,
      verified: Boolean(summary),
    })),
    evidencePath: evidencePath ? "已提供（路径已脱敏）" : "未提供",
    evidenceSummary: summary ?? null,
    environmentMeaning: "仅表示请求核验，不触发、不替代也不自动通过人工验证",
    requestEnvironment: `${MANUAL_ACCEPTANCE_ENV}=1`,
    requestReceived: suppliedValue === "1",
    requiredEvidenceEnvironment: MANUAL_ACCEPTANCE_EVIDENCE_ENV,
    requiredEvidenceShape: {
      FR044: { dualAudioListening: "passed" },
      FR057: { dataSaving: "passed", qualitySelection: "passed" },
      FR058: {
        backgroundPlayback: {
          endPlaybackState: "playing",
          endSystemMediaState: "playing",
          endTime: "UTC ISO-8601：YYYY-MM-DDTHH:mm:ss(.sss)?Z",
          minimumDurationSeconds: 180,
          startPlaybackState: "playing",
          startSystemMediaState: "playing",
          startTime: "UTC ISO-8601：YYYY-MM-DDTHH:mm:ss(.sss)?Z",
        },
        lockScreen: "passed",
        pictureInPicture: "passed",
        systemMediaKeys: "passed",
      },
      version: 1,
    },
    status: validationError ? "manual-blocking" : "confirmed",
    validationError: validationError?.message ?? null,
  };
  let attachmentError: Error | undefined;
  try {
    await testInfo.attach("p3-manual-acceptance-checklist.json", {
      body: Buffer.from(JSON.stringify(attachment, null, 2), "utf8"),
      contentType: "application/json",
    });
  } catch (error) {
    attachmentError = asError(error);
  }
  throwCollectedErrors(
    validationError,
    attachmentError ? [attachmentError] : [],
    "P3 人工验收核验与证据附加均失败",
  );
}

async function readManualAcceptanceEvidence(
  evidencePath: string,
): Promise<ManualAcceptanceSummary> {
  let contents: string;
  try {
    contents = await readFile(evidencePath, "utf8");
  } catch {
    throw new Error("P3 人工验收证据文件无法读取");
  }
  let evidence: unknown;
  try {
    evidence = JSON.parse(contents);
  } catch {
    throw new Error("P3 人工验收证据文件不是有效 JSON");
  }
  return validateManualAcceptanceEvidence(evidence);
}

function validateManualAcceptanceEvidence(
  evidence: unknown,
): ManualAcceptanceSummary {
  const root = requireEvidenceObject(evidence, "根对象");
  if (root.version !== 1) throw new Error("人工证据 version 必须为 1");
  const fr044 = requireEvidenceObject(root.FR044, "FR044");
  const fr057 = requireEvidenceObject(root.FR057, "FR057");
  const fr058 = requireEvidenceObject(root.FR058, "FR058");
  const background = requireEvidenceObject(
    fr058.backgroundPlayback,
    "FR058.backgroundPlayback",
  );
  requirePassed(fr044, "dualAudioListening", "FR044 双音轨人工听辨");
  requirePassed(fr057, "qualitySelection", "FR057 清晰度人工确认");
  requirePassed(fr057, "dataSaving", "FR057 省流量人工确认");
  requirePassed(fr058, "systemMediaKeys", "FR058 系统媒体键");
  requirePassed(fr058, "lockScreen", "FR058 锁屏卡片");
  requirePassed(fr058, "pictureInPicture", "FR058 画中画");
  const startTime = requireEvidenceTime(background, "startTime");
  const endTime = requireEvidenceTime(background, "endTime");
  const durationSeconds = (endTime - startTime) / 1_000;
  if (durationSeconds < 180) {
    throw new Error(
      `FR058 后台或失焦持续播放仅 ${durationSeconds} 秒，必须至少 180 秒`,
    );
  }
  requirePlaying(background, "startPlaybackState");
  requirePlaying(background, "endPlaybackState");
  requirePlaying(background, "startSystemMediaState");
  requirePlaying(background, "endSystemMediaState");
  return {
    backgroundPlayback: {
      durationSeconds,
      endPlaybackState: "playing",
      endSystemMediaState: "playing",
      startPlaybackState: "playing",
      startSystemMediaState: "playing",
    },
    fr044: { dualAudioListening: "passed" },
    fr057: { dataSaving: "passed", qualitySelection: "passed" },
    fr058: {
      lockScreen: "passed",
      pictureInPicture: "passed",
      systemMediaKeys: "passed",
    },
    version: 1,
  };
}

function requireEvidenceObject(
  value: unknown,
  label: string,
): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`人工证据 ${label} 必须为对象`);
  }
  return value as Record<string, unknown>;
}

function requirePassed(
  evidence: Record<string, unknown>,
  field: string,
  label: string,
): void {
  if (evidence[field] !== "passed") {
    throw new Error(`${label} 必须明确记录为 passed`);
  }
}

function requirePlaying(
  evidence: Record<string, unknown>,
  field: string,
): void {
  if (evidence[field] !== "playing") {
    throw new Error(`FR058 ${field} 必须明确记录为 playing`);
  }
}

function requireEvidenceTime(
  evidence: Record<string, unknown>,
  field: string,
): number {
  const value = evidence[field];
  if (typeof value !== "string" || !UTC_ISO_TIME_PATTERN.test(value)) {
    throw new Error(
      `FR058 ${field} 必须为 UTC ISO-8601 时间：YYYY-MM-DDTHH:mm:ss(.sss)?Z`,
    );
  }
  const timestamp = Date.parse(value);
  const canonicalValue = value.includes(".")
    ? value
    : value.replace("Z", ".000Z");
  if (
    !Number.isFinite(timestamp) ||
    new Date(timestamp).toISOString() !== canonicalValue
  ) {
    throw new Error(`FR058 ${field} 必须为有效 UTC ISO-8601 时间`);
  }
  return timestamp;
}

test.describe("Windows 原生 Chrome 与安装态 PWA 验收", () => {
  test.describe.configure({ mode: "serial" });
  test.skip(!ACCEPTANCE_ENABLED, "仅在显式启用 Windows headed 验收时运行");
  test.skip(process.platform !== "win32", "仅支持 Windows 原生 Chrome 验收");

  test("真实 Google Chrome headed 基线", async ({}, testInfo) => {
    test.setTimeout(90_000);
    const profileRoot = resolve(".tmp/e2e-run/windows-chrome-profile");
    await mkdir(profileRoot, { recursive: true });
    const profile = await mkdtemp(`${profileRoot}/run-`);
    let context: BrowserContext | undefined;
    let primaryError: Error | undefined;

    try {
      context = await chromium.launchPersistentContext(profile, {
        baseURL: BASE_URL,
        channel: "chrome",
        headless: false,
        serviceWorkers: "allow",
      });
      const page = context.pages()[0] ?? (await context.newPage());
      await page.goto(APP_START_URL);
      await ensureServiceWorkerControl(page);

      const evidence = await collectEvidence(context, page, "browser");
      expect(evidence.browserProduct).toMatch(/^Chrome\//);
      expect(evidence.browserManifestId).toBe(APP_START_URL);
      expect(evidence.installabilityErrors).toEqual([]);
      expect(evidence.processedManifestErrors).toEqual([]);
      expect(evidence.processedManifestDisplay).toBe("standalone");
      expect(evidence.manifestDisplay).toBe("standalone");
      expect(evidence.serviceWorkerActive).toBe("activated");
      expect(evidence.serviceWorkerControlled).toBe(true);
      expect(evidence.displayModeBrowser).toBe(true);
      expect(evidence.displayModeStandalone).toBe(false);
      await attachEvidence(testInfo, page, evidence);
    } catch (error) {
      primaryError = asError(error);
    }

    const cleanupErrors: Error[] = [];
    await captureCleanup(
      cleanupErrors,
      () => context?.close() ?? Promise.resolve(),
    );
    await captureCleanup(cleanupErrors, () => removeProfile(profile));
    throwCollectedErrors(
      primaryError,
      cleanupErrors,
      "Google Chrome headed 基线执行与清理均失败",
    );
  });

  test("真实安装 PWA 并由 manifest identity 启动为 standalone", async ({}, testInfo) => {
    test.setTimeout(120_000);
    const profileRoot = resolve(".tmp/e2e-run/windows-installed-pwa-profile");
    await mkdir(profileRoot, { recursive: true });
    const profile = await mkdtemp(`${profileRoot}/run-`);
    const manifestId = APP_START_URL;
    let browserSession: CDPSession | undefined;
    let context: BrowserContext | undefined;
    let installed = false;
    let primaryError: Error | undefined;

    try {
      context = await chromium.launchPersistentContext(profile, {
        baseURL: BASE_URL,
        channel: "chrome",
        headless: false,
        serviceWorkers: "allow",
      });
      const browserPage = context.pages()[0] ?? (await context.newPage());
      await browserPage.goto(APP_START_URL);
      await ensureServiceWorkerControl(browserPage);

      browserSession = await context.newCDPSession(browserPage);
      await browserSession.send("PWA.install", { manifestId });
      installed = true;
      const browserModeLaunch = await launchInstalledPage(
        context,
        browserSession,
        manifestId,
        "browser",
      );
      await browserModeLaunch.page.close();
      const standaloneLaunch = await launchInstalledPage(
        context,
        browserSession,
        manifestId,
        "standalone",
      );
      const page = standaloneLaunch.page;

      const evidence = await collectEvidence(context, page, "installed-pwa", {
        browserModeLaunchTargetId: browserModeLaunch.launchTargetId,
        browserModePageTargetId: browserModeLaunch.pageTargetId,
        launchTargetId: standaloneLaunch.launchTargetId,
        launchTargetType: standaloneLaunch.launchTargetType,
        launchWindowId: standaloneLaunch.launchWindowId,
        manifestId,
        pageTargetId: standaloneLaunch.pageTargetId,
        pageWindowId: standaloneLaunch.pageWindowId,
        profile,
      });
      expect(evidence.browserProduct).toMatch(/^Chrome\//);
      expect(evidence.browserManifestId).toBe(manifestId);
      expect(evidence.manifestId).toBe(manifestId);
      expect(evidence.installabilityErrors).toEqual([]);
      expect(evidence.processedManifestErrors).toEqual([]);
      expect(evidence.processedManifestDisplay).toBe("standalone");
      expect(evidence.manifestDisplay).toBe("standalone");
      expect(evidence.serviceWorkerActive).toBe("activated");
      expect(evidence.serviceWorkerControlled).toBe(true);
      expect(evidence.displayModeStandalone).toBe(true);
      expect(evidence.displayModeBrowser).toBe(false);
      expect(evidence.browserModeLaunchTargetId).not.toBe(
        evidence.launchTargetId,
      );
      expect(evidence.browserModePageTargetId).not.toBe(evidence.targetId);
      expect(evidence.launchTargetType).toBe("tab");
      expect(evidence.launchWindowId).toBe(evidence.pageWindowId);
      expect(evidence.targetType).toBe("page");
      expect(evidence.targetId).not.toBe(evidence.launchTargetId);
      await attachEvidence(testInfo, page, evidence);
    } catch (error) {
      primaryError = asError(error);
    }

    const cleanupErrors: Error[] = [];
    if (installed && browserSession) {
      const session = browserSession;
      await captureCleanup(cleanupErrors, () =>
        session.send("PWA.uninstall", { manifestId }),
      );
    }
    await captureCleanup(
      cleanupErrors,
      () => browserSession?.detach() ?? Promise.resolve(),
    );
    await captureCleanup(
      cleanupErrors,
      () => context?.close() ?? Promise.resolve(),
    );
    await captureCleanup(cleanupErrors, () => removeProfile(profile));
    throwCollectedErrors(
      primaryError,
      cleanupErrors,
      "安装态 PWA 基线执行与清理均失败",
    );
  });
});

test.describe("Windows 安装态 PWA P3 业务验收", () => {
  test.describe.configure({ mode: "serial", timeout: 360_000 });
  test.skip(!ACCEPTANCE_ENABLED, "仅在显式启用 Windows headed 验收时运行");
  test.skip(process.platform !== "win32", "仅支持 Windows 原生 Chrome 验收");
  test.skip(!hasFfmpeg, "需要 ffmpeg 生成真实 P3 验收媒体");

  test("FR029 安装态 PWA 时间轴 hover 预览采用最新位置", async ({}, testInfo) => {
    await runBusinessAcceptance(async (state) => {
      try {
        const evidence = await verifyTimelinePreviewInPwa(state);
        await attachBusinessEvidence(
          testInfo,
          state.pwaPage,
          "fr029-pwa-preview",
          evidence,
        );
      } catch (error) {
        const blockingEvidence = {
          automatedStatus: "blocked",
          blocker: asError(error).message,
          requirement: "FR029",
          spriteRequirement:
            "每个 VTT xywh 必须为有效整数且不越界，实际渲染 URL 与自然尺寸必须匹配命中 cue",
        };
        testInfo.annotations.push({
          description: `FR029 真实 sprite 验证阻断：${blockingEvidence.blocker}`,
          type: "blocking",
        });
        try {
          await attachBusinessEvidence(
            testInfo,
            state.pwaPage,
            "fr029-pwa-preview-blocking",
            blockingEvidence,
          );
        } catch (attachmentError) {
          throw new AggregateError(
            [error, attachmentError],
            "FR029 验证与阻断证据附加均失败",
          );
        }
        throw error;
      }
    }, "FR029 业务验收准备、验证与清理失败");
  });

  test("FR034/035/036 安装态 PWA 逐帧、阶梯定位且无 Network Error", async ({}, testInfo) => {
    await runBusinessAcceptance(async (state) => {
      const evidence = await verifyFrameAndSeekInPwa(state);
      await attachBusinessEvidence(
        testInfo,
        state.pwaPage,
        "fr034-036-pwa-core",
        evidence,
      );
    }, "FR034/035/036 业务验收准备、验证与清理失败");
  });

  test("FR045 PWA A 与独立 persistent profile B 续播且旧 revision 不覆盖", async ({}, testInfo) => {
    await runBusinessAcceptance(async (state) => {
      const evidence = await verifyCrossProfileWatchState(state);
      await attachBusinessEvidence(
        testInfo,
        state.pwaPage,
        "fr045-cross-profile",
        evidence,
      );
    }, "FR045 业务验收准备、验证与清理失败");
  });

  test("FR060 PWA A 与 profile B 书签冲突并在重启后恢复", async ({}, testInfo) => {
    await runBusinessAcceptance(async (state) => {
      const evidence = await verifyChaptersBookmarksAndRestart(state);
      await attachBusinessEvidence(
        testInfo,
        state.pwaPage,
        "fr060-bookmark-restart",
        evidence,
      );
    }, "FR060 业务验收准备、验证与清理失败");
  });

  test("FR044/057/058 自动化真实可达能力（不替代人工确认门）", async ({}, testInfo) => {
    test.setTimeout(480_000);
    await runBusinessAcceptance(async (state) => {
      try {
        const evidence = await verifyReachablePlatformCapabilities(
          state,
          testInfo,
        );
        await attachBusinessEvidence(
          testInfo,
          state.pwaPage,
          "fr044-057-058-real-capabilities",
          evidence,
        );
      } catch (error) {
        const blockingEvidence = {
          automatedStatus: "blocked",
          blocker: asError(error).message,
          networkInterception: "未使用 page.route、route.fulfill 或 mock",
          requirements: ["FR044", "FR057", "FR058"],
        };
        testInfo.annotations.push({
          description: `真实能力自动验证阻断：${blockingEvidence.blocker}`,
          type: "blocking",
        });
        try {
          await attachBusinessEvidence(
            testInfo,
            state.pwaPage,
            "fr044-057-058-blocking",
            blockingEvidence,
          );
        } catch (attachmentError) {
          throw new AggregateError(
            [error, attachmentError],
            "真实能力验证与阻断证据附加均失败",
          );
        }
        throw error;
      }
    }, "FR044/057/058 业务验收准备、验证与清理失败");
  });
});

interface BusinessAcceptanceState {
  browserPage?: Page;
  browserPageB?: Page;
  browserProfileB?: string;
  browserSession?: CDPSession;
  context?: BrowserContext;
  contextB?: BrowserContext;
  fixture?: MediaLibraryFixture;
  installed?: boolean;
  pwaPage?: Page;
  profile?: string;
}

interface BusinessAcceptanceReadyState {
  browserPage: Page;
  browserPageB: Page;
  browserProfileB: string;
  browserSession: CDPSession;
  context: BrowserContext;
  contextB: BrowserContext;
  fixture: MediaLibraryFixture;
  installed: true;
  pwaPage: Page;
  profile: string;
}

async function runBusinessAcceptance(
  body: (state: BusinessAcceptanceReadyState) => Promise<void>,
  failureMessage: string,
): Promise<void> {
  const state: BusinessAcceptanceState = {};
  let primaryError: Error | undefined;
  try {
    await setupBusinessAcceptance(state);
    await body(requireBusinessState(state));
  } catch (error) {
    primaryError = asError(error);
  }
  const cleanupErrors = await cleanupBusinessAcceptance(state);
  throwCollectedErrors(primaryError, cleanupErrors, failureMessage);
}

async function setupBusinessAcceptance(
  state: BusinessAcceptanceState,
): Promise<void> {
  state.profile = await createAcceptanceProfile("windows-p3-pwa-a-profile");
  state.context = await launchAcceptanceContext(state.profile);
  state.browserPage =
    state.context.pages()[0] ?? (await state.context.newPage());
  await state.browserPage.goto(APP_START_URL);
  await ensureServiceWorkerControl(state.browserPage);
  await ensureSetup(state.browserPage.request);

  state.browserSession = await state.context.newCDPSession(state.browserPage);
  await state.browserSession.send("PWA.install", { manifestId: APP_START_URL });
  state.installed = true;
  state.pwaPage = (
    await launchInstalledPage(
      state.context,
      state.browserSession,
      APP_START_URL,
      "standalone",
    )
  ).page;
  await loginThroughUI(state.pwaPage, true);

  state.browserProfileB = await createAcceptanceProfile(
    "windows-p3-browser-b-profile",
  );
  state.contextB = await launchAcceptanceContext(state.browserProfileB);
  state.browserPageB =
    state.contextB.pages()[0] ?? (await state.contextB.newPage());
  await loginThroughUI(state.browserPageB, false);

  state.fixture = await createMediaLibraryFixture(
    state.pwaPage.request,
    businessMediaOptions(),
  );
}

async function cleanupBusinessAcceptance(
  state: BusinessAcceptanceState,
): Promise<Error[]> {
  const errors: Error[] = [];
  await captureCleanup(errors, () => leavePlaybackPages(state));
  if (state.fixture && state.browserPage) {
    const fixture = state.fixture;
    const page = state.browserPage;
    await captureCleanup(errors, () =>
      cleanupMediaLibraryFixture(page.request, fixture),
    );
  }
  state.fixture = undefined;
  if (state.installed && state.browserSession) {
    const session = state.browserSession;
    await captureCleanup(errors, () =>
      session.send("PWA.uninstall", { manifestId: APP_START_URL }),
    );
  }
  state.installed = false;
  await captureCleanup(
    errors,
    () => state.browserSession?.detach() ?? Promise.resolve(),
  );
  await captureCleanup(
    errors,
    () => state.contextB?.close() ?? Promise.resolve(),
  );
  await captureCleanup(
    errors,
    () => state.context?.close() ?? Promise.resolve(),
  );
  if (state.browserProfileB) {
    await captureCleanup(errors, () => removeProfile(state.browserProfileB!));
  }
  if (state.profile)
    await captureCleanup(errors, () => removeProfile(state.profile!));
  clearBusinessState(state);
  return errors;
}

async function removeProfile(profile: string): Promise<void> {
  await rm(profile, {
    force: true,
    maxRetries: 5,
    recursive: true,
    retryDelay: 100,
  });
}

async function ensureServiceWorkerControl(page: Page): Promise<void> {
  await page.evaluate(() => navigator.serviceWorker.ready);
  const controlled = await page.evaluate(() =>
    Boolean(navigator.serviceWorker.controller),
  );
  if (!controlled) await page.reload({ waitUntil: "domcontentloaded" });
  await expect
    .poll(
      () => page.evaluate(() => Boolean(navigator.serviceWorker.controller)),
      {
        timeout: 15_000,
      },
    )
    .toBe(true);
}

type PwaDisplayMode = "browser" | "standalone";

interface InstalledPageLaunch {
  launchTargetId: string;
  launchTargetType: string;
  launchWindowId: number;
  page: Page;
  pageTargetId: string;
  pageWindowId: number;
}

async function launchInstalledPage(
  context: BrowserContext,
  browserSession: CDPSession,
  manifestId: string,
  displayMode: PwaDisplayMode,
): Promise<InstalledPageLaunch> {
  const existingPages = new Set(collectContextPages(context));
  const existingTargetIds = await collectPageTargetIds(browserSession);
  const existingWindowIds = await collectPageWindowIds(
    browserSession,
    existingTargetIds,
  );
  await browserSession.send("PWA.changeAppUserSettings", {
    displayMode,
    manifestId,
  });
  const launched = await browserSession.send("PWA.launch", { manifestId });
  const launchedTarget = await browserSession.send("Target.getTargetInfo", {
    targetId: launched.targetId,
  });
  const launchedWindow = await browserSession.send(
    "Browser.getWindowForTarget",
    {
      targetId: launched.targetId,
    },
  );
  if (displayMode === "standalone")
    expect(existingWindowIds.has(launchedWindow.windowId)).toBe(false);
  const matched = await waitForTargetPageInWindow(
    context,
    browserSession,
    existingPages,
    existingTargetIds,
    launchedWindow.windowId,
  );
  await prepareLaunchedPage(matched.page, displayMode);
  const pageWindowId = await readPageWindowId(browserSession, matched.targetId);
  expect(pageWindowId).toBe(launchedWindow.windowId);
  return {
    launchTargetId: launched.targetId,
    launchTargetType: launchedTarget.targetInfo.type,
    launchWindowId: launchedWindow.windowId,
    page: matched.page,
    pageTargetId: matched.targetId,
    pageWindowId,
  };
}

async function prepareLaunchedPage(
  page: Page,
  displayMode: PwaDisplayMode,
): Promise<void> {
  await page.waitForLoadState("domcontentloaded");
  await ensureServiceWorkerControl(page);
  await expect
    .poll(
      () =>
        page.evaluate(
          (mode) => matchMedia(`(display-mode: ${mode})`).matches,
          displayMode,
        ),
      {
        timeout: 15_000,
      },
    )
    .toBe(true);
  await expect(
    page.evaluate(
      (mode) =>
        matchMedia(
          `(display-mode: ${mode === "browser" ? "standalone" : "browser"})`,
        ).matches,
      displayMode,
    ),
  ).resolves.toBe(false);
}

async function expectStandaloneRoute(
  page: Page,
  expectedPathname: string,
): Promise<void> {
  await prepareLaunchedPage(page, "standalone");
  await expect
    .poll(() => page.evaluate(() => location.pathname), { timeout: 15_000 })
    .toBe(expectedPathname);
}

function collectContextPages(context: BrowserContext): Page[] {
  const browserContexts = context.browser()?.contexts() ?? [context];
  return browserContexts.flatMap((browserContext) => browserContext.pages());
}

async function collectPageTargetIds(
  browserSession: CDPSession,
): Promise<Set<string>> {
  const { targetInfos } = await browserSession.send("Target.getTargets");
  return new Set(
    targetInfos
      .filter((target) => target.type === "page")
      .map((target) => target.targetId),
  );
}

async function collectPageWindowIds(
  browserSession: CDPSession,
  targetIds: ReadonlySet<string>,
): Promise<Set<number>> {
  const windowIds = new Set<number>();
  for (const targetId of targetIds) {
    windowIds.add(await readPageWindowId(browserSession, targetId));
  }
  return windowIds;
}

async function waitForTargetPageInWindow(
  context: BrowserContext,
  browserSession: CDPSession,
  existingPages: ReadonlySet<Page>,
  existingTargetIds: ReadonlySet<string>,
  expectedWindowId: number,
): Promise<{ page: Page; targetId: string }> {
  let matched: { page: Page; targetId: string } | undefined;
  await expect
    .poll(
      async () => {
        const { targetInfos } = await browserSession.send("Target.getTargets");
        const pages = collectContextPages(context).filter(
          (page) =>
            !existingPages.has(page) &&
            !page.isClosed() &&
            page.url().startsWith(BASE_URL),
        );
        for (const target of targetInfos) {
          if (
            target.type !== "page" ||
            existingTargetIds.has(target.targetId) ||
            !target.url.startsWith(BASE_URL)
          ) {
            continue;
          }
          const windowId = await readPageWindowId(
            browserSession,
            target.targetId,
          );
          if (windowId !== expectedWindowId) continue;
          const page = pages.find(
            (candidate) => candidate.url() === target.url,
          );
          if (!page) continue;
          matched = { page, targetId: target.targetId };
          return true;
        }
        return false;
      },
      {
        message: `Chrome 未暴露窗口 ${expectedWindowId} 对应的安装态 PWA 页面目标`,
        timeout: 20_000,
      },
    )
    .toBe(true);
  if (!matched) throw new Error("安装态 PWA 页面目标匹配结果缺失");
  return matched;
}

async function readPageWindowId(
  browserSession: CDPSession,
  targetId: string,
): Promise<number> {
  const window = await browserSession.send("Browser.getWindowForTarget", {
    targetId,
  });
  return window.windowId;
}

async function collectCdpEvidence(
  context: BrowserContext,
  page: Page,
  knownTargetId?: string,
) {
  const browser = context.browser();
  if (!browser) throw new Error("无法取得 Chrome 浏览器实例");
  let browserSession: CDPSession | undefined;
  let pageSession: CDPSession | undefined;
  let evidence:
    | {
        browserAppId: { appId?: string };
        installability: { installabilityErrors: unknown[] };
        processedManifest: {
          errors: unknown[];
          manifest: { display?: string };
          url: string;
        };
        target: { targetInfo: { targetId: string; type: string } };
        version: { product: string; protocolVersion: string; revision: string };
      }
    | undefined;
  let primaryError: Error | undefined;
  try {
    browserSession = await browser.newBrowserCDPSession();
    pageSession = await context.newCDPSession(page);
    const frameTree = await pageSession.send("Page.getFrameTree");
    const targetId = knownTargetId ?? frameTree.frameTree.frame.id;
    evidence = {
      browserAppId: await pageSession.send("Page.getAppId"),
      installability: await pageSession.send("Page.getInstallabilityErrors"),
      processedManifest: await pageSession.send("Page.getAppManifest"),
      target: await browserSession.send("Target.getTargetInfo", { targetId }),
      version: await browserSession.send("Browser.getVersion"),
    };
  } catch (error) {
    primaryError = asError(error);
  }
  const cleanupErrors: Error[] = [];
  await captureCleanup(
    cleanupErrors,
    () => pageSession?.detach() ?? Promise.resolve(),
  );
  await captureCleanup(
    cleanupErrors,
    () => browserSession?.detach() ?? Promise.resolve(),
  );
  throwCollectedErrors(
    primaryError,
    cleanupErrors,
    "CDP 证据采集与 Session 清理均失败",
  );
  if (!evidence) throw new Error("CDP 证据采集结果缺失");
  return evidence;
}

async function collectEvidence(
  context: BrowserContext,
  page: Page,
  mode: "browser" | "installed-pwa",
  installed?: {
    browserModeLaunchTargetId: string;
    browserModePageTargetId: string;
    launchTargetId: string;
    launchTargetType: string;
    launchWindowId: number;
    manifestId: string;
    pageTargetId: string;
    pageWindowId: number;
    profile: string;
  },
) {
  const { browserAppId, installability, processedManifest, target, version } =
    await collectCdpEvidence(context, page, installed?.pageTargetId);
  const pageEvidence = await page.evaluate(async () => {
    const manifestLink = document.querySelector<HTMLLinkElement>(
      'link[rel~="manifest"]',
    );
    if (!manifestLink) throw new Error("页面缺少 Web App Manifest 链接");
    const manifestUrl = new URL(manifestLink.href, location.href).href;
    const manifest = (await fetch(manifestUrl).then((response) => {
      if (!response.ok)
        throw new Error(`读取 Manifest 失败：HTTP ${response.status}`);
      return response.json();
    })) as { display?: string; id?: string; start_url?: string };
    const registration = await navigator.serviceWorker.getRegistration();
    const manifestId = new URL(
      manifest.id || manifest.start_url || "/",
      manifestUrl,
    ).href;

    return {
      displayModeBrowser: matchMedia("(display-mode: browser)").matches,
      displayModeStandalone: matchMedia("(display-mode: standalone)").matches,
      manifestDisplay: manifest.display ?? null,
      manifestId,
      manifestUrl,
      pageUrl: location.href,
      serviceWorkerActive: registration?.active?.state ?? null,
      serviceWorkerControlled: Boolean(navigator.serviceWorker.controller),
      userAgent: navigator.userAgent,
    };
  });

  return {
    ...pageEvidence,
    acceptanceMode: mode,
    browserManifestId: browserAppId.appId ?? null,
    browserModeLaunchTargetId: installed?.browserModeLaunchTargetId ?? null,
    browserModePageTargetId: installed?.browserModePageTargetId ?? null,
    installedManifestId: installed?.manifestId ?? null,
    launchTargetId: installed?.launchTargetId ?? null,
    launchTargetType: installed?.launchTargetType ?? null,
    launchWindowId: installed?.launchWindowId ?? null,
    browserProduct: version.product,
    browserProtocolVersion: version.protocolVersion,
    browserRevision: version.revision,
    installabilityErrors: installability.installabilityErrors,
    manifestId: pageEvidence.manifestId,
    processedManifestDisplay: normalizeProcessedManifestDisplay(
      processedManifest.manifest.display,
    ),
    processedManifestDisplayRaw: processedManifest.manifest.display ?? null,
    processedManifestErrors: processedManifest.errors,
    processedManifestUrl: processedManifest.url,
    profile: installed?.profile ?? null,
    pageWindowId: installed?.pageWindowId ?? null,
    targetId: target.targetInfo.targetId,
    targetType: target.targetInfo.type,
  };
}

function normalizeProcessedManifestDisplay(
  display: string | undefined,
): string | null {
  if (!display) return null;
  return display === "kStandalone" ? "standalone" : display;
}

async function attachEvidence(
  testInfo: TestInfo,
  page: Page,
  evidence: object,
): Promise<void> {
  await testInfo.attach("windows-headed-evidence.json", {
    body: Buffer.from(JSON.stringify(evidence, null, 2), "utf8"),
    contentType: "application/json",
  });
  await testInfo.attach("windows-headed-page.png", {
    body: await page.screenshot(),
    contentType: "image/png",
  });
}

function businessMediaOptions() {
  return {
    files: [
      {
        name: PREVIEW_FILE,
        write: (path: string) =>
          writeVideoFixture(path, {
            duration: LONG_DURATION,
            frameRate: 10,
            height: 180,
            width: 320,
          }),
      },
      {
        name: EXACT_FILE,
        write: (path: string) =>
          writeNumberedVideo(path, {
            duration: 10,
            frameRate: EXACT_FRAME_RATE,
            height: 180,
            width: 320,
          }),
      },
      {
        name: WATCH_FILE,
        write: (path: string) =>
          writeVideoFixture(path, {
            duration: 60,
            frameRate: 10,
            height: 180,
            width: 320,
          }),
      },
      {
        name: CHAPTER_FILE,
        write: (path: string) => copyFileSync(CHAPTER_SOURCE, path),
      },
      { name: TRACK_FILE, write: writeTrackFixture },
    ],
    label: "P3 Windows 安装态 PWA 业务验收",
    prefix: "p3-windows-acceptance-",
  };
}

function writeTrackFixture(path: string): void {
  const subtitlePath = `${path}.embedded.srt`;
  writeFileSync(
    subtitlePath,
    `1\n00:00:01,000 --> 00:00:07,000\n${TRACK_SUBTITLE_TEXT}\n`,
  );
  let primaryError: Error | undefined;
  try {
    writeSubtitleAudioVideoFixture(path, subtitlePath);
  } catch (error) {
    primaryError = asError(error);
  }
  const cleanupErrors: Error[] = [];
  try {
    rmSync(subtitlePath, { force: true });
  } catch (error) {
    cleanupErrors.push(asError(error));
  }
  throwCollectedErrors(
    primaryError,
    cleanupErrors,
    "字幕音轨夹具写入与临时字幕清理均失败",
  );
}

async function createAcceptanceProfile(name: string): Promise<string> {
  const root = resolve(`.tmp/e2e-run/${name}`);
  await mkdir(root, { recursive: true });
  return mkdtemp(`${root}/run-`);
}

function launchAcceptanceContext(profile: string): Promise<BrowserContext> {
  return chromium.launchPersistentContext(profile, {
    baseURL: BASE_URL,
    channel: "chrome",
    headless: false,
    serviceWorkers: "allow",
  });
}

async function loginThroughUI(page: Page, standalone: boolean): Promise<void> {
  await page.goto(new URL("/login", BASE_URL).href);
  if (standalone) await expectStandaloneRoute(page, "/login");
  await expect(page.getByLabel("用户名")).toBeVisible({ timeout: 15_000 });
  await page.getByLabel("用户名").fill(TEST_USER);
  await page.getByLabel("密码").fill(TEST_PASS);
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page.getByRole("heading", { name: "概览" })).toBeVisible({
    timeout: 15_000,
  });
  if (standalone) await expectStandaloneRoute(page, "/");
}

function requireBusinessState(
  state: BusinessAcceptanceState,
): BusinessAcceptanceReadyState {
  if (
    !state.browserPage ||
    !state.browserPageB ||
    !state.browserProfileB ||
    !state.browserSession ||
    !state.context ||
    !state.contextB ||
    !state.fixture ||
    !state.installed ||
    !state.pwaPage ||
    !state.profile
  ) {
    throw new Error("P3 Windows 业务验收环境尚未准备完成");
  }
  return state as BusinessAcceptanceReadyState;
}

async function leavePlaybackPages(
  state: BusinessAcceptanceState,
): Promise<void> {
  const pages = [state.pwaPage, state.browserPageB, state.browserPage].filter(
    (page): page is Page => Boolean(page && !page.isClosed()),
  );
  const results = await Promise.allSettled(
    pages.map((page) => page.goto("about:blank").then(() => undefined)),
  );
  const failures = results
    .filter(
      (result): result is PromiseRejectedResult => result.status === "rejected",
    )
    .map((result) => asError(result.reason));
  if (failures.length > 0) throw new AggregateError(failures, "离开播放页失败");
}

async function captureCleanup(
  errors: Error[],
  run: () => unknown | Promise<unknown>,
): Promise<void> {
  try {
    await run();
  } catch (error) {
    errors.push(asError(error));
  }
}

async function runWithCleanup<T>(
  body: () => Promise<T>,
  cleanups: readonly (() => unknown | Promise<unknown>)[],
  failureMessage: string,
): Promise<T> {
  let completed = false;
  let primaryError: Error | undefined;
  let result: T | undefined;
  try {
    result = await body();
    completed = true;
  } catch (error) {
    primaryError = asError(error);
  }
  const cleanupErrors: Error[] = [];
  for (const cleanup of cleanups) {
    await captureCleanup(cleanupErrors, cleanup);
  }
  throwCollectedErrors(primaryError, cleanupErrors, failureMessage);
  if (!completed) throw new Error(`${failureMessage}：结果缺失`);
  return result as T;
}

function clearBusinessState(state: BusinessAcceptanceState): void {
  for (const key of Object.keys(state) as Array<
    keyof BusinessAcceptanceState
  >) {
    delete state[key];
  }
}

function asError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}

function throwCollectedErrors(
  primaryError: Error | undefined,
  cleanupErrors: readonly Error[],
  message: string,
): void {
  const errors = primaryError
    ? [primaryError, ...cleanupErrors]
    : [...cleanupErrors];
  if (errors.length === 1) throw errors[0];
  if (errors.length > 1) throw new AggregateError(errors, message);
}

function requiredMediaID(fixture: MediaLibraryFixture, name: string): number {
  const mediaID = fixture.mediaByName.get(name);
  if (!mediaID) throw new Error(`未扫描到 P3 验收媒体：${name}`);
  return mediaID;
}

async function gotoPwaPlayback(
  state: BusinessAcceptanceReadyState,
  mediaID: number,
): Promise<Page> {
  await state.pwaPage.goto(new URL(`/play/${mediaID}`, BASE_URL).href);
  await expectStandaloneRoute(state.pwaPage, `/play/${mediaID}`);
  return state.pwaPage;
}

async function gotoBrowserPlayback(
  state: BusinessAcceptanceReadyState,
  mediaID: number,
): Promise<Page> {
  await state.browserPageB.goto(new URL(`/play/${mediaID}`, BASE_URL).href);
  await expect(
    state.browserPageB.evaluate(
      () => matchMedia("(display-mode: browser)").matches,
    ),
  ).resolves.toBe(true);
  return state.browserPageB;
}

async function restartInstalledPwa(
  state: BusinessAcceptanceReadyState,
): Promise<Page> {
  const cleanupErrors: Error[] = [];
  if (!state.pwaPage.isClosed()) {
    await captureCleanup(cleanupErrors, () =>
      state.pwaPage.goto("about:blank"),
    );
    await captureCleanup(cleanupErrors, () => state.pwaPage.close());
  }
  let primaryError: Error | undefined;
  let restartedPage: Page | undefined;
  try {
    const launch = await launchInstalledPage(
      state.context,
      state.browserSession,
      APP_START_URL,
      "standalone",
    );
    restartedPage = launch.page;
    state.pwaPage = launch.page;
    await expect(
      launch.page.getByRole("heading", { name: "概览" }),
    ).toBeVisible({ timeout: 15_000 });
    await expectStandaloneRoute(launch.page, "/");
  } catch (error) {
    primaryError = asError(error);
  }
  throwCollectedErrors(
    primaryError,
    cleanupErrors,
    "安装态 PWA 重启与旧页面清理均失败",
  );
  if (!restartedPage) throw new Error("安装态 PWA 重启页面缺失");
  return restartedPage;
}

async function attachBusinessEvidence(
  testInfo: TestInfo,
  page: Page,
  name: string,
  evidence: object,
): Promise<void> {
  await testInfo.attach(`${name}.json`, {
    body: Buffer.from(JSON.stringify(evidence, null, 2), "utf8"),
    contentType: "application/json",
  });
  await testInfo.attach(`${name}.png`, {
    body: await page.screenshot(),
    contentType: "image/png",
  });
}

interface TimelinePreviewStatus {
  profile_id: string;
  sprite_urls?: Record<string, string>;
  status: "available" | "pending";
  task_id?: number;
  vtt_url?: string;
}

interface TimelineSpriteEvidence {
  byteLength: number;
  contentType: string;
  height: number;
  name: string;
  url: string;
  width: number;
}

interface TimelineCueReference {
  endSeconds: number;
  height: number;
  name: string;
  startSeconds: number;
  width: number;
  x: number;
  y: number;
}

interface TimelineCueEvidence extends TimelineCueReference {
  spriteHeight: number;
  spriteURL: string;
  spriteWidth: number;
}

async function verifyTimelinePreviewInPwa(state: BusinessAcceptanceReadyState) {
  const mediaID = requiredMediaID(state.fixture, PREVIEW_FILE);
  const preview = await prepareTimelinePreview(state.pwaPage, mediaID);
  const page = await gotoPwaPlayback(state, mediaID);
  const { video } = await waitForPlayablePlayer(page, LONG_DURATION - 5);
  const progress = page.getByTestId("video-progress-preview");
  await expect(progress).toBeVisible({ timeout: 15_000 });
  await waitForTimelineSprite(progress);

  const duration = await video.evaluate(
    (node) => (node as HTMLVideoElement).duration,
  );
  const hoverFractions = [0.2, 0.35, 0.6, 0.8];
  for (const fraction of hoverFractions) {
    await moveToFraction(progress, fraction);
  }
  const latestExpectedSeconds = duration * 0.8;
  const overlay = progress.getByTestId("timeline-preview-overlay");
  await expectPreviewTime(overlay, latestExpectedSeconds);
  const expectedCue = requireTimelineCueAt(preview.cues, latestExpectedSeconds);
  const renderedSprite = await verifyRenderedTimelineSprite(
    progress,
    expectedCue,
  );
  await expect(page.getByText(/Network Error/i)).toHaveCount(0);

  return {
    automated: [
      "FR029 hover 预览",
      "快速 hover 后采用最新 80% 位置",
      "每个 VTT xywh 坐标均在对应 sprite 自然尺寸内",
      "实际渲染 sprite URL、自然尺寸与命中 cue 一致",
    ],
    displayModeStandalone: await isStandalone(page),
    fixtureProvisioning:
      "通过已登录 PWA 页面的 request 准备，不作为 UI 导入证据",
    hoverFractions,
    latestDisplayedTime: await overlay.textContent(),
    latestExpectedSeconds,
    mediaID,
    preview,
    renderedSprite,
  };
}

async function prepareTimelinePreview(
  page: Page,
  mediaID: number,
): Promise<
  TimelinePreviewStatus & {
    cues: TimelineCueEvidence[];
    spriteCount: number;
    sprites: TimelineSpriteEvidence[];
    vttCueCount: number;
  }
> {
  const request = page.request;
  let response = await request.get(`/api/play/${mediaID}/timeline-preview`);
  expect([200, 202]).toContain(response.status());
  let status = (await response.json()) as TimelinePreviewStatus;
  if (response.status() === 202) {
    expect(status.task_id).toBeGreaterThan(0);
    await waitUnifiedTask(request, status.task_id!);
    await expect
      .poll(
        async () => {
          response = await request.get(
            `/api/play/${mediaID}/timeline-preview`,
            {
              params: { profile: status.profile_id },
            },
          );
          if (response.status() !== 200) return response.status();
          status = (await response.json()) as TimelinePreviewStatus;
          return status.status;
        },
        { timeout: 120_000 },
      )
      .toBe("available");
  }
  expect(status.status).toBe("available");
  const vttURL = resolveRequiredHttpUrl(status.vtt_url, BASE_URL, "VTT");
  const vttResponse = await request.get(vttURL);
  expect(vttResponse.ok()).toBeTruthy();
  const vtt = await vttResponse.text();
  const references = parseTimelineCueReferences(vtt);
  const spriteNames = [...new Set(references.map(({ name }) => name))];
  expect(spriteNames.length).toBeGreaterThanOrEqual(2);
  const sprites: TimelineSpriteEvidence[] = [];
  for (const name of spriteNames) {
    const spriteURL = resolveRequiredHttpUrl(
      status.sprite_urls?.[name],
      vttURL,
      `sprite ${name}`,
    );
    const spriteResponse = await request.get(spriteURL);
    if (!spriteResponse.ok()) {
      throw new Error(
        `FR029 sprite ${name} 请求失败：HTTP ${spriteResponse.status()} ${spriteURL}`,
      );
    }
    const contentType = spriteResponse.headers()["content-type"] ?? "";
    expect(contentType).toMatch(/^image\//u);
    const body = await spriteResponse.body();
    expect(body.byteLength).toBeGreaterThan(100);
    const dimensions = await readImageDimensions(page, spriteURL);
    expect(dimensions.width).toBeGreaterThan(0);
    expect(dimensions.height).toBeGreaterThan(0);
    sprites.push({
      byteLength: body.byteLength,
      contentType,
      height: dimensions.height,
      name,
      url: spriteURL,
      width: dimensions.width,
    });
  }
  const spritesByName = new Map(sprites.map((sprite) => [sprite.name, sprite]));
  const cues = references.map((reference) =>
    validateTimelineCueBounds(reference, spritesByName),
  );
  return {
    ...status,
    cues,
    spriteCount: sprites.length,
    sprites,
    vttCueCount: cues.length,
  };
}

function parseTimelineCueReferences(vtt: string): TimelineCueReference[] {
  const references = vtt
    .replace(/\r\n?/gu, "\n")
    .split(/\n{2,}/u)
    .flatMap(parseTimelineCueBlock);
  const xywhCount = [...vtt.matchAll(/#xywh=/gu)].length;
  if (xywhCount === 0) throw new Error("FR029 VTT 未包含 xywh cue");
  if (references.length !== xywhCount) {
    throw new Error(
      `FR029 VTT xywh cue 未全部解析：发现 ${xywhCount} 个，解析 ${references.length} 个`,
    );
  }
  return references;
}

function parseTimelineCueBlock(block: string): TimelineCueReference[] {
  const lines = block
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
  const resourceLines = lines.filter((line) => line.includes("#xywh="));
  if (resourceLines.length === 0) return [];
  const timingLine = lines.find((line) => line.includes("-->"));
  const timing = timingLine && /^(\S+)\s+-->\s+(\S+)$/u.exec(timingLine);
  if (!timing) throw new Error("FR029 VTT xywh cue 缺少有效时间范围");
  const startSeconds = parsePreviewTimestamp(timing[1]!);
  const endSeconds = parsePreviewTimestamp(timing[2]!);
  if (endSeconds <= startSeconds) {
    throw new Error(`FR029 VTT cue 时间范围无效：${timingLine}`);
  }
  return resourceLines.map((line) =>
    parseTimelineCueResource(line, startSeconds, endSeconds),
  );
}

function parseTimelineCueResource(
  line: string,
  startSeconds: number,
  endSeconds: number,
): TimelineCueReference {
  const match =
    /^(sprite-\d{3}\.jpg)#xywh=([^,]+),([^,]+),([^,]+),([^,\s]+)$/u.exec(line);
  if (!match) throw new Error(`FR029 VTT xywh cue 格式无效：${line}`);
  const rawCoordinates = match.slice(2);
  if (rawCoordinates.some((value) => !/^-?\d+$/u.test(value))) {
    throw new Error(`FR029 VTT xywh 必须为整数：${line}`);
  }
  const [x, y, width, height] = rawCoordinates.map(Number) as [
    number,
    number,
    number,
    number,
  ];
  if (![x, y, width, height].every(Number.isSafeInteger)) {
    throw new Error(`FR029 VTT xywh 超出安全整数范围：${line}`);
  }
  if (x < 0 || y < 0 || width <= 0 || height <= 0) {
    throw new Error(`FR029 VTT xywh 坐标或尺寸无效：${line}`);
  }
  return { endSeconds, height, name: match[1]!, startSeconds, width, x, y };
}

function parsePreviewTimestamp(value: string): number {
  const match = /^(?:(\d{2,}):)?([0-5]\d):([0-5]\d)\.(\d{3})$/u.exec(value);
  if (!match) throw new Error(`FR029 VTT 时间戳无效：${value}`);
  return (
    Number(match[1] ?? 0) * 3_600 +
    Number(match[2]) * 60 +
    Number(match[3]) +
    Number(match[4]) / 1_000
  );
}

function validateTimelineCueBounds(
  reference: TimelineCueReference,
  spritesByName: ReadonlyMap<string, TimelineSpriteEvidence>,
): TimelineCueEvidence {
  const sprite = spritesByName.get(reference.name);
  if (!sprite) throw new Error(`FR029 cue 缺少 sprite 资源：${reference.name}`);
  if (
    reference.x + reference.width > sprite.width ||
    reference.y + reference.height > sprite.height
  ) {
    throw new Error(
      `FR029 cue 越过 sprite 边界：${reference.name}#xywh=${reference.x},${reference.y},${reference.width},${reference.height}，natural=${sprite.width}x${sprite.height}`,
    );
  }
  return {
    ...reference,
    spriteHeight: sprite.height,
    spriteURL: sprite.url,
    spriteWidth: sprite.width,
  };
}

function resolveRequiredHttpUrl(
  value: string | undefined,
  baseURL: string,
  label: string,
): string {
  const candidate = value?.trim();
  if (!candidate) throw new Error(`FR029 ${label} 缺少非空真实 URL`);
  const resolved = new URL(candidate, baseURL);
  if (resolved.protocol !== "http:" && resolved.protocol !== "https:") {
    throw new Error(`FR029 ${label} URL 协议无效：${resolved.protocol}`);
  }
  return resolved.href;
}

function readImageDimensions(
  page: Page,
  url: string,
): Promise<{ height: number; width: number }> {
  return page.evaluate(
    (spriteURL) =>
      new Promise<{ height: number; width: number }>((resolve, reject) => {
        const image = new Image();
        image.onload = () =>
          resolve({ height: image.naturalHeight, width: image.naturalWidth });
        image.onerror = () =>
          reject(new Error(`FR029 sprite 图片解码失败：${spriteURL}`));
        image.src = spriteURL;
      }),
    url,
  );
}

async function waitForTimelineSprite(progress: Locator): Promise<void> {
  await expect(async () => {
    await moveToFraction(progress, 0.2);
    await expect(progress.getByTestId("timeline-preview-sprite")).toBeVisible();
  }).toPass({ intervals: [1_000], timeout: 120_000 });
}

function requireTimelineCueAt(
  cues: readonly TimelineCueEvidence[],
  mediaTime: number,
): TimelineCueEvidence {
  const cue = cues.find(
    ({ endSeconds, startSeconds }) =>
      mediaTime >= startSeconds && mediaTime < endSeconds,
  );
  if (!cue) throw new Error(`FR029 未找到 ${mediaTime} 秒对应的已验证 cue`);
  return cue;
}

async function verifyRenderedTimelineSprite(
  progress: Locator,
  cue: TimelineCueEvidence,
) {
  const sprite = progress.getByTestId("timeline-preview-sprite");
  const image = sprite.locator("img");
  await expect(sprite).toBeVisible();
  await expect
    .poll(() =>
      image.evaluate((node) => ({
        height: (node as HTMLImageElement).naturalHeight,
        width: (node as HTMLImageElement).naturalWidth,
      })),
    )
    .toEqual({ height: cue.spriteHeight, width: cue.spriteWidth });
  const rendered = await image.evaluate((node) => {
    const imageNode = node as HTMLImageElement;
    const container = imageNode.parentElement;
    return {
      containerHeight: container?.clientHeight ?? 0,
      containerWidth: container?.clientWidth ?? 0,
      currentSrc: imageNode.currentSrc || imageNode.src,
      naturalHeight: imageNode.naturalHeight,
      naturalWidth: imageNode.naturalWidth,
      transform: imageNode.style.transform,
    };
  });
  const currentSrc = new URL(rendered.currentSrc, progress.page().url()).href;
  expect(currentSrc).toBe(cue.spriteURL);
  expect(rendered.containerWidth).toBe(cue.width);
  expect(rendered.containerHeight).toBe(cue.height);
  expect(rendered.transform).toBe(`translate(${-cue.x}px, ${-cue.y}px)`);
  return { ...rendered, currentSrc, cue };
}

async function moveToFraction(
  progress: Locator,
  fraction: number,
): Promise<void> {
  const box = await progress.boundingBox();
  if (!box) throw new Error("PWA 播放进度条不可见");
  await progress
    .page()
    .mouse.move(box.x + box.width * fraction, box.y + box.height / 2);
}

async function expectPreviewTime(
  overlay: Locator,
  expectedSeconds: number,
): Promise<void> {
  await expect
    .poll(async () =>
      Math.abs(
        parseDisplayedTime(await overlay.textContent()) - expectedSeconds,
      ),
    )
    .toBeLessThanOrEqual(1.5);
}

function parseDisplayedTime(value: string | null): number {
  const [minutes = "0", seconds = "0"] = (value ?? "").trim().split(":");
  return Number(minutes) * 60 + Number(seconds);
}

async function verifyFrameAndSeekInPwa(state: BusinessAcceptanceReadyState) {
  const pageErrors: string[] = [];
  const onPageError = (error: Error) => pageErrors.push(error.message);
  state.pwaPage.on("pageerror", onPageError);
  return runWithCleanup(
    async () => {
      const exact = await verifyExactFrameSteps(state);
      const tiers = await verifyTieredSeek(state);
      expect(
        pageErrors.filter((message) => /Network Error/i.test(message)),
      ).toEqual([]);
      await expect(state.pwaPage.getByText(/Network Error/i)).toHaveCount(0);
      return {
        automated: [
          "FR034 真实编号帧前后逐帧",
          "FR035 五档秒级阶梯定位",
          "FR036 核心无 Network Error",
        ],
        displayModeStandalone: await isStandalone(state.pwaPage),
        exact,
        pageErrors,
        tiers,
      };
    },
    [() => state.pwaPage.off("pageerror", onPageError)],
    "FR034/035/036 验证与页面错误监听清理均失败",
  );
}

async function verifyExactFrameSteps(state: BusinessAcceptanceReadyState) {
  const mediaID = requiredMediaID(state.fixture, EXACT_FILE);
  const page = await gotoPwaPlayback(state, mediaID);
  const { player, video } = await waitForPlayablePlayer(page, 9);
  await pausePlayer(player);
  await expect(player).toHaveAttribute("data-frame-presentation", "exact", {
    timeout: 30_000,
  });
  await setMediaTime(video, 4, 0.3);
  const startFrame = await readFrameMarker(video);
  expect(startFrame).toBeGreaterThanOrEqual(0);

  const nextGeneration = await clearFrameStepResult(player);
  await clickPlayerButton(player, "后一帧");
  const nextStep = await waitForExactFrameStep(
    player,
    nextGeneration,
    startFrame + 1,
  );
  const nextFrame = nextStep.marker;
  expect(nextFrame).toBe(startFrame + 1);
  expect(nextStep.confirmedFrame).toBe(nextFrame);

  const previousGeneration = await clearFrameStepResult(player);
  expect(previousGeneration).toBe(nextStep.requestId);
  await clickPlayerButton(player, "前一帧");
  const previousStep = await waitForExactFrameStep(
    player,
    previousGeneration,
    nextFrame - 1,
  );
  const restoredFrame = previousStep.marker;
  expect(restoredFrame).toBe(nextFrame - 1);
  expect(restoredFrame).toBe(startFrame);
  expect(previousStep.confirmedFrame).toBe(restoredFrame);

  return {
    framePresentation: "exact",
    mediaID,
    nextFrame,
    nextStep,
    previousStep,
    restoredFrame,
    startFrame,
  };
}

interface ExactFrameStepObservation {
  confirmedFrame: number;
  errorCode: "ok";
  marker: number;
  mode: "exact-verified";
  requestId: number;
  result: string;
  status: "completed";
}

interface ExactFrameStepSnapshot {
  marker: number;
  result: string | null;
}

async function clearFrameStepResult(player: Locator): Promise<number> {
  const result = await player.getAttribute("data-frame-step-result");
  const requestId = frameStepRequestIdBaseline(result);
  await player.evaluate((node) =>
    node.removeAttribute("data-frame-step-result"),
  );
  expect(await player.getAttribute("data-frame-step-result")).toBeNull();
  return requestId;
}

function frameStepRequestIdBaseline(result: string | null): number {
  if (result === "pending") return INITIAL_FRAME_STEP_REQUEST_ID;
  const parsed = parseExactFrameStepResult(result);
  if (!parsed) {
    throw new Error(`FR034 逐帧 requestId 基线无效：${result ?? "缺失"}`);
  }
  return parsed.requestId;
}

async function waitForExactFrameStep(
  player: Locator,
  previousRequestId: number,
  expectedFrame: number,
): Promise<ExactFrameStepObservation> {
  if (!Number.isSafeInteger(previousRequestId) || previousRequestId < 0) {
    throw new Error(`FR034 previous requestId 基线无效：${previousRequestId}`);
  }
  if (!Number.isSafeInteger(expectedFrame) || expectedFrame < 0) {
    throw new Error(`FR034 目标帧无效：${expectedFrame}`);
  }
  let observation: ExactFrameStepObservation | undefined;
  await expect
    .poll(
      async () => {
        const snapshot = await readExactFrameStepSnapshot(player);
        const parsed = parseExactFrameStepResult(snapshot.result);
        if (!parsed || parsed.requestId <= previousRequestId) return null;
        observation = { ...parsed, marker: snapshot.marker };
        return observation;
      },
      { timeout: 15_000 },
    )
    .toEqual({
      confirmedFrame: expectedFrame,
      errorCode: "ok",
      marker: expectedFrame,
      mode: "exact-verified",
      requestId: expect.any(Number),
      result: expect.stringMatching(/^\d+:completed:exact-verified:\d+:ok$/u),
      status: "completed",
    });
  if (!observation) throw new Error("FR034 未保存本次原子逐帧快照");
  expect(observation.requestId).toBeGreaterThan(previousRequestId);
  return observation;
}

function parseExactFrameStepResult(
  result: string | null,
): Omit<ExactFrameStepObservation, "marker"> | null {
  const value = result ?? "";
  const match = /^(\d+):(completed):(exact-verified):(\d+):(ok)$/u.exec(value);
  if (!match) return null;
  const requestId = parseNonNegativeSafeInteger(match[1]);
  const confirmedFrame = parseNonNegativeSafeInteger(match[4]);
  if (requestId === null || confirmedFrame === null) return null;
  return {
    confirmedFrame,
    errorCode: match[5] as "ok",
    mode: match[3] as "exact-verified",
    requestId,
    result: value,
    status: match[2] as "completed",
  };
}

function parseNonNegativeSafeInteger(value: string | undefined): number | null {
  if (!value || !/^\d+$/u.test(value)) return null;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed >= 0 ? parsed : null;
}

function readExactFrameStepSnapshot(
  player: Locator,
): Promise<ExactFrameStepSnapshot> {
  return player.evaluate((node, marker) => {
    const root = node as HTMLElement;
    const video = root.querySelector("video");
    if (!(video instanceof HTMLVideoElement)) {
      return {
        marker: -1,
        result: root.getAttribute("data-frame-step-result"),
      };
    }
    const width = (marker.bits + 2) * marker.cellSize;
    const canvas = document.createElement("canvas");
    canvas.width = width;
    canvas.height = marker.cellSize;
    const context = canvas.getContext("2d", { willReadFrequently: true });
    let frame = -1;
    if (context) {
      context.drawImage(
        video,
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
        const luma =
          ((pixels.data[offset] ?? 0) +
            (pixels.data[offset + 1] ?? 0) +
            (pixels.data[offset + 2] ?? 0)) /
          3;
        return luma >= 160;
      };
      if (white(0) && white(marker.bits + 1)) {
        frame = 0;
        for (let bit = 0; bit < marker.bits; bit += 1) {
          if (white(bit + 1)) frame += 2 ** bit;
        }
      }
    }
    return {
      marker: frame,
      result: root.getAttribute("data-frame-step-result"),
    };
  }, FRAME_MARKER);
}

async function verifyTieredSeek(state: BusinessAcceptanceReadyState) {
  const mediaID = requiredMediaID(state.fixture, PREVIEW_FILE);
  const page = await gotoPwaPlayback(state, mediaID);
  const { player, video } = await waitForPlayablePlayer(
    page,
    LONG_DURATION - 5,
  );
  await pausePlayer(player);
  await setMediaTime(video, 65, 0.8);
  const operations = [
    { direction: "next", seconds: 0.5 },
    { direction: "next", seconds: 1 },
    { direction: "previous", seconds: 5 },
    { direction: "next", seconds: 30 },
    { direction: "previous", seconds: 60 },
  ] as const;
  const observations: Array<{
    actual: number;
    expected: number;
    label: string;
  }> = [];
  let expectedTime = 65;
  for (const operation of operations) {
    const label = `${operation.seconds} 秒`;
    await selectSeekTier(page, player, label);
    expectedTime +=
      operation.direction === "next" ? operation.seconds : -operation.seconds;
    await clickPlayerButton(
      player,
      `${operation.direction === "next" ? "前进" : "后退"} ${label}`,
    );
    await expectMediaTime(video, expectedTime, 0.8);
    observations.push({
      actual: await currentTime(video),
      expected: expectedTime,
      label,
    });
  }
  return { mediaID, observations };
}

interface WatchStateEvidence {
  last_session_id?: string;
  position_seconds: number;
  revision: number;
}

async function verifyCrossProfileWatchState(
  state: BusinessAcceptanceReadyState,
) {
  const mediaID = requiredMediaID(state.fixture, WATCH_FILE);
  const watchPath = `/api/play/${mediaID}/watch-state`;
  const pwaPage = await gotoPwaPlayback(state, mediaID);
  await waitForPlayablePlayer(pwaPage, 55);
  const pwaRequests: Array<{ event_seq: number; session_id: string }> = [];
  const observePwaRequest = (request: {
    method(): string;
    postDataJSON(): unknown;
    url(): string;
  }) => {
    if (request.method() !== "PUT" || !request.url().endsWith(watchPath))
      return;
    pwaRequests.push(
      request.postDataJSON() as { event_seq: number; session_id: string },
    );
  };
  pwaPage.on("request", observePwaRequest);

  return runWithCleanup(
    async () => {
      await reportPosition(pwaPage, 20);
      await expect
        .poll(
          async () =>
            (await readWatchState(pwaPage.request, watchPath)).position_seconds,
        )
        .toBeGreaterThan(19);
      const stateAfterA = await readWatchState(pwaPage.request, watchPath);

      const pageB = await gotoBrowserPlayback(state, mediaID);
      await waitForPlayablePlayer(pageB, 55);
      await expect
        .poll(
          () =>
            pageB
              .locator("video")
              .evaluate((node) => (node as HTMLVideoElement).currentTime),
          {
            timeout: 15_000,
          },
        )
        .toBeGreaterThan(18);
      await reportPosition(pageB, 35);
      await expect
        .poll(
          async () =>
            (await readWatchState(pageB.request, watchPath)).position_seconds,
        )
        .toBeGreaterThan(34);
      const stateAfterB = await readWatchState(pageB.request, watchPath);

      const conflict = pwaPage.waitForResponse(
        (response) =>
          response.request().method() === "PUT" &&
          response.url().endsWith(watchPath) &&
          response.status() === 409,
      );
      await reportPosition(pwaPage, 25);
      const conflictResponse = await conflict;
      const conflictBody = (await conflictResponse.json()) as {
        code: string;
        current: WatchStateEvidence;
      };
      expect(conflictBody.code).toBe("WATCH_STATE_CONFLICT");
      expect(conflictBody.current.position_seconds).toBeGreaterThan(34);
      const finalState = await readWatchState(pwaPage.request, watchPath);
      expect(finalState.position_seconds).toBeGreaterThan(34);
      expect(finalState.revision).toBeGreaterThanOrEqual(stateAfterB.revision);
      expect(state.profile).not.toBe(state.browserProfileB);
      expect(
        new Set(pwaRequests.map((request) => request.session_id)).size,
      ).toBeGreaterThan(0);
      await expect(pwaPage.getByText(/Network Error/i)).toHaveCount(0);

      return {
        automated: [
          "PWA A 写入续播点",
          "独立 persistent profile B 续播",
          "PWA A 旧 revision 收到 409 且未覆盖",
        ],
        conflict: conflictBody,
        displayModeStandalone: await isStandalone(pwaPage),
        finalState,
        mediaID,
        profileDirectoriesDistinct: state.profile !== state.browserProfileB,
        pwaSessions: [
          ...new Set(pwaRequests.map((request) => request.session_id)),
        ],
        stateAfterA,
        stateAfterB,
      };
    },
    [() => pwaPage.off("request", observePwaRequest)],
    "FR045 验证与请求监听清理均失败",
  );
}

async function reportPosition(page: Page, position: number): Promise<void> {
  await page.locator("video").evaluate((node, target) => {
    const video = node as HTMLVideoElement;
    video.currentTime = target;
    video.dispatchEvent(new Event("timeupdate"));
    video.pause();
    video.dispatchEvent(new Event("pause"));
  }, position);
}

async function readWatchState(
  request: APIRequestContext,
  watchPath: string,
): Promise<WatchStateEvidence> {
  const response = await request.get(watchPath);
  expect(response.ok()).toBeTruthy();
  return (await response.json()) as WatchStateEvidence;
}

interface BookmarkEvidence {
  id: string;
  note: string | null;
  position_ms: number;
  revision: number;
  title: string;
}

async function verifyChaptersBookmarksAndRestart(
  state: BusinessAcceptanceReadyState,
) {
  const mediaID = requiredMediaID(state.fixture, CHAPTER_FILE);
  await waitForMetadataParse(state.pwaPage.request, mediaID);
  const chapters = await readChapters(state.pwaPage.request, mediaID);
  const pwaPage = await gotoPwaPlayback(state, mediaID);
  const { video } = await waitForPlayablePlayer(pwaPage, 14);
  await openMarkersPanel(pwaPage);
  await pwaPage.getByRole("button", { name: "跳转到章节 Main，0:05" }).click();
  await expectMediaTime(video, 5, 0.3);

  const created = await createBookmarkInUI(pwaPage, video, mediaID);
  await beginBookmarkDraft(
    pwaPage,
    created.title,
    "PWA A 旧 revision 草稿",
    "PWA A 本地备注",
  );
  const pageB = await gotoBrowserPlayback(state, mediaID);
  await waitForPlayablePlayer(pageB, 14);
  await openMarkersPanel(pageB);
  await updateBookmarkInUI(
    pageB,
    created.title,
    "Profile B 最新书签",
    "Profile B 最新备注",
  );
  const afterB = (await listBookmarks(pageB.request, mediaID))[0];
  expect(afterB).toMatchObject({ revision: 2, title: "Profile B 最新书签" });

  await pwaPage.getByRole("button", { name: "保存修改" }).click();
  await expect(
    pwaPage.getByText("书签已在其他设备更新", { exact: true }),
  ).toBeVisible();
  await expect(
    pwaPage.getByText("已重新加载服务端最新书签，未覆盖其他设备的修改", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(pwaPage.getByLabel("书签标题")).toHaveValue(
    "PWA A 旧 revision 草稿",
  );
  await expect(
    pwaPage.getByText("Profile B 最新书签", { exact: true }),
  ).toBeVisible();

  await restartInstalledPwa(state);
  const restartedPage = await gotoPwaPlayback(state, mediaID);
  await waitForPlayablePlayer(restartedPage, 14);
  await openMarkersPanel(restartedPage);
  await expect(
    restartedPage.getByRole("button", { name: "编辑书签 Profile B 最新书签" }),
  ).toBeVisible();
  const restored = (await listBookmarks(restartedPage.request, mediaID))[0];
  expect(restored).toMatchObject({ revision: 2, title: "Profile B 最新书签" });

  return {
    automated: [
      "真实内嵌章节跳转",
      "PWA A 创建书签",
      "profile B 抢先修改",
      "PWA A 冲突保留草稿",
      "PWA 重启恢复服务端最新 revision",
    ],
    chapters,
    created,
    displayModeStandalone: await isStandalone(restartedPage),
    mediaID,
    restored,
  };
}

async function waitForMetadataParse(
  request: APIRequestContext,
  mediaID: number,
): Promise<void> {
  await expect
    .poll(
      async () => {
        const response = await request.get("/api/tasks", {
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
          throw new Error(task.error || `元数据解析任务${task.status}`);
        }
        return task?.status ?? "missing";
      },
      { timeout: 30_000 },
    )
    .toBe("succeeded");
}

async function readChapters(request: APIRequestContext, mediaID: number) {
  const response = await request.get(`/api/library/media/${mediaID}/chapters`);
  expect(response.ok()).toBeTruthy();
  const chapters = (
    (await response.json()) as {
      items: Array<{
        end_ms: number;
        source: string;
        start_ms: number;
        title: string;
      }>;
    }
  ).items;
  expect(
    chapters.map(({ end_ms, start_ms, title }) => ({
      end_ms,
      start_ms,
      title,
    })),
  ).toEqual([
    { end_ms: 5_000, start_ms: 0, title: "开场" },
    { end_ms: 10_000, start_ms: 5_000, title: "Main" },
    { end_ms: 15_000, start_ms: 10_000, title: "结尾" },
  ]);
  return chapters;
}

async function openMarkersPanel(page: Page): Promise<void> {
  const addButton = page.getByRole("button", { name: "在当前时间新增书签" });
  if (!(await addButton.isVisible())) {
    await page.getByRole("button", { name: /^章节与书签/ }).click();
  }
  await expect(addButton).toBeVisible();
}

async function createBookmarkInUI(
  page: Page,
  video: Locator,
  mediaID: number,
): Promise<BookmarkEvidence> {
  await setMediaTime(video, 2, 0.2);
  await page.getByRole("button", { name: "在当前时间新增书签" }).click();
  await page.getByLabel("书签标题").fill("PWA A 原始书签");
  await page.getByLabel("书签备注").fill("PWA A 原始备注");
  await page.getByRole("button", { name: "保存书签" }).click();
  await expect
    .poll(async () => (await listBookmarks(page.request, mediaID)).length, {
      timeout: 15_000,
    })
    .toBe(1);
  const bookmark = (await listBookmarks(page.request, mediaID))[0];
  if (!bookmark) throw new Error("PWA A 未创建真实书签");
  expect(bookmark).toMatchObject({ revision: 1, title: "PWA A 原始书签" });
  return bookmark;
}

async function beginBookmarkDraft(
  page: Page,
  currentTitle: string,
  nextTitle: string,
  nextNote: string,
): Promise<void> {
  await page.getByRole("button", { name: `编辑书签 ${currentTitle}` }).click();
  await page.getByLabel("书签标题").fill(nextTitle);
  await page.getByLabel("书签备注").fill(nextNote);
}

async function updateBookmarkInUI(
  page: Page,
  currentTitle: string,
  nextTitle: string,
  nextNote: string,
): Promise<void> {
  await beginBookmarkDraft(page, currentTitle, nextTitle, nextNote);
  await page.getByRole("button", { name: "保存修改" }).click();
  await expect(
    page.getByRole("button", { name: `编辑书签 ${nextTitle}` }),
  ).toBeVisible({
    timeout: 15_000,
  });
}

async function listBookmarks(
  request: APIRequestContext,
  mediaID: number,
): Promise<BookmarkEvidence[]> {
  const response = await request.get(`/api/library/media/${mediaID}/bookmarks`);
  expect(response.ok()).toBeTruthy();
  return ((await response.json()) as { items: BookmarkEvidence[] }).items;
}

interface TrackEvidence {
  available: boolean;
  capability: string;
  codec?: string;
  id: string;
  kind: "audio" | "subtitle";
  language?: string;
  source: string;
}

interface TrackManifestEvidence {
  tracks: TrackEvidence[];
}

interface ManualBlockingEvidence {
  reason: string;
  requirement: "FR044" | "FR057" | "FR058";
  status: "manual-blocking";
}

async function verifyReachablePlatformCapabilities(
  state: BusinessAcceptanceReadyState,
  testInfo: TestInfo,
) {
  const tracks = await verifyRealSubtitleAudioPath(state);
  const hls = await verifyRealHlsPreflight(state);
  const runtime = await verifyRateLoopAndPlatform(state);
  const manualGates: ManualBlockingEvidence[] = [
    {
      reason:
        "双音轨请求、HLS 重载与选择状态已自动验证，440Hz/880Hz 的可听差异仍需人工听辨",
      requirement: "FR044",
      status: "manual-blocking",
    },
    {
      reason:
        "MediaSession 的系统媒体键、锁屏卡片与后台音频必须人工操作 Windows 系统级控件",
      requirement: "FR058",
      status: "manual-blocking",
    },
  ];
  if (!runtime.hlsUiSelected) {
    manualGates.push({
      reason:
        "已生成并读取真实 ABR HLS，但产品 UI 仍选择原文件直连；清晰度与省流量验收不得用 page.route 伪造",
      requirement: "FR057",
      status: "manual-blocking",
    });
  }
  if (!runtime.platform.pictureInPictureAutomated) {
    manualGates.push({
      reason: "当前 Google Chrome 未暴露可自动操作的真实画中画能力",
      requirement: "FR058",
      status: "manual-blocking",
    });
  }
  for (const gate of manualGates) {
    testInfo.annotations.push({
      description: `${gate.requirement}：${gate.reason}`,
      type: gate.status,
    });
  }
  return {
    automated: [
      "FR044 内嵌字幕真实渲染",
      "FR044 双音轨真实 audio-reload HLS 请求与选择状态（不含听辨）",
      "FR057 真实 ABR HLS 产物预检",
      "FR057 倍速与 A-B 循环",
      "FR058 原生能力状态与可达时的真实 PiP 进入/退出",
    ],
    displayModeStandalone: await isStandalone(state.pwaPage),
    hls,
    manualGates,
    networkInterception: "未使用 page.route、route.fulfill 或 mock",
    runtime,
    tracks,
  };
}

async function verifyRealSubtitleAudioPath(
  state: BusinessAcceptanceReadyState,
) {
  const mediaID = requiredMediaID(state.fixture, TRACK_FILE);
  const manifest = await loadTrackManifest(state.pwaPage.request, mediaID);
  const page = await gotoPwaPlayback(state, mediaID);
  const { video } = await waitForPlayablePlayer(page, 10);
  await chooseSubtitle(page, /zho.*mov_text/u);
  await setMediaTime(video, 2, 0.3);
  await expect(page.getByTestId("subtitle-overlay")).toContainText(
    TRACK_SUBTITLE_TEXT,
  );

  const reloadResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      response.url().endsWith(`/api/play/${mediaID}/audio-reload`),
    { timeout: 120_000 },
  );
  const hlsRequest = page.waitForRequest(
    (request) =>
      /\/api\/play\/hls\/\d+\/profiles\/audio-h264-aac-[a-f0-9]{24}\/tasks\/\d+\/master\.m3u8$/u.test(
        new URL(request.url()).pathname,
      ),
    { timeout: 120_000 },
  );
  await chooseAudio(page, /jpn.*aac.*1 声道/u);
  const reload = (await (await reloadResponse).json()) as {
    profile_id: string;
    requested_track_id: string;
    space_id: string;
    task_id: string;
    url: string;
  };
  const manifestRequest = await hlsRequest;
  const headers = await manifestRequest.allHeaders();
  expect(headers["x-jianvideo-space-id"]).toBe(reload.space_id);
  await expectAudioSelection(page, /jpn.*aac.*1 声道/u);
  await expect(page.getByText(/Network Error/i)).toHaveCount(0);
  return {
    audioCount: manifest.tracks.filter((track) => track.kind === "audio")
      .length,
    displayModeStandalone: await isStandalone(page),
    hlsManifestURL: manifestRequest.url(),
    mediaID,
    reload,
    subtitleCount: manifest.tracks.filter((track) => track.kind === "subtitle")
      .length,
    subtitleText: await page.getByTestId("subtitle-overlay").textContent(),
  };
}

async function loadTrackManifest(
  request: APIRequestContext,
  mediaID: number,
): Promise<TrackManifestEvidence> {
  let manifest = await getTrackManifest(request, mediaID);
  if (!hasRequiredTracks(manifest)) {
    const refresh = await request.post(
      `/api/library/media/${mediaID}/metadata/refresh`,
    );
    expect(refresh.status()).toBe(202);
    await waitUnifiedTask(
      request,
      ((await refresh.json()) as { task_id: number }).task_id,
    );
    await expect
      .poll(async () => {
        manifest = await getTrackManifest(request, mediaID);
        return hasRequiredTracks(manifest);
      })
      .toBe(true);
  }
  expect(
    manifest.tracks.filter((track) => track.kind === "audio"),
  ).toHaveLength(2);
  expect(
    manifest.tracks.filter(
      (track) => track.kind === "subtitle" && track.source === "embedded",
    ),
  ).toHaveLength(1);
  return manifest;
}

async function getTrackManifest(
  request: APIRequestContext,
  mediaID: number,
): Promise<TrackManifestEvidence> {
  const response = await request.get(`/api/play/${mediaID}/tracks`);
  expect(response.ok()).toBeTruthy();
  return (await response.json()) as TrackManifestEvidence;
}

function hasRequiredTracks(manifest: TrackManifestEvidence): boolean {
  return (
    manifest.tracks.filter((track) => track.kind === "audio").length === 2 &&
    manifest.tracks.filter(
      (track) => track.kind === "subtitle" && track.source === "embedded",
    ).length === 1
  );
}

async function verifyRealHlsPreflight(state: BusinessAcceptanceReadyState) {
  const mediaID = requiredMediaID(state.fixture, PREVIEW_FILE);
  const masterURL = await createHlsAbr(state.pwaPage.request, mediaID);
  const masterResponse = await state.pwaPage.request.get(masterURL);
  if (!masterResponse.ok()) {
    throw new Error(
      `FR057 真实 HLS preflight 阻塞：master HTTP ${masterResponse.status()}`,
    );
  }
  const master = await masterResponse.text();
  const variants = [...master.matchAll(/^([^#\r\n]+\/index\.m3u8)$/gmu)].map(
    (match) => match[1]!,
  );
  if (variants.length === 0) {
    throw new Error("FR057 真实 HLS preflight 阻塞：master 未包含真实变体");
  }
  const variantURL = new URL(variants[0]!, new URL(masterURL, BASE_URL)).href;
  const variantResponse = await state.pwaPage.request.get(variantURL);
  expect(variantResponse.ok()).toBeTruthy();
  const variant = await variantResponse.text();
  const segment = variant.split(/\r?\n/u).find((line) => line.endsWith(".ts"));
  if (!segment)
    throw new Error("FR057 真实 HLS preflight 阻塞：变体未包含 TS 分片");
  const segmentResponse = await state.pwaPage.request.get(
    new URL(segment, variantURL).href,
  );
  expect(segmentResponse.ok()).toBeTruthy();
  expect((await segmentResponse.body()).byteLength).toBeGreaterThan(0);
  return { masterURL, mediaID, segment, variants };
}

async function verifyRateLoopAndPlatform(state: BusinessAcceptanceReadyState) {
  const mediaID = requiredMediaID(state.fixture, PREVIEW_FILE);
  const hlsRequests: string[] = [];
  const onRequest = (request: { url(): string }) => {
    if (request.url().includes(`/api/play/hls/${mediaID}/`))
      hlsRequests.push(request.url());
  };
  state.pwaPage.on("request", onRequest);
  return runWithCleanup(
    async () => {
      const page = await gotoPwaPlayback(state, mediaID);
      const { player, video } = await waitForPlayablePlayer(
        page,
        LONG_DURATION - 5,
      );
      await pausePlayer(player);
      await selectPlaybackRate(page, player, 1.5);
      const loop = await verifyAbLoop(page, player, video);
      const platform = await verifyRealPlatformState(page, player);
      const hlsUiSelected = hlsRequests.length > 0;
      if (hlsUiSelected) {
        await expect(
          page.getByRole("button", { name: "清晰度" }),
        ).toBeVisible();
      }
      await expect(page.getByText(/Network Error/i)).toHaveCount(0);
      return {
        displayModeStandalone: await isStandalone(page),
        hlsRequests,
        hlsUiSelected,
        loop,
        mediaID,
        platform,
        playbackRate: await video.evaluate(
          (node) => (node as HTMLVideoElement).playbackRate,
        ),
      };
    },
    [() => state.pwaPage.off("request", onRequest)],
    "FR057/058 验证与 HLS 请求监听清理均失败",
  );
}

async function verifyAbLoop(page: Page, player: Locator, video: Locator) {
  const pointA = 6;
  const pointB = 7.5;
  return runWithCleanup(
    async () => {
      await setMediaTime(video, pointA, 0.3);
      await setAbPoint(page, player, "设置 A 点");
      await setMediaTime(video, pointB, 0.3);
      await setAbPoint(page, player, "设置 B 点");
      await expect(player.getByText(/^A \d/u)).toHaveText(
        /A 0:06\.\d{3} · B 0:07\.\d{3}/u,
      );
      await setMediaTime(video, pointA, 0.3);
      await armLoopObserver(video, pointA, pointB);
      await playPlayer(player);
      await expect
        .poll(async () => (await readLoopTransitions(video)).length, {
          timeout: 8_000,
        })
        .toBeGreaterThanOrEqual(1);
      const transitions = await readLoopTransitions(video);
      const transition = transitions[0]!;
      expect(transition.from).toBeGreaterThanOrEqual(pointB - 0.2);
      expect(transition.to).toBeLessThanOrEqual(pointA + 0.2);
      expect(transition.bObservedAt).toBeLessThan(transition.aObservedAt);
      expect(transition.playingAtB).toBe(true);
      expect(transition.playingAtA).toBe(true);
      await pausePlayer(player);
      return { pointA, pointB, transitions };
    },
    [
      () => removeLoopObserver(video),
      () => setAbPoint(page, player, "清除 A-B"),
    ],
    "A-B 循环验证与观察器、循环点清理均失败",
  );
}

async function verifyRealPlatformState(page: Page, player: Locator) {
  const native = await page.evaluate(() => ({
    mediaSession: typeof navigator.mediaSession !== "undefined",
    pictureInPicture:
      document.pictureInPictureEnabled === true &&
      typeof document.querySelector("video")?.requestPictureInPicture ===
        "function",
  }));
  await expect(player).toHaveAttribute(
    "data-media-session",
    native.mediaSession ? "available" : "unavailable",
  );
  let pictureInPictureAutomated = false;
  if (native.pictureInPicture) {
    await player.hover();
    await page.getByRole("button", { name: "画中画" }).click();
    await expect
      .poll(() =>
        page.evaluate(
          () =>
            document.pictureInPictureElement ===
            document.querySelector("video"),
        ),
      )
      .toBe(true);
    await page.getByRole("button", { name: "退出画中画" }).click();
    await expect
      .poll(() =>
        page.evaluate(() => document.pictureInPictureElement === null),
      )
      .toBe(true);
    pictureInPictureAutomated = true;
  }
  const mediaSession = await page.evaluate(() => ({
    metadataTitle: navigator.mediaSession?.metadata?.title ?? null,
    playbackState: navigator.mediaSession?.playbackState ?? "unavailable",
  }));
  return { ...native, ...mediaSession, pictureInPictureAutomated };
}

async function waitUnifiedTask(
  request: APIRequestContext,
  taskID: number,
): Promise<void> {
  await expect
    .poll(
      async () => {
        const response = await request.get(`/api/tasks/${taskID}`);
        expect(response.ok()).toBeTruthy();
        const task = (await response.json()) as {
          error?: string;
          status: string;
        };
        if (task.status === "failed" || task.status === "canceled") {
          throw new Error(task.error || `任务 ${taskID} ${task.status}`);
        }
        return task.status;
      },
      { timeout: 180_000 },
    )
    .toBe("succeeded");
}

async function waitForPlayablePlayer(
  page: Page,
  minimumDuration: number,
): Promise<{ player: Locator; video: Locator }> {
  const player = page.getByTestId("video-player-root");
  const video = player.locator("video");
  await expect(player).toBeVisible({ timeout: 30_000 });
  await expect
    .poll(() => video.evaluate((node) => (node as HTMLVideoElement).duration), {
      timeout: 30_000,
    })
    .toBeGreaterThan(minimumDuration);
  await expect
    .poll(
      () =>
        video.evaluate(
          (node) => (node as HTMLVideoElement).seekable.length > 0,
        ),
      {
        timeout: 30_000,
      },
    )
    .toBe(true);
  return { player, video };
}

async function pausePlayer(player: Locator): Promise<void> {
  const video = player.locator("video");
  if (!(await video.evaluate((node) => (node as HTMLVideoElement).paused))) {
    await player.hover();
    await player
      .getByRole("button", { name: "暂停", exact: true })
      .evaluate((button: HTMLButtonElement) => button.click());
  }
  await expect
    .poll(() => video.evaluate((node) => (node as HTMLVideoElement).paused))
    .toBe(true);
  await expect
    .poll(() => video.evaluate((node) => !(node as HTMLVideoElement).seeking))
    .toBe(true);
}

async function playPlayer(player: Locator): Promise<void> {
  const video = player.locator("video");
  if (await video.evaluate((node) => (node as HTMLVideoElement).paused)) {
    await player.hover();
    await player
      .getByRole("button", { name: "播放", exact: true })
      .evaluate((button: HTMLButtonElement) => button.click());
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
  tolerance: number,
): Promise<void> {
  await video.evaluate((node, value) => {
    (node as HTMLVideoElement).currentTime = value;
  }, target);
  await expect
    .poll(() => video.evaluate((node) => !(node as HTMLVideoElement).seeking), {
      timeout: 15_000,
    })
    .toBe(true);
  await expectMediaTime(video, target, tolerance);
}

async function expectMediaTime(
  video: Locator,
  expected: number,
  tolerance: number,
): Promise<void> {
  await expect
    .poll(() => currentTime(video), { timeout: 15_000 })
    .toBeGreaterThanOrEqual(Math.max(0, expected - tolerance));
  expect(await currentTime(video)).toBeLessThanOrEqual(
    expected + tolerance + 0.05,
  );
}

function currentTime(video: Locator): Promise<number> {
  return video.evaluate((node) => (node as HTMLVideoElement).currentTime);
}

async function clickPlayerButton(player: Locator, name: string): Promise<void> {
  await player.hover();
  await player
    .getByRole("button", { name, exact: true })
    .evaluate((button: HTMLButtonElement) => button.click());
}

async function selectSeekTier(
  page: Page,
  player: Locator,
  label: string,
): Promise<void> {
  if (
    await player.getByRole("button", { name: `定位档位：${label}` }).isVisible()
  )
    return;
  await player.hover();
  await player
    .getByRole("button", { name: /^定位档位：/u })
    .evaluate((button: HTMLButtonElement) => button.click());
  await page.getByRole("menuitem", { name: label, exact: true }).click();
  await expect(
    player.getByRole("button", { name: `定位档位：${label}` }),
  ).toBeVisible();
}

async function selectPlaybackRate(
  page: Page,
  player: Locator,
  rate: number,
): Promise<void> {
  await player.hover();
  await player
    .getByRole("button", { name: "播放速度" })
    .evaluate((button: HTMLButtonElement) => button.click());
  await page.getByRole("menuitem", { name: `${rate}×`, exact: true }).click();
  await expect(player.getByRole("button", { name: "播放速度" })).toContainText(
    `${rate}×`,
  );
}

async function readFrameMarker(video: Locator): Promise<number> {
  return video.evaluate((node, marker) => {
    const width = (marker.bits + 2) * marker.cellSize;
    const canvas = document.createElement("canvas");
    canvas.width = width;
    canvas.height = marker.cellSize;
    const context = canvas.getContext("2d", { willReadFrequently: true });
    if (!context) return -1;
    context.drawImage(
      node as HTMLVideoElement,
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
      const luma =
        ((pixels.data[offset] ?? 0) +
          (pixels.data[offset + 1] ?? 0) +
          (pixels.data[offset + 2] ?? 0)) /
        3;
      return luma >= 160;
    };
    if (!white(0) || !white(marker.bits + 1)) return -1;
    let frame = 0;
    for (let bit = 0; bit < marker.bits; bit += 1) {
      if (white(bit + 1)) frame += 2 ** bit;
    }
    return frame;
  }, FRAME_MARKER);
}

async function chooseSubtitle(page: Page, name: RegExp): Promise<void> {
  await page.getByRole("button", { name: "字幕轨道" }).click();
  const radio = page.getByRole("menuitemradio", { name });
  const item = radio.or(page.getByRole("menuitem", { name })).first();
  await expect(item).toBeVisible();
  const content = page.waitForResponse(
    (response) =>
      response.request().method() === "GET" &&
      /\/api\/play\/\d+\/subtitles\/[^/]+\/content$/u.test(
        new URL(response.url()).pathname,
      ),
  );
  await item.dispatchEvent("click");
  expect((await content).status()).toBe(200);
}

async function chooseAudio(page: Page, name: RegExp): Promise<void> {
  await page.getByRole("button", { name: "音轨" }).click();
  const item = page.getByRole("menuitem", { name });
  await expect(item).toBeVisible();
  await item.dispatchEvent("click");
}

async function expectAudioSelection(
  page: Page,
  effective: RegExp,
): Promise<void> {
  await page.getByRole("button", { name: "音轨" }).click();
  const item = page.getByRole("menuitem", { name: effective });
  await expect(item).toContainText("当前播放", { timeout: 120_000 });
  await expect(item).not.toContainText("切换中");
  await page.keyboard.press("Escape");
}

async function setAbPoint(
  page: Page,
  player: Locator,
  label: "设置 A 点" | "设置 B 点" | "清除 A-B",
): Promise<void> {
  await player.hover();
  await player
    .getByRole("button", { name: "A-B 循环" })
    .evaluate((button: HTMLButtonElement) => button.click());
  await page.getByRole("menuitem", { name: label, exact: true }).click();
}

interface LoopTransitionEvidence {
  aObservedAt: number;
  bObservedAt: number;
  from: number;
  playingAtA: boolean;
  playingAtB: boolean;
  to: number;
}

async function armLoopObserver(
  video: Locator,
  pointA: number,
  pointB: number,
): Promise<void> {
  await video.evaluate(
    (node, points) => {
      type ObservedVideo = HTMLVideoElement & {
        __p3LoopObserver?: {
          cleanup: () => void;
          highWater: number;
          reachedBAt: number | null;
          reachedBPlaying: boolean;
          transitions: LoopTransitionEvidence[];
        };
      };
      const media = node as ObservedVideo;
      const observer = {
        cleanup: () => undefined,
        highWater: media.currentTime,
        reachedBAt: null as number | null,
        reachedBPlaying: false,
        transitions: [] as LoopTransitionEvidence[],
      };
      const record = () => {
        const current = media.currentTime;
        const observedAt = performance.now();
        observer.highWater = Math.max(observer.highWater, current);
        if (current >= points.pointB - 0.2 && observer.reachedBAt === null) {
          observer.reachedBAt = observedAt;
          observer.reachedBPlaying = !media.paused;
        }
        if (
          observer.reachedBAt !== null &&
          observer.highWater >= points.pointB - 0.2 &&
          current <= points.pointA + 0.2
        ) {
          observer.transitions.push({
            aObservedAt: observedAt,
            bObservedAt: observer.reachedBAt,
            from: observer.highWater,
            playingAtA: !media.paused,
            playingAtB: observer.reachedBPlaying,
            to: current,
          });
          observer.highWater = current;
          observer.reachedBAt = null;
          observer.reachedBPlaying = false;
        }
      };
      for (const event of ["seeking", "seeked", "timeupdate"]) {
        media.addEventListener(event, record, true);
      }
      observer.cleanup = () => {
        for (const event of ["seeking", "seeked", "timeupdate"]) {
          media.removeEventListener(event, record, true);
        }
      };
      media.__p3LoopObserver = observer;
    },
    { pointA, pointB },
  );
}

function readLoopTransitions(
  video: Locator,
): Promise<LoopTransitionEvidence[]> {
  return video.evaluate(
    (node) =>
      (
        node as HTMLVideoElement & {
          __p3LoopObserver?: { transitions: LoopTransitionEvidence[] };
        }
      ).__p3LoopObserver?.transitions ?? [],
  );
}

async function removeLoopObserver(video: Locator): Promise<void> {
  await video.evaluate((node) => {
    const media = node as HTMLVideoElement & {
      __p3LoopObserver?: { cleanup: () => void };
    };
    media.__p3LoopObserver?.cleanup();
    delete media.__p3LoopObserver;
  });
}

function isStandalone(page: Page): Promise<boolean> {
  return page.evaluate(() => matchMedia("(display-mode: standalone)").matches);
}
