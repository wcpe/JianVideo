import { render, screen, act } from '@testing-library/react';
import { MemoryRouter, Routes, Route, Link } from 'react-router-dom';
import userEvent from '@testing-library/user-event';
import { describe, it, expect } from 'vitest';

import TopProgressBar from './TopProgressBar';

describe('TopProgressBar（FR-96 全局顶部加载条）', () => {
  it('首次渲染不显示进度条（避免初次进入闪一下）', () => {
    render(
      <MemoryRouter initialEntries={['/']}>
        <TopProgressBar />
      </MemoryRouter>,
    );
    expect(screen.queryByRole('progressbar')).toBeNull();
  });

  it('路由切换时出现进度条', async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter initialEntries={['/']}>
        <TopProgressBar />
        <Routes>
          <Route path="/" element={<Link to="/browse">去浏览</Link>} />
          <Route path="/browse" element={<div>浏览页</div>} />
        </Routes>
      </MemoryRouter>,
    );
    // 初始无进度条
    expect(screen.queryByRole('progressbar')).toBeNull();

    await act(async () => {
      await user.click(screen.getByText('去浏览'));
    });

    // 路由切换后进度条出现并带无障碍标签
    expect(screen.getByRole('progressbar', { name: '页面加载进度' })).toBeInTheDocument();
  });
});
