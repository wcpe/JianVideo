import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { describe, it, expect, vi, afterEach } from 'vitest';
import { MantineProvider } from '@mantine/core';
import MediaThumbnail from './MediaThumbnail';

function renderThumb(props?: Partial<React.ComponentProps<typeof MediaThumbnail>>) {
  return render(
    <MantineProvider>
      <MediaThumbnail
        mediaID={props?.mediaID ?? 1}
        fileName={props?.fileName ?? 'pic.png'}
        aspectRatio={props?.aspectRatio}
        objectFit={props?.objectFit}
        overlay={props?.overlay}
        requestSize={props?.requestSize}
      />
    </MantineProvider>,
  );
}

describe('MediaThumbnail', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it('图片以 contain 自适应显示并提供 srcset/sizes', () => {
    renderThumb();
    const img = screen.getByAltText('pic.png') as HTMLImageElement;
    expect(img.style.objectFit).toBe('contain');
    // srcset 含 160/320/640 三档候选
    expect(img.getAttribute('srcset')).toContain('size=160');
    expect(img.getAttribute('srcset')).toContain('size=320');
    expect(img.getAttribute('srcset')).toContain('size=640');
    expect(img.getAttribute('srcset')).toContain('640w');
    // sizes 提供按列宽选择依据
    expect(img.getAttribute('sizes')).toBeTruthy();
  });

  it('传入 requestSize 时基准 src 用该档、sizes 收敛为该列宽（FR-141）', () => {
    renderThumb({ requestSize: 640 });
    const img = screen.getByAltText('pic.png') as HTMLImageElement;
    // 基准 src 用给定档（而非默认 320）
    expect(img.getAttribute('src')).toContain('size=640');
    // sizes 收敛为该列宽像素，让浏览器据真实列宽从 srcset 选档
    expect(img.getAttribute('sizes')).toBe('640px');
    // srcset 仍保留三档候选
    expect(img.getAttribute('srcset')).toContain('size=160');
    expect(img.getAttribute('srcset')).toContain('size=640');
  });

  it('初始以骨架覆盖占位，加载完成后骨架移除', () => {
    const { container } = renderThumb();
    // 图片始终在 DOM（可被角色查询命中），加载中骨架覆盖其上
    const img = screen.getByAltText('pic.png') as HTMLImageElement;
    expect(container.querySelector('.mantine-Skeleton-root')).toBeTruthy();
    // 触发图片 onLoad 后骨架移除
    fireEvent.load(img);
    expect(container.querySelector('.mantine-Skeleton-root')).toBeNull();
  });

  it('命中 202「生成中」时轮询重试并最终显示', async () => {
    vi.useFakeTimers();
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce({ status: 202 }) // 第一次探测：生成中
      .mockResolvedValueOnce({ status: 200 }); // 第二次探测：已就绪（若再触发 error）
    vi.stubGlobal('fetch', fetchMock);

    const { container } = renderThumb();
    const img = screen.getByAltText('pic.png') as HTMLImageElement;

    // 首次加载失败 → 探测得 202 → 安排重试（仍为加载中，骨架在）
    await act(async () => {
      fireEvent.error(img);
      await Promise.resolve();
    });
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/library/thumbnail/1'),
      expect.anything(),
    );
    expect(container.querySelector('.mantine-Skeleton-root')).toBeTruthy();

    // 推进重试间隔，触发 <img> 重新请求（reloadKey 自增）
    await act(async () => {
      vi.advanceTimersByTime(1600);
    });

    // 重新请求后加载成功，骨架移除
    const img2 = screen.getByAltText('pic.png') as HTMLImageElement;
    fireEvent.load(img2);
    expect(container.querySelector('.mantine-Skeleton-root')).toBeNull();
  });

  it('探测当前请求档位并显式携带 probe 参数', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ status: 202 });
    vi.stubGlobal('fetch', fetchMock);

    renderThumb({ requestSize: 640 });
    const img = screen.getByAltText('pic.png') as HTMLImageElement;
    Object.defineProperty(img, 'currentSrc', {
      configurable: true,
      value: 'http://localhost/api/library/thumbnail/1?size=640',
    });
    fireEvent.error(img);

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/library/thumbnail/1?size=640&probe=1',
        expect.anything(),
      );
    });
  });

  it('传入 objectFit=cover 时图片以 cover 填充（FR-99）', () => {
    renderThumb({ objectFit: 'cover' });
    const img = screen.getByAltText('pic.png') as HTMLImageElement;
    expect(img.style.objectFit).toBe('cover');
  });

  it('渲染传入的 overlay 叠层内容（FR-99）', () => {
    renderThumb({ overlay: <div data-testid="ov">叠层</div> });
    expect(screen.getByTestId('ov')).toBeTruthy();
  });

  it('当前媒体封面变化时主动重新加载列表缩略图（FR2-059）', () => {
    const { container } = renderThumb({ mediaID: 9 });
    const before = screen.getByAltText('pic.png');
    fireEvent.load(before);

    act(() => {
      window.dispatchEvent(new CustomEvent('jianvideo:cover-changed', { detail: { mediaID: 9 } }));
    });

    expect(screen.getByAltText('pic.png')).not.toBe(before);
    expect(container.querySelector('.mantine-Skeleton-root')).toBeTruthy();
  });

  it('加载失败且非 202 时显示降级占位', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ status: 404 });
    vi.stubGlobal('fetch', fetchMock);

    renderThumb();
    const img = screen.getByAltText('pic.png') as HTMLImageElement;
    fireEvent.error(img);

    await waitFor(() => {
      expect(screen.getByLabelText('pic.png 缩略图不可用')).toBeTruthy();
    });
    // 降级态不再渲染 img
    expect(screen.queryByAltText('pic.png')).toBeNull();
  });
});
