import {
  expect,
  test,
  type APIRequestContext,
  type Locator,
  type Page,
} from "@playwright/test";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import {
  mkdirSync,
  readFileSync,
  readdirSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, join } from "node:path";
import { login } from "./helpers";
import { writeSubtitleAudioVideoFixture } from "./media-playback-fixtures";

test.use({ serviceWorkers: "block" });
test.describe.configure({ mode: "serial" });

const VIDEO_BASENAME = "fr2-044-subtitle-audio-tracks";
const SINGLE_AUDIO_BASENAME = "fr2-044-single-audio";
const IMAGE_SUBTITLE_BASENAME = "fr2-044-image-subtitle";
const IMAGE_SUBTITLE_CODEC = "hdmv_pgs_subtitle";
const CUE_TIME = 2;

function requireMediaTools(): void {
  try {
    execFileSync("ffmpeg", ["-version"], { stdio: "ignore" });
    execFileSync("ffprobe", ["-version"], { stdio: "ignore" });
  } catch {
    throw new Error("FR2-044 真实验收缺少 ffmpeg 或 ffprobe");
  }
}

async function removeFixtureRoot(path: string): Promise<void> {
  for (let attempt = 0; attempt < 20; attempt += 1) {
    try {
      rmSync(path, { recursive: true, force: true });
      return;
    } catch (error) {
      if (attempt === 19) throw error;
      await new Promise((resolve) => setTimeout(resolve, 250));
    }
  }
}

const subtitleTexts = {
  embedded: "内嵌字幕可见",
  srt: "外挂 SRT 可见",
  ass: "外挂 ASS 可见",
  ssa: "外挂 SSA 可见",
  vtt: "外挂 VTT 可见",
} as const;

const uploadTexts = {
  srt: "上传 SRT 可见",
  ass: "上传 ASS 可见",
  ssa: "上传 SSA 可见",
  vtt: "上传 VTT 可见",
} as const;

test("真实后端枚举字幕音轨并完成 API 与播放器主路径", async ({ page }) => {
  test.setTimeout(180_000);
  requireMediaTools();
  const fixture = createFixture();
  let libraryID = 0;
  let settingsSnapshot: TranscodingSettingsSnapshot | undefined;
  let primaryFailure: { error: unknown } | undefined;

  try {
    await login(page);
    settingsSnapshot = await configureSoftwareTranscoding(page.request);
    libraryID = await createLibrary(page.request, fixture.mediaDir);
    const media = await scanMedia(page.request, libraryID);
    const initial = await loadCompleteManifest(
      page.request,
      media.multiAudioID,
      1,
    );
    await verifyInitialManifest(page.request, media.multiAudioID, initial);
    const imageSubtitleManifest = await loadCompleteManifest(
      page.request,
      media.imageSubtitleID,
      2,
    );
    await verifyImageSubtitleManifest(
      page.request,
      media.imageSubtitleID,
      imageSubtitleManifest,
    );
    await verifySingleAudioUnsupported(page.request, media.singleAudioID);
    const uploaded = await verifyApiUploads(
      page.request,
      media.multiAudioID,
      fixture,
    );
    expectSourcesUnchanged(fixture);
    await verifyDeleteAuditAndRemainingTracks(
      page.request,
      media.multiAudioID,
      uploaded,
    );
    await verifyPlayerUI(page, media.multiAudioID, fixture);
    await verifyImageSubtitleUI(page, media.imageSubtitleID);
    await verifySingleAudioUI(page, media.singleAudioID);
    expectSourcesUnchanged(fixture);
  } catch (error) {
    primaryFailure = { error };
  } finally {
    const cleanupFailures: unknown[] = [];
    if (settingsSnapshot) {
      try {
        await restoreTranscodingSettings(page.request, settingsSnapshot);
      } catch (error) {
        cleanupFailures.push(error);
      }
    }
    if (libraryID) {
      try {
        await deleteLibrary(page.request, libraryID);
      } catch (error) {
        cleanupFailures.push(error);
      }
    }
    try {
      await removeFixtureRoot(fixture.root);
    } catch (error) {
      cleanupFailures.push(error);
    }
    if (primaryFailure || cleanupFailures.length > 0) {
      throw new AggregateError(
        primaryFailure
          ? [primaryFailure.error, ...cleanupFailures]
          : cleanupFailures,
        primaryFailure ? "FR2-044 验证或清理失败" : "FR2-044 清理失败",
      );
    }
  }
});

type SubtitleFormat = "srt" | "ass" | "ssa" | "vtt";

interface Fixture {
  root: string;
  mediaDir: string;
  mediaPath: string;
  imageSubtitlePath: string;
  singleAudioPath: string;
  sourcePaths: string[];
  sourceFiles: string[];
  sourceSnapshot: Record<string, FileSnapshot>;
  sidecars: Record<SubtitleFormat, string>;
  uploads: Record<SubtitleFormat, string>;
  uiUpload: string;
}

interface FileSnapshot {
  hash: string;
  mtimeMs: number;
}

interface AudioReloadResponse {
  task_id: string;
  profile_id: string;
  requested_track_id: string;
  space_id: string;
  url: string;
}

interface HLSStatusResponse {
  available: boolean;
  effective_track_id?: string;
  profile_id: string;
  task: { id: string; status: string; error?: string | null } | null;
  url: string;
}

interface Track {
  id: string;
  kind: "audio" | "subtitle";
  label: string;
  source: "embedded" | "sidecar" | "uploaded";
  format?: string;
  codec?: string;
  language?: string;
  title?: string;
  channels?: number;
  channel_layout?: string;
  is_default: boolean;
  available: boolean;
  capability: string;
  unsupported_reason?: string;
  stream_index?: number;
}

interface TrackCapability {
  available: boolean;
  capability: string;
  unsupported_reason?: string;
}

interface TrackManifest {
  tracks: Track[];
  backend: Record<"audio" | "subtitle", TrackCapability>;
  selection: {
    audio: {
      selected_track_id: string | null;
      effective_track_id: string | null;
    };
    subtitle: {
      selected_track_id: string | null;
      effective_track_id: string | null;
    };
  };
}

