import type {
  PlaybackCommandContext,
  PlaybackTrack,
  TrackFacet,
  TrackKind,
  TrackSelectionState,
} from '@jianvideo/player-core';
import {
  createAudioReload,
  getHLSStatus,
  type AudioReloadTaskResponse,
  type HLSPreviewStatus,
} from '@/api/play';
import type { SubtitleEntry } from '@/types';
import { getTrackContent, type TrackResponse } from '@/api/subtitle';
import { parseWebVTT } from '@/utils/subtitle';

type ContentLoader = (mediaId: number, trackId: string, signal?: AbortSignal) => Promise<string>;
type CueListener = (cues: readonly SubtitleEntry[]) => void;
type CommandListener = (command: PlaybackCommandContext) => void;
type RequestAwareSelection = TrackSelectionState & { readonly requestId: number };
type AudioReloadCreator = (
  mediaId: number,
  trackId: string,
  signal?: AbortSignal,
) => Promise<AudioReloadTaskResponse>;
type HLSStatusLoader = (
  mediaId: number,
  profileId: string,
  taskId?: string,
  signal?: AbortSignal,
) => Promise<HLSPreviewStatus>;
type AudioSourceTransaction = (
  url: string,
  spaceId: string,
  command: PlaybackCommandContext,
  signal: AbortSignal,
) => Promise<void>;

const AUDIO_POLL_INTERVAL_MS = 250;
const AUDIO_POLL_TIMEOUT_MS = 30_000;

export interface WebAudioReloadDependencies {
  readonly createReload?: AudioReloadCreator;
  readonly getStatus?: HLSStatusLoader;
  readonly pollIntervalMs?: number;
  readonly pollTimeoutMs?: number;
  readonly supportsReload?: () => boolean;
  readonly switchSource?: AudioSourceTransaction;
}

export interface WebTrackSource {
  readonly mediaId: number;
  readonly requestId: number;
  readonly response: TrackResponse;
  readonly sourceEpoch: number;
  readonly sourceId: string;
}

export class WebTrackFacet implements TrackFacet {
  private audioAbortController: AbortController | null = null;
  private audioGeneration = 0;
  private readonly confirmedSelections = new Map<TrackKind, TrackSelectionState>();
  private cues: readonly SubtitleEntry[] = [];
  private readonly listeners = new Set<CueListener>();
  private source: WebTrackSource | null = null;
  private subtitleAbortController: AbortController | null = null;
  private subtitleGeneration = 0;
  private readonly loadContent: ContentLoader;
  private readonly onCommand?: CommandListener;
  private readonly audio: Required<WebAudioReloadDependencies>;
  private selections = new Map<TrackKind, TrackSelectionState>();

  constructor(
    loadContent: ContentLoader = getTrackContent,
    onCommand?: CommandListener,
    audio: WebAudioReloadDependencies = {},
  ) {
    this.loadContent = loadContent;
    this.onCommand = onCommand;
    this.audio = {
      createReload: audio.createReload ?? createAudioReload,
      getStatus: audio.getStatus ?? getHLSStatus,
      pollIntervalMs: audio.pollIntervalMs ?? AUDIO_POLL_INTERVAL_MS,
      pollTimeoutMs: audio.pollTimeoutMs ?? AUDIO_POLL_TIMEOUT_MS,
      supportsReload: audio.supportsReload ?? (() => true),
      switchSource: audio.switchSource ?? missingAudioTransaction,
    };
  }

  load(source: WebTrackSource): void {
    this.cancelAllPending();
    this.source = source;
    this.cues = [];
    this.selections = new Map([
      ['audio', this.createSelection('audio')],
      ['subtitle', this.createSelection('subtitle')],
    ]);
    this.syncConfirmedSelections();
    this.publishCues();
  }

  clear(): void {
    this.cancelAllPending();
    this.source = null;
    this.cues = [];
    this.selections.clear();
    this.confirmedSelections.clear();
    this.publishCues();
  }

