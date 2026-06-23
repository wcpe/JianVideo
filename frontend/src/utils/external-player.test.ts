import { describe, it, expect } from 'vitest'
import { buildExternalStreamURL } from './external-player'

// FR-79：外部播放器深链 URL 拼接（纯函数）
describe('buildExternalStreamURL（FR-79）', () => {
  const origin = 'http://192.168.1.10:8080'
  const token = 'abc123token'

  it('拼出免登 share 流端点的绝对地址', () => {
    expect(buildExternalStreamURL(origin, token, 7)).toBe(
      'http://192.168.1.10:8080/api/share/abc123token/media/7/stream',
    )
  })

  it('last_position > 0 时追加 #t= 续播点（取整秒）', () => {
    expect(buildExternalStreamURL(origin, token, 7, 123.4)).toBe(
      'http://192.168.1.10:8080/api/share/abc123token/media/7/stream#t=123',
    )
  })

  it('last_position 为 0 时不带 #t=', () => {
    expect(buildExternalStreamURL(origin, token, 7, 0)).toBe(
      'http://192.168.1.10:8080/api/share/abc123token/media/7/stream',
    )
  })

  it('last_position 缺省时不带 #t=', () => {
    expect(buildExternalStreamURL(origin, token, 7)).toBe(
      'http://192.168.1.10:8080/api/share/abc123token/media/7/stream',
    )
  })

  it('last_position 为负数 / 非有限值时不带 #t=（防御非法输入）', () => {
    expect(buildExternalStreamURL(origin, token, 7, -5)).toBe(
      'http://192.168.1.10:8080/api/share/abc123token/media/7/stream',
    )
    expect(buildExternalStreamURL(origin, token, 7, Number.NaN)).toBe(
      'http://192.168.1.10:8080/api/share/abc123token/media/7/stream',
    )
  })
})