function createFixture(): Fixture {
  const root = mkdtempSync(join(tmpdir(), "jianvideo-fr2-044-"));
  const mediaDir = join(root, "media");
  const uploadDir = join(root, "uploads");
  mkdirSync(mediaDir);
  mkdirSync(uploadDir);
  const embeddedInput = join(uploadDir, "embedded-input.srt");
  writeFileSync(embeddedInput, srtContent(subtitleTexts.embedded));
  const mediaPath = join(mediaDir, `${VIDEO_BASENAME}.mp4`);
  writeSubtitleAudioVideoFixture(mediaPath, embeddedInput);
  verifyPlayableFixtureStreams(mediaPath);
  const imageInput = join(uploadDir, "image-subtitle.sup");
  writeMinimalPGSFixture(imageInput);
  const imageSubtitlePath = join(mediaDir, `${IMAGE_SUBTITLE_BASENAME}.mkv`);
  writeImageSubtitleFixture(
    imageSubtitlePath,
    mediaPath,
    embeddedInput,
    imageInput,
  );
  verifyImageSubtitleFixtureStreams(imageSubtitlePath);
  const singleAudioPath = join(mediaDir, `${SINGLE_AUDIO_BASENAME}.mp4`);
  writeSingleAudioFixture(singleAudioPath, mediaPath);
  verifySingleAudioStreams(singleAudioPath);
  const sidecars = writeSubtitleSet(mediaDir, VIDEO_BASENAME, subtitleTexts);
  const uploads = writeSubtitleSet(uploadDir, "api-upload", uploadTexts);
  const uiUpload = join(uploadDir, "ui-unique-track.vtt");
  writeFileSync(uiUpload, vttContent("UI 独特字幕可见"));
  const sourcePaths = [
    mediaPath,
    imageSubtitlePath,
    singleAudioPath,
    ...Object.values(sidecars),
  ];
  return {
    root,
    mediaDir,
    mediaPath,
    imageSubtitlePath,
    singleAudioPath,
    sourcePaths,
    sourceFiles: readdirSync(mediaDir).sort(),
    sourceSnapshot: snapshotFiles(sourcePaths),
    sidecars,
    uploads,
    uiUpload,
  };
}

function writeSingleAudioFixture(outputPath: string, sourcePath: string): void {
  execFileSync(
    "ffmpeg",
    [
      "-y",
      "-i",
      sourcePath,
      "-map",
      "0:v:0",
      "-map",
      "0:a:0",
      "-c",
      "copy",
      "-movflags",
      "+faststart",
      outputPath,
    ],
    { stdio: "ignore" },
  );
}

function writeMinimalPGSFixture(outputPath: string): void {
  writeFileSync(
    outputPath,
    Buffer.from([0x50, 0x47, 0, 0, 0, 0, 0, 0, 0, 0, 0x80, 0, 0]),
  );
}

function writeImageSubtitleFixture(
  outputPath: string,
  sourcePath: string,
  textSubtitlePath: string,
  imageSubtitlePath: string,
): void {
  execFileSync(
    "ffmpeg",
    [
      "-y",
      "-i",
      sourcePath,
      "-i",
      textSubtitlePath,
      "-f",
      "sup",
      "-i",
      imageSubtitlePath,
      "-map",
      "0:v:0",
      "-map",
      "0:a:0",
      "-map",
      "0:a:1",
      "-map",
      "1:0",
      "-map",
      "2:0",
      "-c:v",
      "copy",
      "-c:a",
      "copy",
      "-c:s:0",
      "srt",
      "-c:s:1",
      "copy",
      "-metadata:s:a:0",
      "language=eng",
      "-metadata:s:a:0",
      "title=四百四十赫兹音轨",
      "-disposition:a:0",
      "default",
      "-metadata:s:a:1",
      "language=jpn",
      "-metadata:s:a:1",
      "title=八百八十赫兹音轨",
      "-disposition:a:1",
      "0",
      "-metadata:s:s:0",
      "language=zho",
      "-metadata:s:s:0",
      "title=内嵌中文字幕",
      "-disposition:s:0",
      "default",
      "-metadata:s:s:1",
      "language=zho",
      "-metadata:s:s:1",
      "title=内嵌图片字幕",
      "-disposition:s:1",
      "0",
      "-t",
      "12",
      outputPath,
    ],
    { stdio: "ignore" },
  );
}

interface ProbeStream {
  codec_name: string;
  codec_type: "video" | "audio" | "subtitle";
  pix_fmt?: string;
  channels?: number;
  channel_layout?: string;
  nb_read_packets?: string;
  tags?: { language?: string; handler_name?: string; title?: string };
  disposition: { default: number };
}

function verifyPlayableFixtureStreams(path: string): void {
  const streams = probeStreams(path);
  verifyFixtureVideoStream(streams);
  verifyFixtureAudioStreams(
    streams.filter((stream) => stream.codec_type === "audio"),
  );
  expect(
    streams.find((stream) => stream.codec_type === "subtitle"),
  ).toMatchObject({
    codec_name: "mov_text",
    tags: { language: "zho", handler_name: "内嵌中文字幕" },
    disposition: { default: 1 },
  });
}

function verifyImageSubtitleFixtureStreams(path: string): void {
  const streams = probeStreams(path);
  verifyFixtureVideoStream(streams);
  verifyFixtureAudioStreams(
    streams.filter((stream) => stream.codec_type === "audio"),
  );
  verifyFixtureSubtitleStreams(
    streams.filter((stream) => stream.codec_type === "subtitle"),
  );
}

function verifyFixtureVideoStream(streams: ProbeStream[]): void {
  expect(streams.find((stream) => stream.codec_type === "video")).toMatchObject(
    {
      codec_name: "h264",
      pix_fmt: "yuv420p",
    },
  );
}

function verifyFixtureSubtitleStreams(subtitles: ProbeStream[]): void {
  expect(subtitles).toHaveLength(2);
  expect(
    subtitles.find((stream) => stream.codec_name === "subrip"),
  ).toMatchObject({
    tags: { language: "zho", title: "内嵌中文字幕" },
    disposition: { default: 1 },
  });
  expect(
    subtitles.find((stream) => stream.codec_name === IMAGE_SUBTITLE_CODEC),
  ).toMatchObject({
    nb_read_packets: "1",
    tags: { language: "zho", title: "内嵌图片字幕" },
    disposition: { default: 0 },
  });
}

function probeStreams(path: string): ProbeStream[] {
  const output = execFileSync(
    "ffprobe",
    ["-v", "error", "-count_packets", "-show_streams", "-of", "json", path],
    { encoding: "utf8" },
  );
  return (JSON.parse(output) as { streams: ProbeStream[] }).streams;
}

function verifySingleAudioStreams(path: string): void {
  const streams = probeStreams(path);
  expect(
    streams.filter((stream) => stream.codec_type === "video"),
  ).toHaveLength(1);
  expect(
    streams.filter((stream) => stream.codec_type === "audio"),
  ).toHaveLength(1);
  expect(
    streams.filter((stream) => stream.codec_type === "subtitle"),
  ).toHaveLength(0);
}

