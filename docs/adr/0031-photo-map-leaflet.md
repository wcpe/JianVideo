# ADR-0031：照片地图采用 leaflet + react-leaflet + OSM 在线瓦片

## 状态
已接受

## 背景
FR-39 要求基于照片 EXIF GPS（FR-31 已提取并存 `gps_lat`/`gps_lon`）展示地理分布地图。前端需要一个地图渲染库 + 瓦片源。候选：

- **leaflet + react-leaflet**：轻量开源地图库，OSM 在线瓦片免 API key、免账号；react-leaflet 是其 React 封装，与本项目 React 19 技术栈一致。
- **mapbox-gl / maplibre-gl**：矢量瓦片体验更好，但 mapbox 需 token/账号，maplibre 体量更大；对「轻量 nas 相册」属过度。

PRD FR-39 行即点名「leaflet + OSM 在线瓦片」。引入新前端依赖按依赖管理规则需用户确认——已确认采用 react-leaflet + leaflet。

## 决策
- **前端引入 `leaflet` + `react-leaflet`（+ devDep `@types/leaflet`）**，新增 `/map` 照片地图页：拉取带 GPS 的媒体（后端 `has_gps` 过滤），在 OSM 在线瓦片上按坐标打标记，标记弹窗显示缩略图与名称。
- **瓦片源用 OpenStreetMap 在线瓦片**（`https://{s}.tile.openstreetmap.org/...`，含 OSM 归属声明），不自建瓦片服务、不引入离线瓦片。
- **后端新增 `has_gps` 结构化筛选**（`gps_lat != 0 OR gps_lon != 0`），并入 FR-35 的 `MediaFilter`，走参数化查询；地图页分页累积拉取地理标记子集。
- leaflet 默认 marker 图标路径在打包后丢失，**经 Vite 资源 URL 重新指向**（`L.Icon.Default.mergeOptions`）。

## 理由
- leaflet + OSM 零账号、零 key、开源免费，契合单用户自托管「轻量」定位；react-leaflet 与 React 19 一致，集成成本低。
- 复用 FR-31 已落库的 GPS 字段与 FR-35 的筛选框架，后端仅加一个 `has_gps` 条件，改动面小。
- 仅展示地理分布（只读浏览），不需要矢量瓦片/导航等重型能力，leaflet 足够。

## 后果
- 新增前端运行时依赖 `leaflet`、`react-leaflet` 与开发依赖 `@types/leaflet`（已写入 `frontend/package.json`）。
- **瓦片显示依赖联网**访问 OSM 瓦片服务：离线 / 内网隔离环境地图瓦片不可见（标记逻辑仍在）；瓦片加载属真机/在线维度。使用须遵守 [OSM 瓦片使用政策](https://operations.osmfoundation.org/policies/tiles/)（个人/轻量用途可接受）。
- 仅对带 GPS EXIF 的照片显示标记；真实 GPS 端到端依赖 FR-31 真机提取（与 FR-38 同）。
- 地图页一次性分页拉取全部地理标记媒体（每页 100），地理标记子集通常有界；若未来量大需改为按视口/聚合加载（YAGNI，暂不做）。

## 备选方案
- **mapbox-gl-js**：体验好但需 token/账号、商用授权约束，与「无账号轻量」取向不符，不采用。
- **maplibre-gl**：开源无 token，但体量与矢量瓦片复杂度对本用例过度，不采用。
- **仅列出 GPS 坐标不画地图**：成本最低但不满足 FR-39「地理分布视图」，不采用。
