export type PlaybackState =
  | 'idle'
  | 'loading'
  | 'ready'
  | 'playing'
  | 'paused'
  | 'seeking'
  | 'ended'
  | 'error'
  | 'disposed';

export type PlaybackCompletionStatus = 'completed' | 'superseded' | 'canceled' | 'unsupported' | 'failed';
export type PlaybackErrorCategory = 'network' | 'media' | 'decode' | 'unsupported' | 'unknown';
export type CapabilityAvailability = 'available' | 'unavailable';
export type FramePresentationCapability = 'exact' | 'approximate' | 'unavailable';
export type PlaybackSourceMode = 'direct' | 'stream' | 'adaptive' | 'live';
export type SeekReason = 'user' | 'step' | 'tier' | 'restore' | 'ab_loop';
export type SeekBoundaryPolicy = 'clamp';
export type SeekTier =
  | { readonly count: 1; readonly kind: 'frame' }
  | { readonly kind: 'seconds'; readonly value: 0.5 | 1 | 5 | 30 | 60 };
export type FrameStepDirection = 'next' | 'previous';
export type FrameStepPrecision = 'exact-verified' | 'approximate' | 'unsupported';

export interface TimeRange {
  readonly end: number;
  readonly start: number;
}

export interface PlaybackSourceMetadata {
  readonly duration?: number;
  readonly isLive?: boolean;
  readonly title?: string;
}

export interface PlaybackSource {
  readonly id: string;
  readonly metadata?: PlaybackSourceMetadata;
  readonly mode: PlaybackSourceMode;
  readonly payload?: unknown;
}

export interface PlaybackCapabilities {
  readonly framePresentation: FramePresentationCapability;
  readonly loadControl: CapabilityAvailability;
  readonly preview: CapabilityAvailability;
  readonly quality: CapabilityAvailability;
  readonly seek: CapabilityAvailability;
  readonly tracks?: CapabilityAvailability;
}

export interface PlaybackError {
  readonly category: PlaybackErrorCategory;
  readonly code?: string;
  readonly message: string;
}

export interface PlaybackSnapshot {
  readonly buffered: readonly TimeRange[];
  readonly capabilities: PlaybackCapabilities;
  readonly currentTime: number;
  readonly duration: number;
  readonly error: PlaybackError | null;
  readonly playbackRate: number;
  readonly requestId: number;
  readonly seekable: readonly TimeRange[];
  readonly sourceEpoch: number;
  readonly sourceId: string | null;
  readonly state: PlaybackState;
}

export interface PlaybackCommandContext {
  readonly requestId: number;
  readonly sourceEpoch: number;
  readonly sourceId: string;
}

export interface PlaybackCommandResult {
  readonly error?: PlaybackError;
  readonly requestId: number;
  readonly status: PlaybackCompletionStatus;
}

export interface SeekRequest extends PlaybackCommandContext {
  readonly boundaryPolicy: SeekBoundaryPolicy;
  readonly reason: SeekReason;
  readonly requestedTime: number;
  readonly targetTime: number;
}

export interface SeekResult extends PlaybackCommandResult {
  readonly clamped: boolean;
  readonly confirmedTime: number;
  readonly requestedTime: number;
  readonly targetTime: number;
}

interface PlaybackBackendEventContext {
  readonly eventId: number;
  readonly requestId: number;
  readonly sourceEpoch: number;
  readonly sourceId: string;
}

export type PlaybackBackendEvent = PlaybackBackendEventContext &
  (
    | { readonly snapshot: PlaybackSnapshot; readonly type: 'snapshotChanged' }
    | { readonly capabilities: PlaybackCapabilities; readonly type: 'capabilitiesChanged' }
    | { readonly type: 'ended' }
    | { readonly error: PlaybackError; readonly type: 'error' }
  );

export type PlaybackBackendListener = (event: PlaybackBackendEvent) => void;

export interface PlaybackBackend {
  load(source: PlaybackSource, command: PlaybackCommandContext): Promise<void>;
  play(command: PlaybackCommandContext): Promise<void>;
  pause(command: PlaybackCommandContext): Promise<void>;
  seek(request: SeekRequest): Promise<SeekResult>;
  getSnapshot(): PlaybackSnapshot;
  subscribe(listener: PlaybackBackendListener): () => void;
  dispose(): void;
}