  updateResponse(response: TrackResponse, command?: PlaybackCommandContext): void {
    if (!this.source) return;
    if (command && !this.isCurrentSource(command)) return;
    const previousAudio = this.selections.get('audio');
    const previousSubtitle = this.selections.get('subtitle');
    const confirmedAudio = this.confirmedSelections.get('audio');
    const confirmedSubtitle = this.confirmedSelections.get('subtitle');
    const cancelAudio =
      this.audioAbortController !== null &&
      !trackSelectable(
        response,
        'audio',
        previousAudio?.selectedTrackId,
        this.audio.supportsReload(),
      );
    const cancelSubtitle =
      this.subtitleAbortController !== null &&
      !trackSelectable(response, 'subtitle', previousSubtitle?.selectedTrackId, true);
    this.source = {
      ...this.source,
      requestId: Math.max(command?.requestId ?? 0, this.source.requestId),
      response,
    };
    if (cancelAudio) this.cancelAudioPending();
    if (cancelSubtitle) this.cancelSubtitlePending();
    this.selections.set(
      'audio',
      this.refreshSelection('audio', cancelAudio ? confirmedAudio : previousAudio),
    );
    this.selections.set(
      'subtitle',
      this.refreshSelection('subtitle', cancelSubtitle ? confirmedSubtitle : previousSubtitle),
    );
    this.confirmedSelections.set('audio', this.refreshSelection('audio', confirmedAudio));
    this.confirmedSelections.set('subtitle', this.refreshSelection('subtitle', confirmedSubtitle));
    if (this.getSelectionState('subtitle').effectiveTrackId === null) {
      this.cues = [];
      this.publishCues();
    }
  }

  getTracks(kind: TrackKind): readonly PlaybackTrack[] {
    const tracks = this.source?.response.tracks.filter((track) => track.kind === kind) ?? [];
    if (kind !== 'audio' || this.audio.supportsReload()) return tracks;
    return tracks.map((track) =>
      track.capability === 'reload'
        ? {
            ...track,
            capability: 'unsupported',
            unsupportedReason: '当前浏览器不支持 HLS 音轨重载',
          }
        : track,
    );
  }

  getSelectionState(kind: TrackKind): TrackSelectionState {
    const state = this.selections.get(kind);
    if (!state) throw new Error('轨道源尚未加载');
    return state;
  }

  getCues(): readonly SubtitleEntry[] {
    return this.cues;
  }

  subscribeCues(listener: CueListener): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  async selectTrack(
    kind: TrackKind,
    trackId: string | null,
    command: PlaybackCommandContext,
  ): Promise<void> {
    this.requireCurrentSource(command);
    this.onCommand?.(command);
    if (kind === 'audio') {
      if (trackId === null) throw new Error('音轨不能为空');
      await this.selectAudio(trackId, command);
      return;
    }
    if (trackId === null) {
      this.closeSubtitles(command);
      return;
    }
    await this.selectSubtitle(trackId, command);
  }

  dispose(): void {
    this.cancelAllPending();
    this.source = null;
    this.cues = [];
    this.selections.clear();
    this.listeners.clear();
  }

  private async selectAudio(trackId: string, command: PlaybackCommandContext): Promise<void> {
    const source = this.requireSource();
    this.requireAudio(trackId);
    const previous = this.confirmedSelection('audio');
    const request = this.beginAudioRequest(trackId, command);
    if (previous.effectiveTrackId === trackId) {
      this.commitAudio(trackId, command, request.generation);
      return;
    }
    try {
      const created = await this.audio.createReload(
        source.mediaId,
        trackId,
        request.controller.signal,
      );
      this.requireAudioGeneration(command, request.generation);
      if (created.requested_track_id !== trackId) throw new Error('服务端返回的目标音轨不匹配');
      const ready = await this.pollAudioReady(source.mediaId, created, trackId, request);
      await this.audio.switchSource(
        ready.url,
        created.space_id,
        command,
        request.controller.signal,
      );
      this.commitAudio(trackId, command, request.generation);
    } catch (error: unknown) {
      this.rollbackAudio(previous, command, request.generation);
      throw error;
    }
  }

  private beginAudioRequest(trackId: string, command: PlaybackCommandContext) {
    this.cancelAudioPending();
    const controller = new AbortController();
    const generation = this.audioGeneration;
    this.audioAbortController = controller;
    this.advanceSourceRequest(command);
    this.selections.set('audio', {
      ...this.getSelectionState('audio'),
      requestId: command.requestId,
      selectedTrackId: trackId,
      sourceEpoch: command.sourceEpoch,
      sourceId: command.sourceId,
    } as RequestAwareSelection);
    return { controller, generation };
  }

  private async pollAudioReady(
    mediaId: number,
    created: AudioReloadTaskResponse,
    trackId: string,
    request: { controller: AbortController; generation: number },
  ): Promise<HLSPreviewStatus> {
    const deadline = Date.now() + this.audio.pollTimeoutMs;
    while (Date.now() <= deadline) {
      const status = await this.audio.getStatus(
        mediaId,
        created.profile_id,
        created.task_id,
        request.controller.signal,
      );
      const task = status.task;
      if (task && task.id === created.task_id) {
        if (task.status === 'failed' || task.status === 'canceled') {
          throw new Error(task.status === 'failed' ? '音轨版本生成失败' : '音轨版本生成已取消');
        }
        if (task.status === 'succeeded') {
          if (!status.available) throw new Error('音轨版本尚不可播放');
          if (status.effective_track_id !== trackId) {
            throw new Error('服务端确认的实际音轨不匹配');
          }
          return status;
        }
      }
      await wait(this.audio.pollIntervalMs, request.controller.signal);
    }
    throw new Error('音轨切换等待超时');
  }

