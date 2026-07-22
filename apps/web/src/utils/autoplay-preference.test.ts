import { describe, it, expect, beforeEach } from 'vitest';
import { readAutoplayNext, writeAutoplayNext } from './autoplay-preference';

describe('autoplay-preference (FR2-047)', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('缺省开启自动连播', () => {
    expect(readAutoplayNext()).toBe(true);
  });

  it('写入后可读取', () => {
    writeAutoplayNext(false);
    expect(readAutoplayNext()).toBe(false);
    writeAutoplayNext(true);
    expect(readAutoplayNext()).toBe(true);
  });

  it('兼容 "0"/"false" 关闭', () => {
    localStorage.setItem('jianvideo-autoplay-next', '0');
    expect(readAutoplayNext()).toBe(false);
    localStorage.setItem('jianvideo-autoplay-next', 'false');
    expect(readAutoplayNext()).toBe(false);
  });
});
