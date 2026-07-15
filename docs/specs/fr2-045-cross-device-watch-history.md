# 功能规格：跨设备续播与观看历史

> 状态：开发中　·　关联 PRD：FR2-045　·　阶段：P3 `0.24.x`

## 1. 背景与目标

现有续播能力把 `last_position`、`watched`、`last_watched_at` 直接保存在 `media_files`，能满足单设备基础续播，但缺少独立的观看状态真源、并发写入保护和完整观看历史查询契约。多个浏览器或设备同时播放同一媒体时，延迟到达的旧进度可能覆盖较新的进度。

P3 仍为单用户阶段。本功能以 `space_id + media_id` 作为唯一观看主体，在服务端持久化统一观看状态，使同一 Space 内任意设备读取同一续播位置与观看历史；不在 P3 提前引入用户维度。P5 建立多用户模型后，再把状态迁移为 `space_id + user_id + media_id`。

目标：

- 建立观看状态与观看历史的唯一真源，避免继续以媒体表字段承载写入并发语义。
- 保证跨设备读取一致，并阻止延迟、乱序或重复的旧进度覆盖新进度。
- 统一续播、继续观看、观看历史、完成判定和观看次数的口径。
- 兼容现有 `media_files.last_position`、`watched`、`last_watched_at` 字段与依赖它们的查询，并提供可校验、可重入的迁移路径。

前置依赖：FR2-007（Space 归属）、FR2-017（迁移框架）、ADR-0057（`player-core` 与 `media-client` 边界）。

## 2. 需求（要什么）

### 2.1 单用户与跨设备边界

- P3 的观看状态唯一键固定为 `space_id + media_id`；同一 Space 的浏览器、桌面端或移动浏览器共享一份状态。
- P3 不按设备拆分最终状态，设备标识只用于并发与幂等控制，不构成查询维度。
- 缺失 Space header 时沿用默认 Space；非法、不存在或无权限 Space 继续遵循 FR2-007，禁止跨 Space 读取或更新。
- P5 多用户化前，不新增可空 `user_id` 制造双重语义；P5 迁移时再显式扩展唯一键并回填现有单用户 owner。

### 2.2 续播与完成判定

- 播放器打开媒体时读取服务端观看状态；满足续播条件时，在媒体可 seek 后定位到服务端位置。
- 续播有效位置为 `position_seconds > 1` 且未完成；小于等于 1 秒按从头播放处理。
- 播放过程中约每 10 秒上报一次，并在暂停、seek 稳定、页面隐藏、离开页面和播放结束时补报；客户端同一时刻最多保留一个在途请求，新的周期上报合并为最新位置。
- 观看事件必须携带 `reason: user | ab_loop | restore | system`：用户操作使用 `user`，A-B 循环内部回跳使用 `ab_loop`，续播位置恢复使用 `restore`，播放器或系统驱动的非用户操作使用 `system`。
- 媒体时长已知时，满足以下任一条件即判定完成：
  - 已播放比例 `>= 95%`；
  - 剩余时长 `<= 15 秒`，且已播放比例 `>= 80%`。
- 媒体时长未知时，普通进度不得推断完成；仅播放器明确上报 `ended` 事件时判定完成。
- `reason=ab_loop` 的内部回跳即使位置接近片尾，也不得触发完成判定或 `view_count` 增长；它只按普通位置事件更新可接受的观看状态。
- 完成后真源中的 `completed=true`、`position_seconds=0`，不再进入继续观看；仍保留 `last_watched_at`，因此继续出现在观看历史。
- 从头重播已完成媒体并产生低于完成阈值的新进度后，状态转为 `completed=false`，重新具备续播资格。
- `view_count` 只在非 `ab_loop` 事件造成 `completed: false -> true` 的状态转换时增加一次；重复 `ended`、重试或重复事件不得重复计数。

### 2.3 观看历史与继续观看

