import mpegts from 'mpegts.js';
import { PlaybackBackendError } from '@jianvideo/player-core';
import type {
  PlaybackBackend,
  PlaybackBackendEvent,
  PlaybackBackendListener,
  PlaybackCapabilities,
  PlaybackCommandContext,
  PlaybackError,
  PlaybackSnapshot,
  PlaybackSource,
  PlaybackState,
  SeekRequest,
  SeekResult,
  TimeRange,
} from '@jianvideo/player-core';
import type Hls from 'hls.js';
import type { TrackResponse } from '@/api/subtitle';
import { WebQualityFacet } from './WebQualityFacet';
import { WebTrackFacet } from './WebTrackFacet';
import {
  WebFramePresentationFacet,
  type ResolvePresentedFrameIdentity,
  type WebFrameTimelineEntry,
} from './WebFramePresentationFacet';

export interface WebPlaybackSourcePayload {
  readonly kind: 'native' | 'mpegts' | 'hls' | 'unsupported';
  readonly url: string;
  readonly mediaId?: number;
  readonly hlsSpaceId?: string;
  readonly trackResponse?: TrackResponse;
  readonly frameTimeline?: readonly WebFrameTimelineEntry[];
  readonly nominalFrameRate?: number;
  readonly resolvePresentedFrameIdentity?: ResolvePresentedFrameIdentity;
}

export interface WebPlaybackBackendCallbacks {
  getHlsRequestHeaders?(): Readonly<Record<string, string>>;
  hlsReadyTimeoutMs?: number;
  loadHlsModule?: () => Promise<{ default: typeof Hls }>;
  supportsHlsRuntime?(): boolean;
  onAbrLevelChange?(level: string | null): void;
  onPlaybackError?(error: PlaybackError): void;
  onWaitingChange?(waiting: boolean): void;
}

interface ActiveSource {
  readonly command: PlaybackCommandContext;
  readonly payload: WebPlaybackSourcePayload;
}

interface PlaybackRestorePoint {
  readonly currentTime: number;
  readonly intent: 'paused' | 'playing';
  readonly playbackRate: number;
}

interface AudioTransactionBase {
  readonly payload: WebPlaybackSourcePayload;
}

interface HlsFailureGuard {
  readonly promise: Promise<never>;
  reject(error: unknown): void;
  readonly token: number;
}

type PlaybackBackendEventPayload = PlaybackBackendEvent extends infer Event
  ? Event extends PlaybackBackendEvent
    ? Omit<Event, 'eventId' | 'requestId' | 'sourceEpoch' | 'sourceId'>
    : never
  : never;

const MEDIA_ERR_NETWORK = 2;
const MEDIA_ERR_DECODE = 3;
const MEDIA_ERR_SRC_NOT_SUPPORTED = 4;
const HLS_READY_TIMEOUT_MS = 10_000;

const BASE_CAPABILITIES: PlaybackCapabilities = {
  framePresentation: 'unavailable',
  loadControl: 'unavailable',
  preview: 'unavailable',
  quality: 'unavailable',
  seek: 'available',
  tracks: 'unavailable',
};

const INITIAL_SNAPSHOT: PlaybackSnapshot = {
  buffered: [],
  capabilities: { ...BASE_CAPABILITIES, seek: 'unavailable' },
  currentTime: 0,
  duration: 0,
  error: null,
  playbackRate: 1,
  requestId: 0,
  seekable: [],
  sourceEpoch: 0,
  sourceId: null,
  state: 'idle',
};

export class WebPlaybackBackend implements PlaybackBackend {
  private active: ActiveSource | null = null;
  private activeToken = 0;
  private audioCommandRevision = 0;
  private audioTransactionBase: AudioTransactionBase | null = null;
  private audioTransactionGeneration = 0;
  private disposed = false;
  private eventId = 0;
  private hls: Hls | null = null;
  private hlsFailureGuard: HlsFailureGuard | null = null;
  private readonly listeners = new Set<PlaybackBackendListener>();
  private mediaCleanups: Array<() => void> = [];
  private mpegtsPlayer: mpegts.Player | null = null;
  private playbackCurrentTime = 0;
  private playbackIntent: 'paused' | 'playing' = 'paused';
  private playbackRate = 1;
  private playbackRevision = 0;
  private readyReject: ((error: unknown) => void) | null = null;
  private readyResolve: (() => void) | null = null;
  private readyTimer: ReturnType<typeof setTimeout> | null = null;
  private reloadTimer: ReturnType<typeof setTimeout> | null = null;
  private resumeOnMpegtsReady = false;
  private seekAvailable = false;
  private snapshot: PlaybackSnapshot = INITIAL_SNAPSHOT;
  private readonly video: HTMLVideoElement;
  private readonly callbacks: WebPlaybackBackendCallbacks;
  readonly framePresentation: WebFramePresentationFacet;
  readonly loadControl: WebQualityFacet;
  readonly quality: WebQualityFacet;
  readonly tracks: WebTrackFacet;

