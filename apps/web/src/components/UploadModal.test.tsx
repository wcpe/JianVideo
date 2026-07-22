import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MantineProvider } from '@mantine/core';
import { describe, it, expect, beforeEach, vi } from 'vitest';

import UploadModal from './UploadModal';
import * as libraryApi from '@/api/library';
import * as settingsApi from '@/api/settings';

// 桩：库目录与设置默认值
vi.mock('@/api/library', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/library')>();
  return {
    ...actual,
    getLibraryPaths: vi.fn(),
    uploadMedia: vi.fn(),
  };
});
vi.mock('@/api/settings', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/settings')>();
  return {
    ...actual,
    getSettings: vi.fn(),
  };
});

function renderModal() {
  return render(
    <MantineProvider>
      <UploadModal opened onClose={() => {}} />
    </MantineProvider>,
  );
}

describe('UploadModal', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(libraryApi.getLibraryPaths).mockResolvedValue([
      { id: 1, path: 'D:/media', type: 'local', label: '本地', enabled: true, created_at: '' },
    ]);
    vi.mocked(settingsApi.getSettings).mockResolvedValue({
      upload_target_dir: 'D:/media',
      upload_naming_rule: 'original',
    });
  });

  it('打开时展示拖拽区与选择文件按钮', async () => {
    renderModal();
    expect(await screen.findByText('上传媒体')).toBeInTheDocument();
    expect(screen.getByText('选择文件')).toBeInTheDocument();
    // 默认上传目录提示来自设置
    await waitFor(() => expect(screen.getByText('默认：D:/media')).toBeInTheDocument());
  });

  it('选择文件后点击开始上传调用 uploadMedia', async () => {
    const user = userEvent.setup();
    vi.mocked(libraryApi.uploadMedia).mockResolvedValue({
      status: 'uploaded',
      library_id: 1,
      file_path: 'D:/media/a.jpg',
      scan_task: 9,
    });
    renderModal();
    await screen.findByText('上传媒体');

    // 通过隐藏的 file input 注入文件（FileButton 内部 input）
    const file = new File(['x'], 'a.jpg', { type: 'image/jpeg' });
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    await user.upload(input, file);

    const startBtn = await screen.findByRole('button', { name: /开始上传/ });
    await user.click(startBtn);

    await waitFor(() => expect(libraryApi.uploadMedia).toHaveBeenCalledTimes(1));
    const [arg] = vi.mocked(libraryApi.uploadMedia).mock.calls[0];
    expect((arg as File).name).toBe('a.jpg');
  });
});
