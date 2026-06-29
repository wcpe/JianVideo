import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MantineProvider } from '@mantine/core';
import { IconClock, IconSun } from '@tabler/icons-react';

import CommandPalette, { type Command } from './CommandPalette';

// 构造一组命令（含跳转与直接执行各一条）；run 为 spy 便于断言执行
function makeCommands(): { commands: Command[]; runs: Record<string, ReturnType<typeof vi.fn>> } {
  const runs = {
    timeline: vi.fn(),
    theme: vi.fn(),
    logout: vi.fn(),
  };
  const commands: Command[] = [
    { id: 'timeline', label: '时间轴', icon: IconClock, run: runs.timeline },
    { id: 'theme', label: '切换主题', icon: IconSun, run: runs.theme },
    { id: 'logout', label: '退出登录', icon: IconSun, run: runs.logout },
  ];
  return { commands, runs };
}

function renderPalette(props: { opened?: boolean; onClose?: () => void; commands: Command[] }) {
  const { opened = true, onClose = vi.fn(), commands } = props;
  return render(
    <MantineProvider>
      <CommandPalette opened={opened} onClose={onClose} commands={commands} />
    </MantineProvider>,
  );
}

describe('CommandPalette 命令面板（FR-74）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('未打开时不渲染命令', () => {
    const { commands } = makeCommands();
    renderPalette({ opened: false, commands });
    expect(screen.queryByText('时间轴')).toBeNull();
  });

  it('打开时渲染全部命令并自动聚焦输入框', () => {
    const { commands } = makeCommands();
    renderPalette({ opened: true, commands });
    expect(screen.getByText('时间轴')).toBeVisible();
    expect(screen.getByText('切换主题')).toBeVisible();
    expect(screen.getByText('退出登录')).toBeVisible();
  });

  it('输入关键字按 label 模糊过滤命令', async () => {
    const user = userEvent.setup();
    const { commands } = makeCommands();
    renderPalette({ opened: true, commands });

    const input = screen.getByRole('textbox', { name: '命令' });
    await user.type(input, '主题');

    // 仅命中「切换主题」，其余命令被过滤掉
    expect(screen.getByText('切换主题')).toBeVisible();
    expect(screen.queryByText('时间轴')).toBeNull();
    expect(screen.queryByText('退出登录')).toBeNull();
  });

  it('↑↓ 移动高亮、Enter 执行当前高亮命令', async () => {
    const user = userEvent.setup();
    const { commands, runs } = makeCommands();
    const onClose = vi.fn();
    renderPalette({ opened: true, commands, onClose });

    const input = screen.getByRole('textbox', { name: '命令' });
    input.focus();
    // 默认高亮首项（时间轴）；↓ 一次到第二项（切换主题）
    await user.keyboard('{ArrowDown}');
    await user.keyboard('{Enter}');

    expect(runs.theme).toHaveBeenCalledTimes(1);
    expect(runs.timeline).not.toHaveBeenCalled();
    // 执行后关闭面板
    expect(onClose).toHaveBeenCalled();
  });

  it('Enter 默认执行首个命令', async () => {
    const user = userEvent.setup();
    const { commands, runs } = makeCommands();
    renderPalette({ opened: true, commands });

    const input = screen.getByRole('textbox', { name: '命令' });
    input.focus();
    await user.keyboard('{Enter}');

    expect(runs.timeline).toHaveBeenCalledTimes(1);
  });

  it('Esc 关闭面板', async () => {
    const user = userEvent.setup();
    const { commands } = makeCommands();
    const onClose = vi.fn();
    renderPalette({ opened: true, commands, onClose });

    const input = screen.getByRole('textbox', { name: '命令' });
    input.focus();
    await user.keyboard('{Escape}');

    expect(onClose).toHaveBeenCalled();
  });

  it('点击命令项执行并关闭', async () => {
    const user = userEvent.setup();
    const { commands, runs } = makeCommands();
    const onClose = vi.fn();
    renderPalette({ opened: true, commands, onClose });

    await user.click(screen.getByText('退出登录'));

    expect(runs.logout).toHaveBeenCalledTimes(1);
    expect(onClose).toHaveBeenCalled();
  });

  it('过滤后高亮回到首项，Enter 执行过滤结果首项', async () => {
    const user = userEvent.setup();
    const { commands, runs } = makeCommands();
    renderPalette({ opened: true, commands });

    const input = screen.getByRole('textbox', { name: '命令' });
    await user.type(input, '退出');
    await user.keyboard('{Enter}');

    expect(runs.logout).toHaveBeenCalledTimes(1);
    expect(runs.timeline).not.toHaveBeenCalled();
  });
});
