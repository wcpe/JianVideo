import { describe, expect, it } from 'vitest';
import { createSnippet, findUiPreview, groupUiPreviews, uiPreviewCatalog } from './index';

describe('ui package', () => {
  it('代码片段指向 packages 导入路径', () => {
    const snippet = createSnippet('MediaCardPreview', '@jianvideo/ui');

    expect(snippet.importPath).toBe('@jianvideo/ui');
    expect(snippet.code).toContain("from '@jianvideo/ui'");
  });

  it('暴露 wiki 首批可复用控件描述', () => {
    expect(uiPreviewCatalog.map((item) => item.id)).toEqual(
      expect.arrayContaining([
        'action-button',
        'text-input',
        'menu-button',
        'data-table',
        'media-card',
        'hls-preview-card',
        'thumbnail-status',
        'task-status',
        'ai-task-status',
        'space-permission-status',
        'empty-state',
        'error-state',
      ]),
    );
  });

  it('支持按分组读取任务与媒体控件', () => {
    const groups = groupUiPreviews(uiPreviewCatalog);

    expect(groups.task.map((item) => item.id)).toEqual(expect.arrayContaining(['task-status', 'ai-task-status']));
    expect(groups.media.map((item) => item.id)).toEqual(
      expect.arrayContaining(['media-card', 'hls-preview-card', 'thumbnail-status']),
    );
  });

  it('HLS 预览卡绑定 HLS 生成中场景与 packages snippet', () => {
    const preview = findUiPreview('hls-preview-card');

    expect(preview.scenarioIds).toContain('hls-pending');
    expect(preview.snippet.importPath).toBe('@jianvideo/ui');
  });
});
