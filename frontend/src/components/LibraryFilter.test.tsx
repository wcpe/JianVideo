import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import LibraryFilter from './LibraryFilter';
import { server } from '@/mocks/beforeAll';

// FR-144 时间轴媒体库筛选：按图库（媒体库目录）筛选时间轴。
// 单选下拉「全部图库」+ 各库；选某库映射 library_id，「全部图库」映射 0（恢复全量）。
// 含「多选模式」入口（Windows 资源管理器式图库切换）：进入后以复选框列出各库，
// 恰选一库即映射该库 library_id，全选/未选/多选回退全量（后端仅支持单 library_id）。

vi.mock('@mantine/notifications', () => ({
  notifications: { show: vi.fn() },
}));

// 三个图库（电影 1 / 电视剧 2 / 动漫 3），与 mock 数据一致
function useLibsResponse() {
  server.use(
    http.get('*/api/library/paths', () =>
      HttpResponse.json({
        items: [
          { id: 1, path: 'D:\\Videos\\Movies', type: 'local', label: '电影', enabled: true, created_at: '2025-01-01T00:00:00Z' },
          { id: 2, path: 'D:\\Videos\\TV', type: 'local', label: '电视剧', enabled: true, created_at: '2025-01-02T00:00:00Z' },
          { id: 3, path: 'D:\\Videos\\Anime', type: 'local', label: '动漫', enabled: true, created_at: '2025-01-03T00:00:00Z' },
        ],
      }),
    ),
    http.get('*/api/library/extensions', () => HttpResponse.json({ items: [] })),
  );
}

function renderFilter(props: Partial<React.ComponentProps<typeof LibraryFilter>> = {}) {
  return render(
    <MantineProvider>
      <LibraryFilter libraryId={props.libraryId ?? 0} onLibraryIdChange={props.onLibraryIdChange ?? (() => {})} />
    </MantineProvider>,
  );
}

describe('LibraryFilter 媒体库筛选（FR-144）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useLibsResponse();
  });

  it('下拉含「全部图库」与各图库项', async () => {
    renderFilter();
    const select = await screen.findByRole('combobox', { name: '图库' });
    await waitFor(() => expect(screen.getByRole('option', { name: '全部图库' })).toBeInTheDocument());
    await waitFor(() => expect(screen.getByRole('option', { name: '电影' })).toBeInTheDocument());
    await waitFor(() => expect(screen.getByRole('option', { name: '动漫' })).toBeInTheDocument());
    expect(select).toBeInTheDocument();
  });

  it('选择某图库映射对应 library_id', async () => {
    const onLibraryIdChange = vi.fn();
    renderFilter({ onLibraryIdChange });
    const select = await screen.findByRole('combobox', { name: '图库' });
    await waitFor(() => expect(screen.getByRole('option', { name: '电视剧' })).toBeInTheDocument());
    await userEvent.selectOptions(select, '2');
    expect(onLibraryIdChange).toHaveBeenCalledWith(2);
  });

  it('选择「全部图库」映射 0（恢复全量）', async () => {
    const onLibraryIdChange = vi.fn();
    renderFilter({ libraryId: 2, onLibraryIdChange });
    const select = await screen.findByRole('combobox', { name: '图库' });
    await userEvent.selectOptions(select, '0');
    expect(onLibraryIdChange).toHaveBeenCalledWith(0);
  });

  it('当前 libraryId 反映为下拉选中值', async () => {
    renderFilter({ libraryId: 3 });
    const select = (await screen.findByRole('combobox', { name: '图库' })) as HTMLSelectElement;
    await waitFor(() => expect(screen.getByRole('option', { name: '动漫' })).toBeInTheDocument());
    expect(select.value).toBe('3');
  });

  it('提供「多选模式」入口按钮，点击进入复选框列表', async () => {
    renderFilter();
    await screen.findByRole('combobox', { name: '图库' });
    const entry = screen.getByRole('button', { name: '多选模式' });
    await userEvent.click(entry);
    // 进入多选后以复选框列出各库
    await waitFor(() =>
      expect(screen.getByRole('checkbox', { name: '电影' })).toBeInTheDocument(),
    );
    expect(screen.getByRole('checkbox', { name: '动漫' })).toBeInTheDocument();
  });

  it('多选模式下恰勾选一个库映射该库 library_id', async () => {
    const onLibraryIdChange = vi.fn();
    renderFilter({ onLibraryIdChange });
    await screen.findByRole('combobox', { name: '图库' });
    await userEvent.click(screen.getByRole('button', { name: '多选模式' }));
    const cb = await screen.findByRole('checkbox', { name: '电视剧' });
    await userEvent.click(cb);
    expect(onLibraryIdChange).toHaveBeenCalledWith(2);
  });

  it('多选模式下勾选多个库回退全量（library_id=0）', async () => {
    const onLibraryIdChange = vi.fn();
    renderFilter({ onLibraryIdChange });
    await screen.findByRole('combobox', { name: '图库' });
    await userEvent.click(screen.getByRole('button', { name: '多选模式' }));
    await userEvent.click(await screen.findByRole('checkbox', { name: '电影' }));
    await userEvent.click(await screen.findByRole('checkbox', { name: '动漫' }));
    // 勾选多个库后端无法单库过滤 → 回退全量
    expect(onLibraryIdChange).toHaveBeenLastCalledWith(0);
  });
});