  constructor(video: HTMLVideoElement, callbacks: WebPlaybackBackendCallbacks = {}) {
    this.video = video;
    this.callbacks = callbacks;
    this.quality = new WebQualityFacet(video);
    this.loadControl = this.quality;
    this.tracks = new WebTrackFacet(
      undefined,
      (command) => {
        if (this.acceptCommand(command)) this.audioCommandRevision += 1;
      },
      {
        supportsReload: callbacks.supportsHlsRuntime ?? browserSupportsHlsRuntime,
        switchSource: (url, spaceId, command, signal) =>
          this.transactAudioSource(url, spaceId, command, signal),
      },
    );
    this.framePresentation = new WebFramePresentationFacet(video, () => {
      this.publishCapabilities(this.activeToken);
    });
  }

  async load(source: PlaybackSource, command: PlaybackCommandContext): Promise<void> {
    if (this.disposed) return;
    const payload = readPayload(source);
    const token = this.beginSource(payload, command, source);
    this.publishCapabilities(token);
    this.publishSnapshot(token, 'loading');
    if (payload.kind === 'unsupported') {
      this.publishSnapshot(token, 'ready');
      return;
    }
    if (payload.kind === 'native') {
      this.loadNative(payload.url, token);
      return;
    }
    if (payload.kind === 'mpegts') {
      await this.loadMpegts(payload.url, token);
      return;
    }
    await this.loadHls(payload.url, token, payload.hlsSpaceId, 'manifest');
  }

  async transactAudioSource(
    url: string,
    spaceId: string,
    command: PlaybackCommandContext,
    signal: AbortSignal,
  ): Promise<void> {
    if (!this.isCurrentSource(command) || signal.aborted) throw abortError();
    this.rebasePlaybackControl({
      currentTime: finiteTime(this.video.currentTime),
      playbackRate: positiveTime(this.video.playbackRate, 1),
    });
    const active = this.active as ActiveSource;
    const base = this.audioTransactionBase ?? { payload: active.payload };
    this.audioTransactionBase = base;
    const commandRevision = this.audioCommandRevision;
    const generation = ++this.audioTransactionGeneration;
    const candidate = { ...base.payload, hlsSpaceId: spaceId, kind: 'hls' as const, url };
    try {
      await this.replacePlayback(candidate, signal);
      this.requireAudioTransaction(generation, commandRevision, signal);
      this.active = { command: this.active?.command ?? command, payload: candidate };
      this.audioTransactionBase = null;
    } catch (error: unknown) {
      if (!this.isAudioTransactionGeneration(generation)) throw abortError();
      try {
        await this.replacePlayback(base.payload);
        if (!this.isAudioTransactionGeneration(generation)) throw abortError();
        if (this.active) this.active = { command: this.active.command, payload: base.payload };
      } catch {
        if (!this.isAudioTransactionGeneration(generation)) throw abortError();
        this.audioTransactionBase = null;
        throw new Error(`${errorMessage(error)}；恢复原播放源失败`);
      }
      this.audioTransactionBase = null;
      throw error;
    }
  }

  async play(command: PlaybackCommandContext): Promise<void> {
    if (!this.acceptCommand(command)) return;
    this.rebasePlaybackControl({ intent: 'playing' });
    this.resumeOnMpegtsReady = false;
    if (this.hls) await this.quality.startLoading(command);
    const result = this.mpegtsPlayer ? this.mpegtsPlayer.play() : this.video.play();
    await result;
    if (this.isCurrentCommand(command)) this.publishSnapshot(this.activeToken, 'playing');
  }

  async pause(command: PlaybackCommandContext): Promise<void> {
    if (!this.acceptCommand(command)) return;
    this.rebasePlaybackControl({ intent: 'paused' });
    this.resumeOnMpegtsReady = false;
    if (this.mpegtsPlayer) this.mpegtsPlayer.pause();
    else this.video.pause();
    if (this.isCurrentCommand(command)) this.publishSnapshot(this.activeToken, 'paused');
  }