function verifyFixtureAudioStreams(audio: ProbeStream[]): void {
  expect(audio).toHaveLength(2);
  expect(audio[0]).toMatchObject({
    codec_name: "aac",
    channels: 1,
    channel_layout: "mono",
    tags: { language: "eng" },
    disposition: { default: 1 },
  });
  expect(audio[0]?.tags?.handler_name ?? audio[0]?.tags?.title).toBe(
    "四百四十赫兹音轨",
  );
  expect(audio[1]).toMatchObject({
    codec_name: "aac",
    channels: 1,
    channel_layout: "mono",
    tags: { language: "jpn" },
    disposition: { default: 0 },
  });
  expect(audio[1]?.tags?.handler_name ?? audio[1]?.tags?.title).toBe(
    "八百八十赫兹音轨",
  );
}

function writeSubtitleSet<T extends Record<SubtitleFormat, string>>(
  directory: string,
  stem: string,
  texts: T,
): Record<SubtitleFormat, string> {
  const paths = {
    srt: join(directory, `${stem}.srt`),
    ass: join(directory, `${stem}.ass`),
    ssa: join(directory, `${stem}.ssa`),
    vtt: join(directory, `${stem}.vtt`),
  };
  writeFileSync(paths.srt, srtContent(texts.srt));
  writeFileSync(paths.ass, assContent(texts.ass));
  writeFileSync(paths.ssa, assContent(texts.ssa, "v4.00"));
  writeFileSync(paths.vtt, vttContent(texts.vtt));
  return paths;
}

function srtContent(text: string): string {
  return `1\n00:00:01,000 --> 00:00:07,000\n${text}\n`;
}

function assContent(text: string, scriptType = "v4.00+"): string {
  return `[Script Info]\nScriptType: ${scriptType}\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:01.00,0:00:07.00,Default,,0,0,0,,${text}\n`;
}

function vttContent(text: string): string {
  return `WEBVTT\n\n00:00:01.000 --> 00:00:07.000\n${text}\n`;
}

function snapshotFiles(paths: string[]): Record<string, FileSnapshot> {
  return Object.fromEntries(
    paths.map((path) => {
      const data = readFileSync(path);
      return [
        basename(path),
        {
          hash: createHash("sha256").update(data).digest("hex"),
          mtimeMs: statSync(path).mtimeMs,
        },
      ];
    }),
  );
}

function expectSourcesUnchanged(fixture: Fixture): void {
  expect(readdirSync(fixture.mediaDir).sort()).toEqual(fixture.sourceFiles);
  expect(snapshotFiles(fixture.sourcePaths)).toEqual(fixture.sourceSnapshot);
}

interface TranscodingSettingsSnapshot {
  transcode_hwaccel_fallback: string;
  transcode_hwaccel_mode: string;
}

async function configureSoftwareTranscoding(
  request: APIRequestContext,
): Promise<TranscodingSettingsSnapshot> {
  const response = await request.get("/api/settings");
  expect(response.ok()).toBeTruthy();
  const settings = (
    (await response.json()) as { settings: Record<string, string> }
  ).settings;
  const snapshot = {
    transcode_hwaccel_fallback: settings.transcode_hwaccel_fallback,
    transcode_hwaccel_mode: settings.transcode_hwaccel_mode,
  };
  if (
    snapshot.transcode_hwaccel_fallback === undefined ||
    snapshot.transcode_hwaccel_mode === undefined
  ) {
    throw new Error("FR2-044 无法保存完整的全局转码设置快照");
  }
  await updateTranscodingSettings(request, {
    transcode_hwaccel_fallback: "1",
    transcode_hwaccel_mode: "software",
  });
  return snapshot;
}

function restoreTranscodingSettings(
  request: APIRequestContext,
  snapshot: TranscodingSettingsSnapshot,
): Promise<void> {
  return updateTranscodingSettings(request, snapshot);
}

async function updateTranscodingSettings(
  request: APIRequestContext,
  settings: TranscodingSettingsSnapshot,
): Promise<void> {
  const response = await request.put("/api/settings", { data: { settings } });
  if (!response.ok()) {
    throw new Error(
      `更新全局转码设置失败：HTTP ${response.status()} ${await response.text()}`,
    );
  }
}

async function deleteLibrary(
  request: APIRequestContext,
  libraryID: number,
): Promise<void> {
  const response = await request.delete(`/api/library/paths/${libraryID}`);
  if (!response.ok()) {
    throw new Error(
      `删除 FR2-044 媒体库 ${libraryID} 失败：HTTP ${response.status()} ${await response.text()}`,
    );
  }
}

async function createLibrary(
  request: APIRequestContext,
  mediaDir: string,
): Promise<number> {
  const response = await request.post("/api/library/paths", {
    data: {
      path: mediaDir.replace(/\\/g, "/"),
      type: "local",
      label: "FR2-044 字幕音轨真实 E2E",
    },
  });
  expect(response.ok()).toBeTruthy();
  return ((await response.json()) as { id: number }).id;
}

async function scanMedia(
  request: APIRequestContext,
  libraryID: number,
): Promise<{
  multiAudioID: number;
  imageSubtitleID: number;
  singleAudioID: number;
}> {
  const scan = await request.post(`/api/library/scan/${libraryID}`, {
    params: { mode: "full" },
  });
  expect(scan.ok()).toBeTruthy();
  await waitLegacyTask(
    request,
    ((await scan.json()) as { task_id: number }).task_id,
  );
  const response = await request.get("/api/library/media", {
    params: { library_id: libraryID, page_size: 20 },
  });
  expect(response.ok()).toBeTruthy();
  const items = (await response.json()) as {
    items: Array<{ id: number; file_name: string }>;
  };
  const multiAudio = items.items.find(
    (item) => item.file_name === `${VIDEO_BASENAME}.mp4`,
  );
  const imageSubtitle = items.items.find(
    (item) => item.file_name === `${IMAGE_SUBTITLE_BASENAME}.mkv`,
  );
  const singleAudio = items.items.find(
    (item) => item.file_name === `${SINGLE_AUDIO_BASENAME}.mp4`,
  );
  expect(multiAudio).toBeTruthy();
  expect(imageSubtitle).toBeTruthy();
  expect(singleAudio).toBeTruthy();
  return {
    multiAudioID: multiAudio!.id,
    imageSubtitleID: imageSubtitle!.id,
    singleAudioID: singleAudio!.id,
  };
}

