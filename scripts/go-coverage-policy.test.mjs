import assert from "node:assert/strict";
import test from "node:test";

import { minimumForPackage } from "./go-coverage-policy.mjs";

test("组合根与 SMB 使用显式覆盖率下限", () => {
  assert.equal(minimumForPackage("github.com/wcpe/JianVideo", 60), 5);
  assert.equal(minimumForPackage("github.com/wcpe/JianVideo/internal/smb", 60), 25);
  assert.equal(minimumForPackage("github.com/wcpe/JianVideo/internal/db/models", 60), 5);
});

test("其他 Go 包沿用默认覆盖率下限", () => {
  assert.equal(minimumForPackage("github.com/wcpe/JianVideo/internal/api", 60), 60);
});
