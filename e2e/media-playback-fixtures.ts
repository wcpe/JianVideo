import { expect, type APIRequestContext } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { rmSync } from "node:fs";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { join, resolve } from "node:path";

export const FRAME_MARKER = { bits: 9, cellSize: 8, x: 16, y: 16 } as const;

export const hasFfmpeg = (() => {
  try {
    execFileSync("ffmpeg", ["-version"], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
})();

type MediaFileFactory = {
  name: string;
  write: (path: string) => void;
};

export type MediaLibraryOptions = {
  files: readonly MediaFileFactory[];
  label: string;
  prefix: string;
};

export type MediaLibraryFixture = {
  directory: string;
  libraryID: number;
  mediaByName: ReadonlyMap<string, number>;
};

export async function createMediaLibraryFixture(
  request: APIRequestContext,
  options: MediaLibraryOptions,
): Promise<MediaLibraryFixture> {
  const root = resolve(".tmp/e2e-run/fixtures");
  await mkdir(root, { recursive: true });
  const directory = await mkdtemp(join(root, options.prefix));
  let libraryID = 0;

  try {
    for (const file of options.files) file.write(join(directory, file.name));
    libraryID = await createLibrary(request, directory, options.label);
    const mediaByName = await scanLibrary(
      request,
      libraryID,
      options.files.map((file) => file.name),
    );
    return { directory, libraryID, mediaByName };
  } catch (error) {
    const cleanupError = await cleanupFixture(request, libraryID, directory);
    if (cleanupError !== null) {
      throw new AggregateError(
        [error, cleanupError],
        "媒体 fixture 准备与清理均失败",
      );
    }
    throw error;
  }
}

export async function cleanupMediaLibraryFixture(
  request: APIRequestContext,
  fixture: MediaLibraryFixture,
): Promise<void> {
  const cleanupError = await cleanupFixture(
    request,
    fixture.libraryID,
    fixture.directory,
  );
  if (cleanupError !== null) throw cleanupError;
}

export async function withMediaLibrary<T>(
  request: APIRequestContext,
  options: MediaLibraryOptions,
  run: (fixture: MediaLibraryFixture) => Promise<T>,
): Promise<T> {
  const fixture = await createMediaLibraryFixture(request, options);
  let primaryError: unknown;

  try {
    return await run(fixture);
  } catch (error) {
    primaryError = error;
    throw error;
  } finally {
    const cleanupError = await cleanupFixture(
      request,
      fixture.libraryID,
      fixture.directory,
    );
    if (cleanupError !== null) {
      if (primaryError !== undefined) {
        throw new AggregateError(
          [primaryError, cleanupError],
          "E2E 主流程与媒体清理均失败",
        );
      }
      throw cleanupError;
    }
  }
}

export function writeVideoFixture(
  path: string,
  options: {
    duration: number;
    frameRate?: number;
    gopSeconds?: number;
    height: number;
    width: number;
  },
): void {
  const frameRate = options.frameRate ?? 24;
  const gopFrames = Math.max(
    1,
    Math.round(frameRate * (options.gopSeconds ?? 2)),
  );
  execFileSync(
    "ffmpeg",
    [
      "-y",
      "-f",
      "lavfi",
      "-i",
      `testsrc2=duration=${options.duration}:size=${options.width}x${options.height}:rate=${frameRate}`,
      "-f",
      "lavfi",
      "-i",
      `sine=frequency=440:duration=${options.duration}`,
      "-c:v",
      "libx264",
      "-profile:v",
      "baseline",
      "-preset",
      "ultrafast",
      "-crf",
      "24",
      "-g",
      String(gopFrames),
      "-pix_fmt",
      "yuv420p",
      "-c:a",
      "aac",
      "-b:a",
      "96k",
      "-shortest",
      "-movflags",
      "+faststart",
      path,
    ],
    { stdio: "ignore" },
  );
}

export function writeTransportStreamFixture(
  path: string,
  options: {
    duration: number;
    frameRate?: number;
    gopSeconds?: number;
    height: number;
    width: number;
  },
): void {
  const intermediate = `${path}.source.mp4`;
  try {
    writeVideoFixture(intermediate, options);
    execFileSync(
      "ffmpeg",
      [
        "-y",
        "-i",
        intermediate,
        "-map",
        "0:v:0",
        "-map",
        "0:a:0?",
        "-c",
        "copy",
        "-f",
        "mpegts",
        path,
      ],
      { stdio: "ignore" },
    );
  } finally {
    rmSync(intermediate, { force: true });
  }
}

export function writeSubtitleAudioVideoFixture(
  path: string,
  embeddedSubtitlePath: string,
): void {
  execFileSync(
    "ffmpeg",
    [
      "-y",
      "-f",
      "lavfi",
      "-i",
      "testsrc2=duration=12:size=320x180:rate=24",
      "-f",
      "lavfi",
      "-i",
      "sine=frequency=440:sample_rate=48000:duration=12",
      "-f",
      "lavfi",
      "-i",
      "sine=frequency=880:sample_rate=48000:duration=12",
      "-i",
      embeddedSubtitlePath,
      "-map",
      "0:v:0",
      "-map",
      "1:a:0",
      "-map",
      "2:a:0",
      "-map",
      "3:0",
      "-c:v",
      "libx264",
      "-preset",
      "ultrafast",
      "-profile:v",
      "baseline",
      "-level",
      "3.0",
      "-pix_fmt",
      "yuv420p",
      "-c:a",
      "aac",
      "-b:a",
      "96k",
      "-ac",
      "1",
      "-c:s",
      "mov_text",
      "-metadata:s:a:0",
      "language=eng",
      "-metadata:s:a:0",
      "title=四百四十赫兹音轨",
      "-metadata:s:a:0",
      "handler_name=四百四十赫兹音轨",
      "-disposition:a:0",
      "default",
      "-metadata:s:a:1",
      "language=jpn",
      "-metadata:s:a:1",
      "title=八百八十赫兹音轨",
      "-metadata:s:a:1",
      "handler_name=八百八十赫兹音轨",
      "-disposition:a:1",
      "0",
      "-metadata:s:s:0",
      "language=zho",
      "-metadata:s:s:0",
      "title=内嵌中文字幕",
      "-metadata:s:s:0",
      "handler_name=内嵌中文字幕",
      "-disposition:s:0",
      "default",
      "-movflags",
      "+faststart",
      path,
    ],
    { stdio: "ignore" },
  );
}

export function writeNumberedVideo(
  path: string,
  options: {
    duration: number;
    frameRate: number;
    height?: number;
    width?: number;
  },
): void {
  const markerWidth = (FRAME_MARKER.bits + 2) * FRAME_MARKER.cellSize;
  const filters = [
    `drawbox=x=${FRAME_MARKER.x}:y=${FRAME_MARKER.y}:w=${markerWidth}:h=${FRAME_MARKER.cellSize}:color=black:t=fill`,
    `drawbox=x=${FRAME_MARKER.x}:y=${FRAME_MARKER.y}:w=${FRAME_MARKER.cellSize}:h=${FRAME_MARKER.cellSize}:color=white:t=fill`,
    `drawbox=x=${FRAME_MARKER.x + (FRAME_MARKER.bits + 1) * FRAME_MARKER.cellSize}:y=${FRAME_MARKER.y}:w=${FRAME_MARKER.cellSize}:h=${FRAME_MARKER.cellSize}:color=white:t=fill`,
  ];
  for (let bit = 0; bit < FRAME_MARKER.bits; bit += 1) {
    const x = FRAME_MARKER.x + (bit + 1) * FRAME_MARKER.cellSize;
    filters.push(
      `drawbox=x=${x}:y=${FRAME_MARKER.y}:w=${FRAME_MARKER.cellSize}:h=${FRAME_MARKER.cellSize}:color=white:t=fill:enable='eq(mod(floor(n/${2 ** bit}),2),1)'`,
    );
  }
  execFileSync(
    "ffmpeg",
    [
      "-y",
      "-f",
      "lavfi",
      "-i",
      `testsrc2=duration=${options.duration}:size=${options.width ?? 320}x${options.height ?? 180}:rate=${options.frameRate}`,
      "-vf",
      filters.join(","),
      "-c:v",
      "libx264",
      "-profile:v",
      "baseline",
      "-preset",
      "ultrafast",
      "-crf",
      "18",
      "-g",
      String(options.frameRate),
      "-pix_fmt",
      "yuv420p",
      "-movflags",
      "+faststart",
      path,
    ],
    { stdio: "ignore" },
  );
}

export async function createHlsAbr(
  request: APIRequestContext,
  mediaID: number,
): Promise<string> {
  const response = await request.post(`/api/play/${mediaID}/hls-abr`, {
    data: { force_rebuild: true, priority: 8 },
  });
  expect(response.status()).toBe(202);
  const body = (await response.json()) as { task_id: number; url: string };
  await waitUnifiedTask(request, body.task_id);
  return body.url;
}

export async function withSetting<T>(
  request: APIRequestContext,
  key: string,
  value: string,
  run: () => Promise<T>,
): Promise<T> {
  const response = await request.get("/api/settings");
  expect(response.ok()).toBeTruthy();
  const settings = (
    (await response.json()) as { settings: Record<string, string> }
  ).settings;
  const previous = settings[key];
  let primaryError: unknown;
  try {
    await updateSetting(request, key, value);
    return await run();
  } catch (error) {
    primaryError = error;
    throw error;
  } finally {
    const restoreValue = previous ?? "";
    try {
      await updateSetting(request, key, restoreValue);
    } catch (restoreError) {
      if (primaryError !== undefined) {
        throw new AggregateError(
          [primaryError, restoreError],
          `E2E 主流程与设置 ${key} 恢复均失败`,
        );
      }
      throw restoreError;
    }
  }
}

async function createLibrary(
  request: APIRequestContext,
  directory: string,
  label: string,
): Promise<number> {
  const response = await request.post("/api/library/paths", {
    data: { label, path: directory.replace(/\\/g, "/"), type: "local" },
  });
  expect(response.ok()).toBeTruthy();
  return ((await response.json()) as { id: number }).id;
}

async function scanLibrary(
  request: APIRequestContext,
  libraryID: number,
  expectedNames: readonly string[],
): Promise<ReadonlyMap<string, number>> {
  const response = await request.post(`/api/library/scan/${libraryID}`, {
    params: { mode: "full" },
  });
  expect(response.ok()).toBeTruthy();
  const taskID = ((await response.json()) as { task_id: number }).task_id;
  await expect
    .poll(
      async () => {
        const tasksResponse = await request.get("/api/library/scan/tasks");
        const tasks = (await tasksResponse.json()) as {
          tasks: Array<{ error?: string; id: number; status: string }>;
        };
        const task = tasks.tasks.find((item) => item.id === taskID);
        if (task?.status === "error")
          throw new Error(task.error || "扫描任务失败");
        return task?.status ?? "missing";
      },
      { timeout: 60_000 },
    )
    .toBe("completed");

  const mediaResponse = await request.get("/api/library/media", {
    params: {
      library_id: libraryID,
      page_size: Math.max(10, expectedNames.length),
    },
  });
  expect(mediaResponse.ok()).toBeTruthy();
  const items = (await mediaResponse.json()) as {
    items: Array<{ file_name: string; id: number }>;
  };
  const mediaByName = new Map(
    items.items.map((item) => [item.file_name, item.id]),
  );
  for (const name of expectedNames)
    expect(mediaByName.get(name)).toBeGreaterThan(0);
  return mediaByName;
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
        if (task.status === "failed")
          throw new Error(task.error || "HLS 任务失败");
        return task.status;
      },
      { timeout: 180_000 },
    )
    .toBe("succeeded");
}

