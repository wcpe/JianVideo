# ADR-0048：引入 framer-motion 动效库

## 状态
已接受

## 背景
现有前端动效全部为纯 CSS transition/keyframes（FR-96），零依赖、契合"简单优先"。但本轮 UI 优化需要列表项进入/退出、共享元素过渡、轮播等纯 CSS 难以优雅实现的动效（FR-135 及时间轴 / 回忆轮播相关）。

## 决策
引入 framer-motion 作为前端动效库，仅用于纯 CSS 难做的复杂动效；常规 hover / 过渡仍用 CSS。所有 framer-motion 动效在 `prefers-reduced-motion` 下全部禁用。

## 理由
- 复杂编排（进入 / 退出、布局动画、共享元素）用 framer-motion 远比手写 CSS/JS 可维护。
- 与 React 19 兼容良好，可按需引入并代码分割控制体积。
- 属"简单优先"的明确例外：有真实变化点（多处复杂动效需求）才引入，而非为未来预留。

## 后果
- 新增一个前端运行时依赖，需纳入包体与构建考量（按需引入 / 懒加载控制体积）。
- 所有动效须统一遵守 `prefers-reduced-motion` 守护（保留现有无障碍测试）。
- 既有纯 CSS 动效不强制迁移，避免无谓改动。

## 备选方案
- 纯 CSS / Web Animations API：零依赖但复杂编排成本高、可维护性差。
- react-spring：能力相近，社区与文档不及 framer-motion。
- 继续不引入：放弃部分高级动效，体验受限（用户已明确要更丰富动效）。
