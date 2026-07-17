import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { mkdirSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { BASE_URL, ensureSetup, login } from './helpers';

test.use({ serviceWorkers: 'block' });

test('FR2-045 双上下文续播、revision 冲突与 Space 隔离', async ({ browser }) => {
  const mediaDir = join(process.cwd(), '.tmp', 'fr2-045-e2e');
  const mediaPath = join(mediaDir, 'cross-device-watch-state.mp4');
  const contextA = await browser.newContext();
  const contextB = await browser.newContext();
  const pageA = await contextA.newPage();
  const pageB = await contextB.newPage();
  let libraryID = 0;

  try {
    mkdirSync(mediaDir, { recursive: true });
    writeVideoFixture(mediaPath);
    await ensureSetup(pageA.request);
    await login(pageA);

    const library = await pageA.request.post('/api/library/paths', {
      data: {
        label: 'FR2-045 跨设备续播 E2E',
        path: mediaDir.replace(/\\/g, '/'),
        type: 'local',
      },
    });
    expect(library.ok()).toBeTruthy();
    libraryID = ((await library.json()) as { id: number }).id;
    const scan = await pageA.request.post(`/api/library/scan/${libraryID}`, {
      params: { mode: 'full' },
    });
    expect(scan.ok()).toBeTruthy();
    await waitLegacyTask(pageA.request, ((await scan.json()) as { task_id: number }).task_id);
    const mediaList = await pageA.request.get(`/api/library/media?library_id=${libraryID}&page_size=10`);
    expect(mediaList.ok()).toBeTruthy();
    const media = ((await mediaList.json()) as { items: Array<{ id: number }> }).items[0]!;
    const watchPath = `/api/play/${media.id}/watch-state`;

    await pageA.goto(`/play/${media.id}`);
    await waitForPlayableVideo(pageA);
    const firstARequest = pageA.waitForRequest(
      (request) => request.url().endsWith(watchPath) && request.method() === 'PUT',
    );
    await setVideoPosition(pageA, 20, 'timeupdate');
    const aProgress = await firstARequest;
    const aProgressBody = aProgress.postDataJSON() as {
      event_seq: number;
      expected_revision: number;
      session_id: string;
    };
    expect(aProgressBody.event_seq).toBe(1);
    expect(aProgressBody.expected_revision).toBe(0);

    const aPauseResponse = pageA.waitForResponse(
      (response) => response.url().endsWith(watchPath) && response.request().method() === 'PUT',
    );
    await pageA.locator('video').evaluate((video: HTMLVideoElement) => video.pause());
    await aPauseResponse;
    const stateAfterA = await readWatchState(pageA.request, watchPath);
    expect(stateAfterA.position_seconds).toBeCloseTo(20, 0);
    expect(stateAfterA.revision).toBeGreaterThan(0);

    await login(pageB);
    const bRequests: Array<{ event_seq: number; session_id: string }> = [];
    pageB.on('request', (request) => {
      if (!request.url().endsWith(watchPath) || request.method() !== 'PUT') return;
      bRequests.push(request.postDataJSON() as { event_seq: number; session_id: string });
    });
    await pageB.goto(`/play/${media.id}`);
    await waitForPlayableVideo(pageB);
    await expect
      .poll(() => pageB.locator('video').evaluate((video: HTMLVideoElement) => video.currentTime))
      .toBeGreaterThan(18);
    await expect.poll(() => bRequests.length).toBeGreaterThan(0);
    expect(bRequests[0]!.session_id).not.toBe(aProgressBody.session_id);
    expect(bRequests[0]!.event_seq).toBe(1);

    await setVideoPosition(pageB, 35, 'pause');
    await expect
      .poll(async () => (await readWatchState(pageB.request, watchPath)).position_seconds)
      .toBeGreaterThan(34);
    const stateAfterB = await readWatchState(pageB.request, watchPath);
    expect(stateAfterB.revision).toBeGreaterThan(stateAfterA.revision);

    const staleConflict = pageA.waitForResponse(
      (response) =>
        response.url().endsWith(watchPath) &&
        response.request().method() === 'PUT' &&
        response.status() === 409,
    );
    await setVideoPosition(pageA, 25, 'pause');
    const conflictResponse = await staleConflict;
    const conflictBody = (await conflictResponse.json()) as {
      code: string;
      current: { position_seconds: number; revision: number };
    };
    expect(conflictBody.code).toBe('WATCH_STATE_CONFLICT');
    expect(conflictBody.current.position_seconds).toBeCloseTo(35, 0);
    expect(conflictBody.current.revision).toBeGreaterThanOrEqual(stateAfterB.revision);

    await expect
      .poll(async () => (await readWatchState(pageA.request, watchPath)).position_seconds)
      .toBeGreaterThan(34);

    const isolated = await pageA.request.get(watchPath, {
      headers: { 'X-JianVideo-Space-Id': 'space-isolated' },
    });
    expect(isolated.status()).toBe(404);
    expect((await isolated.json()).code).toBe('SPACE_NOT_FOUND');
  } finally {
    if (libraryID) await pageA.request.delete(`/api/library/paths/${libraryID}`);
    await contextA.close();
    await contextB.close();
    rmSync(mediaDir, { force: true, recursive: true });
  }
});

async function waitForPlayableVideo(page: Page): Promise<void> {
  await page.locator('video').waitFor({ state: 'attached' });
  await expect
    .poll(() =>
      page.locator('video').evaluate((video: HTMLVideoElement) => ({
        duration: video.duration,
        readyState: video.readyState,
      })),
    )
    .toMatchObject({ readyState: expect.any(Number) });
  await expect
    .poll(() => page.locator('video').evaluate((video: HTMLVideoElement) => video.duration))
    .toBeGreaterThan(50);
}

async function setVideoPosition(page: Page, position: number, eventType: 'pause' | 'timeupdate'): Promise<void> {
  await page.locator('video').evaluate(
    (video: HTMLVideoElement, input: { eventType: 'pause' | 'timeupdate'; position: number }) => {
      video.currentTime = input.position;
      video.dispatchEvent(new Event(input.eventType));
    },
    { eventType, position },
  );
}

async function readWatchState(
  request: APIRequestContext,
  path: string,
): Promise<{ position_seconds: number; revision: number }> {
  const response = await request.get(path);
  expect(response.ok()).toBeTruthy();
  return (await response.json()) as { position_seconds: number; revision: number };
}

async function waitLegacyTask(request: APIRequestContext, taskID: number): Promise<void> {
  await expect
    .poll(
      async () => {
        const response = await request.get('/api/library/scan/tasks');
        expect(response.ok()).toBeTruthy();
        const task = (
          (await response.json()) as { tasks: Array<{ error?: string; id: number; status: string }> }
        ).tasks.find((item) => item.id === taskID);
        if (!task) return 'missing';
        if (task.status === 'error') throw new Error(task.error || '扫描任务失败');
        return task.status;
      },
      { timeout: 20_000 },
    )
    .toBe('completed');
}

function writeVideoFixture(path: string): void {
  execFileSync(
    'ffmpeg',
    [
      '-y',
      '-f',
      'lavfi',
      '-i',
      'testsrc=duration=60:size=160x90:rate=10',
      '-f',
      'lavfi',
      '-i',
      'sine=frequency=440:duration=60',
      '-c:v',
      'libx264',
      '-preset',
      'ultrafast',
      '-pix_fmt',
      'yuv420p',
      '-c:a',
      'aac',
      '-shortest',
      path,
    ],
    { stdio: 'ignore' },
  );
}