async function waitLegacyTask(
  request: APIRequestContext,
  taskID: number,
): Promise<void> {
  await expect
    .poll(
      async () => {
        const response = await request.get("/api/library/scan/tasks");
        expect(response.ok()).toBeTruthy();
        const tasks = (await response.json()) as {
          tasks: Array<{ id: number; status: string; error?: string }>;
        };
        const task = tasks.tasks.find((item) => item.id === taskID);
        if (task?.status === "error")
          throw new Error(task.error || "扫描任务失败");
        return task?.status ?? "missing";
      },
      { timeout: 60_000 },
    )
    .toBe("completed");
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
          status: string;
          error?: string;
        };
        if (task.status === "failed" || task.status === "canceled") {
          throw new Error(task.error || `任务 ${taskID} 未成功`);
        }
        return task.status;
      },
      { timeout: 60_000 },
    )
    .toBe("succeeded");
}

async function getManifest(
  request: APIRequestContext,
  mediaID: number,
): Promise<TrackManifest> {
  const response = await request.get(`/api/play/${mediaID}/tracks`);
  expect(response.status()).toBe(200);
  return (await response.json()) as TrackManifest;
}

async function loadCompleteManifest(
  request: APIRequestContext,
  mediaID: number,
  embeddedSubtitleCount: number,
): Promise<TrackManifest> {
  const first = await getManifest(request, mediaID);
  if (hasExpectedEmbeddedTracks(first, embeddedSubtitleCount)) return first;
  const refresh = await request.post(
    `/api/library/media/${mediaID}/metadata/refresh`,
  );
  expect(refresh.status()).toBe(202);
  await waitUnifiedTask(
    request,
    ((await refresh.json()) as { task_id: number }).task_id,
  );
  let manifest = first;
  // 元数据刷新任务完成后，嵌入轨枚举偶发仍有短延迟；默认 5s 在 CI 上不够
  await expect
    .poll(
      async () => {
        manifest = await getManifest(request, mediaID);
        return hasExpectedEmbeddedTracks(manifest, embeddedSubtitleCount);
      },
      { timeout: 30_000 },
    )
    .toBe(true);
  return manifest;
}

function hasExpectedEmbeddedTracks(
  manifest: TrackManifest,
  embeddedSubtitleCount: number,
): boolean {
  return (
    manifest.tracks.filter((track) => track.kind === "audio").length === 2 &&
    manifest.tracks.filter(
      (track) => track.kind === "subtitle" && track.source === "embedded",
    ).length === embeddedSubtitleCount
  );
}

async function verifyInitialManifest(
  request: APIRequestContext,
  mediaID: number,
  manifest: TrackManifest,
): Promise<void> {
  const audio = manifest.tracks.filter((track) => track.kind === "audio");
  const subtitles = manifest.tracks.filter(
    (track) => track.kind === "subtitle",
  );
  expect(audio).toHaveLength(2);
  expect(subtitles.filter((track) => track.source === "embedded")).toHaveLength(
    1,
  );
  expect(subtitles.filter((track) => track.source === "sidecar")).toHaveLength(
    4,
  );
  verifyAudioTracks(audio, manifest);
  await verifyAudioReloadAPI(request, mediaID, audio);
  await verifyDiscoveredSubtitleContent(request, mediaID, subtitles);
  const repeated = await getManifest(request, mediaID);
  expect(repeated.tracks.map((track) => track.id)).toEqual(
    manifest.tracks.map((track) => track.id),
  );
}

async function verifyImageSubtitleManifest(
  request: APIRequestContext,
  mediaID: number,
  manifest: TrackManifest,
): Promise<void> {
  const audio = manifest.tracks.filter((track) => track.kind === "audio");
  const subtitles = manifest.tracks.filter(
    (track) => track.kind === "subtitle" && track.source === "embedded",
  );
  expect(audio).toHaveLength(2);
  expect(subtitles).toHaveLength(2);
  const text = subtitles.find((track) => track.codec === "subrip");
  expect(text).toMatchObject({
    language: "zho",
    title: "内嵌中文字幕",
    available: true,
    capability: "seamless",
    stream_index: 3,
  });
  verifyImageSubtitleTrack(subtitles);
  await expectTrackContent(
    request,
    mediaID,
    text!.id,
    subtitleTexts.embedded,
    /00:00:01\.\d{3} --> 00:00:07\.\d{3}/u,
  );
  const image = subtitles.find((track) => track.codec === IMAGE_SUBTITLE_CODEC);
  await expectImageSubtitleUnsupported(request, mediaID, image!.id);
}

function verifyImageSubtitleTrack(subtitles: Track[]): void {
  expect(
    subtitles.find((track) => track.codec === IMAGE_SUBTITLE_CODEC),
  ).toMatchObject({
    source: "embedded",
    codec: IMAGE_SUBTITLE_CODEC,
    language: "zho",
    title: "内嵌图片字幕",
    is_default: false,
    available: false,
    capability: "unsupported",
    unsupported_reason: "IMAGE_SUBTITLE_UNSUPPORTED",
    stream_index: 4,
  });
}

function verifyAudioTracks(audio: Track[], manifest: TrackManifest): void {
  const first = audio.find((track) => track.language === "eng");
  const second = audio.find((track) => track.language === "jpn");
  expect(first).toMatchObject({
    source: "embedded",
    codec: "aac",
    language: "eng",
    channels: 1,
    channel_layout: "mono",
    is_default: true,
    available: true,
    capability: "reload",
    stream_index: 1,
  });
  expect(second).toMatchObject({
    source: "embedded",
    codec: "aac",
    language: "jpn",
    channels: 1,
    channel_layout: "mono",
    is_default: false,
    available: true,
    capability: "reload",
    stream_index: 2,
  });
  expect(manifest.backend.audio).toMatchObject({
    available: true,
    capability: "reload",
  });
  expect(manifest.selection.audio.selected_track_id).toBe(first?.id);
  expect(manifest.selection.audio.effective_track_id).toBeNull();
}

async function verifyAudioReloadAPI(
  request: APIRequestContext,
  mediaID: number,
  audio: Track[],
): Promise<void> {
  const reloads: AudioReloadResponse[] = [];
  for (const language of ["eng", "jpn"]) {
    const target = audio.find((track) => track.language === language);
    expect(target).toBeTruthy();
    reloads.push(
      await verifyAudioReloadTarget(request, mediaID, target!, language),
    );
  }
  expect(new Set(reloads.map((reload) => reload.profile_id)).size).toBe(2);
  expect(new Set(reloads.map((reload) => reload.task_id)).size).toBe(2);
}

