import { act, cleanup, render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { MantineProvider } from '@mantine/core';
import VideoPlayer from './VideoPlayer';
import type { PlaybackDescriptor } from '@/types';

// 记录各内核被构造时收到的 URL，断言分发走向
const mpegtsCreate = vi.fn();
const hlsLoadSource = vi.fn();
const hlsHandlers = new Map<string, (...args: unknown[]) => void>();

vi.mock('mpegts.js', () => ({
  default: {
    createPlayer: (config: { url: string }) => {
      mpegtsCreate(config.url);
      return {
        attachMediaElement: () => {},
        load: () => {},
        on: () => {},
        off: () => {},
        pause: () => {},
        unload: () => {},
        destroy: () => {},
      };
    },
  },
}));

// hls.js 桩：isSupported=true，loadSource 记录 URL；事件 on 立即忽略
vi.mock('hls.js', () => {
  class FakeHls {
    static isSupported() {
      return true;
    }
    static Events = { MANIFEST_PARSED: 'p', LEVEL_SWITCHED: 'l', ERROR: 'e' };
    autoLevelCapping = -1;
    currentLevel = -1;
    loadingEnabled = false;
    loadSource(url: string) {
      hlsLoadSource(url);
    }
    attachMedia() {}
    on(event: string, handler: (...args: unknown[]) => void) {
      hlsHandlers.set(event, handler);
    }
    startLoad() {
      this.loadingEnabled = true;
    }
    stopLoad() {
      this.loadingEnabled = false;
    }
    destroy() {}
    levels = [
      { bitrate: 2_500_000, width: 1280, height: 720 },
      { bitrate: 1_000_000, width: 854, height: 480 },
    ];
  }
  return { default: FakeHls };
});

// 桩 MediaSource.isTypeSupported；默认全支持
function stubIsTypeSupported(fn: (mime: string) => boolean) {
  vi.stubGlobal('MediaSource', { isTypeSupported: fn } as unknown as typeof MediaSource);
}

function renderWithDescriptor(descriptor: PlaybackDescriptor) {
  return render(
    <MantineProvider>
      <VideoPlayer url={descriptor.url} descriptor={descriptor} autoPlay={false} />
    </MantineProvider>,
  );
}

async function waitForHlsSource(url?: string) {
  await vi.dynamicImportSettled();
  await waitFor(() => {
    if (url) expect(hlsLoadSource).toHaveBeenCalledWith(url);
    else expect(hlsLoadSource).toHaveBeenCalled();
  });
}

beforeEach(() => {
  mpegtsCreate.mockClear();
  hlsLoadSource.mockClear();
  hlsHandlers.clear();
  stubIsTypeSupported(() => true);
});

afterEach(async () => {
  cleanup();
  await vi.dynamicImportSettled();
  vi.unstubAllGlobals();
  hlsHandlers.clear();
});

describe('自适应播放器按描述符分发（FR-52）', () => {
  it('LEVEL_SWITCHED 后展示当前 ABR 档位', async () => {
    render(
      <MantineProvider>
        <VideoPlayer url="/api/play/hls/7/profiles/abr-h264/master.m3u8" isABR autoPlay={false} />
      </MantineProvider>,
    );
    await waitForHlsSource();
    act(() => {
      hlsHandlers.get('p')?.();
      hlsHandlers.get('l')?.('l', { level: 1 });
    });
    expect(await screen.findByText('自动（当前 480p）')).toBeInTheDocument();
  });

  it('fmp4 描述符（编码受支持）走 hls.js 加载 index.m3u8', async () => {
    renderWithDescriptor({ codec: 'av1', url: '/api/play/hls/7/index.m3u8', path: 'fmp4' });
    await waitForHlsSource('/api/play/hls/7/index.m3u8');
    expect(mpegtsCreate).not.toHaveBeenCalled();
  });

  it('ts 描述符走 mpegts.js', async () => {
    renderWithDescriptor({ codec: 'h264', url: '/api/play/3/stream.ts', path: 'ts' });
    await waitFor(() => expect(mpegtsCreate).toHaveBeenCalledWith('/api/play/3/stream.ts'));
    expect(hlsLoadSource).not.toHaveBeenCalled();
  });

  it('fmp4 描述符但编码不受支持、有 fallbackUrl 时回退走 mpegts.js（不抛错）', async () => {
    stubIsTypeSupported(() => false);
    renderWithDescriptor({
      codec: 'av1',
      url: '/api/play/hls/7/index.m3u8',
      path: 'fmp4',
      fallbackUrl: '/api/play/7/fallback.ts',
    });
    await waitFor(() => expect(mpegtsCreate).toHaveBeenCalledWith('/api/play/7/fallback.ts'));
    expect(hlsLoadSource).not.toHaveBeenCalled();
  });

  it('fmp4 描述符不受支持且无 fallbackUrl 时展示不支持提示，不调用任一内核', async () => {
    stubIsTypeSupported(() => false);
    const { findByRole } = renderWithDescriptor({
      codec: 'av1',
      url: '/api/play/hls/7/index.m3u8',
      path: 'fmp4',
    });
    expect(await findByRole('alert')).toHaveTextContent('不支持');
    expect(hlsLoadSource).not.toHaveBeenCalled();
    expect(mpegtsCreate).not.toHaveBeenCalled();
  });
});
