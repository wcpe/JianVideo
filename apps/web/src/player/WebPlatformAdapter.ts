export const DEFAULT_PLAYER_VISUAL_BRIGHTNESS = 1;
export const MIN_PLAYER_VISUAL_BRIGHTNESS = 0.5;
export const MAX_PLAYER_VISUAL_BRIGHTNESS = 1.5;
export const PLAYER_GESTURE_LOCK_THRESHOLD = 12;
export const PLAYER_GESTURE_MAX_SEEK_SECONDS = 120;

export type PlayerGestureKind = 'seek' | 'volume' | 'brightness';
export type PictureInPictureState =
  'unsupported' | 'idle' | 'requesting' | 'active' | 'exiting' | 'error';

export interface WebPlayerCapabilities {
  readonly backgroundAudio: 'best-effort';
  readonly mediaSession: 'available' | 'unavailable';
  readonly pictureInPicture: 'available' | 'unavailable';
  readonly playerVisualBrightness: 'available';
  readonly systemBrightness: 'unsupported';
  readonly touchSeek: 'available';
  readonly touchVolume: 'available';
}

export interface PlayerGestureInput {
  readonly deltaX: number;
  readonly deltaY: number;
  readonly startX: number;
  readonly threshold?: number;
  readonly width: number;
}

export interface SeekGestureInput {
  readonly deltaX: number;
  readonly duration: number;
  readonly maxOffsetSeconds?: number;
  readonly startTime: number;
  readonly width: number;
}

export interface VerticalGestureInput {
  readonly deltaY: number;
  readonly height: number;
  readonly max: number;
  readonly min: number;
  readonly startValue: number;
}

export interface MediaSessionCommands {
  pause(): unknown;
  play(): unknown;
  seekBy(offset: number): unknown;
  seekTo(time: number): unknown;
  stop(): unknown;
}

export interface MediaSessionMetadataInput {
  readonly applicationName: string;
  readonly artwork?: string;
  readonly title: string;
}

export interface MediaSessionSnapshot {
  readonly currentTime: number;
  readonly duration: number;
  readonly playbackRate: number;
  readonly state: string;
}

type MediaActionDetails = { readonly seekOffset?: number; readonly seekTime?: number };
type MediaActionHandler = ((details?: MediaActionDetails) => void) | null;

export interface MediaSessionPort {
  metadata: unknown;
  playbackState: MediaSessionPlaybackState;
  setActionHandler(action: MediaSessionAction, handler: MediaActionHandler): void;
  setPositionState?(state?: MediaPositionState): void;
}

type MetadataFactory = (metadata: MediaMetadataInit) => unknown;

const MEDIA_SESSION_ACTIONS = [
  'play',
  'pause',
  'seekbackward',
  'seekforward',
  'seekto',
  'stop',
] as const satisfies readonly MediaSessionAction[];

const INTERACTIVE_SELECTOR = [
  'button',
  'a',
  'input',
  'select',
  'textarea',
  '[role="button"]',
  '[role="slider"]',
  '[data-player-gesture-ignore="true"]',
].join(',');

export function detectWebPlayerCapabilities(
  video: HTMLVideoElement | null,
  mediaSession: unknown = typeof navigator === 'undefined' ? undefined : navigator.mediaSession,
): WebPlayerCapabilities {
  const pipDocumentAvailable =
    typeof document !== 'undefined' && document.pictureInPictureEnabled === true;
  const pipVideoAvailable =
    video !== null &&
    typeof video.requestPictureInPicture === 'function' &&
    video.disablePictureInPicture !== true;
  return {
    backgroundAudio: 'best-effort',
    mediaSession: mediaSession === undefined ? 'unavailable' : 'available',
    pictureInPicture: pipDocumentAvailable && pipVideoAvailable ? 'available' : 'unavailable',
    playerVisualBrightness: 'available',
    systemBrightness: 'unsupported',
    touchSeek: 'available',
    touchVolume: 'available',
  };
}