async function verifyAudioReloadTarget(
  request: APIRequestContext,
  mediaID: number,
  target: Track,
  expectedLanguage: string,
): Promise<AudioReloadResponse> {
  const response = await request.post(`/api/play/${mediaID}/audio-reload`, {
    data: { track_id: target.id },
  });
  expect(response.status()).toBe(202);
  const reload = (await response.json()) as AudioReloadResponse;
  const expectedProfile = `audio-h264-aac-${createHash("sha256")
    .update(target.id)
    .digest("hex")
    .slice(0, 24)}`;
  expect(reload).toMatchObject({
    profile_id: expectedProfile,
    requested_track_id: target.id,
    space_id: "space-default",
  });
  expect(reload.task_id).toMatch(/^\d+$/u);
  expect(reload.url).toBe(
    `/api/play/hls/${mediaID}/profiles/${expectedProfile}/tasks/${reload.task_id}/master.m3u8`,
  );
  await verifyAudioReloadOutput(
    request,
    mediaID,
    reload,
    target.id,
    expectedLanguage,
  );
  return reload;
}

async function verifyAudioReloadOutput(
  request: APIRequestContext,
  mediaID: number,
  reload: AudioReloadResponse,
  targetID: string,
  expectedLanguage: string,
): Promise<void> {
  const status = await waitAudioReload(request, mediaID, reload);
  expect(status).toMatchObject({
    available: true,
    effective_track_id: targetID,
    profile_id: reload.profile_id,
    url: reload.url,
  });
  const manifest = await request.get(status.url, {
    headers: { "X-JianVideo-Space-Id": reload.space_id },
  });
  expect(manifest.status()).toBe(200);
  expect(await manifest.text()).toContain("#EXTM3U");
  verifyGeneratedAudioReload(mediaID, reload, expectedLanguage);
}

async function waitAudioReload(
  request: APIRequestContext,
  mediaID: number,
  reload: AudioReloadResponse,
): Promise<HLSStatusResponse> {
  let status: HLSStatusResponse | null = null;
  await expect
    .poll(
      async () => {
        const response = await request.get(`/api/play/${mediaID}/hls-status`, {
          params: { profile_id: reload.profile_id, task_id: reload.task_id },
        });
        expect(response.status()).toBe(200);
        status = (await response.json()) as HLSStatusResponse;
        expect(status.task?.id).toBe(reload.task_id);
        if (
          status.task?.status === "failed" ||
          status.task?.status === "canceled"
        ) {
          throw new Error(
            status.task.error || `音轨任务 ${status.task.status}`,
          );
        }
        return status.task?.status ?? "missing";
      },
      { timeout: 120_000 },
    )
    .toBe("succeeded");
  return status!;
}

function verifyGeneratedAudioReload(
  mediaID: number,
  reload: AudioReloadResponse,
  expectedLanguage: string,
): void {
  const outputDir = join(
    ".tmp",
    "e2e-run",
    "hls",
    reload.space_id,
    String(mediaID),
    reload.profile_id,
    "tasks",
    reload.task_id,
  );
  const segment = readdirSync(outputDir).find((name) => name.endsWith(".ts"));
  expect(segment).toBeTruthy();
  const output = execFileSync(
    "ffprobe",
    [
      "-v",
      "error",
      "-print_format",
      "json",
      "-show_programs",
      join(outputDir, segment!),
    ],
    { encoding: "utf8" },
  );
  const probe = JSON.parse(output) as {
    programs: Array<{
      streams: Array<{
        codec_name: string;
        codec_type: string;
        tags?: Record<string, string>;
      }>;
    }>;
  };
  expect(probe.programs).toHaveLength(1);
  const streams = probe.programs[0]!.streams;
  expect(streams.filter((stream) => stream.codec_type === "video")).toEqual([
    expect.objectContaining({ codec_name: "h264" }),
  ]);
  expect(streams.filter((stream) => stream.codec_type === "audio")).toEqual([
    expect.objectContaining({
      codec_name: "aac",
      tags: expect.objectContaining({ language: expectedLanguage }),
    }),
  ]);
}

async function verifySingleAudioUnsupported(
  request: APIRequestContext,
  mediaID: number,
): Promise<void> {
  let manifest = await getManifest(request, mediaID);
  if (manifest.tracks.filter((track) => track.kind === "audio").length !== 1) {
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
        manifest = await getManifest(request, mediaID);
        return manifest.tracks.filter((track) => track.kind === "audio").length;
      })
      .toBe(1);
  }
  const audio = manifest.tracks.filter((track) => track.kind === "audio");
  expect(audio).toHaveLength(1);
  expect(audio[0]).toMatchObject({
    capability: "unsupported",
    unsupported_reason: "AUDIO_SWITCH_UNSUPPORTED",
  });
  expect(manifest.backend.audio).toMatchObject({
    available: false,
    capability: "unsupported",
    unsupported_reason: "AUDIO_SWITCH_UNSUPPORTED",
  });
  const response = await request.post(`/api/play/${mediaID}/audio-reload`, {
    data: { track_id: audio[0]!.id },
  });
  expect(response.status()).toBe(422);
  expect(await response.json()).toMatchObject({
    code: "AUDIO_RELOAD_UNSUPPORTED",
    reason: "AUDIO_SWITCH_UNSUPPORTED",
  });
}

async function verifyDiscoveredSubtitleContent(
  request: APIRequestContext,
  mediaID: number,
  subtitles: Track[],
): Promise<void> {
  const expected = new Map<string, string>([
    ["embedded", subtitleTexts.embedded],
    ["srt", subtitleTexts.srt],
    ["ass", subtitleTexts.ass],
    ["ssa", subtitleTexts.ssa],
    ["vtt", subtitleTexts.vtt],
  ]);
  for (const track of subtitles) {
    if (track.unsupported_reason === "IMAGE_SUBTITLE_UNSUPPORTED") {
      await expectImageSubtitleUnsupported(request, mediaID, track.id);
      continue;
    }
    const key = track.source === "embedded" ? "embedded" : track.format!;
    await expectTrackContent(request, mediaID, track.id, expected.get(key)!);
  }
}

async function expectImageSubtitleUnsupported(
  request: APIRequestContext,
  mediaID: number,
  trackID: string,
): Promise<void> {
  const response = await request.get(
    `/api/play/${mediaID}/subtitles/${encodeURIComponent(trackID)}/content`,
  );
  expect(response.status()).toBe(422);
  expect(await response.json()).toMatchObject({
    code: "IMAGE_SUBTITLE_UNSUPPORTED",
  });
}

async function expectTrackContent(
  request: APIRequestContext,
  mediaID: number,
  trackID: string,
  text: string,
  timing = /00:00:01\.000 --> 00:00:07\.000/u,
): Promise<void> {
  const response = await request.get(
    `/api/play/${mediaID}/subtitles/${encodeURIComponent(trackID)}/content`,
  );
  expect(response.status()).toBe(200);
  expect(response.headers()["content-type"]).toContain("text/vtt");
  const content = await response.text();
  expect(content).toMatch(/^WEBVTT\r?\n\r?\n/u);
  expect(content).toMatch(timing);
  expect(content).toContain(text);
}