- 观看历史定义为“在当前 Space 内至少产生过一次已接受观看事件的媒体当前状态集合”，不是逐秒事件日志。
- 观看历史按 `last_watched_at DESC, media_id DESC` 游标分页，包含已完成和未完成媒体；软删除、失效或不属于当前 Space 的媒体不返回。
- 继续观看从同一真源查询，仅返回 `completed=false AND position_seconds>1` 的媒体，按最近观看时间倒序。
- 播放详情、继续观看和观看历史返回同一套状态字段，禁止分别从不同表拼出互相矛盾的结果。
- P3 不提供逐次播放会话明细、观看时长统计、单条/全部清除历史 UI，也不做跨 Space 合并。

### 2.4 并发、乱序与幂等

- 每次打开媒体创建新的 `session_id`，同一会话的上报携带严格递增的 `event_seq`。
- 每条观看状态带服务端 `revision`；更新请求必须携带读取到的 `expected_revision`，服务端用单条条件更新或事务内比较交换保证原子性。
- 仅当请求 `session_id` 等于当前状态的 `last_session_id`，且 `event_seq <= last_event_seq` 时，才按当前会话重复或乱序处理：返回当前状态且 `applied=false`，不增加 revision、观看次数或更新时间。
- 不为历史会话保存独立幂等收据；历史 `session_id` 的迟到包不按 `event_seq` 特判，必须因旧 `expected_revision` 返回 `409 WATCH_STATE_CONFLICT` 并丢弃，不得落库或更新兼容字段。
- 客户端收到冲突后先采用服务端状态；仅当播放器仍在前台持续播放或用户刚完成主动 seek 时，才用返回的新 revision 生成一条新事件。页面离开、后台补发、离线队列和历史会话中的旧事件直接丢弃，禁止无条件重试。
- 两台设备确实同时活跃播放时，最后一条基于最新 revision 成功提交的交互成为当前状态；该规则只允许活跃会话重新基线化，不允许延迟旧包覆盖新状态。

## 3. 设计（怎么做）

### 3.1 观看状态真源

新增 `watch_states`，并明确它是 P3 续播、完成状态、继续观看和观看历史的唯一写入与查询真源：

| 字段 | 约束与语义 |
|---|---|
| `space_id` | 非空，Space 归属 |
| `media_id` | 非空，关联媒体 |
| `position_seconds` | 非空，接受后的续播位置，最小为 0 |
| `completed` | 非空，服务端按完成规则派生 |
| `last_watched_at` | 非空，服务端接受事件的时间 |
| `completed_at` | 最近一次进入完成态的时间；未完成为空 |
| `revision` | 非空递增整数，用于比较交换 |
| `last_session_id` | 最近一次成功事件的会话标识 |
| `last_event_seq` | 最近一次成功事件在该会话内的序号 |
| `created_at` / `updated_at` | 审计与迁移校验时间 |

约束与索引：

- 主键或唯一索引：`(space_id, media_id)`。
- 历史查询索引：`(space_id, last_watched_at DESC, media_id DESC)`。
- 继续观看索引：覆盖 `space_id + completed + last_watched_at + media_id`；SQLite 可使用 `completed = 0 AND position_seconds > 1` 的 partial index。
- 所有 repository 方法必须显式接收 `space_id`；禁止只按 `media_id` 更新。
- 不新增观看事件明细表或 receipts 表；`last_session_id + last_event_seq` 只对当前最近会话提供轻量重复/乱序判断，历史会话统一依赖 revision 冲突丢弃。

### 3.2 API 契约

新增或收敛为以下 Space scoped 契约，具体路径可在实现时按现有 API 版本规则落位，但语义不得拆分：

- `GET /api/play/:id/watch-state`：返回当前状态；无记录时返回 revision 为 0 的未观看状态。
- `PUT /api/play/:id/watch-state`：请求至少包含 `position_seconds`、`expected_revision`、`session_id`、`event_seq`、`event_type`、`reason`；`event_type` 为 `progress | pause | seek | ended`，`reason` 为 `user | ab_loop | restore | system`。
- `GET /api/library/watch-history?cursor=&limit=`：返回观看历史游标页。
- `GET /api/library/continue-watching?limit=`：路径保持兼容，但查询改读 `watch_states`。