export interface PresentedFrame {
  readonly mediaTime: number;
  readonly presentationSequence: number;
  readonly sampleSource: 'video-frame-callback' | 'backend';
  readonly sourceEpoch: number;
  readonly sourceFrameIndex?: number;
  readonly sourceId: string;
  readonly stableFrameId?: string;
}

export interface AdjacentFrameTarget {
  readonly frameDuration: number;
  readonly mediaTime: number;
  readonly sourceFrameIndex?: number;
  readonly stableFrameId?: string;
}

export interface FrameStepResult extends PlaybackCommandResult {
  readonly clamped: boolean;
  readonly confirmedMediaTime: number;
  readonly confirmedSourceFrameIndex?: number;
  readonly confirmedStableFrameId?: string;
  readonly correctionCount: number;
  readonly direction: FrameStepDirection;
  readonly frameDuration: number | null;
  readonly precision: FrameStepPrecision;
  readonly startMediaTime: number;
  readonly startSourceFrameIndex?: number;
  readonly startStableFrameId?: string;
  readonly targetMediaTime: number;
  readonly targetSourceFrameIndex?: number;
  readonly targetStableFrameId?: string;
  readonly timestampError: number | null;
}

export type TierSeekResult = PlaybackCommandResult | SeekResult | FrameStepResult;

export interface FramePresentationFacet {
  getCurrentPresentedFrame(command: PlaybackCommandContext): PresentedFrame | null;
  getAdjacentFrameTarget(
    current: PresentedFrame,
    direction: FrameStepDirection,
    command: PlaybackCommandContext,
  ): AdjacentFrameTarget | null;
  getNominalFrameDuration(command: PlaybackCommandContext): number | null;
  waitForPresentedFrame(command: PlaybackCommandContext): Promise<PresentedFrame>;
}

export interface PreviewSprite {
  readonly assetId: string;
  readonly height: number;
  readonly width: number;
  readonly x: number;
  readonly y: number;
}

export interface PreparedPreviewCue {
  readonly endTime: number;
  readonly sprite: PreviewSprite;
  readonly startTime: number;
}

export interface PreparedPreviewTrack {
  readonly cues: readonly PreparedPreviewCue[];
  readonly generationId: string;
  readonly mediaId: string;
  readonly profileId: string;
  readonly sourceFingerprint: string;
}

export interface PreviewHit extends PreparedPreviewCue {
  readonly generationId: string;
  readonly profileId: string;
}

export interface PreviewTrackState {
  readonly generationId: string | null;
  readonly mediaId: string | null;
  readonly profileId: string | null;
  readonly requestId: number;
  readonly sourceEpoch: number;
  readonly sourceId: string | null;
  readonly status: 'empty' | 'ready';
}

export interface PreviewFacet {
  setTrack(track: PreparedPreviewTrack | null, command: PlaybackCommandContext): PreviewTrackState;
  hitTest(mediaTime: number, command: PlaybackCommandContext): PreviewHit | null;
  getState(): PreviewTrackState;
}

export type TrackKind = 'audio' | 'subtitle';
export type TrackSource = 'sidecar' | 'uploaded' | 'embedded' | 'derived';
export type TrackSwitchCapability = 'seamless' | 'reload' | 'unsupported';

export interface PlaybackTrack {
  readonly available?: boolean;
  readonly capability?: TrackSwitchCapability;
  readonly codec?: string;
  readonly default?: boolean;
  readonly forced?: boolean;
  readonly format?: string;
  readonly id: string;
  readonly kind: TrackKind;
  readonly label: string;
  readonly language?: string;
  readonly source?: TrackSource;
  readonly streamIndex?: number;
  readonly title?: string;
  readonly unsupportedReason?: string;
}

export interface TrackSelectionState {
  readonly effectiveTrackId: string | null;
  readonly kind: TrackKind;
  /** 后端确认选择命令时写入对应 command.requestId；旧实现缺省按 0 处理。 */
  readonly requestId?: number;
  readonly selectedTrackId: string | null;
  readonly sourceEpoch: number;
  readonly sourceId: string;
}

export interface TrackFacet {
  getTracks(kind: TrackKind): readonly PlaybackTrack[];
  getSelectionState(kind: TrackKind): TrackSelectionState;
  selectTrack(kind: TrackKind, trackId: string | null, command: PlaybackCommandContext): Promise<void>;
}

export interface TrackSelectionResult extends PlaybackCommandResult {
  readonly effectiveTrackId: string | null;
  readonly kind: TrackKind;
  readonly selectedTrackId: string | null;
}

