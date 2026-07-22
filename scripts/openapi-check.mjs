/**
 * OpenAPI 契约结构校验与最小路径门禁（FR2-071）。
 * 不依赖 oapi-codegen / 第三方 YAML 包：用轻量解析提取 paths 键并对照必选清单。
 * 另校验 media-client / mock 生产源中的 /api/v2 路径与契约对齐（无新 npm 依赖、无 TS 生成）。
 *
 * 用法：
 *   node scripts/openapi-check.mjs [--root <path>]
 * 退出码：契约缺失或结构不合法 / 必选路径缺失 / client 路径漂移 → 1；否则 0。
 */

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

/** 首切必须出现在 openapi.yaml 中的路径（与 media-client / 探活 / 认证对齐） */
export const REQUIRED_PATHS = [
  "/health",
  "/api/auth/login",
  "/api/auth/logout",
  "/api/auth/setup-status",
  "/api/auth/setup",
  "/api/v2/media",
  "/api/v2/media/{id}",
  "/api/v2/tasks/{id}",
];

/**
 * client/mock 生产源必须保留的契约路径针（防手改丢路径）。
 * path 参数在源码中常写为 `/prefix/` 或 `/prefix/${...}`，用子串即可。
 * 不做全量历史端点；仅覆盖 openapi 首切中的 v2 表面。
 */
export const CLIENT_PATH_SURFACE = [
  {
    file: "packages/media-client/src/index.ts",
    // listMedia + getMedia；tasks 仍走 /api/tasks（历史），不强制 v2
    mustContain: ["/api/v2/media"],
  },
  {
    file: "packages/mock/src/index.ts",
    mustContain: ["/api/v2/media", "/api/v2/tasks/"],
  },
];

/**
 * 从 OpenAPI YAML 文本中提取顶层 paths 下的路径键（仅键名，不做完整 YAML 语义解析）。
 * 约定：paths: 后缩进一层的以 `/` 开头的行即为路径键。
 */
export function extractPathKeys(yamlText) {
  const lines = yamlText.split(/\r?\n/);
  let inPaths = false;
  let pathsIndent = 0;
  const keys = [];

  for (const line of lines) {
    if (/^\s*#/.test(line) || line.trim() === "") {
      continue;
    }
    if (!inPaths) {
      const m = line.match(/^(\s*)paths:\s*$/);
      if (m) {
        inPaths = true;
        pathsIndent = m[1].length;
      }
      continue;
    }
    const indent = line.match(/^(\s*)/)[1].length;
    if (indent <= pathsIndent && line.trim() !== "") {
      // 离开 paths 块
      break;
    }
    // 路径键：相对 paths 多一级缩进，以 / 开头
    const keyMatch = line.match(/^(\s+)(\/[^\s:]*):\s*$/);
    if (keyMatch && keyMatch[1].length === pathsIndent + 2) {
      keys.push(keyMatch[2]);
    }
  }
  return keys;
}

