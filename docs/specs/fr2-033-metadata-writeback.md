# 功能规格：系统内编辑元数据与危险回写

> 状态：首切 + 前端二次确认已落地（快照保留期清理二切；视频写回二期）　·　关联 PRD：FR2-033　·　阶段：P5 `0.26.x`　·　前置：[fr2-037](fr2-037-task-queue-center.md)、[fr2-040](fr2-040-audit-events.md)；权限见 FR2-010

## 0. 首切范围（本 PR 只做这些）

| 切片 | 内容 | 首切 |
|------|------|------|
| A | 写回 API 强制 `confirm_writeback=true`；无 confirm → 4xx；viewer 403 | ✅ |
| B | 写回前快照到数据目录 `writeback-snapshots/...`；任务 `metadata.writeback` | ✅ |
| C | **仅图片**有限字段 EXIF/IPTC 写回；视频写回拒绝并说明「仅库内」 | ✅ |
| D | 失败：原文件哈希不变、快照保留、任务 failed + 审计 | ✅ |
| E | 单测：无 confirm / 工具失败不损原文件 / editor+ 可入队 | ✅ |
| F | 前端二次确认文案 + 任务状态 | ✅ |
| G | 视频容器嵌入写回 | 二期 |

## 1. 背景与目标

库内元数据/标签编辑已存在；**写回原文件**（如嵌入标签、改容器元数据）高危。P5 要求：默认只改库；写回必须二次确认、可回滚快照、走任务队列、失败不损坏原文件。
## 2. 需求（要什么）

### 2.1 范围内

- **安全默认**
  - 所有编辑 API 默认 `writeback=false`，只更新 DB / 可重建索引。
  - 写回必须显式 `confirm_writeback=true` + 前端二次确认文案（中文说明不可逆风险）。
- **快照**
  - 写回前将目标原文件复制到 Space 数据目录下 `writeback-snapshots/<media_id>/<ts>-<name>`（或等价），记录路径到任务 payload 与审计 `before`。
  - 快照保留策略：默认 7 天或随回收站设置（可配置）；失败任务保留更久直至用户处理。
- **任务**
  - 类型 `metadata.writeback`；队列执行；成功/失败审计已在 FR2-040 预留动作名。
  - 失败：原文件保持写回前字节；DB 可保持「待写回」或回滚本次库内变更（策略：库内变更与写回解耦——先提交库内，写回失败仅标记 `writeback_status=failed`，不自动改回库，除非用户点回滚）。
- **权限**：Space owner（及可选 editor）见 FR2-010。
- **支持范围（首切）**：常见图片 EXIF/IPTC 有限字段；视频优先「不写回容器」仅库内——视频嵌入写回可标二期，避免半截实现。

### 2.2 不做

- 任意 ffmpeg 重封装当写回。
- 无快照的原地覆盖。
- 批量写回上千文件无配额。

## 3. 设计（怎么做）

- API：`PATCH` 库内元数据；`POST .../writeback` 入队。
- worker：校验快照存在 → 工具写临时文件 → `rename` 替换 → 校验可读 → 完成。
- 任一步失败：保留原文件与快照，任务 failed。

## 4. 任务拆分

- [x] 快照存储路径（`writeback-snapshots/<space>/<media_id>/<ts>-name`；清理策略二切）
- [x] `metadata.writeback` worker（图片有限字段；ImageMagick；可注入失败路径）
- [x] API confirm + 权限（editor+ 写守卫）+ 审计
- [x] 单测：无 confirm 拒绝；视频拒绝；工具失败不损原文件；成功写回；API 202
- [x] 文档同步（API / CHANGELOG / PRD / ARCHITECTURE）
- [x] 二切：前端二次确认与任务状态（`MediaDetailPanel` + `enqueueMetadataWriteback`）
- [ ] 二切：快照保留期清理
- [ ] 二期：视频嵌入写回

## 5. 验收标准

- 无 `confirm_writeback` 调用写回 API → 4xx。
- 模拟写回工具失败：原文件哈希不变，快照仍在，任务 failed。
- 成功路径：原文件字段变更可被 ffprobe/exif 读到；审计含前后摘要。
- 权限：viewer 拒绝。

## 6. 风险 / 待定

- 视频写回范围需产品确认是否延后。
- 大文件快照磁盘占用：需配额提示。
