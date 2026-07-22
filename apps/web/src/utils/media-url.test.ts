import { describe, it, expect } from 'vitest';

import { mediaRawUrl, mediaStreamUrl, mediaHlsMasterUrl } from './media-url';

// 媒体查看器共享地址构造（FR-107）：锁定灯箱与播放页收敛后共用的地址行为不变。
describe('媒体查看器共享地址（FR-107）', () => {
  it('原图地址为相对 raw 路径', () => {
    expect(mediaRawUrl(7)).toBe('/api/library/media/7/raw');
  });

  it('视频流地址绝对化（含 origin），与播放页降级路径一致', () => {
    const url = mediaStreamUrl(9);
    expect(url).toBe(new URL('/api/play/9/stream', window.location.href).toString());
    expect(url).toContain('/api/play/9/stream');
    expect(url.startsWith('http')).toBe(true);
  });

  it('HLS master 地址绝对化', () => {
    const url = mediaHlsMasterUrl(3);
    expect(url).toBe(new URL('/api/play/hls/3/master', window.location.href).toString());
    expect(url).toContain('/api/play/hls/3/master');
    expect(url.startsWith('http')).toBe(true);
  });
});
