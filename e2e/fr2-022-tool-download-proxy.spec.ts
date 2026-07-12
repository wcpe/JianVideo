import { test, expect, type Page } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { createServer, type Server } from "node:http";
import {
  existsSync,
  mkdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import { gzipSync } from "node:zlib";
import { login } from "./helpers";

test.use({ serviceWorkers: "block" });

const screenshotDir = ".tmp/screenshots/fr2-022";
const fixtureDir = ".tmp/fr2-022-tool-fixture";
const toolVersion = "fr2-022-e2e";
const screenshotSuffix = process.env.FR2_022_SCREENSHOT_SUFFIX || "real";

test("工具下载通过自定义本机源安装、写设置并可查任务审计", async ({ page }) => {
  test.setTimeout(120000);
  mkdirSync(screenshotDir, { recursive: true });
  const fixture = buildToolArchive("ffmpeg");
  const source = await serveArchive(fixture.archive);

  try {
    await login(page);
    await page.goto("/system?tab=settings");
    await expect(page.getByRole("heading", { name: "设置" })).toBeVisible();

    await page.getByLabel("自定义下载 URL").fill(source.url);
    await page.getByLabel("自定义 SHA-256").fill(fixture.sha256);
    await page.getByLabel("自定义版本").fill(toolVersion);
    await page.getByText("允许本机 HTTP 测试源").click();
    await expect(
      page.getByRole("switch", { name: "允许本机 HTTP 测试源" }),
    ).toBeChecked();

    const [downloadResponse] = await Promise.all([
      page.waitForResponse(
        (resp) =>
          resp.url().includes("/api/system/tools/download") &&
          resp.request().method() === "POST",
      ),
      page.getByRole("button", { name: "下载工具" }).click(),
    ]);
    expect(downloadResponse.status()).toBe(202);
    const body = (await downloadResponse.json()) as { task_id: string };
    expect(body.task_id).toBeTruthy();

    await waitToolTaskSucceeded(page, body.task_id);
    await expect(
      page.getByText(`任务 ${body.task_id}`, { exact: true }),
    ).toBeVisible({
      timeout: 10000,
    });
    await expect(page.getByText("succeeded").first()).toBeVisible({
      timeout: 10000,
    });

    const settings = await getSettings(page);
    expect(settings.ffmpeg_path).toContain(toolVersion);
    expect(settings.ffmpeg_path.toLowerCase()).toContain("ffmpeg");

    await expect
      .poll(() => hasSystemAudit(page, "task.succeeded", "task"), {
        timeout: 10000,
      })
      .toBe(true);
    await expect
      .poll(() => hasSystemAudit(page, "settings.updated", "settings"), {
        timeout: 10000,
      })
      .toBe(true);

    await page.screenshot({
      path: `${screenshotDir}/tool-download-proxy-${screenshotSuffix}.png`,
      fullPage: true,
    });
  } finally {
    await restoreFFmpegPath(page);
    await source.close();
  }
});

async function restoreFFmpegPath(page: Page) {
  const loginResponse = await page.request.post("/api/auth/login", {
    data: { username: "admin", password: "admin" },
  });
  expect(loginResponse.ok()).toBeTruthy();
  const response = await page.request.put("/api/settings", {
    data: { settings: { ffmpeg_path: "ffmpeg" } },
  });
  expect(response.ok()).toBeTruthy();
}

async function waitToolTaskSucceeded(page: Page, taskID: string) {
  await expect
    .poll(
      async () => {
        const res = await fetchJSON<{
          items?: Array<{ id: string; status: string; error?: string | null }>;
        }>(
          page,
          "/api/tasks?scope=system&type=tool.download&resource_type=tool&resource_id=ffmpeg&page_size=20",
        );
        if (!res.ok) return `http-${res.status}`;
        const items = res.body.items ?? [];
        const task = items.find((item) => item.id === taskID);
        if (!task) return "missing";
        if (task.status === "failed")
          throw new Error(task.error || "工具下载任务失败");
        return task.status;
      },
      { timeout: 45000 },
    )
    .toBe("succeeded");
}

async function getSettings(page: Page): Promise<Record<string, string>> {
  const res = await fetchJSON<{ settings?: Record<string, string> }>(
    page,
    "/api/settings",
  );
  expect(res.ok).toBeTruthy();
  return res.body.settings ?? {};
}

async function hasSystemAudit(
  page: Page,
  action: string,
  resourceType: string,
): Promise<boolean> {
  const res = await fetchJSON<{
    items?: Array<{ action: string; resource_type: string }>;
  }>(
    page,
    `/api/audit/events?scope=system&action=${action}&resource_type=${resourceType}&limit=20`,
  );
  if (!res.ok) return false;
  const items = res.body.items ?? [];
  return items.some(
    (item) => item.action === action && item.resource_type === resourceType,
  );
}

async function fetchJSON<T>(
  page: Page,
  path: string,
): Promise<{ ok: boolean; status: number; body: T }> {
  return page.evaluate(async (url) => {
    const res = await fetch(url, { credentials: "include" });
    const text = await res.text();
    return {
      ok: res.ok,
      status: res.status,
      body: text ? JSON.parse(text) : {},
    };
  }, path) as Promise<{ ok: boolean; status: number; body: T }>;
}

function buildToolArchive(tool: string): { archive: Buffer; sha256: string } {
  mkdirSync(fixtureDir, { recursive: true });
  const ext = process.platform === "win32" ? ".exe" : "";
  const exePath = join(fixtureDir, `${tool}${ext}`);
  if (!existsSync(exePath)) {
    const sourcePath = join(fixtureDir, `${tool}.go`);
    writeFileSync(
      sourcePath,
      `package main

import "fmt"

func main() {
	fmt.Println("${tool} version ${toolVersion}")
}
`,
      "utf8",
    );
    execFileSync("go", ["build", "-o", exePath, sourcePath], {
      stdio: "ignore",
    });
  }
  const entryName = `bin/${tool}${ext}`;
  const archive = gzipSync(tarFile(entryName, readFileSync(exePath), 0o755));
  const sha256 = createHash("sha256").update(archive).digest("hex");
  return { archive, sha256 };
}

function tarFile(name: string, body: Buffer, mode: number): Buffer {
  const header = Buffer.alloc(512, 0);
  header.write(name, 0, 100, "utf8");
  writeOctal(header, mode, 100, 8);
  writeOctal(header, 0, 108, 8);
  writeOctal(header, 0, 116, 8);
  writeOctal(header, body.length, 124, 12);
  writeOctal(header, Math.floor(Date.now() / 1000), 136, 12);
  header.fill(0x20, 148, 156);
  header[156] = "0".charCodeAt(0);
  header.write("ustar\0", 257, 6, "ascii");
  header.write("00", 263, 2, "ascii");
  const checksum = header.reduce((sum, value) => sum + value, 0);
  writeOctal(header, checksum, 148, 8);
  const padding = Buffer.alloc((512 - (body.length % 512)) % 512, 0);
  return Buffer.concat([header, body, padding, Buffer.alloc(1024, 0)]);
}

function writeOctal(
  buffer: Buffer,
  value: number,
  offset: number,
  length: number,
) {
  const text = value
    .toString(8)
    .padStart(length - 1, "0")
    .slice(-(length - 1));
  buffer.write(text, offset, length - 1, "ascii");
  buffer[offset + length - 1] = 0;
}

async function serveArchive(
  archive: Buffer,
): Promise<{ url: string; close: () => Promise<void> }> {
  const server = createServer((req, res) => {
    if (req.url !== "/tool.tar.gz") {
      res.writeHead(404);
      res.end();
      return;
    }
    res.writeHead(200, {
      "Content-Type": "application/gzip",
      "Content-Length": String(archive.length),
    });
    res.end(archive);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string")
    throw new Error("本机下载源监听失败");
  return {
    url: `http://127.0.0.1:${address.port}/tool.tar.gz`,
    close: () => closeServer(server),
  };
}

async function closeServer(server: Server): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    server.close((err) => {
      if (err) reject(err);
      else resolve();
    });
  });
  if (existsSync(fixtureDir)) {
    rmSync(fixtureDir, { recursive: true, force: true });
  }
}
