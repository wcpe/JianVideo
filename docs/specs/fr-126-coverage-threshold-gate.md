# 功能规格：前端测试覆盖率阈值门禁

> 状态：开发中　·　关联 PRD：FR-126　·　分支：claude/suspicious-snyder-718ae6

## 1. 背景与目标

`frontend/vitest.config.ts` 已配 coverage（provider v8 + reporter text/lcov + include/exclude），`test:coverage` 脚本已在，但 `@vitest/coverage-v8` 未装、**无 thresholds**、覆盖率不达标不会失败。本 FR 装齐覆盖率驱动、实测当前值、按策略定阈值并写入配置，使覆盖率不达标即失败。属第十三期（P13），依赖 FR-123/124/125（已落地，测试已补强）。CI 接线见 FR-128。

## 2. 需求（要什么）
- 装 `@vitest/coverage-v8`（版本对齐已装 vitest）。
- 实测当前覆盖率（lines/statements/functions/branches）。
- 在 vitest coverage 配置加 `thresholds`：
  - 策略：lines/statements/functions 固定下限 **70%**；若实测更高，则上调到**贴近当前实测的整数下限**（留小安全余量、取整到 5/10）。
  - branches 按实测定，目标约 **60%**（取实测下方的整数下限）。
- `npm run test:coverage` 在阈值下通过（实测 ≥ 阈值）。
- 范围内：`frontend/package.json`（devDep）、`frontend/vitest.config.ts`（thresholds）。
- 不做（范围外）：CI 接线（FR-128）、再补测试（FR-125 已做）。

## 3. 设计（怎么做）
- `npm install -D @vitest/coverage-v8@<vitest 同版本>`。
- `npm run test:coverage` 读 text reporter 的 “All files” 汇总行得四项实测值。
- `vitest.config.ts` 的 `test.coverage` 加：
  ```ts
  thresholds: { lines: L, statements: S, functions: F, branches: B }
  ```
  L/S/F = max(70, floor_to_5(实测-余量))；B = floor_to_5(branches 实测-余量)，约 60。
- 验证 `npm run test:coverage` 退出 0（达标）。构造性验证门禁可挡：临时把某阈值调到高于实测应失败（验证后改回）。

## 4. 任务拆分
- [ ] 装 @vitest/coverage-v8（对齐 vitest 版本）
- [ ] 实测四项覆盖率，记录实际数字
- [ ] 写入 thresholds（按策略定档）
- [ ] `npm run test:coverage` 达标通过
- [ ] `npm run lint`/`format:check` 不回归
- [ ] 文档同步：PRD 状态、CHANGELOG（按需）、在本 spec 记录最终阈值与实测值

## 5. 验收标准（AC-30）
- `npm run test:coverage` 产出覆盖率且不低于配置阈值（lines/statements/functions ≥ 设定值、branches ≥ 设定值）。
- 阈值写入 `vitest.config.ts`。
- 门禁有效（覆盖率低于阈值时该命令失败——以构造性验证确认）。

## 6. 风险 / 待定
- 最终阈值数字依实测确定（见本 spec「实测与定档」节，落地时补）。
- include/exclude 已有（排除 *.test、main.tsx）；阈值针对该范围。
- 若实测某项偏低（如 branches < 60），按实测下方取整设档、不强拔（强拔需 FR-125 再补测，本 FR 不扩范围）。

## 7. 实测与定档（落地时填）
- 实测（baseline，706 用例）：lines 77.99% / statements 77.99% / functions 62.58% / branches 80.93%
- 定档（务实定档，经用户确认）：lines 75 / statements 75 / functions 60 / branches 75
  - lines/statements 实测约 78，取 75 留余量；branches 实测约 81，取 75；functions 实测 62.58 低于 70 线，按实测下方取 60（不强拔，避免额外补测扩范围）。
- 门禁有效性：构造性验证——临时把 functions 阈值调到 99，`test:coverage` 退出码为 1（门禁挡住）；还原 60 后退出 0。
