import {
  PlaybackBackendError,
  type LoadControlFacet,
  type PlaybackCommandContext,
  type PlaybackQuality,
  type QualityFacet,
  type QualityFacetListener,
  type QualityFacetState,
  type QualitySelection,
  type QualityTarget,
} from '@jianvideo/player-core';
import type Hls from 'hls.js';

interface IndexedQuality {
  readonly index: number;
  readonly quality: PlaybackQuality;
}

export class WebQualityFacet implements QualityFacet, LoadControlFacet {
  private actualQualityId: string | null = null;
  private command: PlaybackCommandContext | null = null;
  private hls: Hls | null = null;
  private indexed: readonly IndexedQuality[] = [];
  private readonly listeners = new Set<QualityFacetListener>();
  private maxHeight: number | null = null;
  private playbackRate = 1;
  private selection: QualitySelection = { mode: 'auto' };
  private readonly unavailable = new Set<number>();
  private readonly video: HTMLVideoElement;

  constructor(video: HTMLVideoElement) {
    this.video = video;
  }

  load(command: PlaybackCommandContext): void {
    this.command = command;
    this.hls = null;
    this.indexed = [];
    this.unavailable.clear();
    this.actualQualityId = null;
    this.maxHeight = null;
    this.playbackRate = 1;
    this.selection = { mode: 'auto' };
    this.video.defaultPlaybackRate = 1;
    this.video.playbackRate = 1;
    this.emit(command);
  }

  attach(hls: Hls, command: PlaybackCommandContext): void {
    this.requireCommand(command);
    this.hls = hls;
    this.emit(command);
  }

  detach(command: PlaybackCommandContext): void {
    if (!this.isCurrentSource(command)) return;
    this.hls = null;
    this.indexed = [];
    this.actualQualityId = null;
    this.unavailable.clear();
    this.emit(command);
  }

  refreshLevels(command: PlaybackCommandContext): void {
    const hls = this.requireHls(command);
    this.unavailable.clear();
    this.indexed = createIndexedQualities(hls);
    this.applySelection(hls);
    this.applyCap(hls);
    this.actualQualityId = qualityAt(this.indexed, hls.currentLevel)?.id ?? null;
    this.emit(command);
  }

  handleLevelSwitched(levelIndex: number, command: PlaybackCommandContext): void {
    if (!this.isCurrentSource(command)) return;
    this.actualQualityId = qualityAt(this.indexed, levelIndex)?.id ?? null;
    this.emit(command);
  }

  handleLevelError(levelIndex: number, command: PlaybackCommandContext): boolean {
    const hls = this.requireHls(command);
    const failed = this.indexed.find((entry) => entry.index === levelIndex);
    if (failed === undefined) return false;
    this.unavailable.add(levelIndex);
    this.indexed = this.indexed.filter((entry) => entry.index !== levelIndex);
    if (this.selection.mode !== 'manual') {
      this.emit(command);
      return false;
    }
    const fallback = lowerQuality(this.indexed, failed.quality, this.maxHeight);
    if (fallback === null) {
      if (this.maxHeight === null) {
        this.selection = { mode: 'auto' };
        hls.currentLevel = -1;
      } else {
        hls.stopLoad();
      }
      this.emit(command);
      return true;
    }
    this.selection = { mode: 'manual', quality: toTarget(fallback.quality) };
    hls.currentLevel = fallback.index;
    this.emit(command);
    return true;
  }

  getState(): QualityFacetState {
    return {
      actualQualityId: this.actualQualityId,
      playbackRate: this.playbackRate,
      qualities: this.indexed.map((entry) => entry.quality),
      selection: this.selection,
    };
  }

  selectQuality(selection: QualitySelection, command: PlaybackCommandContext): Promise<void> {
    const hls = this.requireHls(command);
    if (selection.mode === 'auto') {
      this.selection = selection;
      hls.currentLevel = -1;
      this.emit(command);
      return Promise.resolve();
    }
    const matched = matchQuality(this.indexed, selection.quality, this.maxHeight);
    if (matched === null) return Promise.reject(unavailableQuality());
    this.selection = { mode: 'manual', quality: toTarget(matched.quality) };
    hls.currentLevel = matched.index;
    this.emit(command);
    return Promise.resolve();
  }

  setAutoQualityCap(maxHeight: number | null, command: PlaybackCommandContext): Promise<void> {
    const hls = this.requireHls(command);
    if (maxHeight !== null && highestQuality(this.indexed, maxHeight) === null) {
      return Promise.reject(new PlaybackBackendError('unsupported', '当前视频无 480p 或更低档位'));
    }
    this.maxHeight = maxHeight;
    this.applyCap(hls);
    this.emit(command);
    return Promise.resolve();
  }

  setPlaybackRate(rate: number, command: PlaybackCommandContext): Promise<void> {
    this.requireCommand(command);
    this.video.defaultPlaybackRate = rate;
    this.video.playbackRate = rate;
    if (this.video.playbackRate !== rate) {
      this.video.defaultPlaybackRate = 1;
      this.video.playbackRate = 1;
      this.playbackRate = 1;
      this.emit(command);
      return Promise.reject(new PlaybackBackendError('unsupported', '当前浏览器不支持该倍速'));
    }
    this.playbackRate = rate;
    this.emit(command);
    return Promise.resolve();
  }

  getLoadingState(): 'loading' | 'stopped' {
    return this.hls?.loadingEnabled ? 'loading' : 'stopped';
  }

