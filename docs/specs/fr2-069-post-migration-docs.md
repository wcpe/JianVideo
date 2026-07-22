# 功能规格：迁移后真貌文档同步

> 状态：开发中　·　关联 PRD：FR2-069　·　阶段：对齐-A　·　分支：未指定

## 1. 背景与目标

FR2-066/067/068 落地后，ARCHITECTURE、architecture-invariants、README 须与代码真貌一致，删除根单体 / `frontend/` 源码树等过期叙述。

## 2. 需求（要什么）

- 范围内：ARCHITECTURE §1–2 总览与目录树、invariants 落点与依赖方向、README 状态/结构/快速开始。
- 不做：改业务代码；批量改写历史 specs/ADR 正文（仅现行真貌入口对齐）。

## 3. 设计（怎么做）

- ARCHITECTURE 运行时图画 `apps/server` + embed `web/dist` + `apps/web`。
- invariants：根禁止业务 Go；`library` 命名与代码一致；`packages` 不反向依赖 apps。
- README：版本以 `VERSION` 为准；构建命令与 FR2-068 一致。

## 4. 任务拆分

- [x] ARCHITECTURE §2 图与模块表落点说明
- [x] architecture-invariants §0/§1/红线
- [x] README 全文对齐
- [x] PRD/CHANGELOG/本 spec

## 5. 验收标准

- 文档描述与 `ls apps/`、`go.work`、无根 `main.go` 一致。
- 无「cd frontend / go run main.go」作为现行推荐路径。

## 6. 风险 / 待定

- 历史 ADR/旧 specs 仍含 `frontend/` 字样，作历史查询保留。