export interface PlaybackQuality {
  readonly bandwidth?: number;
  readonly height?: number;
  readonly id: string;
  readonly label: string;
}

export interface QualityTarget {
  readonly bandwidth?: number;
  readonly height: number;
}

export type QualitySelection =
  | { readonly mode: 'auto' }
  | { readonly mode: 'manual'; readonly quality: QualityTarget };

export interface QualityFacetState {
  readonly actualQualityId: string | null;
  readonly playbackRate: number;
  readonly qualities: readonly PlaybackQuality[];
  readonly selection: QualitySelection;
}

export type QualityFacetListener = (state: QualityFacetState, command: PlaybackCommandContext) => void;

export interface QualityFacet {
  getState(): QualityFacetState;
  selectQuality(selection: QualitySelection, command: PlaybackCommandContext): Promise<void>;
  setAutoQualityCap(maxHeight: number | null, command: PlaybackCommandContext): Promise<void>;
  setPlaybackRate(rate: number, command: PlaybackCommandContext): Promise<void>;
  subscribe(listener: QualityFacetListener): () => void;
}

export interface LoadControlFacet {
  getLoadingState(): 'loading' | 'stopped';
  startLoading(command: PlaybackCommandContext): Promise<void>;
  stopLoading(command: PlaybackCommandContext): Promise<void>;
}

export interface PlaybackQualityState {
  readonly actualQuality: PlaybackQuality | null;
  readonly dataSaver: boolean;
  readonly dataSaverBlocked: boolean;
  readonly manualQuality: PlaybackQuality | null;
  readonly playbackRate: number;
  readonly qualityMode: 'auto' | 'manual';
  readonly qualities: readonly PlaybackQuality[];
}

export interface AbLoopState {
  readonly a: number | null;
  readonly b: number | null;
  readonly enabled: boolean;
}

export interface PlaybackBackendBinding {
  readonly backend: PlaybackBackend;
  readonly initialSeekTier?: SeekTier;
  readonly facets?: {
    readonly framePresentation?: FramePresentationFacet;
    readonly loadControl?: LoadControlFacet;
    readonly preview?: PreviewFacet;
    readonly quality?: QualityFacet;
    readonly tracks?: TrackFacet;
  };
}

interface PlaybackEventContext {
  readonly requestId: number;
}

export type PlaybackEvent = PlaybackEventContext &
  (
    | { readonly snapshot: PlaybackSnapshot; readonly type: 'snapshotChanged' }
    | {
        readonly capabilities: PlaybackCapabilities;
        readonly eventId: number;
        readonly sourceEpoch: number;
        readonly sourceId: string;
        readonly type: 'capabilitiesChanged';
      }
    | {
        readonly result: PlaybackCommandResult | SeekResult | FrameStepResult | TrackSelectionResult;
        readonly snapshot?: PlaybackSnapshot;
        readonly sourceEpoch: number;
        readonly sourceId: string | null;
        readonly type: 'commandCompleted';
      }
    | {
        readonly result: FrameStepResult;
        readonly snapshot?: PlaybackSnapshot;
        readonly sourceEpoch: number;
        readonly sourceId: string | null;
        readonly type: 'frameStepCompleted';
      }
    | {
        readonly effectiveTrackId: string | null;
        readonly kind: TrackKind;
        readonly selectedTrackId: string | null;
        readonly sourceEpoch: number;
        readonly sourceId: string;
        readonly type: 'trackSelectionChanged';
      }
    | {
        readonly result: TrackSelectionResult;
        readonly snapshot?: PlaybackSnapshot;
        readonly sourceEpoch: number;
        readonly sourceId: string | null;
        readonly type: 'trackSelectionCompleted';
      }
    | {
        readonly previousTier: SeekTier | null;
        readonly sourceEpoch: number;
        readonly sourceId: string | null;
        readonly tier: SeekTier;
        readonly type: 'seekTierChanged';
      }
    | {
        readonly state: PlaybackQualityState;
        readonly sourceEpoch: number;
        readonly sourceId: string | null;
        readonly type: 'qualityStateChanged';
      }
    | {
        readonly state: AbLoopState;
        readonly sourceEpoch: number;
        readonly sourceId: string | null;
        readonly type: 'abLoopChanged';
      }
    | {
        readonly error: PlaybackError;
        readonly eventId: number;
        readonly sourceEpoch: number;
        readonly sourceId: string;
        readonly type: 'error';
      }
  );

export type PlaybackListener = (event: PlaybackEvent) => void;
