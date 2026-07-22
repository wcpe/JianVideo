/**
 * 仓库根目录卫生门（FR2-065）。
 * 规则真源见 docs/specs/fr2-065-root-hygiene.md —— 改规则先改 spec 再改本文件。
 *
 * 用法：
 *   node scripts/root-hygiene.mjs [--strict] [--root <path>]
 * 退出码：运行期禁止项违规 → 1；--strict 且存在迁移债务 → 1；否则 0。
 */

import fs from "node:fs";
import path from "node:path";

/** 允许出现在根目录的文件名（精确匹配） */
export const ALLOWED_FILES = new Set([
  ".editorconfig",
  ".gitattributes",
  ".gitignore",
  ".golangci.yml",
  "CHANGELOG.md",
  "LICENSE",
  "Makefile",
  "README.md",
  "SECURITY.md",
  "VERSION",
  "go.mod",
  "go.sum",
  "go.work",
  "go.work.sum",
  "package.json",
  "package-lock.json",
  "pnpm-lock.yaml",
  "pnpm-workspace.yaml",
  "playwright.config.ts",
  "turbo.json",
  "Taskfile.yml",
]);

/** 允许出现在根目录的目录名（精确匹配） */
export const ALLOWED_DIRS = new Set([
  ".git",
  ".github",
  ".claude",
  ".tmp",
  ".turbo",
  ".idea",
  ".narrafork",
  ".vscode",
  "apps",
  "packages",
  "docs",
  "scripts",
  "config",
  "e2e",
  "data",
  "deploy",
  "api",
  "node_modules",
  "dist",
  "build",
  "third_party",
  "backups", // 若配置指向 data 外，仍可能出现；优先 data/backups
]);

/** 根目录禁止的运行期 / 缓存目录名 */
export const FORBIDDEN_RUNTIME_DIRS = new Set([
  "hls",
  "thumbnails",
  "image_cache",
  "covers",
  "metadata_temp",
  "timeline_previews",
  "playwright-report",
  "test-results",
  "coverage",
]);

/** 迁移债务目录（A 期迁完后应为空；若再出现则为违规/债务） */
export const DEBT_DIRS = new Set([]);

/**
 * @param {string} root
 * @param {{ strict?: boolean }} [options]
 * @returns {{ ok: boolean, violations: string[], debt: string[], unknown: string[] }}
 */
export function checkRoot(root, options = {}) {
  const strict = Boolean(options.strict);
  const abs = path.resolve(root);
  const entries = fs.readdirSync(abs, { withFileTypes: true });

  /** @type {string[]} */
  const violations = [];
  /** @type {string[]} */
  const debt = [];
  /** @type {string[]} */
  const unknown = [];

  for (const ent of entries) {
    const name = ent.name;
    if (name === "." || name === "..") continue;

    if (ent.isDirectory()) {
      if (FORBIDDEN_RUNTIME_DIRS.has(name)) {
        violations.push(`禁止的运行期目录: ${name}/`);
        continue;
      }
      if (DEBT_DIRS.has(name)) {
        debt.push(`迁移债务目录: ${name}/`);
        if (!ALLOWED_DIRS.has(name)) {
          // 已在 allow 中标为债务
        }
        continue;
      }
      if (!ALLOWED_DIRS.has(name)) {
        // 工具/临时目录不在 allow 也不在 forbidden：记 unknown，不直接 fail（避免 IDE 目录误杀）
        if (name.startsWith(".")) {
          unknown.push(`未登记隐藏目录: ${name}/`);
        } else {
          violations.push(`未允许的根目录: ${name}/`);
        }
      }
      continue;
    }

    if (ent.isFile() || ent.isSymbolicLink()) {
      if (name === "coverage.out" || name.endsWith(".out")) {
        if (name === "coverage.out" || name.endsWith(".test")) {
          violations.push(`禁止的根级产物: ${name}`);
          continue;
        }
      }
      if (/\.db(-wal|-shm)?$/i.test(name)) {
        violations.push(`禁止的根级数据库文件: ${name}（应位于 data/）`);
        continue;
      }
      if (name.endsWith(".go")) {
        debt.push(`迁移债务文件: ${name}`);
        continue;
      }
      if (!ALLOWED_FILES.has(name)) {
        if (name.startsWith(".")) {
          unknown.push(`未登记隐藏文件: ${name}`);
        } else if (
          name.endsWith(".log") ||
          name.endsWith(".exe") ||
          name === "jianvideo" ||
          name.startsWith("jianvideo-")
        ) {
          violations.push(`禁止的根级产物: ${name}`);
        } else {
          unknown.push(`未登记根文件: ${name}`);
        }
      }
    }
  }

  const ok =
    violations.length === 0 && (!strict || debt.length === 0);

  return { ok, violations, debt, unknown };
}

/**
 * CLI 入口
 * @param {string[]} argv
 * @param {{ cwd?: string, log?: (s: string) => void }} [io]
 * @returns {number} exit code
 */
export function main(argv = process.argv.slice(2), io = {}) {
  const log = io.log ?? console.log;
  let strict = false;
  let root = io.cwd ?? process.cwd();

  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === "--strict") strict = true;
    else if (a === "--root") {
      root = argv[++i] ?? root;
    } else if (a === "--help" || a === "-h") {
      log(
        "用法: node scripts/root-hygiene.mjs [--strict] [--root <path>]\n" +
          "  默认债务模式：运行期垃圾 FAIL，main.go/internal/frontend 仅报告。\n" +
          "  --strict：迁移债务也 FAIL（FR2-067 后使用）。",
      );
      return 0;
    }
  }

  const result = checkRoot(root, { strict });
  if (result.violations.length) {
    log("根目录卫生：发现禁止项");
    for (const v of result.violations) log(`  ✗ ${v}`);
  }
  if (result.debt.length) {
    log(strict ? "根目录卫生：迁移债务（strict 下失败）" : "根目录卫生：迁移债务（暂不阻断）");
    for (const d of result.debt) log(`  · ${d}`);
  }
  if (result.unknown.length) {
    log("根目录卫生：未登记项（信息）");
    for (const u of result.unknown) log(`  ? ${u}`);
  }
  if (result.ok) {
    log(
      strict
        ? "根目录卫生：通过（strict）"
        : "根目录卫生：通过（债务模式）",
    );
    return 0;
  }
  log("根目录卫生：未通过");
  return 1;
}

const isDirect =
  process.argv[1] &&
  path.resolve(process.argv[1]) === path.resolve(new URL(import.meta.url).pathname);

// Windows 下 import.meta.url 路径与 argv[1] 比较可能不一致，用更稳妥方式：
const invoked =
  Boolean(process.argv[1]) &&
  path.basename(process.argv[1]).replace(/\\/g, "/") === "root-hygiene.mjs";

if (invoked) {
  process.exitCode = main();
}
