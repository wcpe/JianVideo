#!/usr/bin/env node
import { spawnSync } from "node:child_process";

const defaultMinimum = Number(process.env.GO_COVERAGE_MIN ?? "60");
const packageMinimums = new Map([
  ["github.com/wcpe/JianVideo/internal/smb", 25],
]);

const goEnv = { ...process.env, GIN_MODE: "release" };
const listed = spawnSync("go", ["list", "./..."], {
  encoding: "utf8",
  env: goEnv,
});

if (listed.status !== 0) {
  if (listed.stdout) {
    process.stdout.write(listed.stdout);
  }
  if (listed.stderr) {
    process.stderr.write(listed.stderr);
  }
  process.exit(listed.status ?? 1);
}

const packages = listed.stdout
  .split(/\r?\n/)
  .map((line) => line.trim())
  .filter((pkg) => pkg && !pkg.includes("/frontend/node_modules/"));

const result = spawnSync("go", ["test", "-cover", "-p", "1", "-parallel", "1", ...packages], {
  encoding: "utf8",
  env: goEnv,
});

if (result.stdout) {
  process.stdout.write(result.stdout);
}
if (result.stderr) {
  process.stderr.write(result.stderr);
}
if (result.status !== 0) {
  process.exit(result.status ?? 1);
}

const coverageLine = /^ok\s+(\S+)\s+(?:[\d.]+s|\([^)]+\))\s+coverage:\s+([\d.]+)%/gm;
const failures = [];
let checked = 0;

for (const match of result.stdout.matchAll(coverageLine)) {
  const pkg = match[1];
  const coverage = Number(match[2]);
  const minimum = packageMinimums.get(pkg) ?? defaultMinimum;
  checked += 1;
  if (coverage < minimum) {
    failures.push({ pkg, coverage, minimum });
  }
}

if (checked === 0) {
  console.error("未读取到 Go 覆盖率结果，覆盖率门禁失败");
  process.exit(1);
}

if (failures.length > 0) {
  console.error("Go 覆盖率门禁未通过：");
  for (const item of failures) {
    console.error(
      `- ${item.pkg}: ${item.coverage.toFixed(1)}% < ${item.minimum.toFixed(1)}%`,
    );
  }
  process.exit(1);
}

console.log(
  `Go 覆盖率门禁通过：检查 ${checked} 个包，默认阈值 ${defaultMinimum.toFixed(1)}%`,
);
