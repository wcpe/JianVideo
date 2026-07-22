import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { MantineProvider } from '@mantine/core';
import { describe, it, expect, beforeEach, vi } from 'vitest';

import UpdateIndicator from './UpdateIndicator';
import type { UpdateCheckResult } from '@/types';

// mock 更新检查 api，避免真实直连 GitHub
const mockCheckUpdate = vi.fn<(channel: string, force?: boolean) => Promise<UpdateCheckResult>>();
vi.mock('@/api/system', () => ({
  checkUpdate: (channel: string, force?: boolean) => mockCheckUpdate(channel, force),
}));

// mock 设置读取，频道持久化来源；默认返回 stable
const mockGetSettings = vi.fn<() => Promise<Record<string, string>>>();
vi.mock('@/api/settings', () => ({
  getSettings: () => mockGetSettings(),
  // 与真实模块导出的常量保持一致，供组件按键读取频道
  SETTING_KEY_UPDATE_CHANNEL: 'update_channel',
}));

// mock useNavigate，断言跳转目标而不触发真实路由
const mockNavigate = vi.fn();
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>();
  return { ...actual, useNavigate: () => mockNavigate };
});

// 构造一条更新检查结果，has_update 与目标版本可定制
function makeResult(overrides: Partial<UpdateCheckResult>): UpdateCheckResult {
  return {
    current: '0.8.0',
    latest: 'v0.9.0',
    has_update: false,
    tag: 'v0.9.0',
    prerelease: false,
    channel: 'stable',
    notes: '',
    asset_name: 'jianvideo-linux-amd64',
    ...overrides,
  };
}

function renderIndicator() {
  return render(
    <MantineProvider>
      <MemoryRouter>
        <UpdateIndicator />
      </MemoryRouter>
    </MantineProvider>,
  );
}

describe('UpdateIndicator 页眉「更新可用」提示（FR-58）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetSettings.mockResolvedValue({ update_channel: 'stable' });
  });

  it('检查返回「有更新」：渲染提示（含目标版本），点击跳转系统信息 tab 更新区', async () => {
    mockCheckUpdate.mockResolvedValue(
      makeResult({ has_update: true, latest: 'v0.9.0', tag: 'v0.9.0' }),
    );

    renderIndicator();

    // 提示按钮出现，文案含目标版本号
    const btn = await screen.findByRole('button', { name: /更新可用/ });
    expect(btn).toBeInTheDocument();
    expect(screen.getByText(/v0\.9\.0/)).toBeInTheDocument();

    // 非 force 调用（命中后端 TTL 缓存即廉价返回）
    expect(mockCheckUpdate).toHaveBeenCalledWith('stable', false);

    // 点击精确跳转到控制台「应用更新」一级 tab（FR-113 拍平为一级 tab）
    const user = userEvent.setup();
    await user.click(btn);
    expect(mockNavigate).toHaveBeenCalledWith('/system?tab=update');
  });

  it('检查返回「无更新」：不渲染提示，不影响其余布局', async () => {
    mockCheckUpdate.mockResolvedValue(makeResult({ has_update: false }));

    renderIndicator();

    // 等检查完成
    await waitFor(() => expect(mockCheckUpdate).toHaveBeenCalled());
    expect(screen.queryByRole('button', { name: /更新可用/ })).not.toBeInTheDocument();
  });

  it('检查失败（reject）：静默不渲染提示、不抛错', async () => {
    mockCheckUpdate.mockRejectedValue(new Error('网络不可达'));

    renderIndicator();

    await waitFor(() => expect(mockCheckUpdate).toHaveBeenCalled());
    // 失败静默：无提示按钮，且未因未捕获错误导致渲染崩溃
    expect(screen.queryByRole('button', { name: /更新可用/ })).not.toBeInTheDocument();
  });

  it('读设置失败时回退 stable 频道仍能检查', async () => {
    mockGetSettings.mockRejectedValue(new Error('读设置失败'));
    mockCheckUpdate.mockResolvedValue(makeResult({ has_update: true }));

    renderIndicator();

    await screen.findByRole('button', { name: /更新可用/ });
    expect(mockCheckUpdate).toHaveBeenCalledWith('stable', false);
  });
});
