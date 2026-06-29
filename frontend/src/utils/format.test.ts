import { describe, it, expect } from 'vitest';
import { formatSize, formatDuration, formatAperture, formatShutter, formatIso } from './format';

describe('formatSize', () => {
  it.each([
    [0, '-'],
    [500, '500 B'],
    [1024, '1.0 KB'],
    [1024 * 1024, '1.0 MB'],
    [1024 * 1024 * 1024, '1.00 GB'],
    [1536, '1.5 KB'],
  ])('formatSize(%s) → %s', (input, expected) => {
    expect(formatSize(input)).toBe(expected);
  });
});

describe('formatDuration', () => {
  it.each([
    [0, '-'],
    [30, '0:30'],
    [65, '1:05'],
    [3600, '1:00:00'],
    [3661, '1:01:01'],
    [59, '0:59'],
  ])('formatDuration(%s) → %s', (input, expected) => {
    expect(formatDuration(input)).toBe(expected);
  });
});

describe('formatAperture（FR-106）', () => {
  it.each([
    ['f/2.8', 'f/2.8'], // 后端裸值原样
    ['f/1.8', 'f/1.8'],
    ['F/4', 'f/4'], // 大写归一
    ['1.8', 'f/1.8'], // 纯数值补前缀
    ['2', 'f/2'],
    ['', ''],
    [undefined, ''],
    [null, ''],
  ])('formatAperture(%s) → %s', (input, expected) => {
    expect(formatAperture(input as string | null | undefined)).toBe(expected);
  });
});

describe('formatShutter（FR-106）', () => {
  it.each([
    ['1/200', '1/200s'], // 后端裸值补 s
    ['1/60', '1/60s'],
    ['1/200s', '1/200s'], // 已带 s 不重复
    ['2', '2s'], // 长曝光整秒
    ['', ''],
    [undefined, ''],
    [null, ''],
  ])('formatShutter(%s) → %s', (input, expected) => {
    expect(formatShutter(input as string | null | undefined)).toBe(expected);
  });
});

describe('formatIso（FR-106）', () => {
  it.each([
    [100, 'ISO 100'],
    [6400, 'ISO 6400'],
    [0, ''],
    [undefined, ''],
    [null, ''],
  ])('formatIso(%s) → %s', (input, expected) => {
    expect(formatIso(input as number | null | undefined)).toBe(expected);
  });
});