  async seek(request: SeekRequest): Promise<SeekResult> {
    const base = seekBase(request);
    if (!this.acceptCommand(request)) {
      return { ...base, confirmedTime: finiteTime(this.video.currentTime), status: 'superseded' };
    }
    try {
      this.video.currentTime = request.targetTime;
      const confirmedTime = finiteTime(this.video.currentTime);
      this.rebasePlaybackControl({ currentTime: confirmedTime });
      this.publishSnapshot(this.activeToken);
      return { ...base, confirmedTime, status: 'completed' };
    } catch {
      return {
        ...base,
        confirmedTime: finiteTime(this.video.currentTime),
        error: { category: 'media', message: '浏览器拒绝定位到目标时间' },
        status: 'failed',
      };
    }
  }

  getSnapshot(): PlaybackSnapshot {
    return this.snapshot;
  }

  subscribe(listener: PlaybackBackendListener): () => void {
    if (this.disposed) return () => undefined;
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.audioTransactionGeneration += 1;
    this.audioTransactionBase = null;
    this.activeToken += 1;
    this.releaseResources();
    this.framePresentation.dispose();
    this.tracks.dispose();
    this.active = null;
    this.snapshot = { ...this.snapshot, error: null, state: 'disposed' };
    this.listeners.clear();
  }

  private rebasePlaybackControl(update: Partial<PlaybackRestorePoint>): void {
    const currentTime = update.currentTime ?? this.playbackCurrentTime;
    const intent = update.intent ?? this.playbackIntent;
    const playbackRate = update.playbackRate ?? this.playbackRate;
    if (
      currentTime === this.playbackCurrentTime &&
      intent === this.playbackIntent &&
      playbackRate === this.playbackRate
    ) {
      return;
    }
    this.playbackCurrentTime = currentTime;
    this.playbackIntent = intent;
    this.playbackRate = playbackRate;
    this.playbackRevision += 1;
  }

  private captureRestorePoint(): PlaybackRestorePoint {
    return {
      currentTime: this.playbackCurrentTime,
      intent: this.playbackIntent,
      playbackRate: this.playbackRate,
    };
  }

  private async replacePlayback(
    payload: WebPlaybackSourcePayload,
    signal?: AbortSignal,
  ): Promise<void> {
    this.activeToken += 1;
    const token = this.activeToken;
    this.video.defaultPlaybackRate = this.playbackRate;
    this.releasePlaybackResources();
    this.callbacks.onAbrLevelChange?.(null);
    this.installMediaListeners(token);
    const replacement = this.loadAndRestorePlayback(payload, token, signal);
    const guard = payload.kind === 'hls' ? this.createHlsFailureGuard(token) : null;
    try {
      if (guard) await Promise.race([replacement, guard.promise]);
      else await replacement;
    } finally {
      if (this.hlsFailureGuard?.token === token) this.hlsFailureGuard = null;
    }
  }

  private async loadAndRestorePlayback(
    payload: WebPlaybackSourcePayload,
    token: number,
    signal?: AbortSignal,
  ): Promise<void> {
    await abortable(this.loadPayload(payload, token), signal);
    if (!this.isActiveToken(token)) throw abortError();
    await this.restorePlayback(token);
  }

  private async loadPayload(payload: WebPlaybackSourcePayload, token: number): Promise<void> {
    if (payload.kind === 'unsupported') return;
    if (payload.kind === 'native') {
      this.loadNative(payload.url, token);
      return;
    }
    if (payload.kind === 'mpegts') {
      await this.loadMpegts(payload.url, token);
      return;
    }
    await this.loadHls(payload.url, token, payload.hlsSpaceId);
  }

  private async restorePlayback(token: number): Promise<void> {
    while (this.isActiveToken(token)) {
      const revision = this.playbackRevision;
      const restore = this.captureRestorePoint();
      this.video.playbackRate = restore.playbackRate;
      this.video.currentTime = restore.currentTime;
      if (restore.intent === 'playing') {
        if (this.hls) await this.quality.startLoading(this.active!.command);
        const result = this.mpegtsPlayer ? this.mpegtsPlayer.play() : this.video.play();
        await result;
      } else if (this.mpegtsPlayer) this.mpegtsPlayer.pause();
      else this.video.pause();
      if (!this.isActiveToken(token)) throw abortError();
      if (revision !== this.playbackRevision) continue;
      this.publishSnapshot(token, restore.intent === 'playing' ? 'playing' : 'paused');
      return;
    }
    throw abortError();
  }

