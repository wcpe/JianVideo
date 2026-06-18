# ADR-0023：ORM 规则修正——确认 GORM 为项目唯一 ORM

## 状态
已接受

## 背景
`.claude/rules/architecture-invariants.md` 第 17 条规定"禁止引入 ORM 框架，数据库操作使用标准 `database/sql` + 参数化查询"。但项目自初始化起就使用 GORM（`gorm.io/gorm` + `gorm.io/driver/sqlite`）作为数据访问层，全库 11+ 个文件引用 GORM API。规则与代码完全矛盾，导致后续修复工作无法判断是否违反架构不变量。

本 ADR 取代旧规则中"禁止 ORM"的决策。

## 决策
将架构不变量中的 ORM 规则改为：**使用 GORM 作为唯一 ORM，禁止引入其他 ORM 框架（如 XORM、Ent）**。

## 理由
- GORM 是项目的既成事实，从第一个数据库迁移开始就使用
- 项目使用 SQLite 作为数据库，GORM 对 SQLite 的支持成熟稳定
- GORM 提供了迁移（AutoMigrate）、链式查询、事务管理等能力，大幅提升了开发效率
- 单用户桌面应用的规模下，GORM 的性能开销完全可接受
- 重写为 `database/sql` 需要修改 11+ 个文件，工作量巨大且无实际收益

## 后果
- `.claude/rules/architecture-invariants.md` 第 17 行已更新
- 后续开发可继续使用 GORM，无需额外审批
- 禁止引入其他 ORM 框架

## 备选方案
1. **移除 GORM，改用 `database/sql`**：需要重写所有数据库操作，工作量巨大，且对于单用户桌面应用无实际收益
2. **允许所有 ORM**：过于宽松，可能导致多种 ORM 混用，增加维护复杂度
