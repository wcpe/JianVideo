export type ComponentState = 'default' | 'loading' | 'disabled' | 'empty' | 'error' | 'selected' | 'dense' | 'mobile';
export type UiPreviewGroup = 'basic' | 'media' | 'task' | 'space' | 'feedback' | 'pixi' | 'theme';

export interface MuseumSnippet {
  readonly importPath: string;
  readonly code: string;
}

export interface UiPreviewDescriptor {
  readonly id: string;
  readonly title: string;
  readonly group: UiPreviewGroup;
  readonly states: readonly ComponentState[];
  readonly snippet: MuseumSnippet;
  readonly scenarioIds: readonly string[];
}

export function createSnippet(importName: string, packagePath: string): MuseumSnippet {
  return {
    importPath: packagePath,
    code: `import { ${importName} } from '${packagePath}';`,
  };
}

export const uiPreviewCatalog: readonly UiPreviewDescriptor[] = [
  preview('action-button', '按钮', 'basic', 'ActionButtonPreview', ['default', 'loading', 'disabled']),
  preview('text-input', '输入框', 'basic', 'TextInputPreview', ['default', 'error', 'disabled']),
  preview('menu-button', '菜单按钮', 'basic', 'MenuButtonPreview', ['default', 'selected', 'disabled']),
  preview('data-table', '数据表格', 'basic', 'DataTablePreview', ['default', 'empty', 'loading', 'dense']),
  preview('media-card', '媒体卡片', 'media', 'MediaCardPreview', ['default', 'loading', 'error', 'selected'], [
    'normal-library',
    'million-assets',
  ]),
  preview('hls-preview-card', 'HLS 预览卡', 'media', 'HlsPreviewCardPreview', ['default', 'loading', 'error'], [
    'hls-pending',
    'transcode-failed',
  ]),
  preview('thumbnail-status', '缩略图状态', 'media', 'ThumbnailStatusPreview', ['default', 'loading', 'error', 'empty'], [
    'missing-thumbnail',
    'normal-library',
  ]),
  preview('task-status', '转码任务状态', 'task', 'TaskStatusPreview', ['default', 'loading', 'error'], [
    'hls-pending',
    'transcode-failed',
  ]),
  preview('ai-task-status', 'AI 任务状态', 'task', 'AiTaskStatusPreview', ['default', 'loading', 'error'], [
    'ai-review-pending',
  ]),
  preview('space-permission-status', 'Space 权限状态', 'space', 'SpacePermissionStatusPreview', [
    'default',
    'disabled',
    'error',
  ], ['permission-denied']),
  preview('empty-state', '空态', 'feedback', 'EmptyStatePreview', ['empty', 'default'], ['empty-library']),
  preview('error-state', '错误态', 'feedback', 'ErrorStatePreview', ['error', 'default'], ['transcode-failed']),
] as const;

export function groupUiPreviews(items: readonly UiPreviewDescriptor[]): Record<UiPreviewGroup, readonly UiPreviewDescriptor[]> {
  return {
    basic: items.filter((item) => item.group === 'basic'),
    media: items.filter((item) => item.group === 'media'),
    task: items.filter((item) => item.group === 'task'),
    space: items.filter((item) => item.group === 'space'),
    feedback: items.filter((item) => item.group === 'feedback'),
    pixi: items.filter((item) => item.group === 'pixi'),
    theme: items.filter((item) => item.group === 'theme'),
  };
}

export function findUiPreview(id: string): UiPreviewDescriptor {
  const previewItem = uiPreviewCatalog.find((item) => item.id === id);
  if (!previewItem) {
    throw new Error(`未知 UI 预览：${id}`);
  }
  return previewItem;
}

function preview(
  id: string,
  title: string,
  group: UiPreviewGroup,
  importName: string,
  states: readonly ComponentState[],
  scenarioIds: readonly string[] = [],
): UiPreviewDescriptor {
  return {
    id,
    title,
    group,
    states,
    scenarioIds,
    snippet: createSnippet(importName, '@jianvideo/ui'),
  };
}
