# 功能规格：外部工具自动下载与代理

> 状态：已审核接受　·　关联 PRD：FR2-022　·　阶段：P2 `0.23.x`　·　分支：待定

## 1. 背景与目标

系统已支持 ffmpeg/ffprobe/magick 路径设置、代理设置、代理连通测试和自更新下载校验，但没有外部工具自动下载源、镜像 registry、每工具自定义 URL、完整性校验、安装目录策略或下载任务队列。P2 需要让用户在受限网络下安全下载 ffmpeg/ffprobe/ImageMagick，并且下载过程可代理、可校验、可追踪。

目标：

- 建立外部工具源 registry 与镜像/自定义 URL 支持。
- 工具下载走任务队列，支持进度、取消、失败重试。
- 所有下载必须有完整性校验和安全安装边界。
- 下载完成后写入运行期工具路径设置并热应用。

前置依赖：FR2-024（配置 registry）、FR2-037（任务队列）、FR2-040（审计核心）。工具下载是系统级任务，系统级任务/审计的 Space 归属需按 FR2-037/FR2-040 的 `scope=system` 或等价 ADR 口径执行。

## 2. 需求（要什么）

- 工具范围：ffmpeg、ffprobe、ImageMagick `magick`。
- 下载源：内置官方/镜像元数据 + 用户自定义 URL。
- 支持代理下载，代理连通测试目标可按下载源配置。
- 下载前展示版本、平台、架构、大小、sha256；自定义 URL 必须要求用户提供 sha256。
- 下载文件保存到受控工具目录，不允许路径穿越或覆盖任意文件。
- 解压/安装后探测工具版本，成功后更新 `ffmpeg_path`/`ffprobe_path`/`magick_path`。
- 下载任务走 FR2-037，状态和进度可查。
- 范围内：工具 registry、下载任务、校验、安装目录、代理测试、设置热应用、UI。
- 不做（范围外）：自动后台升级工具、驱动安装、系统 PATH 修改、无校验自定义下载、把 `data/tools/*` 作为普通可重建缓存一键清理。

## 3. 设计（怎么做）

配置与 registry：

- `tool_sources` 内置为代码/JSON registry，包含 tool、platform、arch、version、url、sha256、size、mirror label。
- 用户自定义 URL 不落入内置 registry，作为一次性下载请求或保存为设置项。

安全：

- 只允许 `https` 或用户显式确认的 `http`；默认拒绝 file/ftp 等协议。
- 下载大小上限可配置，有默认限制。
- 下载先进入临时目录；解压时拒绝 symlink、hardlink 和 path traversal，并校验每个条目路径在目标目录内。
- 安装目录固定在数据目录下 `tools/<tool>/<version>/`。
- sha256 校验和版本探测通过后，才原子 rename 到 `tools/<tool>/<version>/`。

任务：

- `tool.download`：下载、校验、解压、探测、应用设置。
- 进度记录 downloaded/total/current step。

API：

- `GET /api/system/tools`
- `GET /api/system/tools/sources`
- `POST /api/system/tools/download`
- 代理连通测试复用并扩展现有 `POST /api/system/proxy/test`，增加可选下载源目标参数；不新增重复的工具专用代理测试端点。

## 4. 任务拆分

- [ ] 定义工具源 registry 与平台/架构匹配。
- [ ] 实现下载任务：代理、进度、sha256 校验、解压安全、版本探测。
- [ ] 实现工具目录与路径热应用。
- [ ] 新增工具源、下载 API，并扩展现有代理测试 API 支持下载源目标。
- [ ] 前端设置/系统页增加工具下载与进度展示。
- [ ] 接入审计事件。
- [ ] 补单元测试：URL 校验、sha256、路径穿越、symlink/hardlink 拒绝、代理脱敏。
- [ ] 补集成测试：httptest 下载源、checksum 错误拒绝、安装后 detect。
- [ ] 补 E2E：选择工具源、下载入队、进度、失败重试。
- [ ] 文档同步：PRD 状态、ARCHITECTURE、API、CHANGELOG。

## 5. 验收标准

- 使用测试下载源能下载工具包、校验 sha256、安装到受控目录并探测版本。
- checksum 错误、路径穿越压缩包、symlink/hardlink、自定义 URL 缺 sha256 都会被拒绝。
- 代理 URL 中的凭据不出现在日志、API 或审计事件明文中。
- 下载任务可查看进度、失败可重试、可取消。
- 下载成功后对应工具路径设置热应用，无需重启。
- `data/tools/*` 不参与 FR2-048 的普通缓存一键清理，除非后续另写工具资产卸载规格。
- `go test`、httptest 集成测试、Playwright 工具下载 E2E、`pnpm run quality` 全绿。
- Go 单二进制 serve 后，用本地测试源下载并应用工具路径实跑通过。

## 6. 风险 / 待定

- 已确认：HTTP 自定义 URL 默认拒绝；只有安全白名单、本地测试源或用户显式配置时允许。
- 真实官方/镜像源可用性会随时间变化，自动化验收使用本地 httptest 源；真实下载作为手动真机验收记录。
