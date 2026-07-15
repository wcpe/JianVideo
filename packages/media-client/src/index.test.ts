import { describe, expect, it } from "vitest";
import {
  ApiError,
  createApiClient,
  createQueryKeys,
  cancelTask,
  getMedia,
  getTask,
  getTaskStats,
  detectDeviceCapabilities,
  getWatchState,
  listContinueWatching,
  listTasks,
  listMedia,
  listWatchHistory,
  normalizeLegacyTaskState,
  retryTask,
  taskPollInterval,
  updateWatchState,
  WatchStateConflictError,
  type FetchLike,
} from "./index";

describe("media-client package", () => {
  it("query key 包含 Space 维度", () => {
    expect(createQueryKeys().mediaList({ spaceId: "default" }, 2)).toEqual([
      "media",
      "list",
      "default",
      2,
    ]);
  });

  it("兼容旧任务状态并映射到 ADR-0055 状态", () => {
    expect(normalizeLegacyTaskState("completed")).toBe("succeeded");
    expect(normalizeLegacyTaskState("error")).toBe("failed");
  });

  it("保留 ADR-0055 原生任务状态", () => {
    expect(normalizeLegacyTaskState("pending")).toBe("pending");
    expect(normalizeLegacyTaskState("running")).toBe("running");
    expect(normalizeLegacyTaskState("succeeded")).toBe("succeeded");
    expect(normalizeLegacyTaskState("failed")).toBe("failed");
    expect(normalizeLegacyTaskState("canceled")).toBe("canceled");
  });

  it("未知任务状态抛出中文错误", () => {
    expect(() => normalizeLegacyTaskState("paused")).toThrow("未知任务状态");
  });

  it("请求携带 Space 上下文和鉴权头", async () => {
    const requests: Request[] = [];
    const client = createApiClient({
      authToken: "token-a",
      baseUrl: "https://mock.local",
      fetch: (input, init) => {
        const request = new Request(input, init);
        requests.push(request);
        return Promise.resolve(Response.json({ ok: true }));
      },
      space: { spaceId: "space-a" },
    });

    await client.request("/api/v2/ping");

    expect(requests).toHaveLength(1);
    expect(requests[0]?.headers.get("Authorization")).toBe("Bearer token-a");
    expect(requests[0]?.headers.get("X-JianVideo-Space-Id")).toBe("space-a");
  });

  it("规范化接口错误", async () => {
    const client = createApiClient({
      fetch: () =>
        Promise.resolve(
          Response.json(
            { code: "SPACE_FORBIDDEN", message: "无权访问此 Space" },
            { status: 403 },
          ),
        ),
      space: { spaceId: "space-a" },
    });

    await expect(client.request("/api/v2/media")).rejects.toMatchObject({
      code: "SPACE_FORBIDDEN",
      message: "无权访问此 Space",
      status: 403,
    });
  });

  it("按 Space 和分页生成稳定 query key", () => {
    const keys = createQueryKeys();

    expect(
      keys.mediaList({ spaceId: "space-a" }, { page: 1, pageSize: 2 }),
    ).toEqual(["media", "list", "space-a", { page: 1, pageSize: 2 }]);
    expect(
      keys.mediaList({ spaceId: "space-b" }, { page: 1, pageSize: 2 }),
    ).not.toEqual(
      keys.mediaList({ spaceId: "space-a" }, { page: 1, pageSize: 2 }),
    );
    expect(keys.taskDetail({ spaceId: "space-a" }, "task-1")).toEqual([
      "tasks",
      "detail",
      "space-a",
      "task-1",
    ]);
  });

  it("对 mock fetch 跑通媒体分页、详情和任务轮询", async () => {
    const { createMockFetch } = (await import(
      new URL("../../mock/src/index.ts", import.meta.url).href
    )) as {
      readonly createMockFetch: () => FetchLike;
    };
    const client = createApiClient({
      fetch: createMockFetch(),
      space: { spaceId: "space-default" },
    });

    const page = await listMedia(client, { page: 1, pageSize: 1 });
    const detail = await getMedia(client, page.items[0]?.id ?? "");
    const runningTask = await getTask(client, "task-transcode-default");
    const finishedTask = await getTask(client, "task-transcode-default");

    expect(page.items).toHaveLength(1);
    expect(page.total).toBeGreaterThan(1);
    expect(detail.spaceId).toBe("space-default");
    expect(runningTask.status).toBe("running");
    expect(finishedTask.status).toBe("succeeded");
    expect(taskPollInterval(runningTask)).toBe(2_000);
    expect(taskPollInterval(finishedTask)).toBe(false);
  });

  it("通过 /api/tasks 查询列表、详情、统计并执行取消和重试", async () => {
    const requests: Request[] = [];
    const client = createApiClient({
      baseUrl: "https://mock.local",
      fetch: (input, init) => {
        const request = new Request(input, init);
        requests.push(request);
        if (
          request.method === "GET" &&
          request.url.endsWith(
            "/api/tasks?page=2&page_size=5&type=transcode&status=failed",
          )
        ) {
          return Promise.resolve(
            Response.json({
              items: [
                {
                  created_at: "2026-07-01T10:00:00Z",
                  error: "编码器不可用",
                  id: "task-failed",
                  priority: 10,
                  progress: 0.4,
                  space_id: "space-default",
                  status: "error",
                  type: "transcode",
                  updated_at: "2026-07-01T10:01:00Z",
                },
              ],
              page: 2,
              page_size: 5,
              total: 1,
            }),
          );
        }
        if (
          request.method === "GET" &&
          request.url.endsWith("/api/tasks/task-failed")
        ) {
          return Promise.resolve(
            Response.json({
              created_at: "2026-07-01T10:00:00Z",
              error: "编码器不可用",
              id: "task-failed",
              priority: 10,
              progress: 0.4,
              space_id: "space-default",
              status: "error",
              type: "transcode",
              updated_at: "2026-07-01T10:01:00Z",
            }),
          );
        }
        if (
          request.method === "GET" &&
          request.url.endsWith("/api/tasks/task-system")
        ) {
          return Promise.resolve(
            Response.json({
              created_at: "2026-07-01T10:00:00Z",
              error: null,
              id: "task-system",
              priority: 1,
              progress: 0,
              space_id: null,
              status: "pending",
              type: "cache.cleanup",
              updated_at: "2026-07-01T10:01:00Z",
            }),
          );
        }
        if (
          request.method === "GET" &&
          request.url.endsWith("/api/tasks/stats?type=transcode")
        ) {
          return Promise.resolve(
            Response.json({
              by_status: { completed: 2, error: 1, running: 1 },
              by_type: { transcode: 4 },
              total: 4,
            }),
          );
        }
        if (
          request.method === "POST" &&
          request.url.endsWith("/api/tasks/task-running/cancel")
        ) {
          return Promise.resolve(
            Response.json({
              created_at: "2026-07-01T10:00:00Z",
              error: null,
              id: "task-running",
              priority: 10,
              progress: 0.5,
              space_id: "space-default",
              status: "canceled",
              type: "transcode",
              updated_at: "2026-07-01T10:02:00Z",
            }),
          );
        }
        if (
          request.method === "POST" &&
          request.url.endsWith("/api/tasks/task-failed/retry")
        ) {
          return Promise.resolve(
            Response.json({
              created_at: "2026-07-01T10:00:00Z",
              error: null,
              id: "task-failed",
              priority: 10,
              progress: 0,
              space_id: "space-default",
              status: "pending",
              type: "transcode",
              updated_at: "2026-07-01T10:02:00Z",
            }),
          );
        }
        return Promise.resolve(
          Response.json(
            { code: "NOT_FOUND", message: "未命中测试接口" },
            { status: 404 },
          ),
        );
      },
      space: { spaceId: "space-default" },
    });

    const page = await listTasks(client, {
      page: 2,
      pageSize: 5,
      status: "failed",
      type: "transcode",
    });
    const detail = await getTask(client, "task-failed");
    const systemTask = await getTask(client, "task-system");
    const stats = await getTaskStats(client, { type: "transcode" });
    const canceled = await cancelTask(client, "task-running");
    const retried = await retryTask(client, "task-failed");

    expect(page).toMatchObject({
      page: 2,
      pageSize: 5,
      total: 1,
      items: [{ id: "task-failed", status: "failed" }],
    });
    expect(detail.status).toBe("failed");
    expect(systemTask.type).toBe("cache.cleanup");
    expect(systemTask.spaceId).toBeNull();
    expect(stats.byStatus).toMatchObject({
      failed: 1,
      running: 1,
      succeeded: 2,
    });
    expect(stats.byType).toEqual({ transcode: 4 });
    expect(canceled.status).toBe("canceled");
    expect(retried.status).toBe("pending");
    expect(
      requests.map(
        (request) => `${request.method} ${new URL(request.url).pathname}`,
      ),
    ).toEqual([
      "GET /api/tasks",
      "GET /api/tasks/task-failed",
      "GET /api/tasks/task-system",
      "GET /api/tasks/stats",
      "POST /api/tasks/task-running/cancel",
      "POST /api/tasks/task-failed/retry",
    ]);
  });

  it("切换 Space 后读取不同媒体列表", async () => {
    const { createMockFetch } = (await import(
      new URL("../../mock/src/index.ts", import.meta.url).href
    )) as {
      readonly createMockFetch: () => FetchLike;
    };
    const mockFetch = createMockFetch();
    const defaultClient = createApiClient({
      fetch: mockFetch,
      space: { spaceId: "space-default" },
    });
    const studioClient = defaultClient.withSpace({ spaceId: "space-studio" });

    const defaultPage = await listMedia(defaultClient, {
      page: 1,
      pageSize: 10,
    });
    const studioPage = await listMedia(studioClient, { page: 1, pageSize: 10 });

    expect(defaultPage.items.map((item) => item.spaceId)).toEqual([
      "space-default",
      "space-default",
    ]);
    expect(studioPage.items.map((item) => item.spaceId)).toEqual([
      "space-studio",
    ]);
  });

  it("网络错误会转成 ApiError", async () => {
    const client = createApiClient({
      fetch: () => Promise.reject(new TypeError("failed")),
      space: { spaceId: "space-default" },
    });

    await expect(client.request("/api/v2/media")).rejects.toBeInstanceOf(
      ApiError,
    );
    await expect(client.request("/api/v2/media")).rejects.toMatchObject({
      code: "NETWORK_ERROR",
      status: 0,
    });
  });

  it("可配置重试策略并在临时失败后成功", async () => {
    let attempts = 0;
    const client = createApiClient({
      fetch: () => {
        attempts += 1;
        if (attempts === 1) {
          return Promise.resolve(
            Response.json(
              { code: "SERVER_BUSY", message: "服务繁忙" },
              { status: 503 },
            ),
          );
        }
        return Promise.resolve(Response.json({ ok: true }));
      },
      retry: { attempts: 2 },
      space: { spaceId: "space-default" },
    });

    await expect(client.request("/api/v2/retry")).resolves.toEqual({
      ok: true,
    });
    expect(attempts).toBe(2);
  });

  it("客户端错误不会触发重试", async () => {
    let attempts = 0;
    const client = createApiClient({
      fetch: () => {
        attempts += 1;
        return Promise.resolve(
          Response.json(
            { code: "BAD_REQUEST", message: "请求无效" },
            { status: 400 },
          ),
        );
      },
      retry: { attempts: 3 },
      space: { spaceId: "space-default" },
    });

    await expect(client.request("/api/v2/bad-request")).rejects.toMatchObject({
      code: "BAD_REQUEST",
      status: 400,
    });
    expect(attempts).toBe(1);
  });

  it("按 session、event_seq 与 expected_revision 读写观看状态", async () => {
    const requests: Request[] = [];
    const client = createApiClient({
      baseUrl: "https://mock.local",
      fetch: (input, init) => {
        const request = new Request(input, init);
        requests.push(request);
        if (request.method === "GET") {
          return Promise.resolve(
            Response.json({
              completed: false,
              completed_at: null,
              last_event_seq: 0,
              last_session_id: "",
              last_watched_at: "0001-01-01T00:00:00Z",
              media_id: 9,
              position_seconds: 0,
              revision: 0,
              space_id: "space-default",
            }),
          );
        }
        return Promise.resolve(
          Response.json({
            applied: true,
            current: {
              completed: false,
              completed_at: null,
              last_event_seq: 3,
              last_session_id: "session-a",
              last_watched_at: "2026-07-15T10:00:00Z",
              media_id: 9,
              position_seconds: 42,
              revision: 1,
              space_id: "space-default",
            },
          }),
        );
      },
      space: { spaceId: "space-default" },
    });

    const initial = await getWatchState(client, "9");
    const updated = await updateWatchState(client, "9", {
      durationSeconds: 120,
      eventSeq: 3,
      eventType: "seek",
      expectedRevision: initial.revision,
      positionSeconds: 42,
      reason: "user",
      sessionId: "session-a",
    });

    expect(initial).toMatchObject({ mediaId: "9", revision: 0 });
    expect(updated).toMatchObject({
      applied: true,
      current: { eventSeq: 3, positionSeconds: 42, revision: 1 },
    });
    expect(
      requests.map((request) => `${request.method} ${request.url}`),
    ).toEqual([
      "GET https://mock.local/api/play/9/watch-state",
      "PUT https://mock.local/api/play/9/watch-state",
    ]);
    expect(await requests[1]?.json()).toEqual({
      duration_seconds: 120,
      event_seq: 3,
      event_type: "seek",
      expected_revision: 0,
      position_seconds: 42,
      reason: "user",
      session_id: "session-a",
    });
  });

  it("409 冲突保留 WATCH_STATE_CONFLICT 与 current 且不重试", async () => {
    let attempts = 0;
    const client = createApiClient({
      fetch: () => {
        attempts += 1;
        return Promise.resolve(
          Response.json(
            {
              applied: false,
              code: "WATCH_STATE_CONFLICT",
              current: {
                completed: false,
                completed_at: null,
                last_event_seq: 4,
                last_session_id: "session-b",
                last_watched_at: "2026-07-15T10:01:00Z",
                media_id: 9,
                position_seconds: 55,
                revision: 2,
                space_id: "space-default",
              },
              message: "观看状态已被其他会话更新",
            },
            { status: 409 },
          ),
        );
      },
      retry: { attempts: 3 },
      space: { spaceId: "space-default" },
    });

    const error = await updateWatchState(client, "9", {
      eventSeq: 5,
      eventType: "pause",
      expectedRevision: 1,
      positionSeconds: 80,
      reason: "system",
      sessionId: "session-a",
    }).catch((caught: unknown) => caught);

    expect(error).toBeInstanceOf(WatchStateConflictError);
    expect(error).toMatchObject({
      code: "WATCH_STATE_CONFLICT",
      current: { positionSeconds: 55, revision: 2 },
      status: 409,
    });
    expect(attempts).toBe(1);
  });

  it("观看历史游标与继续观看返回同一观看状态 DTO", async () => {
    const requests: URL[] = [];
    const client = createApiClient({
      fetch: (input) => {
        const url = new URL(
          input instanceof Request ? input.url : input.toString(),
        );
        requests.push(url);
        const response = {
          items: [
            {
              media: { file_name: "demo.mp4", id: 9 },
              watch_state: {
                completed: false,
                completed_at: null,
                last_event_seq: 3,
                last_session_id: "session-a",
                last_watched_at: "2026-07-15T10:00:00Z",
                media_id: 9,
                position_seconds: 42,
                revision: 1,
                space_id: "space-default",
              },
            },
          ],
          ...(url.pathname.endsWith("/watch-history")
            ? { next_cursor: "cursor-a" }
            : {}),
        };
        return Promise.resolve(Response.json(response));
      },
      space: { spaceId: "space-default" },
    });

    const history = await listWatchHistory<{
      readonly file_name: string;
      readonly id: number;
    }>(client, {
      cursor: "cursor-old",
      limit: 5,
    });
    const continued = await listContinueWatching<{
      readonly file_name: string;
      readonly id: number;
    }>(client, 12);

    expect(history).toMatchObject({
      items: [
        { media: { id: 9 }, watchState: { positionSeconds: 42, revision: 1 } },
      ],
      nextCursor: "cursor-a",
    });
    expect(continued[0]).toMatchObject({
      media: { id: 9 },
      watchState: history.items[0]?.watchState,
    });
    expect(requests.map((url) => `${url.pathname}${url.search}`)).toEqual([
      "/api/library/watch-history?cursor=cursor-old&limit=5",
      "/api/library/continue-watching?limit=12",
    ]);
  });

  it("检测 Web、Desktop、Mobile、TV、车机、触控和网络能力", () => {
    const web = detectDeviceCapabilities({
      navigator: { onLine: true, userAgent: "Mozilla/5.0" },
    });
    const desktop = detectDeviceCapabilities({
      navigator: { userAgent: "Mozilla/5.0 (Windows NT 10.0)" },
    });
    const mobile = detectDeviceCapabilities({
      matchMedia: (query) => ({ matches: query === "(pointer: coarse)" }),
      navigator: {
        connection: { effectiveType: "2g", saveData: true },
        maxTouchPoints: 5,
        onLine: true,
        userAgent: "Mozilla/5.0 (Linux; Android 14; Mobile)",
      },
    });
    const tv = detectDeviceCapabilities({
      navigator: { userAgent: "Mozilla/5.0 AppleTV" },
    });
    const automotive = detectDeviceCapabilities({
      navigator: { userAgent: "Mozilla/5.0 Android Automotive" },
    });

    expect(web.platform).toBe("web");
    expect(desktop.platform).toBe("desktop");
    expect(mobile).toMatchObject({
      network: "constrained",
      platform: "mobile",
      pointer: "coarse",
      touch: true,
    });
    expect(tv.platform).toBe("tv");
    expect(automotive.platform).toBe("automotive");
  });
});
