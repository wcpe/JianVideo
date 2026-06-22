import type { Share, ShareInfo, ShareResourceType } from '@/types'

// 使用构建时环境变量决定是否启用 mock 模式
const useMock = import.meta.env.VITE_USE_MOCK === 'true'

// ─── 公开访问 URL 构造（免登，供分享查看页直接用作 img/video/下载地址）──────

export function shareRawURL(token: string, mediaID: number) {
  return `/api/share/${token}/media/${mediaID}/raw`
}
export function shareThumbnailURL(token: string, mediaID: number) {
  return `/api/share/${token}/media/${mediaID}/thumbnail`
}
export function shareDownloadURL(token: string, mediaID: number) {
  return `/api/share/${token}/media/${mediaID}/download`
}
export function shareStreamURL(token: string, mediaID: number) {
  return `/api/share/${token}/media/${mediaID}/stream`
}

// ─── 真实 API 实现 ──────────────────────────────────

import client from './client'
import type { AxiosError } from 'axios'

function getApiErrorMessage(err: unknown, fallback: string): string {
  const error = err as AxiosError<{ message?: string }>
  return error.response?.data?.message || (err instanceof Error ? err.message : fallback)
}

async function realCreateShare(resourceType: ShareResourceType, resourceID: number, expiresInHours = 0): Promise<Share> {
  try {
    const res = await client.post<Share>('/api/shares', {
      resource_type: resourceType,
      resource_id: resourceID,
      expires_in_hours: expiresInHours,
    })
    return res.data
  } catch (err) {
    throw new Error(getApiErrorMessage(err, '创建分享失败'), { cause: err })
  }
}

async function realListShares(): Promise<Share[]> {
  const res = await client.get<{ shares: Share[] }>('/api/shares')
  return res.data.shares
}

async function realRevokeShare(token: string): Promise<void> {
  await client.delete(`/api/shares/${token}`)
}

async function realGetShareInfo(token: string): Promise<ShareInfo> {
  const res = await client.get<ShareInfo>(`/api/share/${token}`)
  return res.data
}

// ─── Mock API 实现 ──────────────────────────────────

import { mockMediaFiles } from '@/mocks/data'

const mockShares: Share[] = []
let mockTokenSeq = 1

function mockDelay(ms: number): Promise<void> {
  return new Promise(r => setTimeout(r, ms))
}

async function mockCreateShare(resourceType: ShareResourceType, resourceID: number, expiresInHours = 0): Promise<Share> {
  await mockDelay(120)
  const now = new Date()
  const share: Share = {
    token: `mock-token-${mockTokenSeq++}`,
    resource_type: resourceType,
    resource_id: resourceID,
    expires_at: expiresInHours > 0 ? new Date(now.getTime() + expiresInHours * 3600_000).toISOString() : null,
    created_at: now.toISOString(),
  }
  mockShares.unshift(share)
  return share
}

async function mockListShares(): Promise<Share[]> {
  await mockDelay(120)
  return [...mockShares]
}

async function mockRevokeShare(token: string): Promise<void> {
  await mockDelay(120)
  const idx = mockShares.findIndex(s => s.token === token)
  if (idx !== -1) mockShares.splice(idx, 1)
}

async function mockGetShareInfo(token: string): Promise<ShareInfo> {
  await mockDelay(120)
  const share = mockShares.find(s => s.token === token)
  if (!share) throw new Error('分享不存在或已过期')
  if (share.resource_type === 'media') {
    const media = mockMediaFiles.find(m => m.id === share.resource_id)
    return { resource_type: 'media', expires_at: share.expires_at, media }
  }
  // 相册分享 mock：成员列表留空（mock 不维护相册成员细节）
  return { resource_type: 'album', expires_at: share.expires_at, items: [] }
}

// ─── 导出（构建时决定 mock 模式）──────────────────────

export function createShare(resourceType: ShareResourceType, resourceID: number, expiresInHours = 0) {
  return useMock ? mockCreateShare(resourceType, resourceID, expiresInHours) : realCreateShare(resourceType, resourceID, expiresInHours)
}
export function listShares() { return useMock ? mockListShares() : realListShares() }
export function revokeShare(token: string) { return useMock ? mockRevokeShare(token) : realRevokeShare(token) }
export function getShareInfo(token: string) { return useMock ? mockGetShareInfo(token) : realGetShareInfo(token) }
