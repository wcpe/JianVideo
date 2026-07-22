import { describe, it, expect } from 'vitest';
import { mediaDisplayName } from './media';
import type { MediaFile, MediaInference } from '@/types';

function makeFile(overrides: Partial<MediaFile>): MediaFile {
  return {
    id: 1,
    library_id: 1,
    file_path: '/x/real.mp4',
    file_name: 'real.mp4',
    file_size: 0,
    format: 'mp4',
    video_codec: '',
    audio_codec: '',
    duration: 0,
    width: 0,
    height: 0,
    bitrate: 0,
    subtitle_tracks: '',
    added_at: '',
    modified_at: '',
    ...overrides,
  };
}

function makeInference(overrides: Partial<MediaInference>): MediaInference {
  return {
    id: 1,
    media_id: 1,
    space_id: 'space-default',
    kind: 'movie',
    title: '自动片名',
    year: 2024,
    season: 0,
    episode: 0,
    episode_title: '',
    confidence: 0.9,
    source: 'offline_rule',
    rule_version: 'fr2-031-v1',
    manual: false,
    created_at: '',
    updated_at: '',
    ...overrides,
  };
}

describe('mediaDisplayName', () => {
  it('有显示名时优先用 display_name', () => {
    expect(mediaDisplayName(makeFile({ display_name: '我的影片' }))).toBe('我的影片');
  });

  it('显示名为空时回退 file_name', () => {
    expect(mediaDisplayName(makeFile({ display_name: '' }))).toBe('real.mp4');
  });

  it('显示名缺省时回退 file_name', () => {
    expect(mediaDisplayName(makeFile({}))).toBe('real.mp4');
  });

  it('显示名仅空白时回退 file_name', () => {
    expect(mediaDisplayName(makeFile({ display_name: '   ' }))).toBe('real.mp4');
  });

  it('人工推断标题优先于 display_name', () => {
    const file = makeFile({ display_name: '显示名' });
    const inference = makeInference({ title: '人工片名', manual: true, source: 'manual' });
    expect(mediaDisplayName(file, inference)).toBe('人工片名');
  });

  it('高置信自动推断在无显示名时用于展示', () => {
    const file = makeFile({ display_name: '' });
    expect(mediaDisplayName(file, makeInference({ title: '自动片名', confidence: 0.75 }))).toBe(
      '自动片名',
    );
  });

  it('低置信自动候选不覆盖真实文件名', () => {
    const file = makeFile({ display_name: '' });
    expect(mediaDisplayName(file, makeInference({ title: '低置信片名', confidence: 0.74 }))).toBe(
      'real.mp4',
    );
  });
});
