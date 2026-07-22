# 功能规格：架构对齐蓝图（参考 JianArtifact）

> 状态：已交付@v0.25.0　·　关联 PRD：FR2-064　·　阶段：对齐-文档

## 1. 背景与目标

JianVideo 已在 ADR-0054 / ADR-0060 冻结 `apps/*` + `packages/*` 目标，但代码真貌仍是根 `main.go` + `internal/*` + `frontend/`，根目录堆有业务 Go、运行期缓存与双轨配置。参考 JianArtifact 的 monorepo 落点与卫生约定，需要一份可执行的分期蓝图，避免「FR2-002 已交付」被误解为物理迁移已完成。

目标：

- 在 PRD 登记对齐-A～D 的 FR 与依赖序（已完成登记 FR2-065～072）。
- 本 spec 作为蓝图索引：指向各期详细 spec，不重复实现细节。
- 明确兼容合同与默认不做项。

## 2. 需求（要什么）

- 范围内：
  - PRD 功能登记册含 FR2-064～072 及依赖说明。
  - specs 索引可导航到各期规格。
  - 澄清 FR2-002 = 工作区/职责冻结，≠ 根单体与 `frontend/` 已迁完。
- 不做：本 FR 不搬代码、不改运行时行为（实现由后续 FR 承担）。

## 3. 设计（怎么做）

分期与依赖（与 PRD 一致）：

```text
FR2-064（本蓝图）
  → FR2-065 根目录卫生
    → FR2-066 apps/web
      → FR2-067 apps/server
        → FR2-068 工具链 ∥ FR2-069 真貌文档
          → FR2-070 分层 ∥ FR2-071 OpenAPI ∥ FR2-072 deploy
```

兼容合同（全程）：SQLite 数据与 schema、配置语义、REST 路径、`go:embed` 单二进制、历史媒体库。

默认不做：GORM→sqlx、全量 CGO=0、照搬制品域、默认换 Mantine 全套。

参考：JianArtifact `apps/{server,web,wiki}` + `api/openapi.yaml` + `deploy/`；决策仍以本仓库 ADR 为准。

## 4. 任务拆分

- [x] PRD 登记 FR2-064～072
- [x] 补齐 FR2-065～072 的 specs 文件
- [x] 更新 `docs/specs/README.md` 索引
- [x] 文档同步：PRD 状态标开发中

## 5. 验收标准

- PRD §5 存在 FR2-064～072 且状态可信。
- `docs/specs/README.md` 可索引到对齐期全部需 spec 的条目。
- ARCHITECTURE 在 A 期完成前仍描述当前真貌，不伪称已迁完。

## 6. 风险 / 待定

- FR2-002 历史状态「已交付」保留；物理迁移由 065–069 承接，评审时勿要求回退 FR2-002。
