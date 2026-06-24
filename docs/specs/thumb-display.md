# 功能规格：缩略图显示增强

> 状态：开发中　·　关联 PRD：FR-81　·　分支：feature/fr-81-thumb-display

## 1. 背景与目标
解决缩略图四类体验缺陷（扩 FR-D，属第八期界面打磨 P8）：
- 透明 PNG 等带 alpha 的源经后端转 JPEG 时，透明区被默认黑底合成纯黑，深色卡片上显得突兀。
- 前端缩略图固定 `aspectRatio:16/9` + `objectFit:cover`，竖图/正方图被裁切看不全。
- 缺图时 `Loader` 形同虚设（同步置 loading=false），无 202「生成中」轮询、无加载失败兜底。
- 列表只请求单一 320px 尺寸，多列窄列宽下浪费带宽与解码成本。

## 2. 需求（要什么）
- **P1 透明灰底（后端）**：带 alpha 的源（PNG/部分 WEBP/HEIC 等）转缩略图时先合成到固定中性灰底（0x808080）再压 JPEG，消除纯黑。产物仍恒为 `.jpg`。
- **P3 比例自适应（前端）**：固定容器比例 + 中性背景，图片 `object-fit:contain` 居中完整可见，竖图/正方图不裁、不破坏等高网格。
- **P14 骨架占位（前端）**：先显 `Skeleton`，命中 202「生成中」短间隔轮询重试、生成完成自动替换，`onError` 显降级占位。
- **P12 多尺寸 srcset（后端+前端）**：缩略图端点支持 `size` 参数产多尺寸缓存；前端按列宽 `srcset`/`sizes` 请求更小图。

- 范围内：上述四点；`/api/library/thumbnail/:id` 与分享版 `/api/share/.../thumbnail` 共用同一 size 协商。
- 不做（范围外）：WebP/AVIF 输出格式切换、CDN、客户端缓存策略、响应式断点配置化。

## 3. 设计（怎么做）

### P1 透明灰底
- ffmpeg 图片/视频缩略图：`buildImageThumbnailArgs` / `buildVideoThumbnailArgs` 改用 `-filter_complex`，以 `color=c=0x808080` 经 `scale2ref` 适配前景尺寸后 `overlay` 合成，等价于「把 alpha 合成到灰底」。无 alpha 的源叠加灰底不可见、结果不变。
- magick（HEIC/RAW）：`buildMagickThumbnailArgs` 增 `-background #808080 -flatten`，把透明区刷灰后再缩放。
- 灰底色值定义为常量 `thumbnailMatteColor`，不硬编码散落。

### P12 多尺寸缓存与端点 size 参数
- 新增受支持尺寸白名单常量（如 160/320/640），默认仍 320。
- `getThumbnailPath` 扩展为 `thumbnailPathForSize(filePath, size)`：**默认尺寸 320 的缓存路径/名保持与现状完全一致**（不加后缀），非默认尺寸用带尺寸后缀的新名（如 `<hash>_160.jpg`），与默认产物并存。`getThumbnailPath`/`FindThumbnailPath` 保留为 320 的薄封装，**dHash（dhash.go）与健康巡检（FR-73）读取路径不变**。
- 生成函数按 size 缩放（ffmpeg `scale=<size>:-1`、magick `<size>x>`）。
- `serveThumbnail` 解析 `size` 查询参数（非白名单或缺省回落 320），按 size 找缓存：命中回文件、缺失异步生成该尺寸并回 202。

### P3/P14 前端 MediaThumbnail
- 容器固定比例 + 中性背景，`<img object-fit:contain>` 居中。
- 状态机：`loading`（Skeleton）→ 命中 `<img onLoad>` 显示；`onError` 时若服务端回 202 则短间隔（约 1.5s，带次数上限）轮询重载，其余错误显降级占位。
- `srcset` 提供 160/320/640 候选，`sizes` 按典型多列列宽给浏览器选择依据。

## 4. 任务拆分
- [x] 复制规格模板、PRD §4 FR-81 状态改「开发中」
- [x] 后端测试先行：size 路径映射（默认不变）、按 size 生成、灰底滤镜参数断言、端点 size 协商与 202
- [x] 后端实现：灰底合成、多尺寸缓存 key、端点 size 参数
- [x] 前端测试先行：contain、srcset/sizes、202→200 轮询、onError 兜底
- [x] 前端实现：MediaThumbnail 重写
- [x] 文档同步：PRD 状态、ARCHITECTURE 5.1.1、API.md 缩略图端点、CHANGELOG 未发布段

## 5. 验收标准
- 后端单测：`thumbnailPathForSize` 默认 size 路径与 `getThumbnailPath` 完全一致（保 dHash/FR-73）；不同 size 返回不同缓存路径；灰底滤镜/参数含中性灰底；端点按 size 命中缓存、缺失回 202。
- 前端 vitest：断言 `object-fit:contain`；`<img>` 带 `srcset`/`sizes`；202 后轮询重载、最终显示；`onError` 显降级占位。
- 受影响组件 `go test ./internal/library/... ./internal/api/...`、前端 `npm run build` + `npx vitest run` 全绿。
- **真机验收**（需用户确认）：带透明区 PNG 缩略图灰底非纯黑；竖图完整不裁；2 列/4 列布局按列宽请求到更小尺寸、单图字节较 320px 基线下降；缺图先骨架后自动替换、加载失败显占位。

## 6. 风险 / 待定
- ffmpeg `scale2ref`+`overlay` 在极端尺寸/单像素图的边界行为；以单测断言参数构造、真机验视觉。
- 多尺寸缓存增加磁盘占用（每尺寸一份）；受白名单尺寸数约束，可接受。
