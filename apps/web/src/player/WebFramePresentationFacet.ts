import type {
  AdjacentFrameTarget,
  FramePresentationCapability,
  FramePresentationFacet,
  FrameStepDirection,
  PlaybackCommandContext,
  PresentedFrame,
} from '@jianvideo/player-core';

const DEFAULT_FRAME_DURATION = 1 / 30;
const CANCELED_MESSAGE = '帧呈现等待已取消';

export interface WebFrameTimelineEntry {
  readonly mediaTime: number;
  readonly sourceFrameIndex?: number;
  readonly stableFrameId?: string;
}

export interface WebBinaryFrameMarker {
  readonly bits: number;
  readonly cellSize: number;
  readonly threshold?: number;
  readonly x: number;
  readonly y: number;
}

export interface WebPresentedFrameIdentity {
  readonly sourceFrameIndex?: number;
  readonly stableFrameId?: string;
}

export type ResolvePresentedFrameIdentity = (
  metadata: VideoFrameCallbackMetadata,
) => WebPresentedFrameIdentity | null;

export type FramePresentationCapabilityListener = (capability: FramePresentationCapability) => void;

export function createBinaryFrameMarkerResolver(
  video: HTMLVideoElement,
  marker: WebBinaryFrameMarker,
): ResolvePresentedFrameIdentity {
  const config = normalizeMarker(marker);
  if (config === null) return () => null;
  const canvas = document.createElement('canvas');
  const width = (config.bits + 2) * config.cellSize;
  canvas.width = width;
  canvas.height = config.cellSize;
  const context = canvas.getContext('2d', { willReadFrequently: true });
  if (context === null) return () => null;
  return () => readBinaryFrameMarker(video, context, config, width);
}

export interface WebFramePresentationSource {
  readonly frameTimeline?: readonly WebFrameTimelineEntry[];
  readonly nominalFrameRate?: number;
  readonly resolvePresentedFrameIdentity?: ResolvePresentedFrameIdentity;
  readonly seekAvailable: boolean;
}

interface ActiveFrameSource {
  readonly command: PlaybackCommandContext;
  readonly nominalFrameDuration: number;
  readonly resolvePresentedFrameIdentity?: ResolvePresentedFrameIdentity;
  readonly timeline: readonly WebFrameTimelineEntry[];
}

interface FrameWaiter {
  reject(error: Error): void;
  resolve(frame: PresentedFrame): void;
}

export class WebFramePresentationFacet implements FramePresentationFacet {
  private active: ActiveFrameSource | null = null;
  private callbackId: number | null = null;
  private capability: FramePresentationCapability = 'unavailable';
  private disposed = false;
  private generation = 0;
  private presentedFrame: PresentedFrame | null = null;
  private presentationSequence = 0;
  private consumedPresentationSequence = 0;
  private readonly capabilityChanged?: FramePresentationCapabilityListener;
  private readonly video: HTMLVideoElement;
  private readonly waiters = new Set<FrameWaiter>();

  constructor(video: HTMLVideoElement, capabilityChanged?: FramePresentationCapabilityListener) {
    this.video = video;
    this.capabilityChanged = capabilityChanged;
  }

  load(source: WebFramePresentationSource, command: PlaybackCommandContext): void {
    if (this.disposed) return;
    this.resetObservation();
    const timeline = normalizeTimeline(source.frameTimeline);
    this.active = {
      command,
      nominalFrameDuration: resolveFrameDuration(source.nominalFrameRate),
      resolvePresentedFrameIdentity: source.resolvePresentedFrameIdentity,
      timeline,
    };
    this.capability = detectInitialCapability(source.seekAvailable);
    if (this.capability !== 'unavailable') this.scheduleObservation(this.generation);
  }

  getCapability(): FramePresentationCapability {
    return this.capability;
  }