  private commitAudio(trackId: string, command: PlaybackCommandContext, generation: number): void {
    this.requireAudioGeneration(command, generation);
    this.audioAbortController = null;
    const selection = {
      effectiveTrackId: trackId,
      kind: 'audio' as const,
      requestId: command.requestId,
      selectedTrackId: trackId,
      sourceEpoch: command.sourceEpoch,
      sourceId: command.sourceId,
    } as RequestAwareSelection;
    this.selections.set('audio', selection);
    this.confirmedSelections.set('audio', selection);
  }

  private rollbackAudio(
    previous: TrackSelectionState,
    command: PlaybackCommandContext,
    generation: number,
  ): void {
    if (!this.isAudioGeneration(command, generation)) return;
    this.audioAbortController = null;
    const restored = {
      ...previous,
      requestId: command.requestId,
    } as RequestAwareSelection;
    this.selections.set('audio', restored);
    this.confirmedSelections.set('audio', restored);
  }

  private async selectSubtitle(trackId: string, command: PlaybackCommandContext): Promise<void> {
    const source = this.requireSource();
    this.requireSubtitle(trackId);
    const previous = this.confirmedSelection('subtitle');
    const request = this.beginSubtitleRequest(trackId, command);
    try {
      const content = await this.loadContent(source.mediaId, trackId, request.controller.signal);
      this.commitSubtitle(trackId, parseWebVTT(content), command, request.generation);
    } catch (error: unknown) {
      this.rollbackSubtitle(previous, command, request.generation);
      throw error;
    }
  }

  private beginSubtitleRequest(trackId: string, command: PlaybackCommandContext) {
    this.cancelSubtitlePending();
    const controller = new AbortController();
    const generation = this.subtitleGeneration;
    this.subtitleAbortController = controller;
    this.advanceSourceRequest(command);
    this.selections.set('subtitle', {
      ...this.getSelectionState('subtitle'),
      requestId: command.requestId,
      selectedTrackId: trackId,
      sourceEpoch: command.sourceEpoch,
      sourceId: command.sourceId,
    } as RequestAwareSelection);
    return { controller, generation };
  }

  private commitSubtitle(
    trackId: string,
    cues: readonly SubtitleEntry[],
    command: PlaybackCommandContext,
    generation: number,
  ): void {
    this.requireSubtitleGeneration(command, generation);
    this.subtitleAbortController = null;
    const selection = {
      effectiveTrackId: trackId,
      kind: 'subtitle' as const,
      requestId: command.requestId,
      selectedTrackId: trackId,
      sourceEpoch: command.sourceEpoch,
      sourceId: command.sourceId,
    } as RequestAwareSelection;
    this.selections.set('subtitle', selection);
    this.confirmedSelections.set('subtitle', selection);
    this.cues = cues;
    this.publishCues();
  }

  private rollbackSubtitle(
    previous: TrackSelectionState,
    command: PlaybackCommandContext,
    generation: number,
  ): void {
    if (!this.isSubtitleGeneration(command, generation)) return;
    this.subtitleAbortController = null;
    const restored = {
      ...previous,
      requestId: command.requestId,
    } as RequestAwareSelection;
    this.selections.set('subtitle', restored);
    this.confirmedSelections.set('subtitle', restored);
  }

  private closeSubtitles(command: PlaybackCommandContext): void {
    this.cancelSubtitlePending();
    this.advanceSourceRequest(command);
    const selection = {
      effectiveTrackId: null,
      kind: 'subtitle' as const,
      requestId: command.requestId,
      selectedTrackId: null,
      sourceEpoch: command.sourceEpoch,
      sourceId: command.sourceId,
    } as RequestAwareSelection;
    this.selections.set('subtitle', selection);
    this.confirmedSelections.set('subtitle', selection);
    this.cues = [];
    this.publishCues();
  }

  private syncConfirmedSelections(): void {
    this.confirmedSelections.clear();
    for (const kind of ['audio', 'subtitle'] as const) {
      this.confirmedSelections.set(kind, { ...this.getSelectionState(kind) });
    }
  }

  private confirmedSelection(kind: TrackKind): TrackSelectionState {
    return { ...(this.confirmedSelections.get(kind) ?? this.getSelectionState(kind)) };
  }

  private createSelection(kind: TrackKind): RequestAwareSelection {
    const source = this.requireSource();
    return {
      ...source.response.selection[kind],
      kind,
      requestId: source.requestId,
      sourceEpoch: source.sourceEpoch,
      sourceId: source.sourceId,
    };
  }

