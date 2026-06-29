// SystemPage 的区块类型与文案映射（FR-113）：从页面文件拆出，
// 使 SystemPage.tsx 仅导出组件，满足 react-refresh/only-export-components。

// 系统区块取值（FR-113 一级 tab）：运行环境 / 硬件加速 / 编解码 / 应用更新；
// 由 ConsolePage 的一级 tab 驱动选中哪个区块，SystemPage 仅渲染该区块（不再有内层 tab）。
export type SystemSection = 'env' | 'hwaccel' | 'codec' | 'update';

// 区块 → 一级 tab 名称映射（FR-113 后续修复）：内容区 order-2 标题按当前 section 显示对应 tab 名，
// 与 ConsolePage 的 Tabs.Tab 文案保持一致（避免「应用更新」tab 却显示「系统诊断」）。集中一处定义，不散落硬编码。
export const SECTION_TITLES: Record<SystemSection, string> = {
  env: '运行环境',
  hwaccel: '硬件加速',
  codec: '编解码',
  update: '应用更新',
};
