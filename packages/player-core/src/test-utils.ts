import type {
  PlaybackBackend,
  PlaybackBackendEvent,
  PlaybackBackendListener,
  PlaybackCapabilities,
  PlaybackCommandContext,
  PlaybackSnapshot,
  PlaybackSource,
  SeekRequest,
  SeekResult,
} from "./types";

export class Deferred<T> {
  readonly promise: Promise<T>;
  private resolvePromise!: (value: T | PromiseLike<T>) => void;
  private rejectPromise!: (reason?: unknown) => void;

  constructor() {
    this.promise = new Promise<T>((resolve, reject) => {
      this.resolvePromise = resolve;
      this.rejectPromise = reject;
    });
  }

  resolve(value: T): void {
    this.resolvePromise(value);
  }

  reject(reason: unknown): void {
    this.rejectPromise(reason);
  }
}

export const EMPTY_CAPABILITIES: PlaybackCapabilities = {
  framePresentation: "unavailable",
  loadControl: "unavailable",
  preview: "unavailable",
  quality: "unavailable",
  seek: "available",
  tracks: "unavailable",
};

export function createSnapshot(
  overrides: Partial<PlaybackSnapshot> = {},
): PlaybackSnapshot {
  return {
    buffered: [],
    capabilities: EMPTY_CAPABILITIES,
    currentTime: 0,
    duration: 60,
    error: null,
    playbackRate: 1,
    requestId: 0,
    seekable: [{ end: 60, start: 0 }],
    sourceEpoch: 0,
    sourceId: null,
    state: "idle",
    ...overrides,
  };
}

export class FakePlaybackBackend implements PlaybackBackend {
  readonly calls: Array<{
    readonly method: string;
    readonly requestId: number;
    readonly sourceEpoch: number;
    readonly sourceId: string;
    readonly targetTime?: number;
  }> = [];
  disposeCount = 0;
  disposeHandler: (() => void) | undefined;
  loadHandler:
    | ((
        source: PlaybackSource,
        command: PlaybackCommandContext,
      ) => Promise<void>)
    | undefined;
  pauseHandler:
    ((command: PlaybackCommandContext) => Promise<void>) | undefined;
  playHandler: ((command: PlaybackCommandContext) => Promise<void>) | undefined;
  seekHandler: ((request: SeekRequest) => Promise<SeekResult>) | undefined;
  private readonly listeners = new Set<PlaybackBackendListener>();
  private snapshot = createSnapshot();

  async load(
    source: PlaybackSource,
    command: PlaybackCommandContext,
  ): Promise<void> {
    this.recordCall("load", command);
    await this.loadHandler?.(source, command);
    this.snapshot = readySnapshot(this.snapshot, command);
  }

  async play(command: PlaybackCommandContext): Promise<void> {
    this.recordCall("play", command);
    await this.playHandler?.(command);
    this.snapshot = {
      ...this.snapshot,
      requestId: command.requestId,
      state: "playing",
    };
  }

  async pause(command: PlaybackCommandContext): Promise<void> {
    this.recordCall("pause", command);
    await this.pauseHandler?.(command);
    this.snapshot = {
      ...this.snapshot,
      requestId: command.requestId,
      state: "paused",
    };
  }

  async seek(request: SeekRequest): Promise<SeekResult> {
    this.recordCall("seek", request, request.targetTime);
    const result = await this.seekHandler?.(request);
    return result ?? defaultSeekResult(request);
  }

  getSnapshot(): PlaybackSnapshot {
    return this.snapshot;
  }

  private recordCall(
    method: string,
    command: PlaybackCommandContext,
    targetTime?: number,
  ): void {
    const call = {
      method,
      requestId: command.requestId,
      sourceEpoch: command.sourceEpoch,
      sourceId: command.sourceId,
      ...(targetTime === undefined ? {} : { targetTime }),
    };
    this.calls.push(call);
  }

  subscribe(listener: PlaybackBackendListener): () => void {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  emit(event: PlaybackBackendEvent): void {
    for (const listener of this.listeners) {
      listener(event);
    }
  }

  setSnapshot(snapshot: PlaybackSnapshot): void {
    this.snapshot = snapshot;
  }

  dispose(): void {
    this.disposeCount += 1;
    this.disposeHandler?.();
  }
}

function readySnapshot(
  snapshot: PlaybackSnapshot,
  command: PlaybackCommandContext,
): PlaybackSnapshot {
  return {
    ...snapshot,
    error: null,
    requestId: command.requestId,
    sourceEpoch: command.sourceEpoch,
    sourceId: command.sourceId,
    state: "ready",
  };
}

function defaultSeekResult(request: SeekRequest): SeekResult {
  return {
    clamped: request.requestedTime !== request.targetTime,
    confirmedTime: request.targetTime,
    requestId: request.requestId,
    requestedTime: request.requestedTime,
    status: "completed",
    targetTime: request.targetTime,
  };
}
