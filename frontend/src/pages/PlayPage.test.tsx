import { render, screen, waitFor } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import PlayPage from './PlayPage'

// mock VideoPlayer 组件，避免依赖 mpegts.js
vi.mock('@/components/VideoPlayer', () => ({
  default: (props: any) => <div data-testid="video-player" data-url={props.url} />,
}))

function renderPlayPage(route: string) {
  const router = createMemoryRouter(
    [{ path: '/play/:id', element: <PlayPage /> }],
    { initialEntries: [route] },
  )
  return render(
    <MantineProvider>
      <RouterProvider router={router} />
    </MantineProvider>,
  )
}

describe('PlayPage', () => {
  it('渲染加载状态', () => {
    renderPlayPage('/play/1')
    const skeleton = document.querySelector('.mantine-Skeleton-root')
    expect(skeleton).toBeInTheDocument()
  })

  it('无效 ID 显示错误提示', async () => {
    renderPlayPage('/play/0')
    await waitFor(() => {
      expect(screen.getByText('无效的媒体 ID')).toBeInTheDocument()
    })
  })
})
