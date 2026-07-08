import { describe, it, expect } from 'vitest';
import { http, HttpResponse } from 'msw';

import { server } from '@/mocks/beforeAll';
import { listAuditEvents } from './audit';

describe('audit API（FR2-040）', () => {
  it('按筛选条件请求审计事件并返回 cursor 分页结果', async () => {
    const requestedUrls: URL[] = [];
    server.use(
      http.get('*/api/audit/events', ({ request }) => {
        requestedUrls.push(new URL(request.url));
        return HttpResponse.json({
          items: [
            {
              id: 9,
              scope: 'space',
              space_id: 'space-a',
              actor_type: 'user',
              actor_id: 'admin',
              action: 'media.deleted',
              resource_type: 'media',
              resource_id: '42',
              before_json: { file_name: 'old.mp4' },
              after_json: null,
              metadata_json: { request_ip: '127.0.0.1' },
              request_id: 'req-1',
              created_at: '2026-07-08T08:00:00Z',
            },
          ],
          next_cursor: 'cursor-2',
        });
      }),
    );

    const page = await listAuditEvents({
      space_id: 'space-a',
      action: 'media.deleted',
      resource_type: 'media',
      resource_id: '42',
      from: '2026-07-01T00:00:00Z',
      to: '2026-07-08T23:59:59Z',
      cursor: 'cursor-1',
      limit: 20,
    });

    expect(page.items).toHaveLength(1);
    expect(page.next_cursor).toBe('cursor-2');
    const url = requestedUrls[0];
    expect(url.pathname).toBe('/api/audit/events');
    expect(url.searchParams.get('space_id')).toBe('space-a');
    expect(url.searchParams.get('action')).toBe('media.deleted');
    expect(url.searchParams.get('resource_type')).toBe('media');
    expect(url.searchParams.get('resource_id')).toBe('42');
    expect(url.searchParams.get('from')).toBe('2026-07-01T00:00:00Z');
    expect(url.searchParams.get('to')).toBe('2026-07-08T23:59:59Z');
    expect(url.searchParams.get('cursor')).toBe('cursor-1');
    expect(url.searchParams.get('limit')).toBe('20');
  });
});
