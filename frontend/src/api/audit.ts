import type { AuditEventPage, AuditEventQuery } from '@/types';
import client from './client';

const DEFAULT_LIMIT = 20;

function cleanQuery(query: AuditEventQuery): Record<string, string | number> {
  const params: Record<string, string | number> = {};
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === '') continue;
    params[key] = value;
  }
  if (!params.limit) params.limit = DEFAULT_LIMIT;
  return params;
}

export async function listAuditEvents(query: AuditEventQuery = {}): Promise<AuditEventPage> {
  const res = await client.get<AuditEventPage>('/api/audit/events', {
    params: cleanQuery(query),
  });
  return {
    items: res.data.items ?? [],
    next_cursor: res.data.next_cursor ?? null,
  };
}
