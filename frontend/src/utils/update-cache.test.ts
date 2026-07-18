import { describe, it, expect, beforeEach } from 'vitest';
import { loadCachedUpdate, saveCachedUpdate, clearCachedUpdate } from './update-cache';
import type { UpdateCheckResult } from '@/types';

const sample: UpdateCheckResult = {
  current: '0.16.0',
  latest: 'v0.16.1',
  has_update: true,
  tag: 'v0.16.1',
  prerelease: false,
  channel: 'stable',
  notes: '## 更新日志\n- 修复若干问题',
  asset_name: 'jianvideo-windows-amd64',
};

const rcSample: UpdateCheckResult = {
  ...sample,
  latest: 'v0.17.0-rc.1',
  tag: 'v0.17.0-rc.1',
  prerelease: true,
  channel: 'prerelease',
};

describe('update-cache（更新结果本地缓存）', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('无缓存返回 null', () => {
    expect(loadCachedUpdate('stable')).toBeNull();
  });

  it('保存后可按频道读回（含发布说明）', () => {
    saveCachedUpdate('stable', sample);
    const got = loadCachedUpdate('stable');
    expect(got).not.toBeNull();
    expect(got?.latest).toBe('v0.16.1');
    expect(got?.notes).toContain('更新日志');
  });

  it('按频道隔离：写 stable 不影响 prerelease', () => {
    saveCachedUpdate('stable', sample);
    expect(loadCachedUpdate('prerelease')).toBeNull();
  });

  it('损坏的缓存安全返回 null（不抛异常）', () => {
    localStorage.setItem('jianvideo.update.check.stable', '{不是合法 json');
    expect(loadCachedUpdate('stable')).toBeNull();
  });

  it('候选版缓存仅接受严格合法且 prerelease=true 的 RC', () => {
    saveCachedUpdate('prerelease', rcSample);
    expect(loadCachedUpdate('prerelease')).toEqual(rcSample);
  });

  it.each([
    ['旧 dev 标签', { ...rcSample, tag: 'dev', latest: '0.17.0-dev.3.gabc1234' }],
    ['rc.0 标签', { ...rcSample, tag: 'v0.17.0-rc.0', latest: 'v0.17.0-rc.0' }],
    ['latest 非法', { ...rcSample, latest: '0.17.0-rc.1' }],
    ['非预发布结果', { ...rcSample, prerelease: false }],
  ])('候选版缓存遇到%s时清除并返回 null', (_name, invalid) => {
    saveCachedUpdate('prerelease', invalid);
    expect(loadCachedUpdate('prerelease')).toBeNull();
    expect(localStorage.getItem('jianvideo.update.check.prerelease')).toBeNull();
  });

  it('正式版缓存保持历史兼容，不套用 RC 严格校验', () => {
    const historical = { ...sample, tag: 'dev', latest: '0.16.1-dev.1.gabc1234', prerelease: true };
    saveCachedUpdate('stable', historical);
    expect(loadCachedUpdate('stable')).toEqual(historical);
  });

  it('clearCachedUpdate 清除该频道缓存、不影响其他频道（FR-112）', () => {
    saveCachedUpdate('stable', sample);
    saveCachedUpdate('prerelease', rcSample);
    clearCachedUpdate('stable');
    expect(loadCachedUpdate('stable')).toBeNull();
    // 仅清目标频道，prerelease 仍在
    expect(loadCachedUpdate('prerelease')).not.toBeNull();
  });

  it('clearCachedUpdate 对无缓存频道安全无操作（不抛异常）', () => {
    expect(() => clearCachedUpdate('stable')).not.toThrow();
  });
});