  getCurrentPresentedFrame(command: PlaybackCommandContext): PresentedFrame | null {
    if (!this.accepts(command) || this.capability === 'unavailable') return null;
    if (this.presentedFrame) {
      this.consumedPresentationSequence = this.presentedFrame.presentationSequence;
      return this.presentedFrame;
    }
    if (this.capability === 'exact') return null;
    return this.createBackendFrame(command);
  }

  getAdjacentFrameTarget(
    current: PresentedFrame,
    direction: FrameStepDirection,
    command: PlaybackCommandContext,
  ): AdjacentFrameTarget | null {
    if (!this.accepts(command) || this.capability !== 'exact') return null;
    const timeline = this.active?.timeline ?? [];
    const currentIndex = findIdentityIndex(timeline, current);
    if (currentIndex < 0) return null;
    const targetIndex = currentIndex + (direction === 'next' ? 1 : -1);
    return createAdjacentTarget(timeline, currentIndex, targetIndex);
  }

  getNominalFrameDuration(command: PlaybackCommandContext): number | null {
    if (!this.accepts(command) || this.capability === 'unavailable') return null;
    return this.active?.nominalFrameDuration ?? null;
  }

  waitForPresentedFrame(command: PlaybackCommandContext): Promise<PresentedFrame> {
    if (!this.accepts(command) || this.capability !== 'exact') {
      return Promise.reject(new Error(CANCELED_MESSAGE));
    }
    if (
      this.presentedFrame &&
      this.presentedFrame.presentationSequence > this.consumedPresentationSequence
    ) {
      this.consumedPresentationSequence = this.presentedFrame.presentationSequence;
      return Promise.resolve(this.presentedFrame);
    }
    return new Promise((resolve, reject) => {
      this.waiters.add({ reject, resolve });
    });
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.resetObservation();
    this.active = null;
    this.capability = 'unavailable';
  }

  private resetObservation(): void {
    this.generation += 1;
    this.cancelObservation();
    this.rejectWaiters();
    this.presentedFrame = null;
    this.presentationSequence = 0;
    this.consumedPresentationSequence = 0;
  }

  private cancelObservation(): void {
    if (this.callbackId === null) return;
    this.video.cancelVideoFrameCallback?.(this.callbackId);
    this.callbackId = null;
  }

  private rejectWaiters(): void {
    const error = new Error(CANCELED_MESSAGE);
    this.waiters.forEach((waiter) => waiter.reject(error));
    this.waiters.clear();
  }

  private scheduleObservation(generation: number): void {
    if (!this.active || typeof this.video.requestVideoFrameCallback !== 'function') return;
    this.callbackId = this.video.requestVideoFrameCallback((_now, metadata) => {
      this.observeFrame(metadata, generation);
    });
  }

  private observeFrame(metadata: VideoFrameCallbackMetadata, generation: number): void {
    if (this.disposed || generation !== this.generation || !this.active) return;
    this.callbackId = null;
    const identity = resolveMatchedIdentity(this.active, metadata);
    const frame = this.createObservedFrame(metadata, this.active, identity ?? {});
    this.presentedFrame = frame;
    this.setCapability(identity ? 'exact' : 'approximate');
    if (identity) this.resolveWaiters(frame);
    this.scheduleObservation(generation);
  }

  private createObservedFrame(
    metadata: VideoFrameCallbackMetadata,
    active: ActiveFrameSource,
    identity: WebPresentedFrameIdentity,
  ): PresentedFrame {
    this.presentationSequence += 1;
    return {
      mediaTime: metadata.mediaTime,
      presentationSequence: this.presentationSequence,
      sampleSource: 'video-frame-callback',
      sourceEpoch: active.command.sourceEpoch,
      sourceId: active.command.sourceId,
      ...identity,
    };
  }

  private setCapability(capability: FramePresentationCapability): void {
    if (this.capability === capability) return;
    this.capability = capability;
    if (capability !== 'exact') this.rejectWaiters();
    this.capabilityChanged?.(capability);
  }

