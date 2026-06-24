import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, it, expect } from 'vitest'

import RouteTransition from './RouteTransition'

describe('RouteTransition（FR-96 路由切换过渡）', () => {
  it('包裹页面内容并原样渲染（不破坏子内容）', () => {
    render(
      <MemoryRouter initialEntries={['/browse']}>
        <RouteTransition>
          <div>页面内容</div>
        </RouteTransition>
      </MemoryRouter>,
    )
    expect(screen.getByText('页面内容')).toBeInTheDocument()
  })

  it('外层容器挂载渐入动画类 route-fade', () => {
    const { container } = render(
      <MemoryRouter initialEntries={['/albums']}>
        <RouteTransition>
          <span>内容</span>
        </RouteTransition>
      </MemoryRouter>,
    )
    expect(container.querySelector('.route-fade')).not.toBeNull()
  })
})
