import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MemoryRouter } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import PregenDialog from './PregenDialog';
import { server } from '@/mocks/beforeAll';
import type { TranscodePreset } from '@/types';

const mockNotificationShow = vi.fn();

vi.mock('@mantine/notifications', () => ({
  notifications: {
    show: (...args: unknown[]) => mockNotificationShow(...args),
  },
}));

function renderDialog() {
  return render(
    <MantineProvider>
      <MemoryRouter>
        <PregenDialog opened onClose={() => {}} mediaID={42} />
      </MemoryRouter>
    </MantineProvider>,
  );
}

const samplePreset: TranscodePreset = {
  id: 3,
  name: '720p AV1',
  codec: 'av1',
  width: 1280,
  height: 720,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
};

describe('PregenDialog 加入预生成队列弹窗', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('无预设时引导去转码预设页', async () => {
    server.use(http.get('*/api/transcode/presets', () => HttpResponse.json({ items: [] })));
    renderDialog();
    await waitFor(() => {
      expect(screen.getByText(/还没有转码预设/)).toBeVisible();
    });
  });

  it('选预设并加入队列，发起入队请求', async () => {
    let payload: { media_id?: number; preset_id?: number } | null = null;
    server.use(
      http.get('*/api/transcode/presets', () => HttpResponse.json({ items: [samplePreset] })),
      http.post('*/api/transcode/tasks', async ({ request }) => {
        payload = (await request.json()) as { media_id: number; preset_id: number };
        return HttpResponse.json({ status: 'queued', task_id: 1 });
      }),
    );
    renderDialog();

    // 默认选中首个预设后可直接加入
    await screen.findByText('选择预设');
    await userEvent.click(screen.getByRole('button', { name: '加入队列' }));

    await waitFor(() => {
      expect(payload).not.toBeNull();
      expect(payload?.media_id).toBe(42);
      expect(payload?.preset_id).toBe(3);
    });
  });
});