async function updateSetting(
  request: APIRequestContext,
  key: string,
  value: string,
): Promise<void> {
  const response = await request.put("/api/settings", {
    data: { settings: { [key]: value } },
  });
  if (!response.ok()) {
    throw new Error(
      `更新设置 ${key} 失败：HTTP ${response.status()} ${await response.text()}`,
    );
  }
}

async function cleanupFixture(
  request: APIRequestContext,
  libraryID: number,
  directory: string,
): Promise<Error | null> {
  const errors: Error[] = [];
  if (libraryID > 0) {
    try {
      const response = await request.delete(`/api/library/paths/${libraryID}`, {
        timeout: 10_000,
      });
      if (!response.ok() && response.status() !== 404) {
        errors.push(
          new Error(
            `删除媒体库失败：HTTP ${response.status()} ${await response.text()}`,
          ),
        );
      }
    } catch (error) {
      errors.push(asError(error));
    }
  }
  try {
    await removeDirectoryWithRetry(directory);
  } catch (error) {
    errors.push(asError(error));
  }
  if (errors.length === 0) return null;
  return errors.length === 1
    ? errors[0]!
    : new AggregateError(errors, "媒体 fixture 清理失败");
}

async function removeDirectoryWithRetry(directory: string): Promise<void> {
  let lastError: unknown;
  for (let attempt = 0; attempt < 8; attempt += 1) {
    try {
      await rm(directory, {
        force: true,
        maxRetries: 3,
        recursive: true,
        retryDelay: 100,
      });
      return;
    } catch (error) {
      lastError = error;
      await new Promise((resolvePromise) =>
        setTimeout(resolvePromise, 125 * (attempt + 1)),
      );
    }
  }
  throw asError(lastError);
}

function asError(error: unknown): Error {
  return error instanceof Error ? error : new Error(String(error));
}
