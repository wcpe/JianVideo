import { describe, it, expect } from 'vitest'
import { formatSize, formatDuration } from './format'

describe('formatSize', () => {
  it.each([
    [0, '-'],
    [500, '500 B'],
    [1024, '1.0 KB'],
    [1024 * 1024, '1.0 MB'],
    [1024 * 1024 * 1024, '1.00 GB'],
    [1536, '1.5 KB'],
  ])('formatSize(%s) → %s', (input, expected) => {
    expect(formatSize(input)).toBe(expected)
  })
})

describe('formatDuration', () => {
  it.each([
    [0, '-'],
    [30, '0:30'],
    [65, '1:05'],
    [3600, '1:00:00'],
    [3661, '1:01:01'],
    [59, '0:59'],
  ])('formatDuration(%s) → %s', (input, expected) => {
    expect(formatDuration(input)).toBe(expected)
  })
})
