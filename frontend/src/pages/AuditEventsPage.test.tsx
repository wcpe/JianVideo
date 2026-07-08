import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';
import { MantineProvider } from '@mantine/core';

import { server } from '@/mocks/beforeAll';
import AuditEventsPage from './AuditEventsPage';

function renderPage() {
  return render(
    <MantineProvider>
      <AuditEventsPage />
    </MantineProvider>,
  );
}

describe('AuditEventsPage（FR2-040）', () => {
  it('加载审计事件列表并展示脱敏详情 JSON', async () => {
    const requests: string[] = [];
    server.use(
      http.get('*/api/audit/events', ({ request }) => {
        requests.push(request.url);
        return HttpResponse.json({
          items: [
            {
              id: 1,
              scope: 'space',
              space_id: 'space-a',
              actor_type: 'user',
              actor_id: 'admin',
              action: 'settings.updated',
              resource_type: 'settings',
              resource_id: 'network_proxy',
              before_json: { network_proxy: '***' },
              after_json: { network_proxy: '***' },
              metadata_json: { summary: '更新网络代理' },
              request_id: 'req-1',
              created_at: '2026-07-08T08:00:00Z',
            },
          ],
          next_cursor: 'cursor-next',
        });
      }),
    );

    const user = userEvent.setup();
    renderPage();

    expect(await screen.findByRole('heading', { name: '审计事件' })).toBeVisible();
    expect(await screen.findByText('settings.updated')).toBeVisible();
    await user.click(screen.getByRole('button', { name: '查看详情' }));

    const dialog = await screen.findByRole('dialog', { name: '审计事件详情' });
    const redactedValues = within(dialog).getAllByText(
      (_, el) => el?.textContent?.includes('"network_proxy": "***"') ?? false,
    );
    expect(redactedValues.length).toBeGreaterThan(0);
    expect(within(dialog).queryByText('http://user:password@example.com')).toBeNull();
    expect(requests[0]).toContain('/api/audit/events');
  });

  it('提交基础筛选并通过 cursor 加载更多', async () => {
    const requests: URL[] = [];
    server.use(
      http.get('*/api/audit/events', ({ request }) => {
        const url = new URL(request.url);
        requests.push(url);
        if (url.searchParams.get('cursor') === 'cursor-next') {
          return HttpResponse.json({
            items: [
              {
                id: 2,
                scope: 'system',
                space_id: null,
                actor_type: 'system',
                actor_id: 'system',
                action: 'migration.succeeded',
                resource_type: 'migration',
                resource_id: '2026070801',
                before_json: null,
                after_json: { version: '2026070801' },
                metadata_json: null,
                request_id: 'req-2',
                created_at: '2026-07-08T09:00:00Z',
              },
            ],
            next_cursor: null,
          });
        }
        return HttpResponse.json({
          items: [],
          next_cursor: 'cursor-next',
        });
      }),
    );

    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByRole('radio', { name: '系统' }));
    await user.type(screen.getByLabelText('动作'), 'media.deleted');
    await user.type(screen.getByLabelText('资源类型'), 'media');
    await user.type(screen.getByLabelText('资源 ID'), '42');
    await user.type(screen.getByLabelText('Space ID'), 'space-a');
    await user.click(screen.getByRole('button', { name: '查询' }));

    await waitFor(() => expect(requests.length).toBeGreaterThanOrEqual(2));
    const filtered = requests.at(-1);
    expect(filtered?.searchParams.get('scope')).toBe('system');
    expect(filtered?.searchParams.get('action')).toBe('media.deleted');
    expect(filtered?.searchParams.get('resource_type')).toBe('media');
    expect(filtered?.searchParams.get('resource_id')).toBe('42');
    expect(filtered?.searchParams.get('space_id')).toBeNull();

    await user.click(screen.getByRole('button', { name: '加载更多' }));
    expect(await screen.findByText('migration.succeeded')).toBeVisible();
    expect(requests.at(-1)?.searchParams.get('cursor')).toBe('cursor-next');
  });
});
