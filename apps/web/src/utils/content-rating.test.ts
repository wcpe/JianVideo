import { describe, it, expect } from 'vitest';
import {
  CONTENT_RATINGS,
  formatContentRatingLabel,
  contentRatingBadgeColor,
} from './content-rating';

describe('content-rating（FR2-051）', () => {
  it('枚举含 G/PG/PG-13/R/UNRATED', () => {
    expect(CONTENT_RATINGS).toEqual(['G', 'PG', 'PG-13', 'R', 'UNRATED']);
  });

  it('formatContentRatingLabel：空/缺省为未分级', () => {
    expect(formatContentRatingLabel('')).toBe('未分级');
    expect(formatContentRatingLabel(null)).toBe('未分级');
    expect(formatContentRatingLabel(undefined)).toBe('未分级');
  });

  it('formatContentRatingLabel：已知分级返回带说明的标签', () => {
    expect(formatContentRatingLabel('G')).toMatch(/G/);
    expect(formatContentRatingLabel('PG-13')).toMatch(/PG-13/);
    expect(formatContentRatingLabel('R')).toMatch(/R/);
  });

  it('contentRatingBadgeColor：R 为 red，其余分级有稳定色', () => {
    expect(contentRatingBadgeColor('R')).toBe('red');
    expect(contentRatingBadgeColor('PG-13')).toBe('orange');
    expect(contentRatingBadgeColor('PG')).toBe('yellow');
    expect(contentRatingBadgeColor('G')).toBe('teal');
    expect(contentRatingBadgeColor('')).toBe('gray');
  });
});
