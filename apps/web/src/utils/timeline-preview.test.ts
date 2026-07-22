import { describe, expect, it } from 'vitest';

import { parseTimelinePreviewVtt } from './timeline-preview';

const IDENTITY = {
  generationId: 'generation-a',
  mediaId: '9',
  profileId: 'desktop',
  sourceFingerprint: 'source-a',
  spriteUrls: {
    'sprite-000.jpg': 'https://example.test/api/sprite-000.jpg',
    'sprite-001.jpg': 'https://example.test/api/sprite-001.jpg',
  },
};

describe('parseTimelinePreviewVtt', () => {
  it('解析首尾 cue 与 sprite 坐标为 PreparedPreviewTrack', () => {
    const track = parseTimelinePreviewVtt(
      `WEBVTT\n\n00:00:00.000 --> 00:00:02.500\nsprite-000.jpg#xywh=1,2,160,90\n\n00:00:02.500 --> 00:00:05.000\nsprite-001.jpg#xywh=161,2,160,90\n`,
      IDENTITY,
    );

    expect(track).toEqual({
      cues: [
        {
          endTime: 2.5,
          sprite: { assetId: 'sprite-000.jpg', height: 90, width: 160, x: 1, y: 2 },
          startTime: 0,
        },
        {
          endTime: 5,
          sprite: { assetId: 'sprite-001.jpg', height: 90, width: 160, x: 161, y: 2 },
          startTime: 2.5,
        },
      ],
      generationId: 'generation-a',
      mediaId: '9',
      profileId: 'desktop',
      sourceFingerprint: 'source-a',
    });
  });

  it('接受坐标零值但要求尺寸为正整数', () => {
    const track = parseTimelinePreviewVtt(
      'WEBVTT\n\n00:00.000 --> 00:01.000\nsprite-000.jpg#xywh=0,0,160,90\n',
      IDENTITY,
    );

    expect(track.cues[0]?.sprite).toMatchObject({ x: 0, y: 0 });
  });

  it('接受 CRLF 与分钟格式时间戳', () => {
    const track = parseTimelinePreviewVtt(
      'WEBVTT\r\n\r\n00:00.000 --> 00:01.250\r\nsprite-000.jpg#xywh=1,2,160,90\r\n',
      IDENTITY,
    );

    expect(track.cues[0]).toMatchObject({ endTime: 1.25, startTime: 0 });
  });

  it.each([
    ['缺少 WEBVTT 头', '00:00.000 --> 00:01.000\nsprite-000.jpg#xywh=1,2,160,90'],
    ['没有 cue', 'WEBVTT\n'],
    ['时间戳非法', 'WEBVTT\n\ninvalid --> 00:01.000\nsprite-000.jpg#xywh=1,2,160,90'],
    ['终点不大于起点', 'WEBVTT\n\n00:01.000 --> 00:01.000\nsprite-000.jpg#xywh=1,2,160,90'],
    [
      'cue 重叠',
      'WEBVTT\n\n00:00.000 --> 00:02.000\nsprite-000.jpg#xywh=1,2,160,90\n\n00:01.000 --> 00:03.000\nsprite-001.jpg#xywh=1,2,160,90',
    ],
    ['未知 sprite', 'WEBVTT\n\n00:00.000 --> 00:01.000\nunknown.webp#xywh=1,2,160,90'],
    ['sprite 路径遍历', 'WEBVTT\n\n00:00.000 --> 00:01.000\n../sprite-000.jpg#xywh=1,2,160,90'],
    ['缺少 xywh', 'WEBVTT\n\n00:00.000 --> 00:01.000\nsprite-000.jpg'],
    ['坐标非整数', 'WEBVTT\n\n00:00.000 --> 00:01.000\nsprite-000.jpg#xywh=1.5,2,160,90'],
    ['尺寸非正', 'WEBVTT\n\n00:00.000 --> 00:01.000\nsprite-000.jpg#xywh=0,0,0,90'],
    ['多余 cue 内容', 'WEBVTT\n\n00:00.000 --> 00:01.000\nsprite-000.jpg#xywh=1,2,160,90\nextra'],
    [
      '时间行包含设置',
      'WEBVTT\n\n00:00.000 --> 00:01.000 align:start\nsprite-000.jpg#xywh=1,2,160,90',
    ],
  ])('拒绝%s', (_label, vtt) => {
    expect(() => parseTimelinePreviewVtt(vtt, IDENTITY)).toThrow(Error);
  });

  it('拒绝空轨道身份字段', () => {
    expect(() =>
      parseTimelinePreviewVtt('WEBVTT\n\n00:00.000 --> 00:01.000\nsprite-000.jpg#xywh=1,2,160,90', {
        ...IDENTITY,
        generationId: ' ',
      }),
    ).toThrow(Error);
  });
});
