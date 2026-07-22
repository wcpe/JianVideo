import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { checkRoot, main } from "./root-hygiene.mjs";

function makeFixture(names) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "root-hygiene-"));
  for (const name of names) {
    const p = path.join(dir, name);
    if (name.endsWith("/")) {
      fs.mkdirSync(p, { recursive: true });
    } else {
      fs.writeFileSync(p, "x\n");
    }
  }
  return dir;
}

test("干净工程根通过（债务模式）", () => {
  const dir = makeFixture([
    "VERSION",
    "Makefile",
    "go.mod",
    "package.json",
    "pnpm-workspace.yaml",
    "README.md",
    "apps/",
    "packages/",
    "docs/",
    "scripts/",
    "config/",
    "data/",
  ]);
  const r = checkRoot(dir, { strict: false });
  assert.equal(r.ok, true);
  assert.deepEqual(r.violations, []);
});

test("根级数据库文件失败", () => {
  const dir = makeFixture(["VERSION", "jianvideo.db", "docs/"]);
  const r = checkRoot(dir);
  assert.equal(r.ok, false);
  assert.ok(r.violations.some((v) => v.includes("jianvideo.db")));
});

test("根级 hls 目录失败", () => {
  const dir = makeFixture(["VERSION", "hls/", "docs/"]);
  const r = checkRoot(dir);
  assert.equal(r.ok, false);
  assert.ok(r.violations.some((v) => v.includes("hls")));
});

test("根级 main.go 记为债务（迁完后不应出现）", () => {
  const dir = makeFixture(["VERSION", "main.go", "docs/", "apps/"]);
  const r = checkRoot(dir, { strict: false });
  // 无 DEBT_DIRS 时 .go 仍进 debt 列表；ok 在非 strict 下仍为 true
  assert.equal(r.ok, true);
  assert.ok(r.debt.some((d) => d.includes("main.go")));
});

test("strict 模式根级 main.go 失败", () => {
  const dir = makeFixture(["VERSION", "main.go", "docs/"]);
  const r = checkRoot(dir, { strict: true });
  assert.equal(r.ok, false);
  assert.ok(r.debt.some((d) => d.includes("main.go")));
});

test("data 目录允许", () => {
  const dir = makeFixture(["VERSION", "data/", "docs/"]);
  const r = checkRoot(dir);
  assert.equal(r.ok, true);
});

test("CLI 对禁止项返回 1", () => {
  const dir = makeFixture(["coverage.out", "VERSION"]);
  const logs = [];
  const code = main([], { cwd: dir, log: (s) => logs.push(s) });
  // 无 argv 时用 cwd；但 main 默认 process.cwd，需传 --root
  const code2 = main(["--root", dir], { log: (s) => logs.push(s) });
  assert.equal(code2, 1);
  void code;
});

test("CLI 干净根返回 0", () => {
  const dir = makeFixture([
    "VERSION",
    "Makefile",
    "docs/",
    "apps/",
    "packages/",
    "scripts/",
  ]);
  const code = main(["--root", dir], { log: () => {} });
  assert.equal(code, 0);
});
