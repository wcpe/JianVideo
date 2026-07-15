import type { TaskItem, TaskListPage, TaskListQuery, TaskStats } from '@/types';
import client from './client';

function cleanQuery(query: TaskListQuery): Record<string, string | number> {
  const params: Record<string, string | number> = {};
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === '') continue;
    params[key] = value;
  }
  return params;
}

export async function listTasks(query: TaskListQuery = {}): Promise<TaskListPage> {
  const res = await client.get<TaskListPage>('/api/tasks', { params: cleanQuery(query) });
  return res.data;
}

export async function getTaskStats(
  query: Pick<TaskListQuery, 'scope' | 'status' | 'type'> = {},
): Promise<TaskStats> {
  const res = await client.get<TaskStats>('/api/tasks/stats', { params: cleanQuery(query) });
  return res.data;
}

export async function getTask(id: string, signal?: AbortSignal): Promise<TaskItem> {
  const res = await client.get<TaskItem>(`/api/tasks/${encodeURIComponent(id)}`, { signal });
  return res.data;
}

export async function cancelTask(id: string): Promise<TaskItem> {
  const res = await client.post<TaskItem>(`/api/tasks/${encodeURIComponent(id)}/cancel`);
  return res.data;
}

export async function retryTask(id: string): Promise<TaskItem> {
  const res = await client.post<TaskItem>(`/api/tasks/${encodeURIComponent(id)}/retry`);
  return res.data;
}
