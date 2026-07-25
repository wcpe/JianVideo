import type { AIDuplicateGroup, AIModel, AINode, AIResult } from '@/types';
import client from './client';

/** AI 能力状态摘要（FR2-011） */
export interface AIStatus {
  enabled: boolean;
  models: AIModel[];
  nodes: AINode[];
}

/** 语义搜索命中（FR2-012） */
export interface SearchHit {
  media_id: number;
  score: number;
  model_id: string;
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

export async function listAIResultsBySpace(params?: {
  task_type?: string;
  manual?: boolean;
}): Promise<AIResult[]> {
  const res = await client.get<{ items: AIResult[] }>('/api/ai/results', { params });
  return res.data.items ?? [];
}

export async function confirmAIResult(id: number): Promise<void> {
  await client.post(`/api/ai/results/${id}/confirm`);
}

export async function rejectAIResult(id: number): Promise<void> {
  await client.post(`/api/ai/results/${id}/reject`);
}

export async function batchConfirmAIResults(ids: number[]): Promise<{ confirmed: number }> {
  const res = await client.post<{ confirmed: number }>('/api/ai/results/batch/confirm', { ids });
  return res.data;
}

export async function batchRejectAIResults(ids: number[]): Promise<{ rejected: number }> {
  const res = await client.post<{ rejected: number }>('/api/ai/results/batch/reject', { ids });
  return res.data;
}

export async function listAIDuplicates(threshold = 0.92): Promise<AIDuplicateGroup[]> {
  const res = await client.get<{ items: AIDuplicateGroup[] }>('/api/ai/duplicates', {
    params: { threshold },
  });
  return res.data.items ?? [];
}

export async function semanticSearch(q: string, topK = 10): Promise<SearchHit[]> {
  const res = await client.post<{ items: SearchHit[] }>('/api/ai/search', { q, top_k: topK });
  return res.data.items ?? [];
}
