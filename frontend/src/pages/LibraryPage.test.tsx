import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import LibraryPage from './LibraryPage'

describe('LibraryPage', () => {
  it('渲染媒体库管理标题', async () => {
    render(
      <MantineProvider>
        <MemoryRouter initialEntries={['/library']}>
          <LibraryPage />
        </MemoryRouter>
      </MantineProvider>,
    )
    await waitFor(() => {
      expect(screen.getByText('媒体库')).toBeVisible()
    })
  })

  it('显示添加路径输入框和按钮', async () => {
    render(
      <MantineProvider>
        <MemoryRouter initialEntries={['/library']}>
          <LibraryPage />
        </MemoryRouter>
      </MantineProvider>,
    )
    await waitFor(() => {
      expect(screen.getByPlaceholderText('输入目录路径，如 D:\\Videos')).toBeVisible()
      expect(screen.getByRole('button', { name: '添加' })).toBeVisible()
    })
  })
})
