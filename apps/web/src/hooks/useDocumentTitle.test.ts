import { describe, it, expect, afterEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useDocumentTitle } from './useDocumentTitle';

// FR-129 动态文档标题：有页名时「<页面名> - JianVideo」，无页名回退「JianVideo」。

afterEach(() => {
  document.title = '';
});

describe('useDocumentTitle（FR-129）', () => {
  it('有页名时设为「<页面名> - JianVideo」', () => {
    renderHook(() => useDocumentTitle('时间轴'));
    expect(document.title).toBe('时间轴 - JianVideo');
  });

  it('页名为空时回退「JianVideo」', () => {
    renderHook(() => useDocumentTitle(''));
    expect(document.title).toBe('JianVideo');
  });

  it('页名为 undefined 时回退「JianVideo」', () => {
    renderHook(() => useDocumentTitle(undefined));
    expect(document.title).toBe('JianVideo');
  });

  it('仅空白的页名按空处理回退「JianVideo」', () => {
    renderHook(() => useDocumentTitle('   '));
    expect(document.title).toBe('JianVideo');
  });

  it('页名变化时更新标题', () => {
    const { rerender } = renderHook(({ name }) => useDocumentTitle(name), {
      initialProps: { name: '概览' },
    });
    expect(document.title).toBe('概览 - JianVideo');
    rerender({ name: '相册' });
    expect(document.title).toBe('相册 - JianVideo');
  });
});
