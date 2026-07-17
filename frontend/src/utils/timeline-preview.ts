import type { PreparedPreviewCue, PreparedPreviewTrack } from '@jianvideo/player-core';

export interface TimelinePreviewVttIdentity {
  readonly generationId: string;
  readonly mediaId: string;
  readonly profileId: string;
  readonly sourceFingerprint: string;
  readonly spriteUrls: Readonly<Record<string, string>>;
}

const TIMING_PATTERN =
  /^(\d{2}):([0-5]\d):([0-5]\d)\.(\d{3}) --> (\d{2}):([0-5]\d):([0-5]\d)\.(\d{3})$/u;
const SHORT_TIMING_PATTERN = /^([0-5]\d):([0-5]\d)\.(\d{3}) --> ([0-5]\d):([0-5]\d)\.(\d{3})$/u;
const SPRITE_PATTERN = /^(sprite-\d{3}\.jpg)#xywh=(\d+),(\d+),(\d+),(\d+)$/u;

export function parseTimelinePreviewVtt(
  vtt: string,
  identity: TimelinePreviewVttIdentity,
): PreparedPreviewTrack {
  validateIdentity(identity);
  const blocks = normalizeBlocks(vtt);
  const cues: PreparedPreviewCue[] = [];
  let previousEnd = 0;
  for (const block of blocks) {
    const cue = parseCue(block, identity.spriteUrls, previousEnd);
    cues.push(cue);
    previousEnd = cue.endTime;
  }
  if (cues.length === 0) {
    throw new Error('预览 VTT 不包含 cue');
  }
  return { cues, ...trackIdentity(identity) };
}

function normalizeBlocks(vtt: string): string[][] {
  const normalized = vtt.replaceAll('\r\n', '\n').replaceAll('\r', '\n').trimEnd();
  const sections = normalized.split(/\n{2,}/u);
  if (sections.shift()?.trim() !== 'WEBVTT') {
    throw new Error('预览 VTT 头无效');
  }
  return sections.map((section) => section.split('\n'));
}

function parseCue(
  lines: readonly string[],
  spriteUrls: Readonly<Record<string, string>>,
  previousEnd: number,
): PreparedPreviewCue {
  if (lines.length !== 2) {
    throw new Error('预览 cue 格式无效');
  }
  const [startTime, endTime] = parseTiming(lines[0] ?? '');
  if (endTime <= startTime || startTime < previousEnd) {
    throw new Error('预览 cue 时间范围无效');
  }
  return { endTime, sprite: parseSprite(lines[1] ?? '', spriteUrls), startTime };
}

function parseTiming(value: string): readonly [number, number] {
  const long = TIMING_PATTERN.exec(value);
  if (long !== null) {
    return [toSeconds(long.slice(1, 5)), toSeconds(long.slice(5, 9))];
  }
  const short = SHORT_TIMING_PATTERN.exec(value);
  if (short !== null) {
    return [toSeconds(['0', ...short.slice(1, 4)]), toSeconds(['0', ...short.slice(4, 7)])];
  }
  throw new Error('预览 cue 时间戳无效');
}

function toSeconds(parts: readonly string[]): number {
  const [hours = '0', minutes = '0', seconds = '0', milliseconds = '0'] = parts;
  return (
    Number(hours) * 3600 + Number(minutes) * 60 + Number(seconds) + Number(milliseconds) / 1000
  );
}

function parseSprite(
  value: string,
  spriteUrls: Readonly<Record<string, string>>,
): PreparedPreviewCue['sprite'] {
  const match = SPRITE_PATTERN.exec(value);
  if (match === null) {
    throw new Error('预览 sprite 描述无效');
  }
  const [assetName = '', x = '', y = '', width = '', height = ''] = match.slice(1);
  const dimensions = [x, y, width, height].map(Number);
  const [spriteX = -1, spriteY = -1, spriteWidth = 0, spriteHeight = 0] = dimensions;
  const invalidPosition =
    !Number.isInteger(spriteX) || spriteX < 0 || !Number.isInteger(spriteY) || spriteY < 0;
  const invalidSize =
    !Number.isInteger(spriteWidth) ||
    spriteWidth <= 0 ||
    !Number.isInteger(spriteHeight) ||
    spriteHeight <= 0;
  if (spriteUrls[assetName] === undefined || invalidPosition || invalidSize) {
    throw new Error('预览 sprite 身份或坐标无效');
  }
  return { assetId: assetName, height: spriteHeight, width: spriteWidth, x: spriteX, y: spriteY };
}

function validateIdentity(identity: TimelinePreviewVttIdentity): void {
  const values = [
    identity.generationId,
    identity.mediaId,
    identity.profileId,
    identity.sourceFingerprint,
  ];
  if (values.some((value) => value.trim().length === 0)) {
    throw new Error('预览轨道身份不能为空');
  }
}

function trackIdentity(identity: TimelinePreviewVttIdentity): Omit<PreparedPreviewTrack, 'cues'> {
  return {
    generationId: identity.generationId,
    mediaId: identity.mediaId,
    profileId: identity.profileId,
    sourceFingerprint: identity.sourceFingerprint,
  };
}
