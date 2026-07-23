import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';

import { server } from '@/mocks/beforeAll';
import { applyRollback, listRollbackEvents } from './rollback';

describe('rollback API（FR2-041）', () => {
  it('按参数请求可回滚事件列表', async () => {
    const requestedUrls: URL[] = [];
    server.use(
      http.get('*/api/rollback/events', ({ request }) => {
        requestedUrls.push(new URL(request.url));
        return HttpResponse.json({
          items: [
            {
              id: 11,
              scope: 'space',
              space_id: 'space-default',
              action: 'media.deleted',
              resource_type: 'media',
              resource_id: '42',
              before_json: { file_name: 'a.mp4' },
              after_json: { deleted_at: '2026-07-08T08:00:00Z' },
              created_at: '2026-07-08T08:00:00Z',
              rollbackable: true,
              reason_key: '',
            },
          ],
          next_cursor: null,
        });
      }),
    );

    const page = await listRollbackEvents({ days: 30, limit: 20, scope: 'space' });
    expect(page.items).toHaveLength(1);
    expect(page.items[0].rollbackable).toBe(true);
    expect(page.items[0].action).toBe('media.deleted');
    const url = requestedUrls[0];
    expect(url.pathname).toBe('/api/rollback/events');
    expect(url.searchParams.get('days')).toBe('30');
    expect(url.searchParams.get('limit')).toBe('20');
    expect(url.searchParams.get('scope')).toBe('space');
  });

  it('apply 携带 event_id 与 confirm=true', async () => {
    let body: { event_id?: number; confirm?: boolean } = {};
    server.use(
      http.post('*/api/rollback/apply', async ({ request }) => {
        body = (await request.json()) as { event_id?: number; confirm?: boolean };
        return new HttpResponse(null, { status: 204 });
      }),
    );

    await applyRollback(99);
    expect(body.event_id).toBe(99);
    expect(body.confirm).toBe(true);
  });
});
