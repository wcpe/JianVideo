import type { Album, MediaFile } from '@/types';

// 使用构建时环境变量决定是否启用 mock 模式
const useMock = import.meta.env.VITE_USE_MOCK === 'true';

// ─── 真实 API 实现 ──────────────────────────────────

import client from './client';
import type { AxiosError } from 'axios';

function getApiErrorMessage(err: unknown, fallback: string): string {
  const error = err as AxiosError<{ message?: string }>;
  return error.response?.data?.message || (err instanceof Error ? err.message : fallback);
}

async function realListAlbums(): Promise<Album[]> {
  const res = await client.get<{ items: Album[] }>('/api/albums');
  return res.data.items;
}

async function realCreateAlbum(name: string, description = ''): Promise<Album> {
  try {
    const res = await client.post<Album>('/api/albums', { name, description });
    return res.data;
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '创建相册失败，请检查名称'), { cause: err });
  }
}

async function realDeleteAlbum(id: number): Promise<void> {
  await client.delete(`/api/albums/${id}`);
}

async function realListAlbumItems(albumID: number): Promise<MediaFile[]> {
  const res = await client.get<{ items: MediaFile[] }>(`/api/albums/${albumID}/items`);
  return res.data.items;
}

async function realAddAlbumItem(albumID: number, mediaID: number): Promise<void> {
  try {
    await client.post(`/api/albums/${albumID}/items`, { media_id: mediaID });
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '加入相册失败'), { cause: err });
  }
}

async function realRemoveAlbumItem(albumID: number, mediaID: number): Promise<void> {
  await client.delete(`/api/albums/${albumID}/items/${mediaID}`);
}

// ─── Mock API 实现 ──────────────────────────────────

import { mockMediaFiles } from '@/mocks/data';

interface MockAlbumItem {
  album_id: number;
  media_id: number;
}

const mockAlbums: Album[] = [];
const mockAlbumItems: MockAlbumItem[] = [];
let nextMockAlbumId = 1;

function mockDelay(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms));
}

async function mockListAlbums(): Promise<Album[]> {
  await mockDelay(150);
  return mockAlbums.map((a) => ({
    ...a,
    item_count: mockAlbumItems.filter((it) => it.album_id === a.id).length,
  }));
}

async function mockCreateAlbum(name: string, description = ''): Promise<Album> {
  await mockDelay(150);
  const trimmed = name.trim();
  if (!trimmed) throw new Error('相册名称不能为空');
  const now = new Date().toISOString();
  const album: Album = {
    id: nextMockAlbumId++,
    name: trimmed,
    description: description.trim(),
    cover_media_id: 0,
    created_at: now,
    updated_at: now,
  };
  mockAlbums.unshift(album);
  return album;
}

async function mockDeleteAlbum(id: number): Promise<void> {
  await mockDelay(150);
  const idx = mockAlbums.findIndex((a) => a.id === id);
  if (idx !== -1) mockAlbums.splice(idx, 1);
  for (let i = mockAlbumItems.length - 1; i >= 0; i--) {
    if (mockAlbumItems[i].album_id === id) mockAlbumItems.splice(i, 1);
  }
}

async function mockListAlbumItems(albumID: number): Promise<MediaFile[]> {
  await mockDelay(150);
  const ids = mockAlbumItems.filter((it) => it.album_id === albumID).map((it) => it.media_id);
  return ids
    .map((id) => mockMediaFiles.find((m) => m.id === id))
    .filter((m): m is MediaFile => m !== undefined);
}

async function mockAddAlbumItem(albumID: number, mediaID: number): Promise<void> {
  await mockDelay(120);
  if (!mockMediaFiles.some((m) => m.id === mediaID)) throw new Error('媒体不存在');
  if (mockAlbumItems.some((it) => it.album_id === albumID && it.media_id === mediaID)) return;
  mockAlbumItems.push({ album_id: albumID, media_id: mediaID });
}

async function mockRemoveAlbumItem(albumID: number, mediaID: number): Promise<void> {
  await mockDelay(120);
  const idx = mockAlbumItems.findIndex((it) => it.album_id === albumID && it.media_id === mediaID);
  if (idx !== -1) mockAlbumItems.splice(idx, 1);
}

// ─── 导出（构建时决定 mock 模式）──────────────────────

export function listAlbums() {
  return useMock ? mockListAlbums() : realListAlbums();
}
export function createAlbum(name: string, description = '') {
  return useMock ? mockCreateAlbum(name, description) : realCreateAlbum(name, description);
}
export function deleteAlbum(id: number) {
  return useMock ? mockDeleteAlbum(id) : realDeleteAlbum(id);
}
export function listAlbumItems(albumID: number) {
  return useMock ? mockListAlbumItems(albumID) : realListAlbumItems(albumID);
}
export function addAlbumItem(albumID: number, mediaID: number) {
  return useMock ? mockAddAlbumItem(albumID, mediaID) : realAddAlbumItem(albumID, mediaID);
}
export function removeAlbumItem(albumID: number, mediaID: number) {
  return useMock ? mockRemoveAlbumItem(albumID, mediaID) : realRemoveAlbumItem(albumID, mediaID);
}