  private createHlsFailureGuard(token: number): HlsFailureGuard {
    let reject!: (error: unknown) => void;
    const promise = new Promise<never>((_resolve, rejectPromise) => {
      reject = rejectPromise;
    });
    const guard = { promise, reject, token };
    this.hlsFailureGuard = guard;
    return guard;
  }

  private failHlsTransaction(token: number, error: unknown): void {
    if (this.hlsFailureGuard?.token === token) this.hlsFailureGuard.reject(error);
  }

  private requireAudioTransaction(
    generation: number,
    commandRevision: number,
    signal: AbortSignal,
  ): void {
    if (
      signal.aborted ||
      !this.isAudioTransactionGeneration(generation) ||
      commandRevision !== this.audioCommandRevision
    ) {
      throw abortError();
    }
  }

  private isAudioTransactionGeneration(generation: number): boolean {
    return generation === this.audioTransactionGeneration && !this.disposed && this.active !== null;
  }

  private beginSource(
    payload: WebPlaybackSourcePayload,
    command: PlaybackCommandContext,
    source: PlaybackSource,
  ): number {
    this.audioTransactionGeneration += 1;
    this.audioTransactionBase = null;
    this.activeToken += 1;
    this.releaseResources();
    this.active = { command, payload };
    this.playbackCurrentTime = 0;
    this.playbackIntent = 'paused';
    this.playbackRate = 1;
    this.video.defaultPlaybackRate = 1;
    this.video.playbackRate = 1;
    this.video.preload = payload.kind === 'hls' ? 'none' : 'metadata';
    this.quality.load(command);
    this.playbackRevision += 1;
    this.resumeOnMpegtsReady = false;
    this.callbacks.onAbrLevelChange?.(null);
    this.callbacks.onWaitingChange?.(false);
    this.snapshot = createSourceSnapshot(command, source);
    this.seekAvailable = payload.kind !== 'unsupported';
    this.framePresentation.load(
      {
        frameTimeline: payload.frameTimeline,
        nominalFrameRate: payload.nominalFrameRate,
        resolvePresentedFrameIdentity: payload.resolvePresentedFrameIdentity,
        seekAvailable: this.seekAvailable,
      },
      command,
    );
    this.loadTracks(payload, command);
    this.installMediaListeners(this.activeToken);
    return this.activeToken;
  }

  private loadTracks(payload: WebPlaybackSourcePayload, command: PlaybackCommandContext): void {
    if (payload.mediaId === undefined || payload.trackResponse === undefined) {
      this.tracks.clear();
      return;
    }
    this.tracks.load({
      mediaId: payload.mediaId,
      requestId: command.requestId,
      response: payload.trackResponse,
      sourceEpoch: command.sourceEpoch,
      sourceId: command.sourceId,
    });
  }

  private loadNative(url: string, token: number): void {
    if (!this.isActiveToken(token)) return;
    this.video.src = url;
    this.publishSnapshot(token, 'ready');
  }

  private loadMpegts(url: string, token: number): Promise<void> {
    const ready = this.createReadyPromise();
    this.initializeMpegts(url, token);
    return ready;
  }

  private initializeMpegts(url: string, token: number): void {
    if (!this.isActiveToken(token)) return;
    const player = mpegts.createPlayer(
      { type: 'mpegts', url, isLive: true },
      {
        enableWorker: true,
        enableStashBuffer: true,
        stashInitialSize: 1024 * 1024,
        accurateSeek: true,
        seekType: 'range',
      },
    );
    this.mpegtsPlayer = player;
    player.attachMediaElement(this.video);
    this.installMpegtsListeners(player, token);
    player.load();
  }

  private async loadHls(
    url: string,
    token: number,
    spaceId?: string,
    readyMode: 'canplay' | 'manifest' = 'canplay',
  ): Promise<void> {
    const ready = this.createReadyPromise(
      this.callbacks.hlsReadyTimeoutMs ?? HLS_READY_TIMEOUT_MS,
      'HLS 清单就绪超时',
    );
    try {
      const loadModule = this.callbacks.loadHlsModule ?? (() => import('hls.js'));
      const HlsConstructor = (await loadModule()).default;
      if (!this.isActiveToken(token)) return ready;
      if (!HlsConstructor.isSupported()) {
        this.failReady(new Error('当前浏览器不支持 hls.js'));
      } else {
        this.initializeHls(HlsConstructor, url, token, spaceId, readyMode);
      }
    } catch (error: unknown) {
      this.failReady(new Error(`hls.js 加载失败：${errorMessage(error)}`));
    }
    return ready;
  }