  startLoading(command: PlaybackCommandContext): Promise<void> {
    const hls = this.requireHls(command);
    hls.startLoad();
    return Promise.resolve();
  }

  stopLoading(command: PlaybackCommandContext): Promise<void> {
    const hls = this.requireHls(command);
    hls.stopLoad();
    return Promise.resolve();
  }

  subscribe(listener: QualityFacetListener): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  private applySelection(hls: Hls): void {
    if (this.selection.mode === 'auto') {
      hls.currentLevel = -1;
      return;
    }
    const matched = matchQuality(this.indexed, this.selection.quality, this.maxHeight);
    if (matched === null) return;
    this.selection = { mode: 'manual', quality: toTarget(matched.quality) };
    hls.currentLevel = matched.index;
  }

  private applyCap(hls: Hls): void {
    const capped = this.maxHeight === null ? null : highestQuality(this.indexed, this.maxHeight);
    hls.autoLevelCapping = capped?.index ?? -1;
  }

  private requireHls(command: PlaybackCommandContext): Hls {
    this.requireCommand(command);
    if (this.hls === null) throw new PlaybackBackendError('unsupported', '当前播放路径不支持清晰度切换');
    return this.hls;
  }

  private requireCommand(command: PlaybackCommandContext): void {
    if (!this.isCurrentSource(command)) throw new DOMException('清晰度命令已过期', 'AbortError');
    if (this.command !== null && command.requestId >= this.command.requestId) this.command = command;
  }

  private isCurrentSource(command: PlaybackCommandContext): boolean {
    return (
      this.command !== null &&
      command.sourceEpoch === this.command.sourceEpoch &&
      command.sourceId === this.command.sourceId &&
      command.requestId >= this.command.requestId
    );
  }

  private emit(command: PlaybackCommandContext): void {
    if (!this.isCurrentSource(command)) return;
    const state = this.getState();
    this.listeners.forEach((listener) => listener(state, command));
  }
}

function createIndexedQualities(hls: Hls): readonly IndexedQuality[] {
  const heights = new Map<number, number>();
  hls.levels.forEach((level) => heights.set(level.height, (heights.get(level.height) ?? 0) + 1));
  return hls.levels.map((level, index) => {
    const bandwidth = level.bitrate;
    const bitrateLabel = `${Math.round(bandwidth / 1000)} kbps`;
    const label = heights.get(level.height)! > 1 ? `${level.height}p · ${bitrateLabel}` : `${level.height}p`;
    return {
      index,
      quality: {
        bandwidth,
        height: level.height,
        id: `${level.height}-${bandwidth}-${index}`,
        label,
      },
    };
  });
}

function matchQuality(
  indexed: readonly IndexedQuality[],
  target: QualityTarget,
  maxHeight: number | null,
): IndexedQuality | null {
  const candidates = indexed.filter(
    (entry) => entry.quality.height !== undefined && (maxHeight === null || entry.quality.height <= maxHeight),
  );
  const sameHeight = candidates.filter((entry) => entry.quality.height === target.height);
  if (sameHeight.length > 0) return selectBandwidth(sameHeight, target.bandwidth);
  const lower = candidates.filter((entry) => entry.quality.height! < target.height);
  return lower.length > 0 ? highestQuality(lower, null) : lowestQuality(candidates);
}

function lowerQuality(
  indexed: readonly IndexedQuality[],
  failed: PlaybackQuality,
  maxHeight: number | null,
): IndexedQuality | null {
  const candidates = indexed.filter((entry) => {
    if (entry.quality.height === undefined || failed.height === undefined) return false;
    if (maxHeight !== null && entry.quality.height > maxHeight) return false;
    return (
      entry.quality.height < failed.height ||
      (entry.quality.height === failed.height && (entry.quality.bandwidth ?? 0) < (failed.bandwidth ?? 0))
    );
  });
  return highestQuality(candidates, null);
}

function selectBandwidth(indexed: readonly IndexedQuality[], bandwidth: number | undefined): IndexedQuality {
  if (bandwidth === undefined) return highestQuality(indexed, null)!;
  const notHigher = indexed.filter((entry) => (entry.quality.bandwidth ?? 0) <= bandwidth);
  return notHigher.length > 0 ? highestQuality(notHigher, null)! : lowestQuality(indexed)!;
}

function highestQuality(indexed: readonly IndexedQuality[], maxHeight: number | null): IndexedQuality | null {
  const candidates = maxHeight === null
    ? indexed
    : indexed.filter((entry) => (entry.quality.height ?? Number.POSITIVE_INFINITY) <= maxHeight);
  return [...candidates].sort(compareQuality).at(-1) ?? null;
}

function lowestQuality(indexed: readonly IndexedQuality[]): IndexedQuality | null {
  return [...indexed].sort(compareQuality)[0] ?? null;
}

function compareQuality(left: IndexedQuality, right: IndexedQuality): number {
  return (
    (left.quality.height ?? 0) - (right.quality.height ?? 0) ||
    (left.quality.bandwidth ?? 0) - (right.quality.bandwidth ?? 0)
  );
}

function qualityAt(indexed: readonly IndexedQuality[], index: number): PlaybackQuality | null {
  return indexed.find((entry) => entry.index === index)?.quality ?? null;
}

function toTarget(quality: PlaybackQuality): QualityTarget {
  return {
    ...(quality.bandwidth === undefined ? {} : { bandwidth: quality.bandwidth }),
    height: quality.height!,
  };
}

function unavailableQuality(): PlaybackBackendError {
  return new PlaybackBackendError('unsupported', '目标清晰度当前不可用');
}
