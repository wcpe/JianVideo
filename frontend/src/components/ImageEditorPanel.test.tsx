import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import ImageEditorPanel from './ImageEditorPanel';

const enqueueMock = vi.fn();
vi.mock('@/api/library', () => ({
  enqueueImageExport: (...a: unknown[]) => enqueueMock(...a),
  exportDownloadUrl: (id: string | number) => `/api/library/exports/${id}/download`,
  mediaRawUrl: () => '/mock-raw',
}));

vi.mock('@/api/tasks', () => ({
  getTask: vi.fn().mockResolvedValue({ status: 'pending' }),
}));

vi.mock('@/utils/media-url', () => ({
  mediaRawUrl: () => '/mock-raw',
}));

vi.mock('@mantine/notifications', () => ({
  notifications: { show: vi.fn() },
}));

function renderPanel(props: Partial<React.ComponentProps<typeof ImageEditorPanel>> = {}) {
  return render(
    <MantineProvider>
      <ImageEditorPanel opened mediaId={42} onClose={vi.fn()} {...props} />
    </MantineProvider>,
  );
}

describe('ImageEditorPanel FR2-038', () => {
  beforeEach(() => {
    enqueueMock.mockReset();
    enqueueMock.mockResolvedValue({ status: 'queued', task_id: '7' });
  });

  it('渲染滑杆与导出按钮', () => {
    renderPanel();
    expect(screen.getByText('曝光')).toBeInTheDocument();
    expect(screen.getByText('对比度')).toBeInTheDocument();
    expect(screen.getByText('饱和度')).toBeInTheDocument();
    expect(screen.getByText('色温')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '导出' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '重置' })).toBeInTheDocument();
  });

  it('点击导出调用入队 API', async () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: '导出' }));
    await waitFor(() => {
      expect(enqueueMock).toHaveBeenCalledWith(
        42,
        expect.objectContaining({ format: 'jpeg' }),
      );
    });
  });

  it('重置恢复默认参数后仍可导出', async () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: '重置' }));
    fireEvent.click(screen.getByRole('button', { name: '导出' }));
    await waitFor(() => {
      expect(enqueueMock).toHaveBeenCalled();
    });
  });
});
