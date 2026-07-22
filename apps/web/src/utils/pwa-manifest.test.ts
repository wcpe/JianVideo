import { describe, it, expect } from 'vitest';
import { pwaManifest } from './pwa-manifest';

describe('pwaManifest', () => {
  it('以独立窗口启动（standalone）', () => {
    expect(pwaManifest.display).toBe('standalone');
  });

  it('包含中文应用名称与短名称', () => {
    expect(pwaManifest.name).toBe('JianVideo 视频媒体库');
    expect(pwaManifest.short_name).toBe('JianVideo');
  });

  it('配置主题色与背景色', () => {
    expect(pwaManifest.theme_color).toMatch(/^#[0-9a-fA-F]{6}$/);
    expect(pwaManifest.background_color).toMatch(/^#[0-9a-fA-F]{6}$/);
  });

  it('start_url 指向根路径', () => {
    expect(pwaManifest.start_url).toBe('/');
  });

  it('提供 192/512 图标且含 maskable', () => {
    const sizes = pwaManifest.icons.map((i) => i.sizes);
    expect(sizes).toContain('192x192');
    expect(sizes).toContain('512x512');
    const purposes = pwaManifest.icons.map((i) => i.purpose).filter(Boolean);
    expect(purposes.some((p) => p?.includes('maskable'))).toBe(true);
  });
});