async function verifyApiUploads(
  request: APIRequestContext,
  mediaID: number,
  fixture: Fixture,
): Promise<Track[]> {
  const uploaded: Track[] = [];
  for (const format of Object.keys(fixture.uploads) as SubtitleFormat[]) {
    uploaded.push(
      await uploadSubtitle(request, mediaID, fixture.uploads[format], format),
    );
  }
  const first = await getManifest(request, mediaID);
  const listed = first.tracks.filter((track) => track.source === "uploaded");
  expect(listed).toHaveLength(4);
  expect(listed.map((track) => track.id).sort()).toEqual(
    uploaded.map((track) => track.id).sort(),
  );
  const repeated = await getManifest(request, mediaID);
  expect(
    repeated.tracks
      .filter((track) => track.source === "uploaded")
      .map((track) => track.id),
  ).toEqual(listed.map((track) => track.id));
  for (const track of listed) {
    await expectTrackContent(
      request,
      mediaID,
      track.id,
      uploadTexts[track.format as SubtitleFormat],
    );
  }
  return listed;
}

async function uploadSubtitle(
  request: APIRequestContext,
  mediaID: number,
  path: string,
  format: SubtitleFormat,
): Promise<Track> {
  const response = await request.post(`/api/play/${mediaID}/subtitles`, {
    multipart: {
      file: {
        name: basename(path),
        mimeType: format === "vtt" ? "text/vtt" : "text/plain",
        buffer: readFileSync(path),
      },
    },
  });
  expect(response.status()).toBe(201);
  const track = (await response.json()) as Track;
  expect(track).toMatchObject({ source: "uploaded", format, available: true });
  return track;
}

async function verifyDeleteAuditAndRemainingTracks(
  request: APIRequestContext,
  mediaID: number,
  uploaded: Track[],
): Promise<void> {
  const deleted = uploaded[0]!;
  const response = await request.delete(
    `/api/play/${mediaID}/subtitles/${encodeURIComponent(deleted.id)}`,
  );
  expect(response.status()).toBe(204);
  const manifest = await getManifest(request, mediaID);
  expect(manifest.tracks.some((track) => track.id === deleted.id)).toBe(false);
  const missing = await request.get(
    `/api/play/${mediaID}/subtitles/${encodeURIComponent(deleted.id)}/content`,
  );
  expect(missing.status()).toBe(404);
  await expectDeleteAudit(request, deleted.id);
  for (const track of uploaded.slice(1)) {
    await expectTrackContent(
      request,
      mediaID,
      track.id,
      uploadTexts[track.format as SubtitleFormat],
    );
  }
}

async function expectDeleteAudit(
  request: APIRequestContext,
  trackID: string,
): Promise<void> {
  const response = await request.get("/api/audit/events", {
    params: {
      action: "subtitle.deleted",
      resource_type: "subtitle",
      resource_id: trackID,
      limit: 10,
    },
  });
  expect(response.status()).toBe(200);
  const body = (await response.json()) as {
    items: Array<{ action: string; resource_id: string }>;
  };
  expect(body.items).toContainEqual(
    expect.objectContaining({
      action: "subtitle.deleted",
      resource_id: trackID,
    }),
  );
}

async function verifyPlayerUI(
  page: Page,
  mediaID: number,
  fixture: Fixture,
): Promise<void> {
  const tracksResponse = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === `/api/play/${mediaID}/tracks`;
  });
  await page.goto(`/play/${mediaID}`);
  const response = await tracksResponse;
  expect(response.status()).toBe(200);
  expect(
    ((await response.json()) as TrackManifest).tracks.length,
  ).toBeGreaterThan(6);
  const video = page.locator("video");
  await expectVideoPlayable(video);
  await verifyEmbeddedSelection(page, video);
  await verifyRapidSubtitleSwitch(page, video);
  const uiTrackID = await verifyUiUpload(
    page,
    video,
    mediaID,
    fixture.uiUpload,
  );
  await verifyUiDelete(page, mediaID, uiTrackID);
  await verifySubtitleStyles(page, video);
  await verifyAudioReloadTransaction(page, video, mediaID);
}

async function expectVideoPlayable(video: Locator): Promise<void> {
  await expect
    .poll(
      () =>
        video.evaluate((node) => {
          const media = node as HTMLVideoElement;
          return {
            duration: media.duration,
            error: media.error?.code ?? 0,
            readyState: media.readyState,
          };
        }),
      { timeout: 30_000 },
    )
    .toMatchObject({ error: 0, readyState: expect.any(Number) });
  await expect
    .poll(() => video.evaluate((node) => (node as HTMLVideoElement).readyState))
    .toBeGreaterThanOrEqual(2);
  await expect
    .poll(() => video.evaluate((node) => (node as HTMLVideoElement).duration))
    .toBeGreaterThan(10);
  await video.evaluate(async (node) => {
    const media = node as HTMLVideoElement;
    media.muted = true;
    await media.play();
  });
  const startedAt = await video.evaluate(
    (node) => (node as HTMLVideoElement).currentTime,
  );
  await expect
    .poll(() =>
      video.evaluate((node) => (node as HTMLVideoElement).currentTime),
    )
    .toBeGreaterThan(startedAt);
  await video.evaluate((node) => (node as HTMLVideoElement).pause());
}

async function verifyImageSubtitleUI(
  page: Page,
  mediaID: number,
): Promise<void> {
  const tracksResponse = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === `/api/play/${mediaID}/tracks`;
  });
  await page.goto(`/play/${mediaID}`);
  const response = await tracksResponse;
  expect(response.status()).toBe(200);
  verifyImageSubtitleTrack(((await response.json()) as TrackManifest).tracks);
  await page.getByRole("button", { name: "字幕轨道" }).click();
  const item = page.getByRole("menuitem", {
    name: /内嵌图片字幕.*zho.*hdmv_pgs_subtitle.*IMAGE_SUBTITLE_UNSUPPORTED/u,
  });
  await expect(item).toBeVisible();
  await expect(item).toBeDisabled();
  await page.keyboard.press("Escape");
}

async function verifyEmbeddedSelection(
  page: Page,
  video: Locator,
): Promise<void> {
  await chooseSubtitle(page, /zho.*mov_text/u);
  await seekToCue(video);
  await expect(page.getByTestId("subtitle-overlay")).toContainText(
    subtitleTexts.embedded,
  );
  await chooseSubtitle(page, /关闭字幕/u);
  await expect(page.getByTestId("subtitle-overlay")).toBeHidden();
}

