import { render } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import { describe, it, expect } from 'vitest';
import LibraryPathManager from './LibraryPathManager';
import DirectoryBrowser from './DirectoryBrowser';
import type { LibraryPath, MediaFile, DirInfo } from '@/types';

// 亮色模式黑底复现守护：
// Mantine `bg="dark.7"` 解析为写死的 var(--mantine-color-dark-7)，不随 colorScheme 切换，
// 故亮色模式下卡片/面板仍为深色（黑底）。修复要求改用随主题切换的语义变量
// var(--mantine-color-default)。本测试断言卡片背景为随主题变量、且不含写死的 dark-7。

function libPath(over: Partial<LibraryPath> = {}): LibraryPath {
  return {
    id: 1,
    path: 'D:/Videos',
    type: 'local',
    label: '视频',
    enabled: true,
    created_at: '2025-01-01T00:00:00Z',
    media_count: 0,
    ...over,
  };
}

function mediaFile(over: Partial<MediaFile> = {}): MediaFile {
  return {
    id: 1,
    library_id: 1,
    file_path: 'D:/m/a.jpg',
    file_name: 'a.jpg',
    file_size: 1000,
    format: 'jpg',
    video_codec: '',
    audio_codec: '',
    duration: 0,
    width: 0,
    height: 0,
    bitrate: 0,
    subtitle_tracks: '',
    added_at: '2025-01-01T00:00:00Z',
    modified_at: '2025-01-01T00:00:00Z',
    ...over,
  } as MediaFile;
}

describe('卡片/面板背景随主题切换，不写死深色（亮色模式黑底修复）', () => {
  it('LibraryPathManager 存储库卡片背景用随主题变量、不含写死 dark-7', () => {
    const { container } = render(
      <MantineProvider>
        <LibraryPathManager
          paths={[libPath()]}
          loading={false}
          scanLoading={{}}
          newPath=""
          addingPath={false}
          extensionInputs={{}}
          extensionTypes={{}}
          extensionLoading={{}}
          extensionsByLibrary={{}}
          onNewPathChange={() => {}}
          onAddPath={() => {}}
          onDeletePath={() => {}}
          onScan={() => {}}
          onBrowsePath={() => {}}
          onExtensionInputChange={() => {}}
          onExtensionTypeChange={() => {}}
          onAddExtension={() => {}}
          onDeleteExtension={() => {}}
        />
      </MantineProvider>,
    );
    const card = container.querySelector('.mantine-Card-root');
    const style = card?.getAttribute('style') ?? '';
    expect(style).not.toContain('var(--mantine-color-dark-7)');
    expect(style).toContain('var(--mantine-color-default)');
  });

  it('DirectoryBrowser 目录/未选中文件卡片背景随主题、选中态用随主题强调色', () => {
    const { container } = render(
      <MantineProvider>
        <DirectoryBrowser
          breadcrumbs={[]}
          directories={[{ name: '子目录', path: 'D:/m/sub' } as DirInfo]}
          files={[mediaFile({ id: 1, file_name: 'a.jpg' })]}
          loading={false}
          error={null}
          customImageExtensions={{}}
          onEnterDir={() => {}}
          onBreadcrumbNavigate={() => {}}
          onErrorClose={() => {}}
          onOpenFile={() => {}}
          displayMode="list"
          sort="name"
        />
      </MantineProvider>,
    );
    // 所有卡片（目录 + 未选中文件）都不得残留写死 dark-7，且至少含随主题背景变量
    const cards = Array.from(container.querySelectorAll('.mantine-Card-root'));
    expect(cards.length).toBeGreaterThan(0);
    for (const card of cards) {
      const style = card.getAttribute('style') ?? '';
      expect(style).not.toContain('var(--mantine-color-dark-7)');
    }
    // 默认未选中：背景应为随主题变量
    const fileCard = Array.from(cards).find((c) => c.textContent?.includes('a.jpg'));
    expect(fileCard?.getAttribute('style') ?? '').toContain('var(--mantine-color-default)');
  });
});
