import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import ClipExportPanel from './ClipExportPanel';

const enqueueMock = vi.fn();
vi.mock('@/api/library', () => ({
  enqueueClipExport: (...a: unknown[]) => enqueueMock(...a),
  exportDownloadUrl: (id: string | number) => `/api/library/exports/${id}/download`,
}));

vi.mock('@/api/tasks', () => ({
  getTask: vi.fn().mockResolvedValue({ status: 'pending' }),
}));

vi.mock('@mantine/notifications', () => ({
  notifications: { show: vi.fn() },
}));

function renderPanel(props: Partial<React.ComponentProps<typeof ClipExportPanel>> = {}) {
  return render(
    <MantineProvider>
      <ClipExportPanel opened mediaId={9} duration={120} onClose={vi.fn()} {...props} />
    </MantineProvider>,
  );
}

describe('ClipExportPanel FR2-039', () => {
  beforeEach(() => {
    enqueueMock.mockReset();
    enqueueMock.mockResolvedValue({ status: 'queued', task_id: '11' });
  });

  it('渲染起止与导出按钮', () => {
    renderPanel();
    expect(screen.getByText('起始（秒）')).toBeInTheDocument();
    expect(screen.getByText('结束（秒）')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '导出' })).toBeInTheDocument();
  });

  it('点击导出调用入队 API', async () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: '导出' }));
    await waitFor(() => {
      expect(enqueueMock).toHaveBeenCalledWith(
        9,
        expect.objectContaining({ format: 'mp4', start_sec: 0 }),
      );
    });
  });
});