export function classifyPlayerGesture(input: PlayerGestureInput): PlayerGestureKind | null {
  const threshold = input.threshold ?? PLAYER_GESTURE_LOCK_THRESHOLD;
  const horizontal = Math.abs(input.deltaX);
  const vertical = Math.abs(input.deltaY);
  if (Math.max(horizontal, vertical) < threshold) return null;
  if (horizontal > vertical) return 'seek';
  return input.startX >= input.width / 2 ? 'volume' : 'brightness';
}

export function mapSeekGesture(input: SeekGestureInput): number {
  if (!Number.isFinite(input.duration) || input.duration <= 0 || input.width <= 0) return 0;
  const maxOffset = input.maxOffsetSeconds ?? PLAYER_GESTURE_MAX_SEEK_SECONDS;
  const offset = (input.deltaX / input.width) * maxOffset;
  return clamp(input.startTime + offset, 0, input.duration);
}

export function mapVerticalGesture(input: VerticalGestureInput): number {
  if (input.height <= 0) return clamp(input.startValue, input.min, input.max);
  return clamp(input.startValue - input.deltaY / input.height, input.min, input.max);
}

export function isGestureInteractiveTarget(
  target: EventTarget | null,
  surface: HTMLElement,
): boolean {
  if (!(target instanceof Element) || target === surface) return false;
  const interactive = target.closest(INTERACTIVE_SELECTOR);
  return interactive !== null && surface.contains(interactive);
}

export class WebMediaSessionAdapter {
  private readonly session: MediaSessionPort;
  private readonly commands: MediaSessionCommands;
  private readonly createMetadata: MetadataFactory;

  constructor(
    session: MediaSessionPort,
    commands: MediaSessionCommands,
    createMetadata: MetadataFactory = defaultMetadataFactory,
  ) {
    this.session = session;
    this.commands = commands;
    this.createMetadata = createMetadata;
    this.registerHandlers();
  }

  setMetadata(input: MediaSessionMetadataInput): void {
    const artwork = input.artwork ? [{ src: input.artwork }] : undefined;
    const metadata: MediaMetadataInit = {
      album: input.applicationName,
      artist: input.applicationName,
      ...(artwork === undefined ? {} : { artwork }),
      title: input.title,
    };
    this.session.metadata = this.createMetadata(metadata);
  }

  sync(snapshot: MediaSessionSnapshot): void {
    this.session.playbackState = mediaPlaybackState(snapshot.state);
    if (!this.session.setPositionState) return;
    if (!isValidPositionSnapshot(snapshot)) {
      this.clearPositionState();
      return;
    }
    const position = clamp(snapshot.currentTime, 0, snapshot.duration);
    try {
      this.session.setPositionState({
        duration: snapshot.duration,
        playbackRate: snapshot.playbackRate,
        position,
      });
    } catch {
      // 浏览器可逐次拒绝位置同步，不影响普通播放。
    }
  }

  dispose(): void {
    for (const action of MEDIA_SESSION_ACTIONS) this.setActionHandler(action, null);
    this.session.metadata = null;
    this.session.playbackState = 'none';
  }

  private clearPositionState(): void {
    try {
      this.session.setPositionState?.();
    } catch {
      // 浏览器不接受清空位置时保持当前系统展示，不影响普通播放。
    }
  }

  private registerHandlers(): void {
    this.setActionHandler('play', () => void this.commands.play());
    this.setActionHandler('pause', () => void this.commands.pause());
    this.setActionHandler('seekbackward', (details) => {
      void this.commands.seekBy(-(details?.seekOffset ?? 10));
    });
    this.setActionHandler('seekforward', (details) => {
      void this.commands.seekBy(details?.seekOffset ?? 10);
    });
    this.setActionHandler('seekto', (details) => {
      if (Number.isFinite(details?.seekTime)) void this.commands.seekTo(details!.seekTime!);
    });
    this.setActionHandler('stop', () => void this.commands.stop());
  }

