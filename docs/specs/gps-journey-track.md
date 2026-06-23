# 功能规格：GPS 旅程轨迹视图

> 状态：开发中　·　关联 PRD：FR-76　·　分支：feature/fr-76-gpstrack

## 1. 背景与目标
照片地图（FR-39，ADR-0031）已用 leaflet + react-leaflet + OSM 在线瓦片展示带 GPS 的照片散点分布。FR-76（P7）在此之上扩展：把同一行程（按天）内带 GPS 的照片按时间序连成地图轨迹，让用户在地图上看出「这一天去了哪、怎么走的」。纯前端展示层增强，不改后端、不引新依赖。

## 2. 需求（要什么）
- 拉取带 GPS 的媒体后，按媒体时间序（media_time 降级链）排序，按「天」分组，每天一条折线轨迹。
- 范围内：
  - 复用 `groupMediaByDate(files, 'day')` 按天分组；组内点按时间序连成 `<Polyline>`。
  - 保留现有散点 `Marker`（含缩略图弹窗）不变。
  - 提供「轨迹模式」开关：开=散点+轨迹（默认开），关=仅散点。
  - 空数据 / 单点（当天仅 1 个 GPS 点）不画线。
  - 多天多条轨迹，不同天用不同颜色以便区分。
- 不做（范围外）：
  - 不改后端、不加接口、不引依赖（react-leaflet 5.0.0 已含 Polyline）。
  - 不做轨迹动画、距离/速度统计、跨天聚合行程识别（YAGNI，超出 FR-76）。
  - 不动散点 Marker 弹窗内容与图标逻辑。

## 3. 设计（怎么做）
- 复用 ADR-0031 既定技术栈（leaflet + react-leaflet + OSM），无新架构决策，无需新 ADR。
- 数据：`fetchAllGeotagged()` 增加 `sort: 'media_time_asc'`，后端按 `COALESCE(media_time, added_at) ASC` 返回升序；前端再用 `groupMediaByDate(files, 'day')` 分组（该工具按天键倒序分组、组内保持输入顺序，即每组内为时间升序点序）。
- 轨迹构建：纯函数 `buildDayTracks(files)` —— 过滤出含有效 `gps_lat`/`gps_lon` 的点，按天分组，丢弃点数 < 2 的天，输出 `{ date, positions: [lat,lon][] }[]`，每条按固定调色板取色（按下标取模）。放入 `frontend/src/utils/` 便于穷举单测。
- 渲染：`MapPage` 内按 `buildDayTracks` 结果渲染若干 `<Polyline positions={...} color={...}>`；「轨迹模式」开关用 Mantine `Switch` 控制是否渲染折线层；散点 Marker 始终渲染。
- 测试桩：`MapPage.test.tsx` 的 react-leaflet mock 补 `Polyline` 桩（输出 `data-testid="polyline"`）。

## 4. 任务拆分
- [x] 新增 `frontend/src/utils/gpsTrack.ts` 的 `buildDayTracks` 纯函数 + 单测（多天分条、单点丢弃、无 GPS 丢弃、调色板循环）
- [x] `MapPage.tsx`：请求加 `sort:'media_time_asc'`，渲染 Polyline 层 + 轨迹模式 Switch
- [x] `MapPage.test.tsx`：补 Polyline 桩，加多日渲染条数 / 单点不画 / 开关切换测试
- [x] 文档同步：PRD 状态、ARCHITECTURE（照片地图段补轨迹）、CHANGELOG

## 5. 验收标准
- 多天带 GPS 数据：每天（点数 ≥ 2）渲染一条 Polyline，点序为当天时间升序。
- 单点的天 / 无 GPS 媒体：不产生 Polyline，不报错。
- 「轨迹模式」开关关闭时仅散点、无折线；开启时散点 + 折线并存。
- `npx tsc --noEmit`、`npx vitest run`、`npm run build` 三项均过。
- 真机维度（OSM 瓦片联网、真实 EXIF GPS）沿用 ADR-0031 既有限制，标「待真机验」；本 FR 仅在既有地图上叠加折线，瓦片/真实 GPS 不引入新真机风险。

## 6. 风险 / 待定
- 轨迹连线仅按「天」朴素聚合，同一天多段独立出行会被连成一条折线（FR-76 表述为「同一行程」≈ 同一天，按天聚合已满足；更细的行程切分属后续增强，不在本期）。
