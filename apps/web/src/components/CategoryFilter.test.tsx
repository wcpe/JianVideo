import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { MantineProvider } from '@mantine/core';
import { http, HttpResponse } from 'msw';
import CategoryFilter from './CategoryFilter';
import { server } from '@/mocks/beforeAll';

vi.mock('@mantine/notifications', () => ({
  notifications: { show: vi.fn() },
}));

function renderFilter(props: Partial<React.ComponentProps<typeof CategoryFilter>> = {}) {
  return render(
    <MantineProvider>
      <CategoryFilter
        favorite={false}
        onFavoriteChange={() => {}}
        tagId={0}
        onTagIdChange={() => {}}
        {...props}
      />
    </MantineProvider>,
  );
}

describe('CategoryFilter 分类筛选（FR-139，复用标签 FR-41）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    server.use(
      http.get('*/api/library/tags', () =>
        HttpResponse.json({
          items: [{ id: 7, name: '精选', created_at: '2025-01-01T00:00:00Z' }],
        }),
      ),
    );
  });

  it('分类下拉含「收藏夹」与各标签项', async () => {
    renderFilter();
    const select = await screen.findByRole('combobox', { name: '分类' });
    await waitFor(() => expect(screen.getByRole('option', { name: '收藏夹' })).toBeInTheDocument());
    await waitFor(() => expect(screen.getByRole('option', { name: '精选' })).toBeInTheDocument());
    expect(select).toBeInTheDocument();
  });

  it('选择「收藏夹」映射为 favorite=true 且清空标签', async () => {
    const onFavoriteChange = vi.fn();
    const onTagIdChange = vi.fn();
    renderFilter({ tagId: 7, onFavoriteChange, onTagIdChange });
    const select = await screen.findByRole('combobox', { name: '分类' });
    await userEvent.selectOptions(select, 'favorite');
    expect(onFavoriteChange).toHaveBeenCalledWith(true);
    expect(onTagIdChange).toHaveBeenCalledWith(0);
  });

  it('选择某标签映射为 tag_id 且关闭仅收藏', async () => {
    const onFavoriteChange = vi.fn();
    const onTagIdChange = vi.fn();
    renderFilter({ favorite: true, onFavoriteChange, onTagIdChange });
    const select = await screen.findByRole('combobox', { name: '分类' });
    await waitFor(() => expect(screen.getByRole('option', { name: '精选' })).toBeInTheDocument());
    await userEvent.selectOptions(select, 'tag:7');
    expect(onTagIdChange).toHaveBeenCalledWith(7);
    expect(onFavoriteChange).toHaveBeenCalledWith(false);
  });

  it('选择「全部」清空收藏与标签', async () => {
    const onFavoriteChange = vi.fn();
    const onTagIdChange = vi.fn();
    renderFilter({ favorite: true, tagId: 7, onFavoriteChange, onTagIdChange });
    const select = await screen.findByRole('combobox', { name: '分类' });
    await userEvent.selectOptions(select, 'all');
    expect(onFavoriteChange).toHaveBeenCalledWith(false);
    expect(onTagIdChange).toHaveBeenCalledWith(0);
  });

  it('新建标签后调用接口并按新标签筛选', async () => {
    let created: string | null = null;
    server.use(
      http.post('*/api/library/tags', async ({ request }) => {
        const body = (await request.json()) as { name: string };
        created = body.name;
        return HttpResponse.json(
          { id: 42, name: body.name, created_at: '2025-01-01T00:00:00Z' },
          { status: 201 },
        );
      }),
    );
    const onTagIdChange = vi.fn();
    renderFilter({ onTagIdChange });

    await userEvent.click(await screen.findByRole('button', { name: '新建标签' }));
    await userEvent.type(await screen.findByRole('textbox', { name: '标签名' }), '风景');
    await userEvent.click(screen.getByRole('button', { name: '创建' }));

    await waitFor(() => expect(created).toBe('风景'));
    await waitFor(() => expect(onTagIdChange).toHaveBeenCalledWith(42));
  });
});
