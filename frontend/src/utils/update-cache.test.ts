import { describe, it, expect, beforeEach } from 'vitest'
import { loadCachedUpdate, saveCachedUpdate } from './update-cache'
import type { UpdateCheckResult } from '@/types'

const sample: UpdateCheckResult = {
  current: '0.16.0',
  latest: 'v0.16.1',
  has_update: true,
  tag: 'v0.16.1',
  prerelease: false,
  channel: 'stable',
  notes: '## 更新日志\n- 修复若干问题',
  asset_name: 'jianvideo-windows-amd64',
}

describe('update-cache（更新结果本地缓存）', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('无缓存返回 null', () => {
    expect(loadCachedUpdate('stable')).toBeNull()
  })

  it('保存后可按频道读回（含发布说明）', () => {
    saveCachedUpdate('stable', sample)
    const got = loadCachedUpdate('stable')
    expect(got).not.toBeNull()
    expect(got?.latest).toBe('v0.16.1')
    expect(got?.notes).toContain('更新日志')
  })

  it('按频道隔离：写 stable 不影响 prerelease', () => {
    saveCachedUpdate('stable', sample)
    expect(loadCachedUpdate('prerelease')).toBeNull()
  })

  it('损坏的缓存安全返回 null（不抛异常）', () => {
    localStorage.setItem('jianvideo.update.check.stable', '{不是合法 json')
    expect(loadCachedUpdate('stable')).toBeNull()
  })
})
