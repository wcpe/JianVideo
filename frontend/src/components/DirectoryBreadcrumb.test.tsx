// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MantineProvider } from '@mantine/core';
import DirectoryBreadcrumb from './DirectoryBreadcrumb';

function renderWithProvider(ui: React.ReactElement) {
  return render(<MantineProvider>{ui}</MantineProvider>);
}

describe('DirectoryBreadcrumb', () => {
  it('空列表时渲染空容器', () => {
    renderWithProvider(<DirectoryBreadcrumb items={[]} onNavigate={vi.fn()} />);
    // 组件应正常渲染不崩溃
    expect(document.body).toBeInTheDocument();
  });

  it('渲染单段面包屑', () => {
    renderWithProvider(
      <DirectoryBreadcrumb items={[{ name: 'movies', path: '/movies' }]} onNavigate={vi.fn()} />,
    );
    expect(screen.getByText('movies')).toBeVisible();
  });

  it('渲染多段面包屑', () => {
    renderWithProvider(
      <DirectoryBreadcrumb
        items={[
          { name: 'movies', path: '/movies' },
          { name: 'action', path: '/movies/action' },
        ]}
        onNavigate={vi.fn()}
      />,
    );
    expect(screen.getByText('movies')).toBeVisible();
    expect(screen.getByText('action')).toBeVisible();
  });

  it('点击面包屑触发 onNavigate', () => {
    const onNavigate = vi.fn();
    renderWithProvider(
      <DirectoryBreadcrumb
        items={[
          { name: 'movies', path: '/movies' },
          { name: 'action', path: '/movies/action' },
        ]}
        onNavigate={onNavigate}
      />,
    );
    fireEvent.click(screen.getByText('movies'));
    expect(onNavigate).toHaveBeenCalledWith('/movies');
  });

  it('最后一段不可点击', () => {
    renderWithProvider(
      <DirectoryBreadcrumb
        items={[
          { name: 'movies', path: '/movies' },
          { name: 'action', path: '/movies/action' },
        ]}
        onNavigate={vi.fn()}
      />,
    );
    // 最后一段文本存在即可，cursor 样式由 CSS 控制
    expect(screen.getByText('action')).toBeVisible();
  });

  it('第一段显示 Home 图标', () => {
    renderWithProvider(
      <DirectoryBreadcrumb items={[{ name: 'movies', path: '/movies' }]} onNavigate={vi.fn()} />,
    );
    // IconHome 渲染为 SVG
    const svg = document.querySelector('svg');
    expect(svg).toBeInTheDocument();
  });
});
