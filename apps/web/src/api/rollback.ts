import type { RollbackEventPage, RollbackEventQuery } from '@/types';
import client from './client';

const DEFAULT_LIMIT = 50;

function cleanQuery(query: RollbackEventQuery): Record<string, string | number> {
  const params: Record<string, string | number> = {};
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === '') continue;
    params[key] = value;
  }
  if (!params.limit) params.limit = DEFAULT_LIMIT;
  return params;
}

/** 列出近 N 天可回滚标注事件（FR2-041） */
export async function listRollbackEvents(
  query: RollbackEventQuery = {},
): Promise<RollbackEventPage> {
  const res = await client.get<RollbackEventPage>('/api/rollback/events', {
    params: cleanQuery(query),
  });
  return {
    items: res.data.items ?? [],
    next_cursor: res.data.next_cursor ?? null,
  };
}

/** 执行回滚（强制 confirm=true，FR2-041） */
export async function applyRollback(eventId: number): Promise<void> {
  await client.post('/api/rollback/apply', {
    event_id: eventId,
    confirm: true,
  });
}