成功响应返回 `applied`、新 `revision`、`position_seconds`、`completed`、`last_watched_at`。冲突响应返回稳定错误码与当前完整状态，客户端不依赖错误文本判断。

位置必须为有限非负数；服务端夹取到已知时长范围。`session_id` 长度和字符集受限，`event_seq` 必须为非负整数。客户端上报的时长只可在媒体元数据缺失时辅助完成判定，不能覆盖媒体库已知时长。

### 3.3 原子更新流程

一次接受事件在同一数据库事务内完成：

1. 按 `space_id + media_id` 校验媒体存在且可访问。
2. 仅当 `session_id = last_session_id` 时检查 `event_seq`；当前会话重复或乱序直接返回 `applied=false`。
3. 其他请求以 `expected_revision` 比较交换观看状态；不匹配则回滚并返回冲突，历史会话迟到包不得绕过该检查。
4. 按事件 `reason` 计算位置、完成态、`completed_at` 和是否发生首次完成转换；`ab_loop` 禁止产生完成转换。
5. 更新 `watch_states`，revision 加一。
6. 同步兼容投影字段；若非 `ab_loop` 事件发生未完成到完成转换，再原子增加 `view_count`。

任何一步失败时不得出现真源已更新但兼容字段未更新的部分成功状态。

### 3.4 `media_files` 兼容策略

P3 保留以下字段，不删除、不改 JSON 名称：

- `media_files.last_position`
- `media_files.watched`
- `media_files.last_watched_at`

它们降级为 `watch_states` 的兼容投影：

- 每次真源写入成功后，在同一事务内投影 `position_seconds -> last_position`、`completed -> watched`、`last_watched_at -> last_watched_at`。
- 旧统计、列表和尚未迁移的响应可暂时读取投影；新续播、继续观看、历史和写入逻辑必须读取真源。
- 切换完成后禁止业务代码直接写这三个字段；兼容端点也必须调用统一观看状态服务。
- 投影不作为冲突后的恢复来源；发现不一致时以 `watch_states` 为准修复 `media_files`。

### 3.5 迁移与回滚安全

迁移通过 FR2-017 的 migration registry 执行，步骤可重入：

1. 创建 `watch_states`、唯一约束和查询索引。
2. 从 `media_files` 回填：存在 `last_watched_at`、`last_position > 0` 或 `watched = true` 的媒体生成一行；`watched=true` 时位置归零并标记完成，其余位置夹取为非负值。
3. 回填行的 `space_id`、`media_id`、位置、完成态和最近观看时间保持原值语义；当前旧表没有可用的媒体记录 `updated_at`，历史缺少 `last_watched_at` 时记录迁移 warning 并跳过该行，不使用 `modified_at`、`added_at` 或当前时间伪造观看时间。
4. 回填使用 `INSERT ... ON CONFLICT DO NOTHING` 或等价方式，重复运行不覆盖已经由新逻辑产生的更高 revision 状态。
5. 校验唯一键、孤儿行、Space 归属、回填数量，并抽样比对真源与兼容投影。
6. 应用代码切换到真源后继续双写兼容投影；P3 不删除旧列，因此回退到旧版本时仍能读取最后一次投影。

迁移前备份、事务原子性、失败记录和恢复流程沿用 FR2-017，不在本功能另造迁移机制。

### 3.6 P5 用户维度迁移

P5 引入用户与 Space 成员后：

- 新唯一键扩展为 `(space_id, user_id, media_id)`。
- P3 每个 Space 的现有状态回填给该 Space 的既有单用户 owner；无法唯一确定 owner 时迁移阻断，不静默分配。
- P5 客户端与 API 必须从认证上下文取得 `user_id`，不得由请求体任意指定。
- `media_files` 三个兼容字段无法表达多用户状态，P5 必须停止把它们作为用户观看状态接口；删除时机另立迁移规格决定。

