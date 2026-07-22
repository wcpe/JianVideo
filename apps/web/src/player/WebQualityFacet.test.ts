import type { PlaybackCommandContext, QualityFacetState } from '@jianvideo/player-core';
import { describe, expect, it, vi } from 'vitest';
import type Hls from 'hls.js';
import { WebQualityFacet } from './WebQualityFacet';

function command(requestId = 1): PlaybackCommandContext {
  return { requestId, sourceEpoch: 1, sourceId: 'source-a' };
}

function createVideo(): HTMLVideoElement {
  const video = document.createElement('video');
  video.playbackRate = 1;
  return video;
}

function createHls() {
  return {
    autoLevelCapping: -1,
    currentLevel: -1,
    levels: [
      { bitrate: 5_000_000, height: 1080, width: 1920 },
      { bitrate: 2_500_000, height: 720, width: 1280 },
      { bitrate: 800_000, height: 480, width: 854 },
      { bitrate: 1_200_000, height: 480, width: 854 },
      { bitrate: 500_000, height: 360, width: 640 },
    ],
    loadingEnabled: false,
    startLoad: vi.fn(function (this: { loadingEnabled: boolean }) {
      this.loadingEnabled = true;
    }),
    stopLoad: vi.fn(function (this: { loadingEnabled: boolean }) {
      this.loadingEnabled = false;
    }),
  } as unknown as Hls;
}

describe('WebQualityFacet HLS 层级适配', () => {
  it('枚举真实 level，同高变体显示码率且自动模式使用 currentLevel=-1', async () => {
    const facet = new WebQualityFacet(createVideo());
    const hls = createHls();
    facet.load(command());
    facet.attach(hls, command());
    facet.refreshLevels(command());

    expect(facet.getState().qualities.map((quality) => quality.label)).toEqual([
      '1080p',
      '720p',
      '480p · 800 kbps',
      '480p · 1200 kbps',
      '360p',
    ]);
    await facet.selectQuality({ mode: 'auto' }, command(2));
    expect(hls.currentLevel).toBe(-1);
  });

  it('手动语义选择同高度最高带宽变体，清单重排后重新匹配新索引', async () => {
    const facet = new WebQualityFacet(createVideo());
    const hls = createHls();
    facet.load(command());
    facet.attach(hls, command());
    facet.refreshLevels(command());

    await facet.selectQuality({ mode: 'manual', quality: { height: 480 } }, command(2));
    expect(hls.currentLevel).toBe(3);

    (hls as unknown as { levels: typeof hls.levels }).levels = [
      hls.levels[3]!,
      hls.levels[0]!,
      hls.levels[2]!,
      hls.levels[1]!,
      hls.levels[4]!,
    ];
    facet.refreshLevels(command(3));
    expect(hls.currentLevel).toBe(0);
  });

  it('省流量 cap 选择不高于 480p 的最高可用变体且关闭时清除', async () => {
    const facet = new WebQualityFacet(createVideo());
    const hls = createHls();
    facet.load(command());
    facet.attach(hls, command());
    facet.refreshLevels(command());

    await facet.setAutoQualityCap(480, command(2));
    expect(hls.autoLevelCapping).toBe(3);
    await facet.setAutoQualityCap(null, command(3));
    expect(hls.autoLevelCapping).toBe(-1);
  });

  it('无合规 cap 时拒绝且不例外选择最低高档', async () => {
    const facet = new WebQualityFacet(createVideo());
    const hls = createHls();
    (hls as unknown as { levels: typeof hls.levels }).levels = hls.levels.slice(0, 2);
    facet.load(command());
    facet.attach(hls, command());
    facet.refreshLevels(command());

    await expect(facet.setAutoQualityCap(480, command(2))).rejects.toThrow('无 480p 或更低档位');
    expect(hls.currentLevel).toBe(-1);
    expect(hls.autoLevelCapping).toBe(-1);
  });

  it('level 切换只更新实际档位，不覆盖手动选择', async () => {
    const facet = new WebQualityFacet(createVideo());
    const hls = createHls();
    facet.load(command());
    facet.attach(hls, command());
    facet.refreshLevels(command());
    await facet.selectQuality(
      { mode: 'manual', quality: { bandwidth: 800_000, height: 480 } },
      command(2),
    );

    facet.handleLevelSwitched(4);

    expect(facet.getState()).toMatchObject({
      actualQualityId: expect.stringContaining('360'),
      selection: { mode: 'manual', quality: { bandwidth: 800_000, height: 480 } },
    });
  });

  it('手动 level 失败时降到下一低档，省流量下只在合规集合内降级', async () => {
    const facet = new WebQualityFacet(createVideo());
    const hls = createHls();
    facet.load(command());
    facet.attach(hls, command());
    facet.refreshLevels(command());
    await facet.selectQuality({ mode: 'manual', quality: { height: 720 } }, command(2));

    expect(facet.handleLevelError(1)).toBe('fallback');
    expect(hls.currentLevel).toBe(3);

    await facet.setAutoQualityCap(480, command(3));
    expect(facet.handleLevelError(3)).toBe('fallback');
    expect(hls.currentLevel).toBe(2);
    expect(facet.handleLevelError(2)).toBe('fallback');
    expect(hls.currentLevel).toBe(4);
    expect(facet.handleLevelError(4)).toBe('blocked');
    expect(hls.stopLoad).toHaveBeenCalledOnce();
  });

  it('倍速、加载启停与状态订阅均映射公共 API', async () => {
    const video = createVideo();
    const facet = new WebQualityFacet(video);
    const hls = createHls();
    const states: QualityFacetState[] = [];
    facet.subscribe((state) => states.push(state));
    facet.load(command());
    facet.attach(hls, command());
    facet.refreshLevels(command());

    await facet.setPlaybackRate(1.75, command(2));
    await facet.startLoading(command(3));
    await facet.stopLoading(command(4));

    expect(video.playbackRate).toBe(1.75);
    expect(hls.startLoad).toHaveBeenCalledOnce();
    expect(hls.stopLoad).toHaveBeenCalledOnce();
    expect(facet.getLoadingState()).toBe('stopped');
    expect(states.at(-1)?.playbackRate).toBe(1.75);
  });
});