  private initializeHls(
    HlsConstructor: typeof Hls,
    url: string,
    token: number,
    spaceId: string | undefined,
    readyMode: 'canplay' | 'manifest',
  ): void {
    const headers = this.callbacks.getHlsRequestHeaders?.() ?? {};
    const hls = new HlsConstructor({
      autoStartLoad: false,
      enableWorker: true,
      lowLatencyMode: true,
      startFragPrefetch: false,
      xhrSetup: (xhr) => {
        xhr.withCredentials = true;
        if (spaceId) xhr.setRequestHeader('X-JianVideo-Space-Id', spaceId);
        Object.entries(headers).forEach(([name, value]) => xhr.setRequestHeader(name, value));
      },
    });
    let manifestParsed = false;
    let mediaCanPlay = false;
    const isCurrentCandidate = () => this.isActiveToken(token) && this.hls === hls;
    const completeIfReady = () => {
      if (isCurrentCandidate() && manifestParsed && mediaCanPlay) this.completeReady(token);
    };
    this.hls = hls;
    if (this.active) this.quality.attach(hls, this.active.command);
    this.listen('canplay', () => {
      if (!isCurrentCandidate() || !manifestParsed) return;
      mediaCanPlay = true;
      completeIfReady();
    });
    hls.loadSource(url);
    hls.attachMedia(this.video);
    hls.on(HlsConstructor.Events.MANIFEST_PARSED, () => {
      if (!isCurrentCandidate() || !this.active) return;
      manifestParsed = true;
      this.quality.refreshLevels(this.active.command);
      this.publishCapabilities(token);
      if (readyMode === 'manifest') this.completeReady(token);
      else {
        this.quality.restartLoading();
        completeIfReady();
      }
    });
    hls.on(HlsConstructor.Events.LEVEL_SWITCHED, (_event, data) => {
      this.quality.handleLevelSwitched(data.level);
      this.publishAbrLevel(hls, data.level, token);
    });
    hls.on(HlsConstructor.Events.ERROR, (_event, data) => {
      if (!this.isActiveToken(token) || this.hls !== hls) return;
      const level = 'level' in data && typeof data.level === 'number' ? data.level : null;
      const recovery = level === null ? false : this.quality.handleLevelError(level);
      if (recovery === 'blocked') return;
      if (recovery === 'fallback') {
        this.quality.restartLoading();
        if (data.fatal && data.type === HlsConstructor.ErrorTypes.MEDIA_ERROR) hls.recoverMediaError();
        return;
      }
      if (!data.fatal) return;
      const error = new Error('HLS 播放发生致命错误');
      const transactionPending = this.hlsFailureGuard?.token === token;
      const readyPending = this.readyReject !== null;
      this.failReady(error);
      if (transactionPending) this.failHlsTransaction(token, error);
      else if (!readyPending) this.publishHlsFatal(token);
      this.releaseHls();
    });
  }

  private installMpegtsListeners(player: mpegts.Player, token: number): void {
    player.on('loadeddata', () => this.handleMpegtsLoaded(player, token));
    player.on('playing', () => {
      this.setWaiting(false, token);
      this.publishSnapshot(token, 'playing');
    });
    player.on('pause', () => this.publishSnapshot(token, 'paused'));
    player.on('error', () => this.handleMpegtsError(player, token));
  }

  private handleMpegtsLoaded(player: mpegts.Player, token: number): void {
    if (!this.isActiveToken(token) || this.mpegtsPlayer !== player) return;
    const shouldResume = this.resumeOnMpegtsReady;
    this.resumeOnMpegtsReady = false;
    this.completeReady(token);
    if (shouldResume) this.resumeMpegts(player, token);
  }

  private resumeMpegts(player: mpegts.Player, token: number): void {
    void Promise.resolve(player.play()).catch(() => {
      this.setWaiting(true, token);
    });
  }

  private handleMpegtsError(player: mpegts.Player, token: number): void {
    if (!this.isActiveToken(token) || this.mpegtsPlayer !== player) return;
    this.resumeOnMpegtsReady = this.playbackIntent === 'playing';
    this.setWaiting(true, token);
    if (this.reloadTimer) clearTimeout(this.reloadTimer);
    this.reloadTimer = setTimeout(() => this.reloadMpegts(player, token), 1000);
  }

  private reloadMpegts(player: mpegts.Player, token: number): void {
    this.reloadTimer = null;
    if (!this.isActiveToken(token) || this.mpegtsPlayer !== player) return;
    try {
      player.unload();
      player.load();
    } catch {
      this.setWaiting(true, token);
    }
  }

