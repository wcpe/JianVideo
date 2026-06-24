import { describe, it, expect } from 'vitest'
import { appTheme } from './theme'

// FR-92 设计系统 token 守护：确保 appTheme 暴露统一的设计 token 与全局默认，
// 防止后续改动误删地基（第九期其余界面 FR 依赖之）。
describe('设计系统 token（FR-92）', () => {
  it('设全局默认圆角 md，使组件统一继承', () => {
    expect(appTheme.defaultRadius).toBe('md')
  })

  it('开启可见焦点环（无障碍地基）', () => {
    expect(appTheme.focusRing).toBe('auto')
  })

  it('可点控件光标为手型', () => {
    expect(appTheme.cursorType).toBe('pointer')
  })

  it('暴露完整圆角刻度 xs~xl', () => {
    expect(appTheme.radius).toMatchObject({
      xs: expect.any(String),
      sm: expect.any(String),
      md: expect.any(String),
      lg: expect.any(String),
      xl: expect.any(String),
    })
  })

  it('暴露 elevation 阴影刻度 xs~xl（卡片层次）', () => {
    expect(appTheme.shadows).toMatchObject({
      xs: expect.any(String),
      sm: expect.any(String),
      md: expect.any(String),
      lg: expect.any(String),
      xl: expect.any(String),
    })
    // 阴影应为非空 box-shadow 值
    expect(appTheme.shadows?.md).toContain('rgba')
  })

  it('暴露行高刻度 xs~xl', () => {
    expect(appTheme.lineHeights).toMatchObject({
      xs: expect.any(String),
      md: expect.any(String),
      xl: expect.any(String),
    })
  })

  it('标题刻度收小并平衡（h1 不再过大）', () => {
    const h1 = appTheme.headings?.sizes?.h1
    expect(h1?.fontSize).toBe('1.75rem')
    expect(appTheme.headings?.sizes?.h2?.fontSize).toBeDefined()
    expect(appTheme.headings?.sizes?.h3?.fontSize).toBeDefined()
  })

  it('暴露自定义语义 token contentMaxWidth（供布局限宽）', () => {
    expect((appTheme.other as { contentMaxWidth?: string })?.contentMaxWidth).toBe('1280px')
  })
})
