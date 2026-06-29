import { describe, it, expect } from 'vitest';
import { buildDayTracks, TRACK_COLORS } from './gpsTrack';
import type { MediaFile } from '@/types';

/** 构造带 GPS 与媒体时间的测试媒体；lat/lon 为 undefined 表示无 GPS */
function geo(id: number, mediaTime: string, lat?: number, lon?: number): MediaFile {
  return {
    id,
    library_id: 1,
    file_path: `D:\\Photos\\${id}.jpg`,
    file_name: `${id}.jpg`,
    file_size: 0,
    format: 'jpg',
    video_codec: '',
    audio_codec: '',
    duration: 0,
    width: 0,
    height: 0,
    bitrate: 0,
    subtitle_tracks: '',
    added_at: mediaTime,
    modified_at: mediaTime,
    media_time: mediaTime,
    gps_lat: lat,
    gps_lon: lon,
  };
}

describe('buildDayTracks', () => {
  it('按天分组：每天点数≥2 生成一条轨迹，点序为当天时间升序', () => {
    const tracks = buildDayTracks([
      geo(1, '2025-01-01T08:00:00Z', 31.0, 121.0),
      geo(2, '2025-01-01T12:00:00Z', 31.1, 121.1),
      geo(3, '2025-01-02T09:00:00Z', 39.9, 116.4),
      geo(4, '2025-01-02T18:00:00Z', 39.8, 116.3),
    ]);
    expect(tracks).toHaveLength(2);
    const byDate = Object.fromEntries(tracks.map((t) => [t.date, t.positions]));
    expect(byDate['2025-01-01']).toEqual([
      [31.0, 121.0],
      [31.1, 121.1],
    ]);
    expect(byDate['2025-01-02']).toEqual([
      [39.9, 116.4],
      [39.8, 116.3],
    ]);
  });

  it('单点的天不生成轨迹（无法连线）', () => {
    const tracks = buildDayTracks([
      geo(1, '2025-01-01T08:00:00Z', 31.0, 121.0),
      geo(2, '2025-01-02T09:00:00Z', 39.9, 116.4),
      geo(3, '2025-01-02T18:00:00Z', 39.8, 116.3),
    ]);
    expect(tracks.map((t) => t.date)).toEqual(['2025-01-02']);
  });

  it('无 GPS 坐标的媒体被排除', () => {
    const tracks = buildDayTracks([
      geo(1, '2025-01-01T08:00:00Z', 31.0, 121.0),
      geo(2, '2025-01-01T10:00:00Z'),
      geo(3, '2025-01-01T12:00:00Z', 31.2, 121.2),
    ]);
    expect(tracks).toHaveLength(1);
    expect(tracks[0].positions).toEqual([
      [31.0, 121.0],
      [31.2, 121.2],
    ]);
  });

  it('空输入返回空数组', () => {
    expect(buildDayTracks([])).toEqual([]);
  });

  it('每条轨迹按下标循环取调色板颜色', () => {
    const files: MediaFile[] = [];
    // 构造 6 天、每天 2 个点，验证颜色按下标取模循环
    for (let d = 1; d <= 6; d++) {
      const day = String(d).padStart(2, '0');
      files.push(geo(d * 10, `2025-02-${day}T08:00:00Z`, 30 + d, 120 + d));
      files.push(geo(d * 10 + 1, `2025-02-${day}T12:00:00Z`, 30 + d + 0.1, 120 + d + 0.1));
    }
    const tracks = buildDayTracks(files);
    expect(tracks).toHaveLength(6);
    for (let i = 0; i < tracks.length; i++) {
      expect(tracks[i].color).toBe(TRACK_COLORS[i % TRACK_COLORS.length]);
    }
  });
});