## 4. 任务拆分

- [x] 新增 `watch_states` migration、约束、索引与回填校验。
- [x] 建立 Space scoped 观看状态 repository 与事务更新服务。
- [x] 实现 revision 比较交换、仅当前最近会话的序号幂等和稳定冲突响应，不新增 receipts 表。
- [x] 将续播、继续观看和观看历史查询切换到真源。
- [x] 保留并事务同步 `media_files` 兼容投影与 `view_count` 转换计数，接入 `user | ab_loop | restore | system` reason。
- [ ] 在 `media-client` / `player-core` 接入读取、单请求合并、冲突采用与前后台补报。
- [ ] 补迁移、并发、当前会话乱序/重复、历史会话迟到、`ab_loop` 不完成、跨 Space 隔离和 P5 回填预演测试。
- [ ] 使用真实媒体完成独立浏览器上下文与安装态 PWA 上下文的跨端续播验收。

## 5. 验收标准

- 新库与升级库均存在 `(space_id, media_id)` 唯一观看状态；迁移重复执行不产生重复行或覆盖新状态。
- 同一 Space 的设备 A 播放并上报后，设备 B 打开同一媒体能读取并续播到同一位置；不同 Space 不可见。
- 设备 B 提交新进度后，设备 A 的延迟旧 revision 请求返回 `409 WATCH_STATE_CONFLICT`，数据库与兼容字段保持设备 B 的状态。
- 仅当前 `last_session_id` 的重复或倒序 `event_seq` 返回 `applied=false`，且不改变 revision、位置、`last_watched_at` 或 `view_count`；历史会话迟到包返回 revision 冲突并丢弃。
- 已知时长下，95% 阈值以及“剩余不超过 15 秒且至少播放 80%”均能完成；短视频不会仅因总时长小于 15 秒在起播时误判完成。
- 未知时长的普通进度不自动完成，`ended` 可完成；完成后位置清零、退出继续观看但保留在历史。
- `reason=ab_loop` 的回跳不会触发完成或增加 `view_count`；重复完成事件只增加一次，重新从头观看产生新进度后可恢复为未完成，并在下一次完成转换时再增加一次。
- 观看历史按稳定游标倒序分页，继续观看与播放详情对同一媒体返回一致状态。
- `media_files` 三个旧字段与真源投影一致；迁移后旧查询在兼容期内不回归。
- 自动化测试至少覆盖两事务争用同一 revision、当前会话乱序/重复、历史会话迟到、`ab_loop` 回跳、离线旧请求、Space 隔离、迁移重入与投影事务回滚。
- 真实双上下文验收（需用户确认）：使用真实媒体和真实服务端状态，一个上下文为从系统入口启动且确认 `display: standalone` 的安装态 PWA，另一个为独立浏览器配置或另一台设备；完成“上下文 A 中途退出 → 上下文 B 续播 → 上下文 A 旧页面离开补报不覆盖”的完整流程。headless、jsdom、mock 媒体或纯 API 调用不能替代该验收。

## 6. 风险 / 待定

- 已确认：P3 仍为单用户，观看状态按 `space_id + media_id` 持久化；P5 再迁移到用户维度。
- 已确认：`watch_states` 是观看历史与续播真源，`media_files` 字段仅作兼容投影。
- 同一媒体在多设备真正同时播放时不存在可合并的单一“正确位置”；本规格采用 revision 比较交换与活跃会话重新基线化，保证旧包不覆盖，但最终状态仍是最后一次成功的活跃交互。
- 已确认：不新增 receipts 表；当前 `last_session_id` 之外的历史会话不提供序号幂等保证，其迟到包必须依赖旧 revision 冲突丢弃。
- 页面关闭时 `keepalive` 请求仍可能被浏览器丢弃；允许损失最后数秒进度，但不得用无限离线重试换取旧进度覆盖风险。
- SQLite 高并发下需用短事务和条件更新，测试必须覆盖锁竞争，不得通过客户端时间戳比较替代数据库原子性。