  private setActionHandler(action: MediaSessionAction, handler: MediaActionHandler): void {
    try {
      this.session.setActionHandler(action, handler);
    } catch {
      // 浏览器可能只实现部分 action，逐项降级且不影响其余控制。
    }
  }
}

export class WebPictureInPictureAdapter {
  private readonly video: HTMLVideoElement;
  private readonly doc: Document;
  private readonly onStateChange: (state: PictureInPictureState) => void;
  private readonly onError: (message: string) => void;
  private activeSession = 0;
  private currentEnterGeneration: number | null = null;
  private disposed = false;
  private exitInFlight: {
    promise: Promise<boolean>;
    reportFailure: boolean;
    session: number;
  } | null = null;
  private generation = 0;
  private sessionGeneration = 0;
  private staleEnterExpected = false;
  private state: PictureInPictureState;

  constructor(
    video: HTMLVideoElement,
    doc: Document,
    onStateChange: (state: PictureInPictureState) => void,
    onError: (message: string) => void,
  ) {
    this.video = video;
    this.doc = doc;
    this.onStateChange = onStateChange;
    this.onError = onError;
    this.state = this.isAvailable() ? 'idle' : 'unsupported';
    video.addEventListener('enterpictureinpicture', this.handleEnter);
    video.addEventListener('leavepictureinpicture', this.handleLeave);
    this.onStateChange(this.state);
  }

  getState(): PictureInPictureState {
    return this.state;
  }

  async toggle(): Promise<void> {
    if (this.disposed) return;
    if (!this.isAvailable()) {
      this.setState('unsupported');
      return;
    }
    if (this.doc.pictureInPictureElement === this.video || this.state === 'active') {
      await this.requestExit(true);
      return;
    }
    if (this.state === 'requesting' || this.state === 'exiting') return;
    const generation = ++this.generation;
    this.currentEnterGeneration = generation;
    this.setState('requesting');
    try {
      await this.video.requestPictureInPicture();
      await this.completeEnterRequest(generation);
    } catch (error: unknown) {
      await this.failEnterRequest(generation, error);
    }
  }

  async resetForSourceChange(): Promise<void> {
    this.invalidateEntry();
    if (this.disposed) return;
    if (this.exitInFlight || this.doc.pictureInPictureElement === this.video) {
      await this.requestExit(true);
      return;
    }
    this.setState(this.isAvailable() ? 'idle' : 'unsupported');
  }

  dispose(): void {
    if (this.disposed) return;
    this.invalidateEntry();
    this.disposed = true;
    this.video.removeEventListener('enterpictureinpicture', this.handleEnter);
    this.video.removeEventListener('leavepictureinpicture', this.handleLeave);
    this.state = 'unsupported';
    void this.requestExit(false);
  }

  private readonly handleEnter = () => {
    this.activeSession = ++this.sessionGeneration;
    if (this.disposed) {
      void this.requestExit(false);
      return;
    }
    if (this.currentEnterGeneration !== null) {
      this.currentEnterGeneration = null;
      this.setState('active');
      return;
    }
    if (this.staleEnterExpected) {
      void this.requestExit(false);
      return;
    }
    this.setState('active');
  };

  private readonly handleLeave = () => {
    if (this.disposed) return;
    this.activeSession = 0;
    this.setState(
      this.currentEnterGeneration === null
        ? this.isAvailable()
          ? 'idle'
          : 'unsupported'
        : 'requesting',
    );
  };

  private async completeEnterRequest(generation: number): Promise<void> {
    if (generation !== this.currentEnterGeneration) {
      if (generation < this.generation) this.staleEnterExpected = false;
      if (this.disposed && this.doc.pictureInPictureElement === this.video) {
        await this.requestExit(false);
      }
      return;
    }
    if (this.disposed) {
      this.currentEnterGeneration = null;
      await this.requestExit(false);
      return;
    }
    if (this.doc.pictureInPictureElement === this.video) {
      this.activeSession = ++this.sessionGeneration;
      this.currentEnterGeneration = null;
      this.setState('active');
    }
  }