  private installMediaListeners(token: number): void {
    const publish = () => this.publishSnapshot(token);
    this.listen('timeupdate', publish);
    this.listen('durationchange', publish);
    this.listen('loadedmetadata', publish);
    this.listen('progress', publish);
    this.listen('ratechange', () =>
      this.rebasePlaybackControl({ playbackRate: positiveTime(this.video.playbackRate, 1) }),
    );
    this.listen('seeking', () => this.publishSnapshot(token, 'seeking'));
    this.listen('seeked', () => this.publishSnapshot(token, settledMediaState(this.video)));
    this.listen('playing', () => this.handlePlaying(token));
    this.listen('pause', () => this.publishSnapshot(token, 'paused'));
    this.listen('ended', () => this.handleEnded(token));
    this.listen('waiting', () => this.setWaiting(true, token));
    this.listen('stalled', () => this.setWaiting(true, token));
    this.listen('canplay', () => this.setWaiting(false, token));
    this.listen('error', () => this.handleNativeError(token));
  }

  private listen(event: string, listener: EventListener): void {
    this.video.addEventListener(event, listener);
    this.mediaCleanups.push(() => this.video.removeEventListener(event, listener));
  }

  private handlePlaying(token: number): void {
    this.setWaiting(false, token);
    this.publishSnapshot(token, 'playing');
  }

  private handleEnded(token: number): void {
    if (!this.isActiveToken(token)) return;
    this.publishSnapshot(token, 'ended');
    this.publishEvent(token, { type: 'ended' });
  }

  private handleNativeError(token: number): void {
    if (!this.isActiveToken(token)) return;
    const error = nativePlaybackError(this.video.error?.code);
    if (this.hlsFailureGuard?.token === token) {
      const transactionError = new Error(error.message);
      this.failReady(transactionError);
      this.failHlsTransaction(token, transactionError);
      this.releaseHls();
      return;
    }
    if (this.active?.payload.kind !== 'native') return;
    this.snapshot = { ...this.snapshot, error, state: 'error' };
    this.callbacks.onPlaybackError?.(error);
    this.publishEvent(token, { error, type: 'error' });
  }

  private publishHlsFatal(token: number): void {
    const error: PlaybackError = {
      category: 'media',
      code: 'HLS_FATAL',
      message: 'HLS 播放发生致命错误',
    };
    this.snapshot = { ...this.snapshot, error, state: 'error' };
    this.callbacks.onPlaybackError?.(error);
    this.publishEvent(token, { error, type: 'error' });
  }

  private publishAbrLevel(hls: Hls, levelIndex: number, token: number): void {
    if (!this.isActiveToken(token)) return;
    const level = hls.levels[levelIndex];
    if (!level) return;
    const label = level.height > 0 ? `${level.height}p` : `${level.width}x${level.height}`;
    this.callbacks.onAbrLevelChange?.(label);
  }

  private setWaiting(waiting: boolean, token: number): void {
    if (this.isActiveToken(token)) this.callbacks.onWaitingChange?.(waiting);
  }

  private createReadyPromise(timeoutMs?: number, timeoutMessage?: string): Promise<void> {
    this.settleReady();
    return new Promise((resolve, reject) => {
      this.readyResolve = resolve;
      this.readyReject = reject;
      if (timeoutMs !== undefined) {
        this.readyTimer = setTimeout(
          () => this.failReady(new Error(timeoutMessage ?? '媒体就绪超时')),
          timeoutMs,
        );
      }
    });
  }

  private completeReady(token: number): void {
    if (!this.isActiveToken(token)) return;
    this.setWaiting(false, token);
    this.publishSnapshot(token, 'ready');
    this.settleReady();
  }

  private failReady(error: unknown): void {
    const reject = this.readyReject;
    this.clearReady();
    reject?.(error);
  }

  private settleReady(): void {
    const resolve = this.readyResolve;
    this.clearReady();
    resolve?.();
  }

  private clearReady(): void {
    if (this.readyTimer) clearTimeout(this.readyTimer);
    this.readyTimer = null;
    this.readyResolve = null;
    this.readyReject = null;
  }

