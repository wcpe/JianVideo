import {
  findScenario,
  mockScenarios,
  scanMockScenarioForSensitiveInfo,
  type MockScenarioId,
} from '@jianvideo/mock';
import { estimateTextureMemoryBytes, resolveVisibleWindow } from '@jianvideo/render-pixi';
import { themeProfiles } from '@jianvideo/theme';
import { createSnippet, uiPreviewCatalog as sharedUiPreviewCatalog, type UiPreviewDescriptor } from '@jianvideo/ui';

export type WikiGroupId = 'basic' | 'media' | 'task' | 'space' | 'pixi' | 'theme';

export interface WikiGroup {
  readonly id: WikiGroupId;
  readonly title: string;
}

export interface WikiCatalogFilter {
  readonly group?: WikiGroupId;
  readonly query?: string;
}

const wikiOnlyCatalog: readonly UiPreviewDescriptor[] = [
  {
    id: 'pixi-grid-metrics',
    title: 'PixiJS 指标入口',
    group: 'pixi',
    states: ['default', 'dense'],
    scenarioIds: ['million-assets'],
    snippet: createSnippet('resolveVisibleWindow', '@jianvideo/render-pixi'),
  },
  {
    id: 'theme-density-preview',
    title: '主题与密度',
    group: 'theme',
    states: ['default', 'dense', 'mobile'],
    scenarioIds: [],
    snippet: createSnippet('resolveMuseumTheme', '@jianvideo/theme'),
  },
  {
    id: 'api-client-demo',
    title: 'API client 示例',
    group: 'space',
    states: ['default', 'loading', 'error'],
    scenarioIds: ['normal-library', 'permission-denied'],
    snippet: createSnippet('ClientDemoPanel', '@jianvideo/wiki'),
  },
] as const;

export const wikiPreviewCatalog: readonly UiPreviewDescriptor[] = [...sharedUiPreviewCatalog, ...wikiOnlyCatalog] as const;

const wikiGroups: readonly WikiGroup[] = [
  { id: 'basic', title: '基础控件' },
  { id: 'media', title: '媒体控件' },
  { id: 'task', title: '任务队列' },
  { id: 'space', title: 'Space 权限' },
  { id: 'pixi', title: 'PixiJS 样例' },
  { id: 'theme', title: '主题与密度' },
] as const;

export function listWikiScenarioTitles(): readonly string[] {
  return mockScenarios.map((scenario) => scenario.title);
}

export function listWikiGroups(): readonly WikiGroup[] {
  return wikiGroups;
}

export function filterWikiCatalog(filter: WikiCatalogFilter): readonly UiPreviewDescriptor[] {
  const query = filter.query?.trim().toLowerCase() ?? '';
  return wikiPreviewCatalog.filter((item) => {
    const groupMatched = !filter.group || item.group === filter.group;
    const queryMatched =
      query.length === 0 ||
      [item.id, item.title, item.snippet.code, item.group, ...item.states, ...item.scenarioIds]
        .join(' ')
        .toLowerCase()
        .includes(query);
    return groupMatched && queryMatched;
  });
}

export function getWikiScenarioSummary(id: MockScenarioId): string {
  const scenario = findScenario(id);
  return `${scenario.title}：${scenario.summary}`;
}

export function scanWikiMockSensitiveInfo(): readonly string[] {
  return scanMockScenarioForSensitiveInfo();
}

export function getWikiPixiMetricSummary(): string {
  const window = resolveVisibleWindow(1_000_000, 12_000, 120, 24);
  const memoryMb = Math.round(estimateTextureMemoryBytes(144, 160, 90) / 1024 / 1024);
  return `可见窗口 ${String(window.start)}-${String(window.end)}，纹理估算 ${String(memoryMb)} MB`;
}

export function listThemeProfileTitles(): readonly string[] {
  return themeProfiles.map((profile) => profile.title);
}
