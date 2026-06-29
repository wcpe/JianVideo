import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import ConsolePage from './ConsolePage';

// 复制结果功能依赖 navigator.clipboard，jsdom 默认缺失，这里注入避免 SystemPage 报错
const writeText = vi.fn().mockResolvedValue(undefined);

// 把 MemoryRouter 的内存路由 search 暴露到 DOM，供断言 URL query（MemoryRouter 不更新 window.location）
function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location-search">{location.search}</div>;
}

// 在指定初始 URL 下渲染控制台页（默认进运行环境 tab，FR-113 一级 tab）
function renderPage(initialPath = '/system') {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={[initialPath]}>
        <ConsolePage />
        <LocationProbe />
      </MemoryRouter>
    </MantineProvider>,
  );
}

describe('ConsolePage（FR-113 一级 tab）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
      writable: true,
    });
    // jsdom 无 IntersectionObserver，注入空 stub 供锚点导航挂载
    vi.stubGlobal(
      'IntersectionObserver',
      class {
        observe() {}
        unobserve() {}
        disconnect() {}
        takeRecords() {
          return [];
        }
      },
    );
  });

  it('渲染一级 tab：运行环境/硬件加速/编解码/应用更新/设置（含设置，无外层「系统信息」父级）', () => {
    renderPage();
    for (const name of ['运行环境', '硬件加速', '编解码', '应用更新', '设置']) {
      expect(screen.getByRole('tab', { name })).toBeInTheDocument();
    }
    // 不再有外层「系统信息」父级 tab
    expect(screen.queryByRole('tab', { name: '系统信息' })).toBeNull();
  });

  it('缺省进入运行环境 tab，可见运行环境内容与左侧锚点', async () => {
    renderPage();
    expect(screen.getByRole('tab', { name: '运行环境' })).toHaveAttribute('aria-selected', 'true');
    // 运行环境内容渲染：内容区 order-2 标题为「运行环境」（FR-113 后续修复：标题随 section 而非恒为「系统诊断」）
    expect(await screen.findByRole('heading', { level: 2, name: '运行环境' })).toBeVisible();
    // 左侧锚点：运行环境 / FFmpeg
    const nav = screen.getByRole('navigation', { name: '区块导航' });
    expect(nav).toBeInTheDocument();
  });

  it('点击「设置」tab 切换并展示设置项，URL query 变为 tab=settings', async () => {
    const user = userEvent.setup();
    renderPage();
    await user.click(screen.getByRole('tab', { name: '设置' }));
    await waitFor(() => {
      expect(screen.getByRole('tab', { name: '设置' })).toHaveAttribute('aria-selected', 'true');
    });
    expect(await screen.findByLabelText('扫描周期（秒）')).toBeInTheDocument();
    expect(screen.getByTestId('location-search')).toHaveTextContent('tab=settings');
  });

  it('设置 tab 内提供左侧锚点导航（账户安全/扫描/网络等）', async () => {
    renderPage('/system?tab=settings');
    expect(await screen.findByLabelText('扫描周期（秒）')).toBeInTheDocument();
    const nav = screen.getByRole('navigation', { name: '区块导航' });
    expect(nav).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '账户安全' })).toBeInTheDocument();
  });

  // ── 向后兼容深链 ──────────────────────────────
  it('旧深链 ?tab=settings 定位到设置 tab', async () => {
    renderPage('/system?tab=settings');
    expect(screen.getByRole('tab', { name: '设置' })).toHaveAttribute('aria-selected', 'true');
    expect(await screen.findByLabelText('扫描周期（秒）')).toBeInTheDocument();
  });

  it('旧深链 ?tab=system&sys=update 定位到应用更新 tab', async () => {
    renderPage('/system?tab=system&sys=update');
    expect(screen.getByRole('tab', { name: '应用更新' })).toHaveAttribute('aria-selected', 'true');
  });

  it('旧深链 ?tab=system&sys=hwaccel 定位到硬件加速 tab', () => {
    renderPage('/system?tab=system&sys=hwaccel');
    expect(screen.getByRole('tab', { name: '硬件加速' })).toHaveAttribute('aria-selected', 'true');
  });

  it('旧深链 ?tab=system（无 sys）定位到运行环境 tab', () => {
    renderPage('/system?tab=system');
    expect(screen.getByRole('tab', { name: '运行环境' })).toHaveAttribute('aria-selected', 'true');
  });
});
