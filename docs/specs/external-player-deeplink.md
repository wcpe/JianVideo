# 功能规格：外部播放器深链

> 状态：开发中　·　关联 PRD：FR-79　·　分支：feature/fr-79-extplayer　·　依赖：FR-43（分享链接，已交付 v0.5.0）、FR-44（续播位置，已交付）

## 1. 背景与目标

播放页只能在浏览器内播放；想用 VLC / IINA 等外部播放器看库内视频时无路可走——站内三路播放（HLS / fMP4 / 直链 `/api/play/:id/stream`）**全部需要 JWT 鉴权**，外部播放器不带 Cookie / 不会登录，拿到地址也是 401。

FR-79（P7）让用户在播放页一键生成一个**外部播放器可直接消费的网络串流地址**：复用 FR-43 已有的**免登分享流端点** `GET /api/share/:token/media/:mediaId/stream`（原文件直链 + Range，非 HLS），把它拼成绝对 URL 交给用户，在 VLC / IINA 里「打开网络串流」粘贴即可播放；带续播点时附 `#t=` 媒体片段，由播放器客户端跳转。

本期只做**深链 + 复制地址**，不做二维码扫码（无 QR 库，留待 FR-78 引入 QR 依赖时合并）。

## 2. 需求（要什么）

- 范围内：
  - 播放页头部按钮排新增两个入口：「用外部播放器打开」与「复制流地址」。
  - 点击 → 复用 FR-43 `createShare('media', mediaId, expiresInHours)` 创建免登外链（弹窗选有效期，同 FR-43 分享对话框；用户知情这是在创建一个**免登可访问**的外部链接）→ 拿到 token。
  - 拼**绝对** URL：`{window.location.origin}/api/share/{token}/media/{mediaId}/stream`；当 `media.last_position > 0` 时追加续播片段 `#t={Math.floor(last_position)}`。
  - 「复制流地址」用 `useClipboard`（@mantine/hooks）复制该 URL，带「已复制」反馈。
  - 「用外部播放器打开」展示该地址 + 说明「在 VLC / IINA 中打开网络串流并粘贴此地址」，并提供直接复制 / 在浏览器打开（`window.open`）。
  - 续播点 `#t=` 是 URL fragment，**不发送到服务端**，由播放器客户端处理，无需后端改动。
- 不做（范围外）：
  - **二维码扫码**：本期不引入 QR 库，仅深链 + 复制地址。扫码续播留待 FR-78 引入 QR 依赖时合并（spec / 注释标注）。
  - 后端改动：复用现有 FR-43 share stream 端点，后端**零改动**。
  - 把 HLS / 转码管线开放给外部播放器：沿用 FR-43 安全边界，外链只走渐进式原文件 + Range（不触发匿名转码）。需转码格式在外部播放器中的兼容性由播放器自身决定。

## 3. 设计（怎么做）

纯前端，无后端 / 无数据模型 / 无 ADR 改动。

- 复用 `frontend/src/api/share.ts` 的 `createShare(resourceType, resourceID, expiresInHours)` 创建 token。
- 新增纯函数 `buildExternalStreamURL(origin, token, mediaID, lastPosition?)`（放 `frontend/src/utils/external-player.ts`）：
  - 返回 `${origin}/api/share/${token}/media/${mediaID}/stream`；
  - `lastPosition` 为有限正数（`> 0`）时追加 `#t=${Math.floor(lastPosition)}`，否则不带 fragment；
  - 纯字符串拼接、无副作用，便于穷举单测（URL 拼接 / `#t` 续播点 / `last_position=0` 不带 `#t`）。
- 新增组件 `frontend/src/components/ExternalPlayerDialog.tsx`：
  - 入参 `mediaID`、`lastPosition`、`opened` / `onClose`；
  - 复用 FR-43 ShareDialog 的有效期交互（同一组 `EXPIRY_OPTIONS`），点「生成地址」调 `createShare` 拿 token，用上述纯函数拼出绝对地址；
  - 生成后展示只读地址输入框 + 「复制」（`useClipboard`）+「在浏览器打开」（`window.open`），并文案说明在 VLC / IINA 打开网络串流粘贴；
  - 文案说明这是免登外链。
- `frontend/src/pages/PlayPage.tsx`：在现有「分享」按钮旁加「外部播放器」入口按钮，打开该对话框；传入 `media.id` 与 `media.last_position`。

## 4. 任务拆分

- [ ] 纯函数 `buildExternalStreamURL` + 单测（URL 拼接、`#t` 续播点、`last_position=0` 不带 `#t`）。
- [ ] `ExternalPlayerDialog` 组件 + 单测（创建分享 → 拼链流程、复制反馈）。
- [ ] PlayPage 头部加入口按钮，接线对话框。
- [ ] 文档同步：PRD 状态 → 开发中、ARCHITECTURE（如涉及）、CHANGELOG 未发布段。

## 5. 验收标准

- `buildExternalStreamURL` 单测：绝对地址正确；`last_position > 0` 追加 `#t={floor}`；`last_position` 为 0 / 缺省时不带 `#t`。
- 组件单测：选有效期 → 点生成 → 调 `createShare(media, id, hours)` → 展示含 token 的绝对 `/api/share/.../stream` 地址；「复制」点击后有反馈。
- 前端 `npx tsc --noEmit`、`npx vitest run`、`npm run build`（生产构建）全绿；改动文件 eslint 干净。
- **真机验收（待真机验）**：在 VLC / IINA「打开网络串流」粘贴生成地址，外部播放器**确实在线播放**库内视频；带续播点的地址在支持 `#t=` 的播放器从续播点起播。需用户在真机确认通过，单元测试不替代此项。

## 6. 风险 / 待定

- 续播点 `#t=` 由播放器客户端处理，不同外部播放器对 media fragment 的支持程度不一（VLC 支持、部分播放器忽略）——属客户端能力，非本功能缺陷。
- 外链免登：与 FR-43 同等暴露面（任何持有地址者可只读访问该媒体原文件），有效期由用户在创建时选择，知情同意。
- 二维码扫码本期不做（无 QR 库）；FR-78 引入 QR 依赖后可在此基础上叠加「扫码在手机打开 / 续播」。