  private publishCapabilities(token: number): void {
    if (!this.isActiveToken(token)) return;
    const tracks = this.active?.payload.trackResponse
      ? ('available' as const)
      : ('unavailable' as const);
    const hlsCapabilities =
      this.active?.payload.kind === 'hls'
        ? { loadControl: 'available' as const, quality: 'available' as const }
        : {};
    const capabilities = this.seekAvailable
      ? {
          ...BASE_CAPABILITIES,
          ...hlsCapabilities,
          framePresentation: this.framePresentation.getCapability(),
          tracks,
        }
      : { ...BASE_CAPABILITIES, ...hlsCapabilities, seek: 'unavailable' as const, tracks };
    this.snapshot = { ...this.snapshot, capabilities };
    this.publishEvent(token, { capabilities, type: 'capabilitiesChanged' });
  }

  private publishSnapshot(token: number, state = this.snapshot.state): void {
    if (!this.isActiveToken(token)) return;
    this.snapshot = readMediaSnapshot(this.video, this.snapshot, state);
    this.publishEvent(token, { snapshot: this.snapshot, type: 'snapshotChanged' });
  }

  private publishEvent(token: number, event: PlaybackBackendEventPayload): void {
    if (!this.isActiveToken(token) || !this.active) return;
    const identity = this.active.command;
    const published = {
      ...event,
      eventId: this.eventId++,
      requestId: identity.requestId,
      sourceEpoch: identity.sourceEpoch,
      sourceId: identity.sourceId,
    } as PlaybackBackendEvent;
    this.listeners.forEach((listener) => listener(published));
  }

  private acceptCommand(command: PlaybackCommandContext): boolean {
    const active = this.active;
    if (
      this.disposed ||
      !active ||
      active.command.sourceEpoch !== command.sourceEpoch ||
      active.command.sourceId !== command.sourceId ||
      command.requestId < active.command.requestId
    ) {
      return false;
    }
    this.active = { ...active, command };
    this.snapshot = { ...this.snapshot, requestId: command.requestId };
    return true;
  }

  private isCurrentSource(command: PlaybackCommandContext): boolean {
    const current = this.active?.command;
    return (
      !this.disposed &&
      current?.sourceEpoch === command.sourceEpoch &&
      current.sourceId === command.sourceId
    );
  }

  private isCurrentCommand(command: PlaybackCommandContext): boolean {
    const current = this.active?.command;
    return (
      !this.disposed &&
      current?.requestId === command.requestId &&
      current.sourceEpoch === command.sourceEpoch &&
      current.sourceId === command.sourceId
    );
  }

  private isActiveToken(token: number): boolean {
    return !this.disposed && token === this.activeToken && this.active !== null;
  }

  private releaseResources(): void {
    this.releasePlaybackResources();
  }

  private releasePlaybackResources(): void {
    if (this.reloadTimer) clearTimeout(this.reloadTimer);
    this.reloadTimer = null;
    this.resumeOnMpegtsReady = false;
    this.settleReady();
    this.removeMediaListeners();
    this.releaseHls();
    this.releaseMpegts();
    this.video.removeAttribute('src');
    this.video.load();
  }

  private removeMediaListeners(): void {
    this.mediaCleanups.forEach((cleanup) => cleanup());
    this.mediaCleanups = [];
  }

  private releaseHls(): void {
    if (this.active) this.quality.detach(this.active.command);
    this.hls?.destroy();
    this.hls = null;
  }

  private releaseMpegts(): void {
    const player = this.mpegtsPlayer;
    if (!player) return;
    player.pause();
    player.unload();
    player.destroy();
    this.mpegtsPlayer = null;
  }
}

function readPayload(source: PlaybackSource): WebPlaybackSourcePayload {
  const payload = source.payload;
  if (!payload || typeof payload !== 'object') throw unsupportedSource();
  const candidate = payload as Partial<WebPlaybackSourcePayload>;
  if (!isPlaybackKind(candidate.kind) || typeof candidate.url !== 'string')
    throw unsupportedSource();
  return {
    kind: candidate.kind,
    url: candidate.url,
    ...(typeof candidate.mediaId === 'number' ? { mediaId: candidate.mediaId } : {}),
    ...(typeof candidate.hlsSpaceId === 'string' ? { hlsSpaceId: candidate.hlsSpaceId } : {}),
    ...(candidate.trackResponse ? { trackResponse: candidate.trackResponse } : {}),
    ...(Array.isArray(candidate.frameTimeline)
      ? { frameTimeline: candidate.frameTimeline as readonly WebFrameTimelineEntry[] }
      : {}),
    ...(typeof candidate.nominalFrameRate === 'number'
      ? { nominalFrameRate: candidate.nominalFrameRate }
      : {}),
    ...(typeof candidate.resolvePresentedFrameIdentity === 'function'
      ? { resolvePresentedFrameIdentity: candidate.resolvePresentedFrameIdentity }
      : {}),
  };
}