  private async failEnterRequest(generation: number, error: unknown): Promise<void> {
    if (generation !== this.currentEnterGeneration) {
      if (generation < this.generation) this.staleEnterExpected = false;
      return;
    }
    this.currentEnterGeneration = null;
    if (this.doc.pictureInPictureElement === this.video) {
      this.activeSession = this.activeSession || ++this.sessionGeneration;
      this.setState('active');
      return;
    }
    this.fail(error, '进入画中画失败');
  }

  private async requestExit(reportFailure: boolean): Promise<boolean> {
    while (true) {
      const inFlight = this.exitInFlight;
      if (inFlight) {
        inFlight.reportFailure ||= reportFailure;
        const result = await inFlight.promise;
        if (this.doc.pictureInPictureElement !== this.video) return result;
        if (this.activeSession === inFlight.session) return false;
        continue;
      }
      if (this.doc.pictureInPictureElement !== this.video) return true;
      const session = this.activeSession || ++this.sessionGeneration;
      this.activeSession = session;
      return this.startExit(session, reportFailure);
    }
  }

  private startExit(session: number, reportFailure: boolean): Promise<boolean> {
    if (!this.disposed) this.setState('exiting');
    const exit = {
      promise: Promise.resolve(false),
      reportFailure,
      session,
    };
    exit.promise = (async () => {
      try {
        await this.doc.exitPictureInPicture?.();
        return true;
      } catch (error: unknown) {
        if (!this.disposed && this.activeSession === session && exit.reportFailure) {
          this.fail(error, '退出画中画失败');
        } else if (!this.disposed && this.activeSession === session) {
          this.restorePendingState();
        }
        return false;
      } finally {
        if (this.exitInFlight === exit) this.exitInFlight = null;
      }
    })();
    this.exitInFlight = exit;
    return exit.promise;
  }

  private restorePendingState(): void {
    if (this.doc.pictureInPictureElement === this.video) {
      this.setState('active');
      return;
    }
    this.setState(
      this.currentEnterGeneration === null
        ? this.isAvailable()
          ? 'idle'
          : 'unsupported'
        : 'requesting',
    );
  }

  private invalidateEntry(): void {
    if (this.currentEnterGeneration !== null) this.staleEnterExpected = true;
    this.currentEnterGeneration = null;
    this.generation += 1;
  }

  private fail(error: unknown, fallback: string): void {
    if (this.disposed) return;
    this.setState('error');
    this.onError(error instanceof Error && error.message ? error.message : fallback);
  }

  private isAvailable(): boolean {
    return (
      this.doc.pictureInPictureEnabled === true &&
      typeof this.video.requestPictureInPicture === 'function' &&
      this.video.disablePictureInPicture !== true
    );
  }

  private setState(state: PictureInPictureState): void {
    if (this.disposed) return;
    this.state = state;
    this.onStateChange(state);
  }
}

function defaultMetadataFactory(metadata: MediaMetadataInit): unknown {
  return typeof MediaMetadata === 'function' ? new MediaMetadata(metadata) : metadata;
}

function mediaPlaybackState(state: string): MediaSessionPlaybackState {
  if (state === 'playing') return 'playing';
  if (state === 'paused' || state === 'ready' || state === 'ended') return 'paused';
  return 'none';
}

function isValidPositionSnapshot(snapshot: MediaSessionSnapshot): boolean {
  return (
    Number.isFinite(snapshot.duration) &&
    snapshot.duration > 0 &&
    Number.isFinite(snapshot.currentTime) &&
    Number.isFinite(snapshot.playbackRate) &&
    snapshot.playbackRate > 0
  );
}

function clamp(value: number, min: number, max: number): number {
  if (!Number.isFinite(value)) return min;
  return Math.min(max, Math.max(min, value));
}