async function verifyRapidSubtitleSwitch(
  page: Page,
  video: Locator,
): Promise<void> {
  await chooseSubtitle(page, new RegExp(`${VIDEO_BASENAME}\\.srt`, "u"));
  await chooseSubtitle(page, new RegExp(`${VIDEO_BASENAME}\\.vtt`, "u"));
  await seekToCue(video);
  const overlay = page.getByTestId("subtitle-overlay");
  await expect(overlay).toContainText(subtitleTexts.vtt);
  await expect(overlay).not.toContainText(subtitleTexts.srt);
}

async function verifyUiUpload(
  page: Page,
  video: Locator,
  mediaID: number,
  uiUpload: string,
): Promise<string> {
  const uploadResponse = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return (
      url.pathname === `/api/play/${mediaID}/subtitles` &&
      response.request().method() === "POST"
    );
  });
  const refreshResponse = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return (
      url.pathname === `/api/play/${mediaID}/tracks` &&
      response.request().method() === "GET"
    );
  });
  await page.getByLabel("上传字幕文件").setInputFiles(uiUpload);
  expect((await uploadResponse).status()).toBe(201);
  expect((await refreshResponse).status()).toBe(200);
  await seekToCue(video);
  await expect(page.getByTestId("subtitle-overlay")).toContainText(
    "UI 独特字幕可见",
  );
  const manifest = await getManifest(page.request, mediaID);
  const track = manifest.tracks.find(
    (item) => item.source === "uploaded" && item.title === "ui-unique-track",
  );
  expect(track).toBeTruthy();
  return track!.id;
}

async function verifyUiDelete(
  page: Page,
  mediaID: number,
  trackID: string,
): Promise<void> {
  await page.getByRole("button", { name: "字幕轨道" }).click();
  const item = page.getByRole("menuitem", { name: /删除 ui-unique-track/u });
  await expect(item).toBeVisible();
  const deleted = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return (
      url.pathname ===
        `/api/play/${mediaID}/subtitles/${encodeURIComponent(trackID)}` &&
      response.request().method() === "DELETE"
    );
  });
  await item.dispatchEvent("click");
  expect((await deleted).status()).toBe(204);
  await expect(page.getByTestId("subtitle-overlay")).toBeHidden();
  const manifest = await getManifest(page.request, mediaID);
  expect(manifest.tracks.some((track) => track.id === trackID)).toBe(false);
  const missing = await page.request.get(
    `/api/play/${mediaID}/subtitles/${encodeURIComponent(trackID)}/content`,
  );
  expect(missing.status()).toBe(404);
}

async function verifySubtitleStyles(page: Page, video: Locator): Promise<void> {
  await chooseSubtitle(page, new RegExp(`${VIDEO_BASENAME}\\.ass`, "u"));
  await seekToCue(video);
  await choosePreference(page, "字号 大");
  await choosePreference(page, "文字颜色 #ffff00");
  await choosePreference(page, "背景透明度 30%");
  await choosePreference(page, "垂直位置 24%");
  const overlay = page.getByTestId("subtitle-overlay");
  const style = await overlay.evaluate((node) => {
    const overlayElement = node as HTMLElement;
    const text = overlayElement.querySelector("span") as HTMLElement;
    const parent = overlayElement.offsetParent as HTMLElement;
    const overlayStyle = getComputedStyle(overlayElement);
    const textStyle = getComputedStyle(text);
    return {
      backgroundColor: textStyle.backgroundColor,
      bottom: Number.parseFloat(overlayStyle.bottom),
      color: textStyle.color,
      fontSize: Number.parseFloat(textStyle.fontSize),
      parentHeight: parent.getBoundingClientRect().height,
    };
  });
  expect(style.fontSize).toBeGreaterThan(16);
  expect(style.color).toBe("rgb(255, 255, 0)");
  expect(style.backgroundColor).toBe("rgba(0, 0, 0, 0.7)");
  expect(style.bottom / style.parentHeight).toBeCloseTo(0.24, 1);
}

async function verifyAudioReloadTransaction(
  page: Page,
  video: Locator,
  mediaID: number,
): Promise<void> {
  await preparePausedPlayback(page, video);
  await seekToCue(video);
  await expect(page.getByTestId("subtitle-overlay")).toContainText(
    subtitleTexts.ass,
  );
  const initialState = await capturePlaybackState(video);
  const statusURLs: string[] = [];
  const onRequest = (request: { url(): string }) => {
    const url = new URL(request.url());
    if (url.pathname === `/api/play/${mediaID}/hls-status`)
      statusURLs.push(url.toString());
  };
  page.on("request", onRequest);

  const reloadResponse = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname ===
        `/api/play/${mediaID}/audio-reload` &&
      response.request().method() === "POST",
    { timeout: 120_000 },
  );
  const manifestRequest = page.waitForRequest(
    (request) =>
      /\/api\/play\/hls\/\d+\/profiles\/audio-h264-aac-[a-f0-9]{24}\/tasks\/\d+\/master\.m3u8$/u.test(
        new URL(request.url()).pathname,
      ),
    { timeout: 120_000 },
  );
  await chooseAudio(page, /jpn.*aac.*1 声道/u);
  const reload = (await (await reloadResponse).json()) as AudioReloadResponse;
  const hlsRequest = await manifestRequest;
  const hlsHeaders = await hlsRequest.allHeaders();
  const hasAuth =
    /^Bearer\s+\S+/u.test(hlsHeaders.authorization ?? "") ||
    /(?:^|;\s*)auth_token=/u.test(hlsHeaders.cookie ?? "");
  expect(hasAuth).toBe(true);
  expect(hlsHeaders["x-jianvideo-space-id"]).toBe(reload.space_id);
  await expect
    .poll(() =>
      statusURLs.some((value) => {
        const url = new URL(value);
        return (
          url.searchParams.get("profile_id") === reload.profile_id &&
          url.searchParams.get("task_id") === reload.task_id
        );
      }),
    )
    .toBe(true);
  await expectAudioSelection(
    page,
    /jpn.*aac.*1 声道/u,
    /eng.*aac.*1 声道.*默认/u,
  );
  await expectPlaybackState(page, video, initialState, subtitleTexts.ass);
  page.off("request", onRequest);

  const tracks = await getManifest(page.request, mediaID);
  const first = tracks.tracks.find(
    (track) => track.kind === "audio" && track.language === "eng",
  );
  expect(first).toBeTruthy();
  const failedProfile = `audio-h264-aac-${createHash("sha256")
    .update(first!.id)
    .digest("hex")
    .slice(0, 24)}`;
  const failedManifest = new RegExp(
    `/api/play/hls/${mediaID}/profiles/${failedProfile}/tasks/\\d+/master\\.m3u8$`,
    "u",
  );
  await page.route(failedManifest, (route) => route.abort("failed"));
  const rollbackBase = await capturePlaybackState(video);
  const failedReload = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname ===
        `/api/play/${mediaID}/audio-reload` &&
      response.request().method() === "POST",
    { timeout: 120_000 },
  );
  await chooseAudio(page, /eng.*aac.*1 声道.*默认/u);
  expect(
    ((await (await failedReload).json()) as AudioReloadResponse)
      .requested_track_id,
  ).toBe(first!.id);
  await expect(
    page.getByText("切换音轨失败", { exact: true }).last(),
  ).toBeVisible({
    timeout: 30_000,
  });
  await page.unroute(failedManifest);
  await expectAudioSelection(
    page,
    /jpn.*aac.*1 声道/u,
    /eng.*aac.*1 声道.*默认/u,
  );
  await expectPlaybackState(page, video, rollbackBase, subtitleTexts.ass);
  await expect(page.getByText(/Network Error/i)).toHaveCount(0);
}