function isPlaybackKind(kind: unknown): kind is WebPlaybackSourcePayload['kind'] {
  return kind === 'native' || kind === 'mpegts' || kind === 'hls' || kind === 'unsupported';
}

function unsupportedSource(): PlaybackBackendError {
  return new PlaybackBackendError('unsupported', 'Web 播放源描述无效');
}

function createSourceSnapshot(
  command: PlaybackCommandContext,
  source: PlaybackSource,
): PlaybackSnapshot {
  return {
    ...INITIAL_SNAPSHOT,
    duration: finiteTime(source.metadata?.duration ?? 0),
    requestId: command.requestId,
    sourceEpoch: command.sourceEpoch,
    sourceId: command.sourceId,
    state: 'loading',
  };
}

function settledMediaState(video: HTMLVideoElement): PlaybackState {
  if (video.ended) return 'ended';
  return video.paused ? 'paused' : 'playing';
}

function readMediaSnapshot(
  video: HTMLVideoElement,
  previous: PlaybackSnapshot,
  state: PlaybackState,
): PlaybackSnapshot {
  return {
    ...previous,
    buffered: normalizeTimeRanges(video.buffered),
    currentTime: finiteTime(video.currentTime),
    duration: positiveTime(video.duration, previous.duration),
    error: state === 'error' ? previous.error : null,
    playbackRate: positiveTime(video.playbackRate, 1),
    seekable: normalizeTimeRanges(video.seekable),
    state,
  };
}

function normalizeTimeRanges(ranges: TimeRanges): readonly TimeRange[] {
  const normalized: TimeRange[] = [];
  for (let index = 0; index < ranges.length; index += 1) {
    const start = ranges.start(index);
    const end = ranges.end(index);
    if (Number.isFinite(start) && Number.isFinite(end) && start <= end) {
      normalized.push({ end, start });
    }
  }
  normalized.sort((left, right) => left.start - right.start || left.end - right.end);
  return mergeRanges(normalized);
}

function mergeRanges(ranges: readonly TimeRange[]): readonly TimeRange[] {
  const merged: TimeRange[] = [];
  for (const range of ranges) {
    const previous = merged.at(-1);
    if (!previous || range.start > previous.end) merged.push(range);
    else if (range.end > previous.end)
      merged[merged.length - 1] = { start: previous.start, end: range.end };
  }
  return merged;
}

function seekBase(request: SeekRequest): Omit<SeekResult, 'confirmedTime' | 'error' | 'status'> {
  return {
    clamped: request.requestedTime !== request.targetTime,
    requestId: request.requestId,
    requestedTime: request.requestedTime,
    targetTime: request.targetTime,
  };
}

function nativePlaybackError(code: number | undefined): PlaybackError {
  if (code === MEDIA_ERR_NETWORK) {
    return { category: 'network', code: 'MEDIA_ERR_NETWORK', message: '媒体网络读取失败' };
  }
  if (code === MEDIA_ERR_DECODE) {
    return { category: 'decode', code: 'MEDIA_ERR_DECODE', message: '媒体解码失败' };
  }
  if (code === MEDIA_ERR_SRC_NOT_SUPPORTED) {
    return {
      category: 'unsupported',
      code: 'MEDIA_ERR_SRC_NOT_SUPPORTED',
      message: '媒体格式或来源不受支持',
    };
  }
  return {
    category: 'media',
    code: code ? `MEDIA_ERR_${code}` : undefined,
    message: '媒体无法继续播放',
  };
}

function finiteTime(value: number): number {
  return Number.isFinite(value) && value >= 0 ? value : 0;
}

function positiveTime(value: number, fallback: number): number {
  return Number.isFinite(value) && value > 0 ? value : fallback;
}

function browserSupportsHlsRuntime(): boolean {
  return (
    typeof MediaSource !== 'undefined' &&
    typeof MediaSource.isTypeSupported === 'function' &&
    MediaSource.isTypeSupported('video/mp4; codecs="avc1.42E01E,mp4a.40.2"')
  );
}

function abortable<T>(operation: Promise<T>, signal?: AbortSignal): Promise<T> {
  if (!signal) return operation;
  if (signal.aborted) return Promise.reject(abortError());
  return Promise.race([
    operation,
    new Promise<T>((_resolve, reject) => {
      signal.addEventListener('abort', () => reject(abortError()), { once: true });
    }),
  ]);
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : '媒体源切换失败';
}

function abortError(): DOMException {
  return new DOMException('播放源事务已过期', 'AbortError');
}
