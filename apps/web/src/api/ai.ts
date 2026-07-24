import type { AIDuplicateGroup, AIResult } from '@/types';
import client from './client';

/** AI 能力状态摘要（FR2-011） */
export interface AIStatus {
  enabled: boolean;
  models: unknown[];
  nodes: unknown[];
}

export async function getAIStatus(): Promise<AIStatus> {
  const res = await client.get<AIStatus>('/api/ai/status');
  return {
    enabled: Boolean(res.data.enabled),
    models: res.data.models ?? [],
    nodes: res.data.nodes ?? [],
  };
}

export async function listAIResults(mediaID: number): Promise<AIResult[]> {
  const res = await client.get<{ items: AIResult[] }>('/api/ai/results', {
    params: { media_id: mediaID },
  });
  return res.data.items ?? [];
}

export async function confirmAIResult(id: number): Promise<void> {
  await client.post(`/api/ai/results/${id}/confirm`);
}

export async function rejectAIResult(id: number): Promise<void> {
  await client.post(`/api/ai/results/${id}/reject`);
}

export async function listAIDuplicates(threshold = 0.92): Promise<AIDuplicateGroup[]> {
  const res = await client.get<{ items: AIDuplicateGroup[] }>('/api/ai/duplicates', {
    params: { threshold },
  });
  return res.data.items ?? [];
}