  private refreshSelection(
    kind: TrackKind,
    previous: TrackSelectionState | undefined,
  ): RequestAwareSelection {
    if (!previous || !this.selectionExists(previous)) return this.createSelection(kind);
    const source = this.requireSource();
    return {
      ...previous,
      requestId: Math.max(previous.requestId ?? 0, source.requestId),
      sourceEpoch: source.sourceEpoch,
      sourceId: source.sourceId,
    } as RequestAwareSelection;
  }

  private selectionExists(selection: TrackSelectionState): boolean {
    return (
      this.trackExists(selection.kind, selection.selectedTrackId) &&
      this.trackExists(selection.kind, selection.effectiveTrackId)
    );
  }

  private trackExists(kind: TrackKind, trackId: string | null): boolean {
    if (trackId === null) return true;
    return this.getTracks(kind).some((track) => track.id === trackId);
  }

  private requireAudio(trackId: string): void {
    const track = this.getTracks('audio').find((candidate) => candidate.id === trackId);
    if (!track || track.available !== true)
      throw new Error(track?.unsupportedReason ?? '音轨不可用');
    if (track.capability !== 'reload') {
      throw new Error(track.unsupportedReason ?? '当前播放后端不支持该音轨切换方式');
    }
  }

  private requireSubtitle(trackId: string): void {
    const track = this.getTracks('subtitle').find((candidate) => candidate.id === trackId);
    if (!track || track.available !== true || track.capability === 'unsupported') {
      throw new Error(track?.unsupportedReason ?? '字幕轨道不可用');
    }
  }

  private requireCurrentSource(command: PlaybackCommandContext): void {
    if (!this.isCurrentSource(command)) throw abortError();
  }

  private isCurrentSource(command: PlaybackCommandContext): boolean {
    const source = this.requireSource();
    return (
      source.sourceId === command.sourceId &&
      source.sourceEpoch === command.sourceEpoch &&
      command.requestId >= source.requestId
    );
  }

  private advanceSourceRequest(command: PlaybackCommandContext): void {
    const source = this.requireSource();
    this.source = { ...source, requestId: Math.max(source.requestId, command.requestId) };
  }

  private requireAudioGeneration(command: PlaybackCommandContext, generation: number): void {
    if (!this.isAudioGeneration(command, generation)) throw abortError();
  }

  private isAudioGeneration(command: PlaybackCommandContext, generation: number): boolean {
    const state = this.selections.get('audio');
    return (
      generation === this.audioGeneration &&
      state?.requestId === command.requestId &&
      state.sourceEpoch === command.sourceEpoch &&
      state.sourceId === command.sourceId
    );
  }

  private requireSubtitleGeneration(command: PlaybackCommandContext, generation: number): void {
    if (!this.isSubtitleGeneration(command, generation)) throw abortError();
  }

  private isSubtitleGeneration(command: PlaybackCommandContext, generation: number): boolean {
    const state = this.selections.get('subtitle');
    return (
      generation === this.subtitleGeneration &&
      state?.requestId === command.requestId &&
      state.sourceEpoch === command.sourceEpoch &&
      state.sourceId === command.sourceId
    );
  }

  private requireSource(): WebTrackSource {
    if (!this.source) throw new Error('轨道源尚未加载');
    return this.source;
  }

  private cancelAudioPending(): void {
    this.audioGeneration += 1;
    this.audioAbortController?.abort();
    this.audioAbortController = null;
  }

  private cancelSubtitlePending(): void {
    this.subtitleGeneration += 1;
    this.subtitleAbortController?.abort();
    this.subtitleAbortController = null;
  }

  private cancelAllPending(): void {
    this.cancelAudioPending();
    this.cancelSubtitlePending();
  }

  private publishCues(): void {
    this.listeners.forEach((listener) => listener(this.cues));
  }
}

function trackSelectable(
  response: TrackResponse,
  kind: TrackKind,
  trackId: string | null | undefined,
  supportsAudioReload: boolean,
): boolean {
  if (trackId === null || trackId === undefined) return false;
  const track = response.tracks.find(
    (candidate) => candidate.kind === kind && candidate.id === trackId,
  );
  if (!track || track.available !== true || track.capability === 'unsupported') return false;
  return kind !== 'audio' || (supportsAudioReload && track.capability === 'reload');
}

function wait(delayMs: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(abortError());
      return;
    }
    const timer = setTimeout(resolve, delayMs);
    signal.addEventListener(
      'abort',
      () => {
        clearTimeout(timer);
        reject(abortError());
      },
      { once: true },
    );
  });
}

function missingAudioTransaction(): Promise<void> {
  return Promise.reject(new Error('当前播放后端未提供音轨源事务'));
}

function abortError(): DOMException {
  return new DOMException('轨道请求已过期', 'AbortError');
}
