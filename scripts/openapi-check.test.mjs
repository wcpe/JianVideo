/**
 * openapi-check 单元测试（node:test）。
 */
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { describe, it } from "node:test";
import { fileURLToPath } from "node:url";

import {
  CLIENT_PATH_SURFACE,
  REQUIRED_PATHS,
  checkClientPathSurface,
  checkOpenApiAtRoot,
  extractApiV2PathLiterals,
  extractPathKeys,
  isV2PathCoveredByOpenApi,
  validateOpenApiSource,
} from "./openapi-check.mjs";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

const MINIMAL_GOOD = `
openapi: 3.0.3
info:
  title: t
  version: 0.0.0
paths:
  /health:
    get:
      responses:
        "200":
          description: ok
  /api/auth/login:
    post:
      responses:
        "200":
          description: ok
  /api/auth/logout:
    post:
      responses:
        "200":
          description: ok
  /api/auth/setup-status:
    get:
      responses:
        "200":
          description: ok
  /api/auth/setup:
    post:
      responses:
        "200":
          description: ok
  /api/v2/media:
    get:
      responses:
        "200":
          description: ok
  /api/v2/media/{id}:
    get:
      responses:
        "200":
          description: ok
  /api/v2/tasks/{id}:
    get:
      responses:
        "200":
          description: ok
components:
  schemas: {}
`;

describe("extractPathKeys", () => {
  it("提取 paths 下以 / 开头的键", () => {
    const keys = extractPathKeys(MINIMAL_GOOD);
    assert.ok(keys.includes("/health"));
    assert.ok(keys.includes("/api/v2/media/{id}"));
    assert.equal(keys.length, REQUIRED_PATHS.length);
  });

  it("忽略注释与 components 内伪路径", () => {
    const yaml = `
openapi: 3.0.3
info:
  title: t
  version: 0
paths:
  /health:
    get: {}
components:
  schemas:
    /not-a-path:
      type: object
`;
    assert.deepEqual(extractPathKeys(yaml), ["/health"]);
  });
});

describe("validateOpenApiSource", () => {
  it("完整首切契约通过", () => {
    const r = validateOpenApiSource(MINIMAL_GOOD);
    assert.equal(r.ok, true, r.errors.join("; "));
  });

  it("缺少必选路径失败", () => {
    const yaml = `
openapi: 3.0.3
info:
  title: t
  version: 0
paths:
  /health:
    get: {}
components:
  schemas: {}
`;
    const r = validateOpenApiSource(yaml);
    assert.equal(r.ok, false);
    assert.ok(r.errors.some((e) => e.includes("/api/v2/media")));
  });
});

describe("extractApiV2PathLiterals / isV2PathCoveredByOpenApi", () => {
  it("提取模板与普通字符串中的 /api/v2 路径", () => {
    const src = `
      const a = \`/api/v2/media?\${q}\`;
      const b = \`/api/v2/media/\${encodeURIComponent(id)}\`;
      if (p.startsWith("/api/v2/tasks/")) {}
      const legacy = "/api/tasks";
    `;
    const lits = extractApiV2PathLiterals(src);
    assert.ok(lits.includes("/api/v2/media"));
    assert.ok(lits.some((x) => x.includes("/api/v2/media/")));
    assert.ok(lits.some((x) => x.startsWith("/api/v2/tasks")));
    assert.ok(!lits.some((x) => x.includes("/api/tasks")));
  });

  it("openapi 模板覆盖源码路径", () => {
    const paths = ["/api/v2/media", "/api/v2/media/{id}", "/api/v2/tasks/{id}"];
    assert.equal(isV2PathCoveredByOpenApi("/api/v2/media", paths), true);
    assert.equal(isV2PathCoveredByOpenApi("/api/v2/media/", paths), true);
    assert.equal(
      isV2PathCoveredByOpenApi("/api/v2/media/{param}", paths),
      true,
    );
    assert.equal(isV2PathCoveredByOpenApi("/api/v2/tasks/", paths), true);
    assert.equal(isV2PathCoveredByOpenApi("/api/v2/unknown", paths), false);
  });
});

describe("checkClientPathSurface", () => {
  it("仓库 media-client/mock 与首切 openapi 对齐", () => {
    const r = checkClientPathSurface(repoRoot, REQUIRED_PATHS);
    assert.equal(r.ok, true, (r.errors || []).join("; "));
    assert.equal(CLIENT_PATH_SURFACE.length, 2);
  });

  it("缺少 mustContain 针时报错", () => {
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "openapi-client-"));
    const pkg = path.join(tmp, "packages", "media-client", "src");
    fs.mkdirSync(pkg, { recursive: true });
    fs.writeFileSync(path.join(pkg, "index.ts"), "export const x = 1;\n");
    const mockPkg = path.join(tmp, "packages", "mock", "src");
    fs.mkdirSync(mockPkg, { recursive: true });
    fs.writeFileSync(
      path.join(mockPkg, "index.ts"),
      'const p = "/api/v2/media";\nconst t = "/api/v2/tasks/";\n',
    );
    const r = checkClientPathSurface(tmp, REQUIRED_PATHS);
    assert.equal(r.ok, false);
    assert.ok(r.errors.some((e) => e.includes("media-client") && e.includes("/api/v2/media")));
  });

  it("未声明的 /api/v2 路径时报错", () => {
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "openapi-client-"));
    const media = path.join(tmp, "packages", "media-client", "src");
    fs.mkdirSync(media, { recursive: true });
    fs.writeFileSync(
      path.join(media, "index.ts"),
      'const a = "/api/v2/media";\nconst b = "/api/v2/ghost";\n',
    );
    const mock = path.join(tmp, "packages", "mock", "src");
    fs.mkdirSync(mock, { recursive: true });
    fs.writeFileSync(
      path.join(mock, "index.ts"),
      'const a = "/api/v2/media";\nconst t = "/api/v2/tasks/";\n',
    );
    const r = checkClientPathSurface(tmp, REQUIRED_PATHS);
    assert.equal(r.ok, false);
    assert.ok(r.errors.some((e) => e.includes("/api/v2/ghost")));
  });
});

describe("checkOpenApiAtRoot", () => {
  it("仓库根 api/openapi.yaml 通过门禁（含 client 对齐）", () => {
    const r = checkOpenApiAtRoot(repoRoot);
    assert.equal(r.ok, true, (r.errors || []).join("; "));
    for (const p of REQUIRED_PATHS) {
      assert.ok(r.paths.includes(p), `缺少 ${p}`);
    }
    assert.ok(r.client?.ok);
  });

  it("契约文件缺失时报错", () => {
    const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "openapi-check-"));
    const r = checkOpenApiAtRoot(tmp);
    assert.equal(r.ok, false);
    assert.ok(r.errors[0].includes("不存在"));
  });
});
