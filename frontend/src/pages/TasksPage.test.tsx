import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { MantineProvider } from '@mantine/core';
import { Notifications } from '@mantine/notifications';

import { server } from '@/mocks/beforeAll';
import TasksPage from './TasksPage';

function renderPage() {
  return render(
    <MantineProvider>
      <Notifications />
      <TasksPage />
    </MantineProvider>,
  );
}

describe('TasksPage（FR2-037）', () => {
  it('展示任务列表、统计并按状态筛选', async () => {
    const requests: URL[] = [];
    server.use(
      http.get('*/api/tasks', ({ request }) => {
        const url = new URL(request.url);
        requests.push(url);
        const status = url.searchParams.get('status');
        const items =
          status === 'failed'
            ? [
                {
                  id: '12',
                  scope: 'space',
                  space_id: 'space-default',
                  type: 'transcode.hls',
                  status: 'failed',
                  priority: 5,
                  attempts: 1,
                  max_attempts: 3,
                  progress: 0.4,
                  resource_type: 'media',
                  resource_id: '99',
                  error: '编码器不可用',
                  created_at: '2026-07-08T08:00:00Z',
                  updated_at: '2026-07-08T08:01:00Z',
                },
              ]
            : [
                {
                  id: '11',
                  scope: 'space',
                  space_id: 'space-default',
                  type: 'library.scan',
                  status: 'running',
                  priority: 10,
                  attempts: 0,
                  max_attempts: 1,
                  progress: 0.35,
                  resource_type: 'library',
                  resource_id: '1',
                  error: null,
                  created_at: '2026-07-08T08:00:00Z',
                  updated_at: '2026-07-08T08:01:00Z',
                },
              ];
        return HttpResponse.json({ items, page: 1, page_size: 20, total: items.length });
      }),
      http.get('*/api/tasks/stats', () =>
        HttpResponse.json({
          total: 2,
          by_status: { pending: 0, running: 1, succeeded: 0, failed: 1, canceled: 0 },
          by_type: { 'library.scan': 1, 'transcode.hls': 1 },
        }),
      ),
    );

    const user = userEvent.setup();
    renderPage();

    expect(await screen.findByRole('heading', { name: '任务中心' })).toBeVisible();
    expect(await screen.findByText('library.scan')).toBeVisible();
    expect(screen.getAllByText('运行中').length).toBeGreaterThan(0);

    await user.click(screen.getAllByLabelText('状态')[0]);
    await user.click(await screen.findByRole('option', { name: '失败' }));
    await user.click(screen.getByRole('button', { name: '查询' }));

    expect(await screen.findByText('transcode.hls')).toBeVisible();
    expect(screen.getByText('编码器不可用')).toBeVisible();
    await waitFor(() => expect(requests.at(-1)?.searchParams.get('status')).toBe('failed'));
  });

  it('支持取消和重试任务后刷新列表', async () => {
    let canceled = false;
    let retried = false;
    server.use(
      http.get('*/api/tasks', () =>
        HttpResponse.json({
          items: [
            {
              id: '21',
              scope: 'space',
              space_id: 'space-default',
              type: 'library.scan',
              status: canceled ? 'canceled' : 'running',
              priority: 10,
              attempts: 0,
              max_attempts: 1,
              progress: 0.5,
              resource_type: 'library',
              resource_id: '1',
              error: null,
              created_at: '2026-07-08T08:00:00Z',
              updated_at: '2026-07-08T08:01:00Z',
            },
            {
              id: '22',
              scope: 'space',
              space_id: 'space-default',
              type: 'transcode.hls',
              status: retried ? 'pending' : 'failed',
              priority: 5,
              attempts: retried ? 0 : 1,
              max_attempts: 3,
              progress: retried ? 0 : 0.3,
              resource_type: 'media',
              resource_id: '8',
              error: retried ? null : '失败',
              created_at: '2026-07-08T08:00:00Z',
              updated_at: '2026-07-08T08:01:00Z',
            },
          ],
          page: 1,
          page_size: 20,
          total: 2,
        }),
      ),
      http.get('*/api/tasks/stats', () =>
        HttpResponse.json({
          total: 2,
          by_status: {
            pending: retried ? 1 : 0,
            running: canceled ? 0 : 1,
            succeeded: 0,
            failed: retried ? 0 : 1,
            canceled: canceled ? 1 : 0,
          },
          by_type: { 'library.scan': 1, 'transcode.hls': 1 },
        }),
      ),
      http.post('*/api/tasks/21/cancel', () => {
        canceled = true;
        return HttpResponse.json({});
      }),
      http.post('*/api/tasks/22/retry', () => {
        retried = true;
        return HttpResponse.json({});
      }),
    );

    const user = userEvent.setup();
    renderPage();

    expect(await screen.findByText('library.scan')).toBeVisible();
    await user.click(screen.getAllByRole('button', { name: '取消' })[0]);
    expect((await screen.findAllByText('已取消')).length).toBeGreaterThan(0);

    await user.click(screen.getAllByRole('button', { name: '重试' })[1]);
    expect((await screen.findAllByText('排队中')).length).toBeGreaterThan(0);
  });
});