/** 校验 openapi 文本是否具备最小真源形态 */
export function validateOpenApiSource(yamlText) {
  const errors = [];
  if (!/^openapi:\s*3\.\d+\.\d+\s*$/m.test(yamlText) && !/^openapi:\s*['"]?3\./m.test(yamlText)) {
    errors.push("缺少 openapi: 3.x 声明");
  }
  if (!/^\s*info:\s*$/m.test(yamlText)) {
    errors.push("缺少 info 块");
  }
  if (!/^\s*paths:\s*$/m.test(yamlText)) {
    errors.push("缺少 paths 块");
  }
  if (!/^\s*components:\s*$/m.test(yamlText)) {
    errors.push("缺少 components 块");
  }
  const paths = extractPathKeys(yamlText);
  if (paths.length === 0) {
    errors.push("paths 下未解析到任何路径键");
  }
  for (const required of REQUIRED_PATHS) {
    if (!paths.includes(required)) {
      errors.push(`缺少必选路径: ${required}`);
    }
  }
  return { ok: errors.length === 0, errors, paths };
}

/**
 * 从 TS 源码提取 `/api/v2/...` 路径字面量（模板字符串与普通字符串）。
 * 去掉 query；`${...}` 归一为 `{param}`。
 * 注意：须先归一插值再清理，避免 encodeURIComponent(id) 内 `)` 截断路径。
 */
export function extractApiV2PathLiterals(sourceText) {
  const found = new Set();
  // 匹配 "/api/v2/..." 或 `/api/v2/...`（允许中间 ${...}）
  const re = /["'`](\/api\/v2\/(?:[^"'`${?]|\$\{[^}]*\})*)/g;
  let m;
  while ((m = re.exec(sourceText)) !== null) {
    let p = m[1];
    // 模板插值 → {param}（须在任何括号清理之前）
    p = p.replace(/\$\{[^}]+\}/g, "{param}");
    // 去掉 query
    const q = p.indexOf("?");
    if (q !== -1) {
      p = p.slice(0, q);
    }
    if (p.length > 0) {
      found.add(p);
    }
  }
  return [...found].sort();
}

/**
 * 判断源码中的 v2 路径是否被 openapi 路径覆盖。
 * 例：/api/v2/media/{param}、/api/v2/media/ 可匹配 /api/v2/media/{id}
 */
export function isV2PathCoveredByOpenApi(sourcePath, openapiPaths) {
  const candidates = openapiPaths.filter((p) => p.startsWith("/api/v2/"));
  const src = sourcePath.replace(/\/$/, ""); // 去掉仅用于 startsWith 的尾斜杠后再比模板

  for (const op of candidates) {
    const opNorm = op.replace(/\{[^}]+\}/g, "{param}");
    if (src === opNorm || src === op) {
      return true;
    }
    // 源为列表路径、契约同时有详情：/api/v2/media 覆盖契约 /api/v2/media
    if (src === opNorm.replace(/\/\{param\}$/, "")) {
      return true;
    }
    // 源为 /api/v2/media 且契约为 /api/v2/media/{id} 的「列表」已由上条；
    // 源为 /api/v2/media/ 或 /api/v2/media/{param} → 详情
    const opBase = opNorm.replace(/\/\{param\}$/, "");
    if (
      (sourcePath === `${opBase}/` || src === `${opBase}/{param}`) &&
      /\{[^}]+\}$/.test(op)
    ) {
      return true;
    }
  }
  return false;
}

/**
 * 校验 client/mock 生产源与 openapi v2 路径对齐。
 * @param {string} root 仓库根
 * @param {string[]} openapiPaths 契约 paths 键
 */
export function checkClientPathSurface(root, openapiPaths) {
  const errors = [];
  const details = [];

  for (const surface of CLIENT_PATH_SURFACE) {
    const abs = path.join(root, surface.file);
    if (!fs.existsSync(abs)) {
      errors.push(`client 表面文件不存在: ${surface.file}`);
      continue;
    }
    const text = fs.readFileSync(abs, "utf8");
    for (const needle of surface.mustContain) {
      if (!text.includes(needle)) {
        errors.push(`${surface.file} 缺少契约路径引用: ${needle}`);
      }
    }
    const literals = extractApiV2PathLiterals(text);
    for (const lit of literals) {
      if (!isV2PathCoveredByOpenApi(lit, openapiPaths)) {
        errors.push(
          `${surface.file} 含未在 openapi 声明的 /api/v2 路径: ${lit}`,
        );
      }
    }
    details.push({ file: surface.file, literals, ok: true });
  }

  return { ok: errors.length === 0, errors, details };
}

function parseArgs(argv) {
  let root = path.resolve(__dirname, "..");
  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === "--root" && argv[i + 1]) {
      root = path.resolve(argv[++i]);
    }
  }
  return { root };
}

export function checkOpenApiAtRoot(root) {
  const file = path.join(root, "api", "openapi.yaml");
  if (!fs.existsSync(file)) {
    return {
      ok: false,
      errors: [`契约文件不存在: ${path.relative(root, file) || file}`],
      paths: [],
      file,
    };
  }
  const text = fs.readFileSync(file, "utf8");
  const result = validateOpenApiSource(text);
  if (!result.ok) {
    return { ...result, file };
  }
  const client = checkClientPathSurface(root, result.paths);
  return {
    ok: client.ok,
    errors: client.errors,
    paths: result.paths,
    file,
    client,
  };
}

function main() {
  const { root } = parseArgs(process.argv.slice(2));
  const result = checkOpenApiAtRoot(root);
  if (!result.ok) {
    console.error("[openapi-check] 失败：");
    for (const e of result.errors) {
      console.error(`  - ${e}`);
    }
    process.exit(1);
  }
  const clientNote = result.client
    ? `；client/mock v2 路径对齐（${CLIENT_PATH_SURFACE.length} 个表面）`
    : "";
  console.log(
    `[openapi-check] 通过：${path.relative(root, result.file)} 共 ${result.paths.length} 条路径（必选 ${REQUIRED_PATHS.length} 条齐全）${clientNote}`,
  );
}

const isMain =
  process.argv[1] &&
  path.resolve(process.argv[1]) === path.resolve(fileURLToPath(import.meta.url));
if (isMain) {
  main();
}
