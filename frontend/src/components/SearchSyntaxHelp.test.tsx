import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect } from 'vitest';
import { MantineProvider } from '@mantine/core';
import SearchSyntaxHelp from './SearchSyntaxHelp';

function renderHelp() {
  return render(
    <MantineProvider>
      <SearchSyntaxHelp />
    </MantineProvider>,
  );
}

describe('SearchSyntaxHelp（FR-136 搜索语法帮助）', () => {
  it('点击帮助按钮后展示扩展可搜字段与表达式 token', async () => {
    renderHelp();
    const btn = screen.getByRole('button', { name: '搜索语法帮助' });
    await userEvent.click(btn);

    // 可搜字段说明应覆盖显示名与 EXIF（相机/镜头）
    expect(await screen.findByText(/裸词同时匹配/)).toBeInTheDocument();
    expect(screen.getAllByText(/相机/).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/镜头/).length).toBeGreaterThan(0);

    // 表达式 token 应含 camera: 与 lens:
    expect(screen.getByText(/camera:/)).toBeInTheDocument();
    expect(screen.getByText(/lens:/)).toBeInTheDocument();
  });
});
