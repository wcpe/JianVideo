// AnchorNav 的纯逻辑与类型（FR-113）：从组件文件拆出，使 AnchorNav.tsx 仅导出组件，
// 满足 react-refresh/only-export-components（Fast Refresh 要求文件只导出组件）。

// 锚点项：id 对应区块 DOM 元素 id，label 为导航显示文案
export interface AnchorSection {
  id: string;
  label: string;
}

// 区块的滚动定位信息：id 与其顶部相对滚动原点的偏移（像素）
export interface SectionOffset {
  id: string;
  top: number;
}

// scroll-spy 小提前量（像素）：在吸顶偏移之外再多探这一点，
// 使区块顶部恰好贴到吸顶条下沿时即归属该区块，避免临界处差一像素的抖动。
const SPY_LEAD = 8;

/**
 * pickActiveByScroll 纯逻辑：按滚动位置判定当前应高亮的锚点（健壮 scroll-spy，无死区）。
 *
 * 判定线 = scrollPos + stickyOffset + 小提前量：stickyOffset 为「固定页眉 + sticky 一级 tab 条」
 * 占住的视口顶部高度（运行时实测、可注入），使高亮的是「吸顶区下方可读区顶部」所在区块（与肉眼一致），
 * 而非绝对顶部所在区块（后者会偏前一个）。
 *
 * 规则（任何位置都有确定结果、绝不无故回退到无关首项）：
 * - 已触达页面底部（atBottom）→ 取最后一个区块（兜底「末节太短、其顶部永远到不了判定线」的边角，
 *   否则滚到底会停在倒数第二节）；
 * - 否则取「顶部已越过判定线」的**最后一个**区块，即可读区顶部当前所在区块；
 * - 滚到最顶（没有任何区块越过判定线）→ 取第一个区块；
 * - offsets 为空（DOM 尚未就绪/无区块）→ 保留传入的 currentActive，不强行回退首项。
 *
 * offsets 须按文档序（top 升序）传入；抽为纯函数便于穷举单测「中部空隙/未越带/触底/吸顶偏移」等场景。
 */
export function pickActiveByScroll(
  offsets: SectionOffset[],
  scrollPos: number,
  currentActive: string,
  atBottom = false,
  stickyOffset = 0,
): string {
  if (offsets.length === 0) return currentActive;
  if (atBottom) return offsets[offsets.length - 1].id;
  const line = scrollPos + stickyOffset + SPY_LEAD;
  let active = offsets[0].id;
  for (const o of offsets) {
    if (o.top <= line) active = o.id;
    else break;
  }
  return active;
}

/**
 * measureStickyOffset 实测「稳定的吸顶偏移」：固定页眉高度 + sticky 一级 tab 条高度。
 *
 * 关键：必须返回 **stuck 后的稳定值、与当前滚动位置无关**。固定页眉（`.mantine-AppShell-header`，
 * position:fixed）恒占 y=0..headerHeight；一级 tab 条（`.console-tabs > .mantine-Tabs-list`，
 * sticky 于页眉之下，CSS `top: var(--app-shell-header-height)`）stuck 后紧贴页眉下沿。
 * 故可读区顶部 = 页眉高度 + tab 条高度，二者均取 `offsetHeight`（不随滚动变化）——
 * 使「点击时（页面或在顶部、tab 尚未 stuck）」与「滚动后（tab 已 stuck）」测得同一值，
 * 保证点击落点（scroll-margin-top）与高亮判定线一致。
 *
 * 不写死像素：页眉高度取页眉元素 `offsetHeight`（随 AppShell header 配置/安全区变化），
 * tab 条高度取其 `offsetHeight`。任一缺失（无吸顶场景，如单测/其它布局）按 0 计。
 */
export function measureStickyOffset(): number {
  if (typeof document === 'undefined') return 0;
  const header = document.querySelector('.mantine-AppShell-header');
  const list = document.querySelector('.console-tabs > .mantine-Tabs-list');
  const headerH = header instanceof HTMLElement ? header.offsetHeight : 0;
  const listH = list instanceof HTMLElement ? list.offsetHeight : 0;
  return headerH + listH;
}