  private createBackendFrame(command: PlaybackCommandContext): PresentedFrame {
    return {
      mediaTime: finiteMediaTime(this.video.currentTime),
      presentationSequence: this.presentationSequence,
      sampleSource: 'backend',
      sourceEpoch: command.sourceEpoch,
      sourceId: command.sourceId,
    };
  }

  private resolveWaiters(frame: PresentedFrame): void {
    if (this.waiters.size === 0) return;
    this.consumedPresentationSequence = frame.presentationSequence;
    this.waiters.forEach((waiter) => waiter.resolve(frame));
    this.waiters.clear();
  }

  private accepts(command: PlaybackCommandContext): boolean {
    const active = this.active?.command;
    return (
      !this.disposed &&
      active?.sourceEpoch === command.sourceEpoch &&
      active.sourceId === command.sourceId
    );
  }
}

function detectInitialCapability(seekAvailable: boolean): FramePresentationCapability {
  return seekAvailable ? 'approximate' : 'unavailable';
}

function normalizeTimeline(
  timeline: readonly WebFrameTimelineEntry[] | undefined,
): readonly WebFrameTimelineEntry[] {
  if (!timeline?.length) return [];
  const normalized = timeline.filter(isValidTimelineEntry).map((entry) => ({ ...entry }));
  normalized.sort((left, right) => left.mediaTime - right.mediaTime);
  return normalized;
}

function isValidTimelineEntry(entry: WebFrameTimelineEntry): boolean {
  return Number.isFinite(entry.mediaTime) && entry.mediaTime >= 0;
}

function resolveFrameDuration(nominalFrameRate: number | undefined): number {
  if (nominalFrameRate !== undefined && Number.isFinite(nominalFrameRate) && nominalFrameRate > 0) {
    return 1 / nominalFrameRate;
  }
  return DEFAULT_FRAME_DURATION;
}

function resolveMatchedIdentity(
  active: ActiveFrameSource,
  metadata: VideoFrameCallbackMetadata,
): WebPresentedFrameIdentity | null {
  const identity = invokeIdentityProvider(active.resolvePresentedFrameIdentity, metadata);
  if (!identity) return null;
  const matches = active.timeline.filter((entry) => identityMatches(entry, identity));
  return matches.length === 1 ? identity : null;
}

function invokeIdentityProvider(
  provider: ResolvePresentedFrameIdentity | undefined,
  metadata: VideoFrameCallbackMetadata,
): WebPresentedFrameIdentity | null {
  if (!provider) return null;
  try {
    return validateIdentity(provider(metadata));
  } catch {
    return null;
  }
}

function validateIdentity(
  identity: WebPresentedFrameIdentity | null,
): WebPresentedFrameIdentity | null {
  if (!identity || typeof identity !== 'object') return null;
  const sourceFrameIndex = identity.sourceFrameIndex;
  const stableFrameId = identity.stableFrameId;
  const indexed = Number.isInteger(sourceFrameIndex) && (sourceFrameIndex ?? -1) >= 0;
  const stable = typeof stableFrameId === 'string' && stableFrameId.length > 0;
  if (sourceFrameIndex !== undefined && !indexed) return null;
  if (stableFrameId !== undefined && !stable) return null;
  if (!indexed && !stable) return null;
  return {
    ...(indexed ? { sourceFrameIndex } : {}),
    ...(stable ? { stableFrameId } : {}),
  };
}

function identityMatches(
  entry: WebFrameTimelineEntry,
  identity: WebPresentedFrameIdentity,
): boolean {
  const indexed =
    identity.sourceFrameIndex === undefined || entry.sourceFrameIndex === identity.sourceFrameIndex;
  const stable =
    identity.stableFrameId === undefined || entry.stableFrameId === identity.stableFrameId;
  return indexed && stable;
}

