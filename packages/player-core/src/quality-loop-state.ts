import type { AbLoopState, PlaybackQuality, PlaybackQualityState, QualityTarget } from './types';

export const DATA_SAVER_MAX_HEIGHT = 480;
export const AB_LOOP_MIN_DURATION = 0.5;
export const PLAYBACK_RATES = [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2] as const;

export function createQualityState(): PlaybackQualityState {
  return {
    actualQuality: null,
    dataSaver: false,
    dataSaverBlocked: false,
    manualQuality: null,
    playbackRate: 1,
    qualities: [],
    qualityMode: 'auto',
  };
}

export function createAbLoopState(): AbLoopState {
  return { a: null, b: null, enabled: false };
}

export function normalizeQualities(qualities: readonly PlaybackQuality[]): readonly PlaybackQuality[] {
  return qualities.filter(isQuality).map((quality) => ({ ...quality }));
}

export function qualityById(qualities: readonly PlaybackQuality[], id: string | null): PlaybackQuality | null {
  return id === null ? null : (qualities.find((quality) => quality.id === id) ?? null);
}

export function qualityTarget(quality: Pick<PlaybackQuality, 'bandwidth' | 'height'>): QualityTarget | null {
  if (!validHeight(quality.height)) return null;
  return quality.bandwidth === undefined || !validBandwidth(quality.bandwidth)
    ? { height: quality.height }
    : { bandwidth: quality.bandwidth, height: quality.height };
}

export function resolveQuality(
  qualities: readonly PlaybackQuality[],
  target: QualityTarget,
  maxHeight: number | null = null,
): PlaybackQuality | null {
  const candidates = qualities.filter(
    (quality) => validHeight(quality.height) && (maxHeight === null || quality.height <= maxHeight),
  );
  if (candidates.length === 0) return null;
  const sameHeight = candidates.filter((quality) => quality.height === target.height);
  if (sameHeight.length > 0) return selectBandwidth(sameHeight, target.bandwidth);
  const lower = candidates.filter((quality) => quality.height! < target.height);
  if (lower.length > 0) return highestQuality(lower);
  return lowestQuality(candidates);
}

export function highestDataSaverQuality(qualities: readonly PlaybackQuality[]): PlaybackQuality | null {
  const candidates = qualities.filter(
    (quality) => validHeight(quality.height) && quality.height <= DATA_SAVER_MAX_HEIGHT,
  );
  return candidates.length === 0 ? null : highestQuality(candidates);
}

export function isPlaybackRate(rate: number): rate is (typeof PLAYBACK_RATES)[number] {
  return PLAYBACK_RATES.some((candidate) => candidate === rate);
}

export function validLoopPoint(currentTime: number, duration: number): boolean {
  return Number.isFinite(currentTime) && Number.isFinite(duration) && duration > 0 && currentTime >= 0 && currentTime <= duration;
}

export function validLoopRange(a: number, b: number, duration: number): boolean {
  return validLoopPoint(a, duration) && validLoopPoint(b, duration) && b - a >= AB_LOOP_MIN_DURATION;
}

function isQuality(quality: PlaybackQuality): boolean {
  return typeof quality.id === 'string' && quality.id.length > 0 && typeof quality.label === 'string';
}

function selectBandwidth(qualities: readonly PlaybackQuality[], bandwidth: number | undefined): PlaybackQuality {
  if (!validBandwidth(bandwidth)) return highestQuality(qualities);
  const notHigher = qualities.filter(
    (quality) => validBandwidth(quality.bandwidth) && quality.bandwidth <= bandwidth,
  );
  if (notHigher.length > 0) return highestQuality(notHigher);
  return lowestQuality(qualities);
}

function highestQuality(qualities: readonly PlaybackQuality[]): PlaybackQuality {
  return [...qualities].sort(compareQuality).at(-1)!;
}

function lowestQuality(qualities: readonly PlaybackQuality[]): PlaybackQuality {
  return [...qualities].sort(compareQuality)[0]!;
}

function compareQuality(left: PlaybackQuality, right: PlaybackQuality): number {
  return (left.height ?? 0) - (right.height ?? 0) || (left.bandwidth ?? 0) - (right.bandwidth ?? 0);
}

function validHeight(height: number | undefined): height is number {
  return Number.isFinite(height) && height !== undefined && height > 0;
}

function validBandwidth(bandwidth: number | undefined): bandwidth is number {
  return Number.isFinite(bandwidth) && bandwidth !== undefined && bandwidth > 0;
}
