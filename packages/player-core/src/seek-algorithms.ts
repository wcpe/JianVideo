import type {
  AdjacentFrameTarget,
  FrameStepDirection,
  PresentedFrame,
  SeekTier,
  TimeRange,
} from "./types";

export const SEEK_TIERS = [
  { count: 1, kind: "frame" },
  { kind: "seconds", value: 0.5 },
  { kind: "seconds", value: 1 },
  { kind: "seconds", value: 5 },
  { kind: "seconds", value: 30 },
  { kind: "seconds", value: 60 },
] as const satisfies readonly SeekTier[];

export interface ExactFrameVerification {
  readonly identityAvailable: boolean;
  readonly timestampError: number;
  readonly valid: boolean;
}

export function isSeekTier(value: unknown): value is SeekTier {
  if (!isRecord(value)) {
    return false;
  }
  if (value.kind === "frame") {
    return value.count === 1;
  }
  return value.kind === "seconds" && isSecondTier(value.value);
}

export function canonicalSeekTier(tier: SeekTier): SeekTier {
  return tier.kind === "frame"
    ? SEEK_TIERS[0]
    : (SEEK_TIERS.find(
        (item) => item.kind === "seconds" && item.value === tier.value,
      ) ?? tier);
}

export function directionSign(direction: FrameStepDirection): 1 | -1 {
  return direction === "next" ? 1 : -1;
}

export function normalizeRanges(
  ranges: readonly TimeRange[],
): readonly TimeRange[] {
  const valid = ranges
    .filter(
      (range) =>
        Number.isFinite(range.start) &&
        Number.isFinite(range.end) &&
        range.start <= range.end,
    )
    .map((range) => ({ end: range.end, start: range.start }))
    .sort((left, right) => left.start - right.start || left.end - right.end);
  return mergeRanges(valid);
}

export function clampToRanges(
  value: number,
  ranges: readonly TimeRange[],
): number {
  let closest = ranges[0]?.start ?? value;
  let distance = Math.abs(value - closest);
  for (const range of ranges) {
    if (value >= range.start && value <= range.end) {
      return value;
    }
    const candidate = value < range.start ? range.start : range.end;
    const candidateDistance = Math.abs(value - candidate);
    if (candidateDistance < distance) {
      closest = candidate;
      distance = candidateDistance;
    }
  }
  return closest;
}

export function isPositiveDuration(
  value: number | null | undefined,
): value is number {
  return typeof value === "number" && Number.isFinite(value) && value > 0;
}

export function hasTargetIdentity(
  start: PresentedFrame,
  target: AdjacentFrameTarget,
): boolean {
  const indexed =
    start.sourceFrameIndex !== undefined &&
    target.sourceFrameIndex !== undefined;
  const stable =
    start.stableFrameId !== undefined &&
    target.stableFrameId !== undefined &&
    start.stableFrameId !== target.stableFrameId;
  return indexed || stable;
}

export function verifyExactFrame(
  start: PresentedFrame,
  target: AdjacentFrameTarget,
  actual: PresentedFrame,
  direction: FrameStepDirection,
): ExactFrameVerification {
  const timestampError = Math.abs(actual.mediaTime - target.mediaTime);
  const identityAvailable = hasActualIdentity(start, target, actual);
  const valid =
    identityAvailable &&
    isDirectionCorrect(start.mediaTime, actual.mediaTime, direction) &&
    isIdentityAdjacent(start, target, actual, direction) &&
    timestampError <= target.frameDuration + Number.EPSILON;
  return { identityAvailable, timestampError, valid };
}

function isSecondTier(value: unknown): value is 0.5 | 1 | 5 | 30 | 60 {
  return (
    value === 0.5 || value === 1 || value === 5 || value === 30 || value === 60
  );
}

function isRecord(value: unknown): value is Record<PropertyKey, unknown> {
  return typeof value === "object" && value !== null;
}

function mergeRanges(ranges: readonly TimeRange[]): readonly TimeRange[] {
  const merged: TimeRange[] = [];
  for (const range of ranges) {
    const previous = merged.at(-1);
    if (previous === undefined || range.start > previous.end) {
      merged.push(range);
    } else if (range.end > previous.end) {
      merged[merged.length - 1] = { end: range.end, start: previous.start };
    }
  }
  return merged;
}

function hasActualIdentity(
  start: PresentedFrame,
  target: AdjacentFrameTarget,
  actual: PresentedFrame,
): boolean {
  const indexed =
    start.sourceFrameIndex !== undefined &&
    target.sourceFrameIndex !== undefined &&
    actual.sourceFrameIndex !== undefined;
  const stable =
    start.stableFrameId !== undefined &&
    target.stableFrameId !== undefined &&
    actual.stableFrameId !== undefined;
  return indexed || stable;
}

function isDirectionCorrect(
  start: number,
  actual: number,
  direction: FrameStepDirection,
): boolean {
  return direction === "next" ? actual > start : actual < start;
}

function isIdentityAdjacent(
  start: PresentedFrame,
  target: AdjacentFrameTarget,
  actual: PresentedFrame,
  direction: FrameStepDirection,
): boolean {
  if (
    start.sourceFrameIndex !== undefined &&
    target.sourceFrameIndex !== undefined
  ) {
    const expected = start.sourceFrameIndex + directionSign(direction);
    return (
      target.sourceFrameIndex === expected &&
      actual.sourceFrameIndex === expected
    );
  }
  return (
    target.stableFrameId !== undefined &&
    actual.stableFrameId === target.stableFrameId
  );
}
