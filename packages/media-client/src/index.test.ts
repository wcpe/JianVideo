import { describe, expect, it } from 'vitest';
import { createQueryKeys, normalizeLegacyTaskState } from './index';

describe('media-client package', () => {
  it('query key 包含 Space 维度', () => {
    expect(createQueryKeys().mediaList({ spaceId: 'default' }, 2)).toEqual(['media', 'list', 'default', 2]);
  });

  it('兼容旧任务状态并映射到 ADR-0055 状态', () => {
    expect(normalizeLegacyTaskState('completed')).toBe('succeeded');
    expect(normalizeLegacyTaskState('error')).toBe('failed');
  });

  it('保留 ADR-0055 原生任务状态', () => {
    expect(normalizeLegacyTaskState('pending')).toBe('pending');
    expect(normalizeLegacyTaskState('running')).toBe('running');
    expect(normalizeLegacyTaskState('succeeded')).toBe('succeeded');
    expect(normalizeLegacyTaskState('failed')).toBe('failed');
    expect(normalizeLegacyTaskState('canceled')).toBe('canceled');
  });

  it('未知任务状态抛出中文错误', () => {
    expect(() => normalizeLegacyTaskState('paused')).toThrow('未知任务状态');
  });
});
