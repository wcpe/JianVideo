import { mockScenarios } from '@jianvideo/mock';
import { createSnippet, type UiPreviewDescriptor } from '@jianvideo/ui';

export const wikiPreviewCatalog: readonly UiPreviewDescriptor[] = [
  {
    id: 'media-card',
    title: '媒体卡片',
    group: 'media',
    states: ['default', 'loading', 'error', 'selected'],
    snippet: createSnippet('MediaCardPreview', '@jianvideo/ui'),
  },
  {
    id: 'task-status',
    title: '任务状态',
    group: 'task',
    states: ['default', 'loading', 'error'],
    snippet: createSnippet('TaskStatusPreview', '@jianvideo/ui'),
  },
] as const;

export function listWikiScenarioTitles(): readonly string[] {
  return mockScenarios.map((scenario) => scenario.title);
}