async function preparePausedPlayback(
  page: Page,
  video: Locator,
): Promise<void> {
  if (await video.evaluate((node) => (node as HTMLVideoElement).paused)) {
    await page
      .getByRole("button", { name: "播放", exact: true })
      .dispatchEvent("click");
    await expect
      .poll(() => video.evaluate((node) => (node as HTMLVideoElement).paused))
      .toBe(false);
  }
  await page
    .getByRole("button", { name: "暂停", exact: true })
    .dispatchEvent("click");
  await expect
    .poll(() => video.evaluate((node) => (node as HTMLVideoElement).paused))
    .toBe(true);
  await page.getByRole("button", { name: "播放速度" }).dispatchEvent("click");
  await page
    .getByRole("menuitem", { name: "1.5×", exact: true })
    .dispatchEvent("click");
  await expect
    .poll(() =>
      video.evaluate((node) => (node as HTMLVideoElement).playbackRate),
    )
    .toBe(1.5);
}

async function capturePlaybackState(video: Locator) {
  return video.evaluate((node) => {
    const media = node as HTMLVideoElement;
    return {
      currentTime: media.currentTime,
      paused: media.paused,
      playbackRate: media.playbackRate,
    };
  });
}

async function expectPlaybackState(
  page: Page,
  video: Locator,
  expected: Awaited<ReturnType<typeof capturePlaybackState>>,
  subtitleText: string,
): Promise<void> {
  await expect
    .poll(() =>
      video.evaluate((node) => (node as HTMLVideoElement).currentTime),
    )
    .toBeCloseTo(expected.currentTime, 1);
  await expect
    .poll(
      () =>
        video.evaluate((node) => {
          const media = node as HTMLVideoElement;
          return {
            error: media.error?.code ?? 0,
            hasFutureData:
              media.readyState >= HTMLMediaElement.HAVE_FUTURE_DATA,
            paused: media.paused,
            playbackRate: media.playbackRate,
            readyState: media.readyState,
            seeking: media.seeking,
          };
        }),
      { timeout: 30_000 },
    )
    .toMatchObject({
      error: 0,
      hasFutureData: true,
      paused: expected.paused,
      playbackRate: expected.playbackRate,
      readyState: expect.any(Number),
      seeking: false,
    });
  await expect(page.getByTestId("subtitle-overlay")).toContainText(
    subtitleText,
  );
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
  other: RegExp,
): Promise<void> {
  await page.getByRole("button", { name: "音轨" }).click();
  const effectiveItem = page.getByRole("menuitem", { name: effective });
  const otherItem = page.getByRole("menuitem", { name: other });
  await expect(effectiveItem).toContainText("当前播放", { timeout: 15_000 });
  await expect(effectiveItem).not.toContainText("切换中");
  await expect(otherItem).not.toContainText("当前播放");
  await expect(otherItem).not.toContainText("切换中");
  await page.keyboard.press("Escape");
}

async function verifySingleAudioUI(page: Page, mediaID: number): Promise<void> {
  const tracksResponse = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === `/api/play/${mediaID}/tracks`;
  });
  await page.goto(`/play/${mediaID}`);
  expect((await tracksResponse).status()).toBe(200);
  await expectVideoPlayable(page.locator("video"));
  const button = page.getByRole("button", { name: "音轨" });
  await expect(button).toBeVisible();
  await button.click();
  const item = page.getByRole("menuitem", {
    name: /eng.*aac.*1 声道.*默认.*AUDIO_SWITCH_UNSUPPORTED/u,
  });
  await expect(item).toBeVisible();
  await expect(item).toBeDisabled();
}

async function chooseSubtitle(page: Page, name: RegExp): Promise<void> {
  const button = page.getByRole("button", { name: "字幕轨道" });
  await expect(button).toBeVisible({ timeout: 15_000 });
  await button.click();
  const radio = page.getByRole("menuitemradio", { name });
  const item = radio.or(page.getByRole("menuitem", { name })).first();
  await expect(item).toBeVisible();
  const content = name.test("关闭字幕") ? null : waitForSubtitleContent(page);
  await item.dispatchEvent("click");
  if (content) expect((await content).status()).toBe(200);
}

function waitForSubtitleContent(page: Page) {
  return page.waitForResponse((response) => {
    const url = new URL(response.url());
    return (
      /\/api\/play\/\d+\/subtitles\/[^/]+\/content$/u.test(url.pathname) &&
      response.request().method() === "GET"
    );
  });
}

async function choosePreference(page: Page, name: string): Promise<void> {
  const button = page.getByRole("button", { name: "字幕轨道" });
  await expect(button).toBeVisible({ timeout: 15_000 });
  await button.click();
  const item = page.getByRole("menuitem", { name, exact: true });
  await expect(item).toBeVisible();
  await item.dispatchEvent("click");
}

async function seekToCue(video: Locator): Promise<void> {
  await video.evaluate(
    (node, time) =>
      new Promise<void>((resolve) => {
        const media = node as HTMLVideoElement;
        media.pause();
        media.addEventListener(
          "seeked",
          () => {
            media.dispatchEvent(new Event("timeupdate"));
            resolve();
          },
          { once: true },
        );
        media.currentTime = time;
      }),
    CUE_TIME,
  );
  await expect
    .poll(() =>
      video.evaluate((node) => (node as HTMLVideoElement).currentTime),
    )
    .toBeCloseTo(CUE_TIME, 1);
}
