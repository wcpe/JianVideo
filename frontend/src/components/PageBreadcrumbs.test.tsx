import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { describe, it, expect } from 'vitest'

import PageBreadcrumbs from './PageBreadcrumbs'

function renderCrumbs(items: { label: string; to?: string }[]) {
  return render(
    <MantineProvider>
      <MemoryRouter>
        <PageBreadcrumbs items={items} />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('PageBreadcrumbs（FR-95 深层页面包屑）', () => {
  it('带 to 的中间项渲染为链接，指向对应路由', () => {
    renderCrumbs([
      { label: '首页', to: '/' },
      { label: '目录浏览' },
    ])

    const home = screen.getByRole('link', { name: '首页' })
    expect(home).toHaveAttribute('href', '/')
  })

  it('末项（当前页）渲染为纯文本、不是链接', () => {
    renderCrumbs([
      { label: '首页', to: '/' },
      { label: '目录浏览' },
    ])

    // 末项当前页文字可见，但不应是链接
    expect(screen.getByText('目录浏览')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '目录浏览' })).toBeNull()
  })

  it('多级层级按序渲染（首页 › 相册 › 我的相册）', () => {
    renderCrumbs([
      { label: '首页', to: '/' },
      { label: '相册', to: '/albums' },
      { label: '我的相册' },
    ])

    expect(screen.getByRole('link', { name: '首页' })).toHaveAttribute('href', '/')
    expect(screen.getByRole('link', { name: '相册' })).toHaveAttribute('href', '/albums')
    expect(screen.getByText('我的相册')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '我的相册' })).toBeNull()
  })
})
