import { renderHook, act } from '@testing-library/react'
import { describe, it, expect, beforeEach } from 'vitest'

import {
  useNavWidth,
  clampNavWidth,
  NAV_WIDTH_MIN,
  NAV_WIDTH_MAX,
  NAV_WIDTH_DEFAULT,
} from './useNavWidth'

const STORAGE_KEY = 'jianvideo.nav.width'

describe('clampNavWidth（纯逻辑：夹紧导航宽度）', () => {
  it('低于下限夹到 min', () => {
    expect(clampNavWidth(100)).toBe(NAV_WIDTH_MIN)
    expect(clampNavWidth(0)).toBe(NAV_WIDTH_MIN)
    expect(clampNavWidth(-50)).toBe(NAV_WIDTH_MIN)
  })

  it('高于上限夹到 max', () => {
    expect(clampNavWidth(500)).toBe(NAV_WIDTH_MAX)
    expect(clampNavWidth(NAV_WIDTH_MAX + 1)).toBe(NAV_WIDTH_MAX)
  })

  it('范围内保留（取整）', () => {
    expect(clampNavWidth(200)).toBe(200)
    expect(clampNavWidth(240.6)).toBe(241)
  })

  it('非有限数回退默认值', () => {
    // NaN/Infinity 均非有限数，统一回退默认宽度（不参与上下限夹紧）
    expect(clampNavWidth(NaN)).toBe(NAV_WIDTH_DEFAULT)
    expect(clampNavWidth(Infinity)).toBe(NAV_WIDTH_DEFAULT)
  })
})

describe('useNavWidth（展开态宽度持久化）', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('无持久值时初始为默认宽度', () => {
    const { result } = renderHook(() => useNavWidth())
    expect(result.current[0]).toBe(NAV_WIDTH_DEFAULT)
  })

  it('设置宽度后夹紧并持久化到 localStorage', () => {
    const { result } = renderHook(() => useNavWidth())

    act(() => result.current[1](240))
    expect(result.current[0]).toBe(240)
    expect(localStorage.getItem(STORAGE_KEY)).toBe('240')
  })

  it('设置超界宽度被夹紧（上下限）', () => {
    const { result } = renderHook(() => useNavWidth())

    act(() => result.current[1](1000))
    expect(result.current[0]).toBe(NAV_WIDTH_MAX)
    expect(localStorage.getItem(STORAGE_KEY)).toBe(String(NAV_WIDTH_MAX))

    act(() => result.current[1](10))
    expect(result.current[0]).toBe(NAV_WIDTH_MIN)
    expect(localStorage.getItem(STORAGE_KEY)).toBe(String(NAV_WIDTH_MIN))
  })

  it('预置 localStorage 宽度，mount 后初始即为该值（夹紧）', () => {
    localStorage.setItem(STORAGE_KEY, '300')
    const { result } = renderHook(() => useNavWidth())
    expect(result.current[0]).toBe(300)
  })

  it('预置非法持久值时回退默认（夹紧兜底）', () => {
    localStorage.setItem(STORAGE_KEY, 'abc')
    const { result } = renderHook(() => useNavWidth())
    expect(result.current[0]).toBe(NAV_WIDTH_DEFAULT)
  })
})
