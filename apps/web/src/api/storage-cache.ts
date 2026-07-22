import client from './client';

export type CacheKind = 'thumbnail' | 'hls' | 'image_proxy' | 'cover' | 'metadata_temp';

export type CacheKindSummary = {
  kind: CacheKind;
  size_bytes: number;
  file_count: number;
  asset_count: number;
  rebuildable?: boolean;
};

export type StorageCacheSummary = {
  total_size_bytes: number;
  total_file_count: number;
  total_assets: number;
  by_kind: Record<CacheKind, CacheKindSummary>;
};

export type StorageCacheInventoryResult = {
  task_id: number;
};

export type StorageCacheCleanRequest = {
  dry_run: boolean;
  kinds: CacheKind[];
};

export type StorageCacheCleanResult = {
  dry_run: boolean;
  task_id?: number;
  candidate_count: number;
  total_size_bytes: number;
  total_file_count: number;
  deleted_count: number;
  deleted_size_bytes: number;
  failed_count: number;
  error?: string;
};

export async function getStorageCacheSummary(): Promise<StorageCacheSummary> {
  const res = await client.get<StorageCacheSummary>('/api/storage/cache/summary');
  return res.data;
}

export async function inventoryStorageCache(): Promise<StorageCacheInventoryResult> {
  const res = await client.post<StorageCacheInventoryResult>('/api/storage/cache/inventory');
  return res.data;
}

export async function cleanStorageCache(
  input: StorageCacheCleanRequest,
): Promise<StorageCacheCleanResult> {
  const res = await client.post<StorageCacheCleanResult>('/api/storage/cache/clean', input);
  return res.data;
}
