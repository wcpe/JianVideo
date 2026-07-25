import type { AIDuplicateGroup, AIModel, AINode, AIResult } from '@/types';
import client from './client';

/** AI 能力状态摘要（FR2-011） */
export interface AIStatus {
  enabled: boolean;
  models: AIModel[];
  nodes: AINode[];
}

export async function getAIStatus(): Promise<AIStatus> {
  const res = await client.get<{
    enabled: boolean;
    models?: AIModel[];
    nodes?: AINode[];
  }>('/api/ai/status');
  return {
    enabled: Boolean(res.data.enabled),
    models: res.data.models ?? [],
    nodes: res.data.nodes ?? [],
  };
}

export async function listAIModels(): Promise<AIModel[]> {
  const res = await client.get<{ items: AIModel[] }>('/api/ai/models');
  return res.data.items ?? [];
}

export async function listAINodes(): Promise<AINode[]> {
  const res = await client.get<{ items: AINode[] }>('/api/ai/nodes');
  return res.data.items ?? [];
}

export async function updateAIModelStatus(
  id: string,
  status: 'available' | 'disabled',
): Promise<void> {
  await client.put(`/api/ai/models/${encodeURIComponent(id)}/status`, { status });
}

export async function updateAINodeEnabled(id: string, enabled: boolean): Promise<void> {
  await client.put(`/api/ai/nodes/${encodeURIComponent(id)}/enabled`, { enabled });
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