function findIdentityIndex(
  timeline: readonly WebFrameTimelineEntry[],
  current: PresentedFrame,
): number {
  const identity = validateIdentity({
    sourceFrameIndex: current.sourceFrameIndex,
    stableFrameId: current.stableFrameId,
  });
  if (!identity) return -1;
  const matches = timeline
    .map((entry, index) => (identityMatches(entry, identity) ? index : -1))
    .filter((index) => index >= 0);
  return matches.length === 1 ? matches[0] : -1;
}

function createAdjacentTarget(
  timeline: readonly WebFrameTimelineEntry[],
  currentIndex: number,
  targetIndex: number,
): AdjacentFrameTarget | null {
  const current = timeline[currentIndex];
  const target = timeline[targetIndex];
  if (!current || !target) return null;
  return {
    frameDuration: Math.abs(target.mediaTime - current.mediaTime),
    mediaTime: target.mediaTime,
    ...(target.sourceFrameIndex === undefined ? {} : { sourceFrameIndex: target.sourceFrameIndex }),
    ...(target.stableFrameId === undefined ? {} : { stableFrameId: target.stableFrameId }),
  };
}

function normalizeMarker(marker: WebBinaryFrameMarker): Required<WebBinaryFrameMarker> | null {
  const validInteger = (value: number) => Number.isInteger(value) && value >= 0;
  if (!validInteger(marker.x) || !validInteger(marker.y) || !validInteger(marker.cellSize))
    return null;
  if (!Number.isInteger(marker.bits) || marker.bits < 1 || marker.bits > 24 || marker.cellSize < 2)
    return null;
  const threshold = marker.threshold ?? 160;
  if (!Number.isFinite(threshold) || threshold <= 0 || threshold >= 255) return null;
  return { ...marker, threshold };
}

function readBinaryFrameMarker(
  video: HTMLVideoElement,
  context: CanvasRenderingContext2D,
  marker: Required<WebBinaryFrameMarker>,
  width: number,
): WebPresentedFrameIdentity | null {
  try {
    context.drawImage(
      video,
      marker.x,
      marker.y,
      width,
      marker.cellSize,
      0,
      0,
      width,
      marker.cellSize,
    );
    const pixels = context.getImageData(0, 0, width, marker.cellSize);
    return decodeBinaryFrameMarker(pixels, marker);
  } catch {
    return null;
  }
}

function decodeBinaryFrameMarker(
  pixels: ImageData,
  marker: Required<WebBinaryFrameMarker>,
): WebPresentedFrameIdentity | null {
  const cells = marker.bits + 2;
  if (pixels.width < cells * marker.cellSize || pixels.height < marker.cellSize) return null;
  const white = (cell: number) => cellLuma(pixels, cell, marker.cellSize) >= marker.threshold;
  if (!white(0) || !white(cells - 1)) return null;
  let sourceFrameIndex = 0;
  for (let bit = 0; bit < marker.bits; bit += 1) {
    if (white(bit + 1)) sourceFrameIndex += 2 ** bit;
  }
  return { sourceFrameIndex, stableFrameId: `binary-marker:${String(sourceFrameIndex)}` };
}

function cellLuma(pixels: ImageData, cell: number, cellSize: number): number {
  const centerX = cell * cellSize + Math.floor(cellSize / 2);
  const centerY = Math.floor(cellSize / 2);
  let sum = 0;
  let samples = 0;
  for (let y = centerY - 1; y <= centerY + 1; y += 1) {
    for (let x = centerX - 1; x <= centerX + 1; x += 1) {
      const offset = (y * pixels.width + x) * 4;
      sum +=
        ((pixels.data[offset] ?? 0) +
          (pixels.data[offset + 1] ?? 0) +
          (pixels.data[offset + 2] ?? 0)) /
        3;
      samples += 1;
    }
  }
  return sum / samples;
}

function finiteMediaTime(value: number): number {
  return Number.isFinite(value) && value >= 0 ? value : 0;
}
