import {
  FrameStepController,
  type FrameCommandIdentity,
  type FrameOperationOutcome,
} from "./frame-step-controller";
import {
  canonicalSeekTier,
  clampToRanges,
  directionSign,
  isSeekTier,
  normalizeRanges,
} from "./seek-algorithms";
import {
  DATA_SAVER_MAX_HEIGHT,
  createAbLoopState,
  createQualityState,
  highestDataSaverQuality,
  isPlaybackRate,
  normalizeQualities,
  qualityById,
  qualityTarget,
  resolveQuality,
  validLoopPoint,
  validLoopRange,
} from "./quality-loop-state";
import type {
  AbLoopState,
  FrameStepDirection,
  FrameStepResult,
  LoadControlFacet,
  PlaybackBackendBinding,
  PlaybackBackendEvent,
  PlaybackCapabilities,
  PlaybackCommandContext,
  PlaybackCommandResult,
  PlaybackCompletionStatus,
  PlaybackError,
  PlaybackErrorCategory,
  PlaybackEvent,
  PlaybackListener,
  PlaybackQuality,
  PlaybackQualityState,
  PlaybackSnapshot,
  PlaybackSource,
  PlaybackState,
  PlaybackTrack,
  PreparedPreviewTrack,
  PreviewFacet,
  PreviewHit,
  PreviewTrackState,
  QualityFacet,
  QualityFacetState,
  QualitySelection,
  SeekReason,
  SeekRequest,
  SeekResult,
  SeekTier,
  TierSeekResult,
  TrackFacet,
  TrackKind,
  TrackSelectionResult,
  TrackSelectionState,
} from "./types";

const EMPTY_CAPABILITIES: PlaybackSnapshot["capabilities"] = {
  framePresentation: "unavailable",
  loadControl: "unavailable",
  preview: "unavailable",
  quality: "unavailable",
  seek: "unavailable",
  tracks: "unavailable",
};

interface CommandIdentity {
  readonly requestId: number;
  readonly sourceEpoch: number;
  readonly sourceId: string | null;
}

interface PendingCommand {
  readonly promise: Promise<PlaybackCompletionStatus>;
  isSettled(): boolean;
  settle(status: PlaybackCompletionStatus): void;
}

type TerminalSignal =
  | {
      readonly requestId: number;
      readonly revision: number;
      readonly sourceEpoch: number;
      readonly sourceId: string;
      readonly type: "ended";
    }
  | {
      readonly error: PlaybackError;
      readonly requestId: number;
      readonly revision: number;
      readonly sourceEpoch: number;
      readonly sourceId: string;
      readonly type: "error";
    };

type OperationOutcome<T> =
  | { readonly kind: "completed"; readonly value: T }
  | { readonly error: unknown; readonly kind: "failed" }
  | { readonly kind: "controlled"; readonly status: PlaybackCompletionStatus };

type SynchronousOutcome<T> =
  | { readonly kind: "completed"; readonly value: T }
  | { readonly error: unknown; readonly kind: "failed" };

type VersionedTrackSelectionState = Omit<TrackSelectionState, "requestId"> & {
  readonly requestId: number;
};

type TrackSelectionReadOutcome =
  | { readonly kind: "completed"; readonly value: VersionedTrackSelectionState }
  | { readonly error: unknown; readonly kind: "failed" };

type SeekResultBase = Omit<SeekResult, "confirmedTime" | "error" | "status">;

export class PlaybackBackendError extends Error {
  readonly category: PlaybackErrorCategory;
  readonly code?: string;

  constructor(category: PlaybackErrorCategory, message: string, code?: string) {
    super(message);
    this.name = "PlaybackBackendError";
    this.category = category;
    if (code !== undefined) {
      this.code = code;
    }
  }
}

export class PlaybackCore {
  private readonly backend: PlaybackBackendBinding["backend"];
  private readonly frameStepController: FrameStepController;
  private readonly listeners = new Set<PlaybackListener>();
  private readonly completionSnapshots = new Map<number, PlaybackSnapshot>();
  private readonly pending = new Map<number, PendingCommand>();
  private readonly previewFacet: PreviewFacet | undefined;
  private readonly qualityFacet: QualityFacet | undefined;
  private readonly loadControlFacet: LoadControlFacet | undefined;
  private readonly tracksFacet: TrackFacet | undefined;
  private readonly trackSelectionRequestIds = new Map<
    TrackKind,
    VersionedTrackSelectionState
  >();
  private readonly trackSelections = new Map<
    TrackKind,
    VersionedTrackSelectionState
  >();
  private readonly trackSelectionIntents = new Map<
    TrackKind,
    { readonly requestId: number; readonly state: VersionedTrackSelectionState }
  >();
  private readonly unsubscribeBackend: () => void;
  private readonly unsubscribeQuality: () => void;
  private abLoopSeekPending = false;
  private abLoopState: AbLoopState = createAbLoopState();
  private blockedPlaybackIntent: "paused" | "playing" = "paused";
  private disposed = false;
  private frameInterruptRequestId = 0;
  private lastFrameStepResult: FrameStepResult | null = null;
  private latestBackendRequestId = 0;
  private latestCommandRequestId = 0;
  private latestEventId = -1;
  private nextRequestId = 0;
  private nextSourceEpoch = 0;
  private publishVersion = 0;
  private qualityMutationPending = false;
  private qualityReconcilePending = false;
  private qualityState: PlaybackQualityState = createQualityState();
  private seekResumeState: PlaybackState = "ready";
  private seekTier: SeekTier | null = null;
  private snapshot: PlaybackSnapshot = createInitialSnapshot();
  private terminalRevision = 0;
  private terminalSignal: TerminalSignal | null = null;

  constructor(binding: PlaybackBackendBinding) {
    this.backend = binding.backend;
    this.previewFacet = binding.facets?.preview;
    this.qualityFacet = binding.facets?.quality;
    this.loadControlFacet = binding.facets?.loadControl;
    this.tracksFacet = binding.facets?.tracks;
    this.seekTier = isSeekTier(binding.initialSeekTier)
      ? canonicalSeekTier(binding.initialSeekTier)
      : null;
    this.frameStepController = new FrameStepController(
      this.backend,
      binding.facets?.framePresentation,
      {
        allocateRequestId: () => this.allocateRequestId(),
        applyFrameResult: (command, result) => {
          this.applyFrameResult(command, result);
        },
        beginFrameCommand: (command) => {
          this.acceptCommand(command.requestId, false);
        },
        getIdentity: (requestId) => this.identity(requestId),
        getInterruptionStatus: (identity) =>
          this.frameInterruptionStatus(identity),
        getSnapshot: () => this.snapshot,
        normalizeError,
        publishFrameResult: (result, identity) =>
          this.publishFrameResult(result, identity),
        runOperation: <T>(
          requestId: number,
          operation: () => Promise<T>,
        ): Promise<FrameOperationOutcome<T>> =>
          this.runOperation(requestId, operation),
      },
    );
    this.unsubscribeBackend = this.backend.subscribe((event) => {
      this.handleBackendEvent(event);
    });
    this.unsubscribeQuality =
      this.qualityFacet?.subscribe((state, command) => {
        this.handleQualityFacetState(state, command);
      }) ?? (() => undefined);
  }

  async load(source: PlaybackSource): Promise<PlaybackCommandResult> {
    const requestId = this.allocateRequestId();
    this.interruptFrameCommands(requestId);
    this.lastFrameStepResult = null;
    if (this.disposed) {
      return this.publishCommandResult(
        this.result(requestId, "canceled"),
        this.identity(requestId),
      );
    }
    const sourceChanged =
      this.snapshot.sourceId !== null && this.snapshot.sourceId !== source.id;
    const playbackRate = sourceChanged ? 1 : this.qualityState.playbackRate;
    const command = this.createLoadCommand(source, requestId);
    const startedTerminalRevision = this.terminalRevision;
    this.acceptCommand(requestId);
    this.preparePlaybackFeaturesForLoad(sourceChanged);
    this.setPreviewTrack(null, command);
    this.latestEventId = -1;
    const operation = this.runOperation(requestId, () =>
      this.backend.load(source, command),
    );
    this.updateSnapshot(loadingSnapshot(this.snapshot, source, command));
    const result = this.finishLoad(
      command,
      startedTerminalRevision,
      await operation,
    );
    if (result.status === "completed")
      await this.restoreQualityAfterLoad(command, playbackRate);
    return result;
  }

  async play(): Promise<PlaybackCommandResult> {
    if (this.qualityState.dataSaverBlocked) {
      const requestId = this.allocateRequestId();
      const identity = this.identity(requestId);
      const error = {
        category: "unsupported" as const,
        message: "当前视频无 480p 或更低档位，请关闭省流量后播放",
      };
      return this.publishCommandResult(
        this.result(requestId, "unsupported", error),
        identity,
      );
    }
    return this.runStateCommand("playing", async (command) => {
      if (this.qualityState.dataSaver) await this.startLoading(command);
      if (this.isCurrentCommand(command)) await this.backend.play(command);
    });
  }

  async pause(): Promise<PlaybackCommandResult> {
    const result = await this.runStateCommand("paused", (command) =>
      this.backend.pause(command),
    );
    if (result.status === "completed" && this.qualityState.dataSaver)
      await this.stopLoadingForCurrentSource();
    return result;
  }

  async stop(): Promise<PlaybackCommandResult> {
    const requestId = this.allocateRequestId();
    const identity = this.identity(requestId);
    this.interruptFrameCommands(requestId);
    const rejected = this.rejectStateCommand(identity);
    if (rejected !== null) return rejected;
    const command = this.requireCommand(identity);
    const snapshot = this.snapshot;
    const targetTime = stopTarget(snapshot);
    this.acceptCommand(requestId);
    if (isStoppedSnapshot(snapshot, targetTime)) {
      if (this.qualityState.dataSaver)
        await this.stopLoadingForCommand(command);
      return this.completeStop(command, targetTime);
    }
    const startedTerminalRevision = this.terminalRevision;
    const operation = this.runOperation(requestId, () =>
      this.executeStop(command, snapshot, targetTime),
    );
    return this.finishStop(
      command,
      startedTerminalRevision,
      targetTime,
      await operation,
    );
  }

  async seek(
    requestedTime: number,
    reason: SeekReason = "user",
  ): Promise<SeekResult> {
    const requestId = this.allocateRequestId();
    const identity = this.identity(requestId);
    this.interruptFrameCommands(requestId);
    const result = await this.performSeek(requestedTime, reason, identity);
    this.publishSeekCompleted(result, reason, identity);
    if (reason !== "ab_loop" && result.status === "completed")
      await this.enforceAbLoop(result.confirmedTime);
    return result;
  }

  async seekBy(
    offset: number,
    reason: SeekReason = "user",
  ): Promise<SeekResult> {
    const requestId = this.allocateRequestId();
    const identity = this.identity(requestId);
    this.interruptFrameCommands(requestId);
    const fallbackRequestedTime = this.snapshot.currentTime + offset;
    const rejected = this.rejectSeek(fallbackRequestedTime, identity);
    if (rejected !== null)
      return this.completeSeekEvent(rejected, reason, identity);
    const snapshotOutcome = invokeSynchronously(() =>
      this.backend.getSnapshot(),
    );
    if (snapshotOutcome.kind === "failed") {
      return this.completeSeekEvent(
        this.failSeekSnapshotRead(
          fallbackRequestedTime,
          identity,
          snapshotOutcome.error,
        ),
        reason,
        identity,
      );
    }
    const backendSnapshot = snapshotOutcome.value;
    const requestedTime = backendSnapshot.currentTime + offset;
    if (
      !sameCommandSource(identity, backendSnapshot) ||
      !sameCommandSource(identity, this.snapshot)
    ) {
      const result = this.publishSeekResult(
        this.basicSeekResult(requestedTime, requestId, "superseded"),
        identity,
      );
      return this.completeSeekEvent(result, reason, identity);
    }
    if (!Number.isFinite(requestedTime)) {
      return this.completeSeekEvent(
        this.unsupportedSeek(requestedTime, identity),
        reason,
        identity,
      );
    }
    const result = await this.performSeekFromSnapshot(
      requestedTime,
      reason,
      identity,
      backendSnapshot,
    );
    this.publishSeekCompleted(result, reason, identity);
    if (reason !== "ab_loop" && result.status === "completed")
      await this.enforceAbLoop(result.confirmedTime);
    return result;
  }

  stepFrame(direction: FrameStepDirection): Promise<FrameStepResult> {
    return this.frameStepController.step(direction);
  }

  async seekByTier(direction: FrameStepDirection): Promise<TierSeekResult> {
    const tier = this.seekTier;
    if (tier === null) {
      return this.unsupportedTierCommand();
    }
    if (tier.kind === "frame") {
      return this.stepFrame(direction);
    }
    return this.seekBySeconds(direction, tier.value);
  }

  setSeekTier(tier: SeekTier): Promise<PlaybackCommandResult> {
    const requestId = this.allocateRequestId();
    const identity = this.identity(requestId);
    this.interruptFrameCommands(requestId);
    if (this.disposed) {
      return Promise.resolve(
        this.publishCommandResult(this.result(requestId, "canceled"), identity),
      );
    }
    if (!isSeekTier(tier)) {
      return Promise.resolve(
        this.publishCommandResult(
          this.result(requestId, "unsupported"),
          identity,
        ),
      );
    }
    this.acceptCommand(requestId, false);
    this.exitSeeking(requestId);
    this.applySeekTier(canonicalSeekTier(tier), identity);
    return Promise.resolve(
      this.publishCommandResult(this.result(requestId, "completed"), identity),
    );
  }

  getSeekTier(): SeekTier | null {
    return this.seekTier;
  }

  getLastFrameStepResult(): FrameStepResult | null {
    return this.lastFrameStepResult;
  }

  getSnapshot(): PlaybackSnapshot {
    return this.snapshot;
  }

  setPreviewTrack(
    track: PreparedPreviewTrack | null,
    command: PlaybackCommandContext = this.currentPreviewCommand(),
  ): PreviewTrackState | null {
    if (this.previewFacet === undefined || command.sourceId.length === 0) {
      return null;
    }
    return this.previewFacet.setTrack(track, command);
  }

  getPreviewState(): PreviewTrackState | null {
    return this.previewFacet?.getState() ?? null;
  }

  hitTestPreview(
    mediaTime: number,
    command: PlaybackCommandContext = this.currentPreviewCommand(),
  ): PreviewHit | null {
    return this.previewFacet?.hitTest(mediaTime, command) ?? null;
  }

  getTracks(kind: TrackKind): readonly PlaybackTrack[] {
    if (!isTrackKind(kind) || this.tracksFacet === undefined) {
      return [];
    }
    const outcome = invokeSynchronously(
      () => this.tracksFacet?.getTracks(kind) ?? [],
    );
    return outcome.kind === "completed" ? outcome.value : [];
  }

  getTrackSelection(kind: TrackKind): TrackSelectionState | null {
    if (!isTrackKind(kind) || this.tracksFacet === undefined) {
      return null;
    }
    const outcome = this.readTrackSelection(kind);
    return outcome.kind === "completed"
      ? this.currentTrackSelection(kind, outcome.value)
      : null;
  }

  selectTrack(
    kind: TrackKind,
    trackId: string | null,
  ): Promise<TrackSelectionResult>;
  async selectTrack(
    kind: unknown,
    trackId: string | null,
  ): Promise<PlaybackCommandResult | TrackSelectionResult> {
    const requestId = this.allocateRequestId();
    const identity = this.identity(requestId);
    if (!isTrackKind(kind)) {
      return this.result(requestId, "unsupported");
    }
    const rejected = this.rejectTrackSelection(kind, trackId, identity);
    if (rejected !== null) {
      return rejected;
    }
    const command = this.requireCommand(identity);
    const previous = this.readTrackSelection(kind);
    if (previous.kind === "failed") {
      return this.finishTrackReadFailure(kind, identity, previous.error);
    }
    return this.submitTrackSelection(kind, trackId, command, previous.value);
  }

  getQualities(): readonly PlaybackQuality[] {
    return this.qualityState.qualities;
  }

  getQualityState(): PlaybackQualityState {
    return this.qualityState;
  }

  async selectQuality(
    selection: QualitySelection,
  ): Promise<PlaybackCommandResult> {
    const command = this.beginFeatureCommand();
    if (
      command === null ||
      this.qualityFacet === undefined ||
      this.snapshot.capabilities.quality !== "available"
    ) {
      return this.unsupportedFeatureCommand(command);
    }
    this.qualityMutationPending = true;
    try {
      if (selection.mode === "auto")
        return await this.selectAutoQuality(command);
      const target = resolveQuality(
        this.qualityState.qualities,
        selection.quality,
      );
      if (target === null)
        return this.unsupportedFeatureCommand(command, "目标清晰度当前不可用");
      return await this.selectManualQuality(target, command);
    } finally {
      this.qualityMutationPending = false;
    }
  }

  async setDataSaver(enabled: boolean): Promise<PlaybackCommandResult> {
    const command = this.beginFeatureCommand();
    if (
      command === null ||
      this.qualityFacet === undefined ||
      this.snapshot.capabilities.quality !== "available"
    ) {
      return this.unsupportedFeatureCommand(command);
    }
    this.qualityMutationPending = true;
    try {
      return enabled
        ? await this.enableDataSaver(command)
        : await this.disableDataSaver(command);
    } finally {
      this.qualityMutationPending = false;
    }
  }

  async setPlaybackRate(rate: number): Promise<PlaybackCommandResult> {
    const command = this.beginFeatureCommand();
    const qualityFacet = this.qualityFacet;
    if (
      command === null ||
      qualityFacet === undefined ||
      !isPlaybackRate(rate)
    ) {
      return this.unsupportedFeatureCommand(
        command,
        "当前播放路径不支持该倍速",
      );
    }
    const outcome = await this.runFeatureOperation(command, () =>
      qualityFacet.setPlaybackRate(rate, command),
    );
    if (outcome.status !== "completed") {
      this.applyQualityState(
        { ...this.qualityState, playbackRate: 1 },
        command.requestId,
      );
      return outcome;
    }
    this.applyQualityState(
      { ...this.qualityState, playbackRate: rate },
      command.requestId,
    );
    return outcome;
  }

  getAbLoopState(): AbLoopState {
    return this.abLoopState;
  }

  setAbLoopA(): Promise<PlaybackCommandResult> {
    const command = this.beginFeatureCommand();
    if (command === null)
      return Promise.resolve(this.unsupportedFeatureCommand(command));
    const snapshot = this.readBackendSnapshot();
    if (
      snapshot === null ||
      !validLoopPoint(snapshot.currentTime, snapshot.duration)
    ) {
      return Promise.resolve(
        this.unsupportedFeatureCommand(command, "当前位置或媒体时长无效"),
      );
    }
    this.applyAbLoopState(
      { a: snapshot.currentTime, b: null, enabled: false },
      command.requestId,
    );
    return Promise.resolve(
      this.publishCommandResult(
        this.result(command.requestId, "completed"),
        command,
      ),
    );
  }

  setAbLoopB(): Promise<PlaybackCommandResult> {
    const command = this.beginFeatureCommand();
    if (command === null)
      return Promise.resolve(this.unsupportedFeatureCommand(command));
    const snapshot = this.readBackendSnapshot();
    const a = this.abLoopState.a;
    if (
      snapshot === null ||
      a === null ||
      !validLoopRange(a, snapshot.currentTime, snapshot.duration)
    ) {
      return Promise.resolve(
        this.unsupportedFeatureCommand(
          command,
          "B 点必须晚于 A 点至少 0.5 秒且位于媒体时长内",
        ),
      );
    }
    this.applyAbLoopState(
      { a, b: snapshot.currentTime, enabled: true },
      command.requestId,
    );
    return Promise.resolve(
      this.publishCommandResult(
        this.result(command.requestId, "completed"),
        command,
      ),
    );
  }

  clearAbLoop(): Promise<PlaybackCommandResult> {
    const command = this.beginFeatureCommand();
    if (command === null)
      return Promise.resolve(this.unsupportedFeatureCommand(command));
    this.applyAbLoopState(createAbLoopState(), command.requestId);
    return Promise.resolve(
      this.publishCommandResult(
        this.result(command.requestId, "completed"),
        command,
      ),
    );
  }

  subscribe(listener: PlaybackListener): () => void {
    if (this.disposed) {
      safelyInvoke(() => {
        listener({
          requestId: this.snapshot.requestId,
          snapshot: this.snapshot,
          type: "snapshotChanged",
        });
      });
      return () => undefined;
    }
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  dispose(): void {
    if (this.disposed) {
      return;
    }
    this.disposed = true;
    const requestId = this.allocateRequestId();
    this.interruptFrameCommands(requestId);
    this.latestCommandRequestId = requestId;
    this.settleAll("canceled");
    safelyInvoke(this.unsubscribeBackend);
    safelyInvoke(this.unsubscribeQuality);
    this.updateSnapshot({
      ...this.snapshot,
      error: null,
      requestId,
      state: "disposed",
    });
    this.listeners.clear();
    safelyInvoke(() => {
      this.backend.dispose();
    });
  }

  private preparePlaybackFeaturesForLoad(sourceChanged: boolean): void {
    if (sourceChanged) {
      this.abLoopState = createAbLoopState();
      this.qualityState = { ...this.qualityState, playbackRate: 1 };
    }
    this.abLoopSeekPending = false;
    this.blockedPlaybackIntent = "paused";
    this.qualityState = {
      ...this.qualityState,
      actualQuality: null,
      dataSaverBlocked: false,
      qualities: [],
    };
  }

  private async restoreQualityAfterLoad(
    command: PlaybackCommandContext,
    playbackRate: number,
  ): Promise<void> {
    const qualityFacet = this.qualityFacet;
    if (qualityFacet === undefined || !this.isCurrentSource(command)) return;
    const state = this.readQualityFacetState();
    if (state !== null) this.acceptQualityFacetState(state, command);
    const rateError = await this.invokeFeatureOperation(() =>
      qualityFacet.setPlaybackRate(playbackRate, command),
    );
    if (!this.isCurrentSource(command)) return;
    this.applyQualityState(
      {
        ...this.qualityState,
        playbackRate: rateError === null ? playbackRate : 1,
      },
      command.requestId,
    );
    if (this.snapshot.capabilities.quality === "available")
      await this.reconcileQualityState(command);
  }

  private beginFeatureCommand(): PlaybackCommandContext | null {
    const requestId = this.allocateRequestId();
    const identity = this.identity(requestId);
    if (
      this.disposed ||
      !isCommandState(this.snapshot.state) ||
      identity.sourceId === null
    )
      return null;
    const command = this.requireCommand(identity);
    this.interruptFrameCommands(requestId);
    this.acceptCommand(requestId, false);
    this.exitSeeking(requestId);
    return command;
  }

  private unsupportedFeatureCommand(
    command: PlaybackCommandContext | null,
    message?: string,
  ): PlaybackCommandResult {
    const identity = command ?? this.identity(this.allocateRequestId());
    const error =
      message === undefined
        ? undefined
        : { category: "unsupported" as const, message };
    return this.publishCommandResult(
      this.result(
        identity.requestId,
        this.disposed ? "canceled" : "unsupported",
        error,
      ),
      identity,
    );
  }

  private async selectAutoQuality(
    command: PlaybackCommandContext,
  ): Promise<PlaybackCommandResult> {
    const qualityFacet = this.qualityFacet;
    if (qualityFacet === undefined)
      return this.unsupportedFeatureCommand(command);
    const error = await this.invokeFeatureOperation(() =>
      qualityFacet.selectQuality({ mode: "auto" }, command),
    );
    if (error !== null) return this.featureFailure(command, error);
    const cap = this.qualityState.dataSaver ? DATA_SAVER_MAX_HEIGHT : null;
    const capError = await this.invokeFeatureOperation(() =>
      qualityFacet.setAutoQualityCap(cap, command),
    );
    if (capError !== null) return this.featureFailure(command, capError);
    this.applyQualityState(
      { ...this.qualityState, manualQuality: null, qualityMode: "auto" },
      command.requestId,
    );
    return this.publishCommandResult(
      this.result(command.requestId, "completed"),
      command,
    );
  }

  private async selectManualQuality(
    target: PlaybackQuality,
    command: PlaybackCommandContext,
  ): Promise<PlaybackCommandResult> {
    const qualityFacet = this.qualityFacet;
    if (qualityFacet === undefined)
      return this.unsupportedFeatureCommand(command);
    const targetSemantic = qualityTarget(target);
    if (targetSemantic === null)
      return this.unsupportedFeatureCommand(command, "目标清晰度缺少有效高度");
    const disablesSaver =
      this.qualityState.dataSaver &&
      targetSemantic.height > DATA_SAVER_MAX_HEIGHT;
    if (disablesSaver) {
      const capError = await this.invokeFeatureOperation(() =>
        qualityFacet.setAutoQualityCap(null, command),
      );
      if (capError !== null) return this.featureFailure(command, capError);
    }
    const error = await this.invokeFeatureOperation(() =>
      qualityFacet.selectQuality(
        { mode: "manual", quality: targetSemantic },
        command,
      ),
    );
    if (error !== null) return this.featureFailure(command, error);
    this.applyQualityState(
      {
        ...this.qualityState,
        dataSaver: disablesSaver ? false : this.qualityState.dataSaver,
        dataSaverBlocked: false,
        manualQuality: target,
        qualityMode: "manual",
      },
      command.requestId,
    );
    return this.publishCommandResult(
      this.result(command.requestId, "completed"),
      command,
    );
  }

  private async enableDataSaver(
    command: PlaybackCommandContext,
  ): Promise<PlaybackCommandResult> {
    const qualityFacet = this.qualityFacet;
    if (qualityFacet === undefined)
      return this.unsupportedFeatureCommand(command);
    const compatible = highestDataSaverQuality(this.qualityState.qualities);
    if (compatible === null) return this.blockDataSaver(command);
    if (
      this.qualityState.qualityMode === "manual" &&
      (this.qualityState.manualQuality?.height ?? 0) > DATA_SAVER_MAX_HEIGHT
    ) {
      const target = qualityTarget(compatible);
      if (target === null)
        return this.unsupportedFeatureCommand(
          command,
          "目标清晰度缺少有效高度",
        );
      const selectError = await this.invokeFeatureOperation(() =>
        qualityFacet.selectQuality(
          { mode: "manual", quality: target },
          command,
        ),
      );
      if (selectError !== null)
        return this.featureFailure(command, selectError);
    }
    const capError = await this.invokeFeatureOperation(() =>
      qualityFacet.setAutoQualityCap(DATA_SAVER_MAX_HEIGHT, command),
    );
    if (capError !== null) return this.featureFailure(command, capError);
    const manualQuality =
      this.qualityState.qualityMode === "manual" &&
      (this.qualityState.manualQuality?.height ?? 0) > DATA_SAVER_MAX_HEIGHT
        ? compatible
        : this.qualityState.manualQuality;
    this.applyQualityState(
      {
        ...this.qualityState,
        dataSaver: true,
        dataSaverBlocked: false,
        manualQuality,
      },
      command.requestId,
    );
    if (this.snapshot.state !== "playing")
      await this.invokeFeatureOperation(() => this.stopLoading(command));
    return this.publishCommandResult(
      this.result(command.requestId, "completed"),
      command,
    );
  }

  private async disableDataSaver(
    command: PlaybackCommandContext,
  ): Promise<PlaybackCommandResult> {
    const qualityFacet = this.qualityFacet;
    if (qualityFacet === undefined)
      return this.unsupportedFeatureCommand(command);
    const wasBlocked = this.qualityState.dataSaverBlocked;
    const capError = await this.invokeFeatureOperation(() =>
      qualityFacet.setAutoQualityCap(null, command),
    );
    if (capError !== null) return this.featureFailure(command, capError);
    this.applyQualityState(
      { ...this.qualityState, dataSaver: false, dataSaverBlocked: false },
      command.requestId,
    );
    if (wasBlocked && this.blockedPlaybackIntent === "playing") {
      const loadError = await this.invokeFeatureOperation(() =>
        this.startLoading(command),
      );
      if (loadError !== null) return this.featureFailure(command, loadError);
      const playError = await this.invokeFeatureBackendOperation(command, () =>
        this.backend.play(command),
      );
      if (playError !== null) return this.featureFailure(command, playError);
      this.updateSnapshot({
        ...this.snapshot,
        requestId: command.requestId,
        state: "playing",
      });
    }
    this.blockedPlaybackIntent = "paused";
    return this.publishCommandResult(
      this.result(command.requestId, "completed"),
      command,
    );
  }

  private async blockDataSaver(
    command: PlaybackCommandContext,
  ): Promise<PlaybackCommandResult> {
    const actual = this.readBackendSnapshot() ?? this.snapshot;
    // 核心已确认播放意图时也要强制 pause：后端快照可能短暂滞后。
    const shouldPause =
      this.snapshot.state === "playing" || actual.state === "playing";
    this.blockedPlaybackIntent = shouldPause
      ? "playing"
      : this.blockedPlaybackIntent;
    const pauseError = shouldPause
      ? await this.invokeFeatureBackendOperation(command, () =>
          this.backend.pause(command),
        )
      : null;
    const stopError = await this.invokeFeatureOperation(() =>
      this.stopLoading(command),
    );
    this.applyQualityState(
      { ...this.qualityState, dataSaver: true, dataSaverBlocked: true },
      command.requestId,
    );
    this.updateSnapshot({
      ...this.snapshot,
      requestId: command.requestId,
      state: "paused",
    });
    const error = pauseError ?? stopError;
    return error === null
      ? this.publishCommandResult(
          this.result(command.requestId, "completed"),
          command,
        )
      : this.featureFailure(command, error);
  }

  private async runFeatureOperation(
    command: PlaybackCommandContext,
    operation: () => Promise<void>,
  ): Promise<PlaybackCommandResult> {
    const error = await this.invokeFeatureOperation(operation);
    return error === null
      ? this.publishCommandResult(
          this.result(command.requestId, "completed"),
          command,
        )
      : this.featureFailure(command, error);
  }

  private invokeFeatureBackendOperation(
    command: PlaybackCommandContext,
    operation: () => Promise<void>,
  ): Promise<PlaybackError | null> {
    this.latestBackendRequestId = command.requestId;
    return this.invokeFeatureOperation(operation);
  }

  private async invokeFeatureOperation(
    operation: () => Promise<void>,
  ): Promise<PlaybackError | null> {
    try {
      await operation();
      return null;
    } catch (cause: unknown) {
      return normalizeError(cause);
    }
  }

  private featureFailure(
    command: PlaybackCommandContext,
    error: PlaybackError,
  ): PlaybackCommandResult {
    return this.publishCommandResult(
      this.result(command.requestId, completionStatus(error), error),
      command,
    );
  }

  private readQualityFacetState(): QualityFacetState | null {
    const qualityFacet = this.qualityFacet;
    if (qualityFacet === undefined) return null;
    const state = invokeSynchronously(() => qualityFacet.getState());
    return state.kind === "completed" ? state.value : null;
  }

  private handleQualityFacetState(
    state: QualityFacetState,
    command: PlaybackCommandContext,
  ): void {
    if (
      !this.isCurrentSource(command) ||
      command.requestId < this.snapshot.requestId
    )
      return;
    this.acceptQualityFacetState(state, command);
    if (!this.qualityMutationPending && !this.qualityReconcilePending)
      void this.reconcileQualityState(command);
  }

  private acceptQualityFacetState(
    state: QualityFacetState,
    command: PlaybackCommandContext,
  ): void {
    const qualities = normalizeQualities(state.qualities);
    this.applyQualityState(
      {
        ...this.qualityState,
        actualQuality: qualityById(qualities, state.actualQualityId),
        playbackRate: state.playbackRate,
        qualities,
      },
      command.requestId,
    );
  }

  private async reconcileQualityState(
    command: PlaybackCommandContext,
  ): Promise<void> {
    const qualityFacet = this.qualityFacet;
    if (
      this.qualityReconcilePending ||
      qualityFacet === undefined ||
      !this.isCurrentSource(command)
    )
      return;
    this.qualityReconcilePending = true;
    try {
      const maxHeight = this.qualityState.dataSaver
        ? DATA_SAVER_MAX_HEIGHT
        : null;
      const compatible = highestDataSaverQuality(this.qualityState.qualities);
      if (
        this.qualityState.dataSaver &&
        this.qualityState.qualities.length > 0 &&
        compatible === null
      ) {
        await this.blockDataSaver(command);
        return;
      }
      if (
        this.qualityState.dataSaver &&
        this.qualityState.dataSaverBlocked &&
        compatible !== null
      ) {
        this.applyQualityState(
          { ...this.qualityState, dataSaverBlocked: false },
          command.requestId,
        );
      }
      if (
        this.qualityState.qualityMode === "manual" &&
        this.qualityState.manualQuality !== null
      ) {
        const target = qualityTarget(this.qualityState.manualQuality);
        const matched =
          target === null
            ? null
            : resolveQuality(this.qualityState.qualities, target, maxHeight);
        const matchedTarget = matched === null ? null : qualityTarget(matched);
        if (matched !== null && matchedTarget !== null) {
          const error = await this.invokeFeatureOperation(() =>
            qualityFacet.selectQuality(
              { mode: "manual", quality: matchedTarget },
              command,
            ),
          );
          if (error !== null) return;
          this.applyQualityState(
            { ...this.qualityState, manualQuality: matched },
            command.requestId,
          );
        }
      }
      await this.invokeFeatureOperation(() =>
        qualityFacet.setAutoQualityCap(maxHeight, command),
      );
    } finally {
      this.qualityReconcilePending = false;
    }
  }

  private applyQualityState(
    state: PlaybackQualityState,
    requestId: number,
  ): void {
    this.qualityState = state;
    this.updateSnapshot({
      ...this.snapshot,
      playbackRate: state.playbackRate,
      requestId,
    });
    this.publish({
      requestId,
      sourceEpoch: this.snapshot.sourceEpoch,
      sourceId: this.snapshot.sourceId,
      state,
      type: "qualityStateChanged",
    });
  }

  private applyAbLoopState(state: AbLoopState, requestId: number): void {
    this.abLoopState = state;
    this.updateSnapshot({ ...this.snapshot, requestId });
    this.publish({
      requestId,
      sourceEpoch: this.snapshot.sourceEpoch,
      sourceId: this.snapshot.sourceId,
      state,
      type: "abLoopChanged",
    });
  }

  private readBackendSnapshot(): PlaybackSnapshot | null {
    const outcome = invokeSynchronously(() => this.backend.getSnapshot());
    return outcome.kind === "completed" ? outcome.value : null;
  }

  private handleAbLoopSnapshot(
    snapshot: PlaybackSnapshot,
    previousState: PlaybackState,
  ): void {
    const { a, b, enabled } = this.abLoopState;
    if (a !== null && b !== null && !validLoopRange(a, b, snapshot.duration)) {
      this.applyAbLoopState(createAbLoopState(), snapshot.requestId);
      return;
    }
    const resumeState =
      snapshot.state === "ended" && previousState === "playing"
        ? "playing"
        : undefined;
    if (enabled) void this.enforceAbLoop(snapshot.currentTime, resumeState);
  }

  private async enforceAbLoop(
    currentTime: number,
    resumeState?: PlaybackState,
  ): Promise<void> {
    const { a, b, enabled } = this.abLoopState;
    if (
      !enabled ||
      a === null ||
      b === null ||
      currentTime < b ||
      this.abLoopSeekPending
    )
      return;
    this.abLoopSeekPending = true;
    try {
      const requestId = this.allocateRequestId();
      const identity = this.identity(requestId);
      this.interruptFrameCommands(requestId);
      const result = await this.performSeek(
        a,
        "ab_loop",
        identity,
        resumeState,
      );
      this.publishSeekCompleted(result, "ab_loop", identity);
    } finally {
      this.abLoopSeekPending = false;
    }
  }

  private isCurrentSource(command: PlaybackCommandContext): boolean {
    return (
      command.sourceEpoch === this.snapshot.sourceEpoch &&
      command.sourceId === this.snapshot.sourceId
    );
  }

  private async stopLoadingForCurrentSource(): Promise<void> {
    const command = this.currentFeatureCommand();
    if (command !== null) await this.stopLoadingForCommand(command);
  }

  private async stopLoadingForCommand(
    command: PlaybackCommandContext,
  ): Promise<void> {
    const facet = this.loadControlFacet;
    if (!this.isCurrentCommand(command) || facet === undefined) return;
    const state = invokeSynchronously(() => facet.getLoadingState());
    if (state.kind === "completed" && state.value === "stopped") return;
    await this.invokeFeatureOperation(() => facet.stopLoading(command));
  }

  private currentFeatureCommand(): PlaybackCommandContext | null {
    return this.snapshot.sourceId === null
      ? null
      : {
          requestId: this.snapshot.requestId,
          sourceEpoch: this.snapshot.sourceEpoch,
          sourceId: this.snapshot.sourceId,
        };
  }

  private startLoading(command: PlaybackCommandContext): Promise<void> {
    return this.loadControlFacet?.startLoading(command) ?? Promise.resolve();
  }

  private stopLoading(command: PlaybackCommandContext): Promise<void> {
    return this.loadControlFacet?.stopLoading(command) ?? Promise.resolve();
  }

  private async executeStop(
    command: PlaybackCommandContext,
    snapshot: PlaybackSnapshot,
    targetTime: number | null,
  ): Promise<SeekResult | null> {
    try {
      await this.backend.pause(command);
      if (
        !this.isCurrentCommand(command) ||
        targetTime === null ||
        snapshot.currentTime === targetTime
      )
        return null;
      return await this.backend.seek(
        createSeekRequest(command, "user", targetTime, targetTime),
      );
    } finally {
      if (this.qualityState.dataSaver)
        await this.stopLoadingForCommand(command);
    }
  }

  private finishStop(
    command: PlaybackCommandContext,
    startedTerminalRevision: number,
    targetTime: number | null,
    outcome: OperationOutcome<SeekResult | null>,
  ): PlaybackCommandResult {
    if (outcome.kind === "controlled") {
      return this.publishCommandResult(
        this.result(command.requestId, outcome.status),
        command,
      );
    }
    const controlled = this.controlledCommandResult(command, outcome);
    if (controlled !== null) return controlled;
    const terminal = this.terminalCommandResult(
      command,
      startedTerminalRevision,
    );
    if (terminal !== null) return terminal;
    if (outcome.kind === "failed")
      return this.finishStateFailure(command, outcome.error);
    if (outcome.value !== null && outcome.value.status !== "completed") {
      return this.finishStopSeekResult(command, outcome.value);
    }
    return this.completeStop(
      command,
      outcome.value?.confirmedTime ?? targetTime,
    );
  }

  private finishStopSeekResult(
    command: PlaybackCommandContext,
    result: SeekResult,
  ): PlaybackCommandResult {
    const error =
      result.error ??
      (result.status === "failed"
        ? { category: "unknown" as const, message: "停止播放失败" }
        : undefined);
    const state = result.status === "failed" ? "error" : "paused";
    this.updateSnapshot({
      ...this.snapshot,
      error: error ?? null,
      requestId: command.requestId,
      state,
    });
    return this.publishCommandResult(
      this.result(command.requestId, result.status, error),
      command,
    );
  }

  private completeStop(
    command: PlaybackCommandContext,
    targetTime: number | null,
  ): PlaybackCommandResult {
    const currentTime = targetTime ?? this.snapshot.currentTime;
    this.updateSnapshot({
      ...this.snapshot,
      currentTime,
      error: null,
      requestId: command.requestId,
      state: "ready",
    });
    return this.publishCommandResult(
      this.result(command.requestId, "completed"),
      command,
    );
  }

  private async performSeek(
    requestedTime: number,
    reason: SeekReason,
    identity: CommandIdentity,
    resumeState?: PlaybackState,
  ): Promise<SeekResult> {
    const rejected = this.rejectSeek(requestedTime, identity);
    if (rejected !== null) {
      return rejected;
    }
    const snapshotOutcome = invokeSynchronously(() =>
      this.backend.getSnapshot(),
    );
    if (snapshotOutcome.kind === "failed") {
      return this.failSeekSnapshotRead(
        requestedTime,
        identity,
        snapshotOutcome.error,
      );
    }
    return this.performSeekFromSnapshot(
      requestedTime,
      reason,
      identity,
      snapshotOutcome.value,
      false,
      resumeState,
    );
  }

  private async performSeekFromSnapshot(
    requestedTime: number,
    reason: SeekReason,
    identity: CommandIdentity,
    snapshot: PlaybackSnapshot,
    skipBoundaryRequest = false,
    resumeState?: PlaybackState,
  ): Promise<SeekResult> {
    const ranges = normalizeRanges(snapshot.seekable);
    if (ranges.length === 0) {
      return this.unsupportedSeek(requestedTime, identity);
    }
    const targetTime = clampToRanges(requestedTime, ranges);
    if (skipBoundaryRequest && targetTime === snapshot.currentTime) {
      return this.completeClampedSeek(
        requestedTime,
        targetTime,
        identity,
        snapshot,
      );
    }
    return this.submitSeek(
      requestedTime,
      targetTime,
      reason,
      identity,
      resumeState,
    );
  }

  private async submitSeek(
    requestedTime: number,
    targetTime: number,
    reason: SeekReason,
    identity: CommandIdentity,
    resumeState?: PlaybackState,
  ): Promise<SeekResult> {
    const command = this.requireCommand(identity);
    const base = seekResultBase(identity.requestId, requestedTime, targetTime);
    const request = createSeekRequest(
      command,
      reason,
      requestedTime,
      targetTime,
    );
    const startedTerminalRevision = this.terminalRevision;
    this.acceptCommand(identity.requestId);
    const operation = this.runOperation(identity.requestId, () =>
      this.backend.seek(request),
    );
    this.beginSeek(identity.requestId, resumeState);
    return this.finishSeek(
      command,
      startedTerminalRevision,
      base,
      await operation,
    );
  }

  private async seekBySeconds(
    direction: FrameStepDirection,
    seconds: number,
  ): Promise<SeekResult> {
    const requestId = this.allocateRequestId();
    const identity = this.identity(requestId);
    this.interruptFrameCommands(requestId);
    const rejected = this.rejectSeek(this.snapshot.currentTime, identity);
    if (rejected !== null) {
      return this.completeSeekEvent(rejected, "tier", identity);
    }
    const outcome = invokeSynchronously(() => this.backend.getSnapshot());
    if (outcome.kind === "failed") {
      return this.completeSeekEvent(
        this.failSeekSnapshotRead(
          this.snapshot.currentTime,
          identity,
          outcome.error,
        ),
        "tier",
        identity,
      );
    }
    const requestedTime =
      outcome.value.currentTime + directionSign(direction) * seconds;
    const result = await this.performSeekFromSnapshot(
      requestedTime,
      "tier",
      identity,
      outcome.value,
      true,
    );
    return this.completeSeekEvent(result, "tier", identity);
  }

  private completeClampedSeek(
    requestedTime: number,
    targetTime: number,
    identity: CommandIdentity,
    snapshot: PlaybackSnapshot,
  ): SeekResult {
    this.acceptCommand(identity.requestId, false);
    const result: SeekResult = {
      clamped: requestedTime !== targetTime,
      confirmedTime: targetTime,
      requestId: identity.requestId,
      requestedTime,
      status: "completed",
      targetTime,
    };
    this.updateSnapshot({
      ...this.snapshot,
      currentTime: snapshot.currentTime,
      error: null,
      requestId: identity.requestId,
    });
    return this.publishSeekResult(result, identity);
  }

  private unsupportedTierCommand(): PlaybackCommandResult {
    const requestId = this.allocateRequestId();
    const identity = this.identity(requestId);
    this.interruptFrameCommands(requestId);
    return this.publishCommandResult(
      this.result(requestId, this.disposed ? "canceled" : "unsupported"),
      identity,
    );
  }

  private applySeekTier(tier: SeekTier, identity: CommandIdentity): void {
    const previousTier = this.seekTier;
    this.seekTier = tier;
    this.updateSnapshot({ ...this.snapshot, requestId: identity.requestId });
    if (sameSeekTier(previousTier, tier)) {
      return;
    }
    this.publish({
      previousTier,
      requestId: identity.requestId,
      sourceEpoch: identity.sourceEpoch,
      sourceId: identity.sourceId,
      tier,
      type: "seekTierChanged",
    });
  }

  private async runStateCommand(
    state: "playing" | "paused",
    operation: (command: PlaybackCommandContext) => Promise<void>,
  ): Promise<PlaybackCommandResult> {
    const requestId = this.allocateRequestId();
    const identity = this.identity(requestId);
    this.interruptFrameCommands(requestId);
    const rejected = this.rejectStateCommand(identity);
    if (rejected !== null) {
      return rejected;
    }
    const command = this.requireCommand(identity);
    const startedTerminalRevision = this.terminalRevision;
    this.acceptCommand(requestId);
    const pending = this.runOperation(requestId, () => operation(command));
    this.exitSeeking(requestId);
    return this.finishStateCommand(
      command,
      startedTerminalRevision,
      state,
      await pending,
    );
  }

  private finishLoad(
    command: PlaybackCommandContext,
    startedTerminalRevision: number,
    outcome: OperationOutcome<void>,
  ): PlaybackCommandResult {
    const controlled = this.controlledCommandResult(command, outcome);
    if (controlled !== null) {
      return controlled;
    }
    const terminal = this.terminalCommandResult(
      command,
      startedTerminalRevision,
    );
    if (terminal !== null) {
      return terminal;
    }
    if (outcome.kind === "failed") {
      return this.finishLoadFailure(command, outcome.error);
    }
    const snapshotOutcome = invokeSynchronously(() =>
      this.backend.getSnapshot(),
    );
    if (snapshotOutcome.kind === "failed") {
      return this.finishLoadSnapshotFailure(
        command,
        startedTerminalRevision,
        snapshotOutcome.error,
      );
    }
    const interrupted = this.interruptedCommandResult(
      command,
      startedTerminalRevision,
    );
    if (interrupted !== null) {
      return interrupted;
    }
    this.updateSnapshot(readySnapshot(snapshotOutcome.value, command));
    return (
      this.interruptedCommandResult(command, startedTerminalRevision) ??
      this.completedCommandResult(command)
    );
  }

  private finishLoadSnapshotFailure(
    command: PlaybackCommandContext,
    startedTerminalRevision: number,
    cause: unknown,
  ): PlaybackCommandResult {
    return (
      this.interruptedCommandResult(command, startedTerminalRevision) ??
      this.finishLoadFailure(command, cause, "failed")
    );
  }

  private finishLoadFailure(
    command: PlaybackCommandContext,
    cause: unknown,
    forcedStatus?: "failed",
  ): PlaybackCommandResult {
    if (!this.isCurrentCommand(command)) {
      return this.publishCommandResult(
        this.result(command.requestId, "superseded"),
        command,
      );
    }
    const error = normalizeError(cause);
    const status = forcedStatus ?? completionStatus(error);
    this.updateSnapshot({
      ...this.snapshot,
      error,
      requestId: command.requestId,
      state: "error",
    });
    return this.publishCommandResult(
      this.result(command.requestId, status, error),
      command,
    );
  }

  private finishStateCommand(
    command: PlaybackCommandContext,
    startedTerminalRevision: number,
    state: "playing" | "paused",
    outcome: OperationOutcome<void>,
  ): PlaybackCommandResult {
    const controlled = this.controlledCommandResult(command, outcome);
    if (controlled !== null) {
      return controlled;
    }
    const terminal = this.terminalCommandResult(
      command,
      startedTerminalRevision,
    );
    if (terminal !== null) {
      return terminal;
    }
    if (outcome.kind === "failed") {
      return this.finishStateFailure(command, outcome.error);
    }
    this.updateSnapshot({
      ...this.snapshot,
      error: null,
      requestId: command.requestId,
      state,
    });
    return (
      this.interruptedCommandResult(command, startedTerminalRevision) ??
      this.completedCommandResult(command)
    );
  }

  private finishStateFailure(
    command: PlaybackCommandContext,
    cause: unknown,
  ): PlaybackCommandResult {
    if (!this.isCurrentCommand(command)) {
      return this.publishCommandResult(
        this.result(command.requestId, "superseded"),
        command,
      );
    }
    const error = normalizeError(cause);
    const status = completionStatus(error);
    const state = status === "failed" ? "error" : this.snapshot.state;
    this.updateSnapshot({
      ...this.snapshot,
      error,
      requestId: command.requestId,
      state,
    });
    return this.publishCommandResult(
      this.result(command.requestId, status, error),
      command,
    );
  }

  private controlledCommandResult<T>(
    command: PlaybackCommandContext,
    outcome: OperationOutcome<T>,
  ): PlaybackCommandResult | null {
    if (outcome.kind === "controlled") {
      return this.publishCommandResult(
        this.result(command.requestId, outcome.status),
        command,
      );
    }
    if (!this.isCurrentCommand(command)) {
      return this.publishCommandResult(
        this.result(command.requestId, "superseded"),
        command,
      );
    }
    return null;
  }

  private interruptedCommandResult(
    command: PlaybackCommandContext,
    startedTerminalRevision: number,
  ): PlaybackCommandResult | null {
    if (!this.isCurrentCommand(command)) {
      return this.publishCommandResult(
        this.result(command.requestId, "superseded"),
        command,
      );
    }
    return this.terminalCommandResult(command, startedTerminalRevision);
  }

  private terminalCommandResult(
    command: PlaybackCommandContext,
    startedTerminalRevision: number,
  ): PlaybackCommandResult | null {
    const terminal = this.terminalSignalSince(command, startedTerminalRevision);
    if (terminal === null) {
      return null;
    }
    this.restoreTerminal(terminal);
    const result =
      terminal.type === "error"
        ? this.result(command.requestId, "failed", terminal.error)
        : this.result(command.requestId, "superseded");
    return this.publishCommandResult(result, command);
  }

  private completedCommandResult(
    command: PlaybackCommandContext,
  ): PlaybackCommandResult {
    return this.publishCommandResult(
      this.result(command.requestId, "completed"),
      command,
    );
  }

  private finishSeek(
    command: PlaybackCommandContext,
    startedTerminalRevision: number,
    base: SeekResultBase,
    outcome: OperationOutcome<SeekResult>,
  ): SeekResult {
    if (outcome.kind === "controlled") {
      const result = controlledSeekResult(
        base,
        this.snapshot.currentTime,
        outcome.status,
      );
      return this.publishSeekResult(result, command);
    }
    if (!this.isCurrentCommand(command)) {
      return this.publishSeekResult(
        controlledSeekResult(base, this.snapshot.currentTime, "superseded"),
        command,
      );
    }
    const terminal = this.terminalSignalSince(command, startedTerminalRevision);
    if (terminal?.type === "error") {
      return this.finishTerminalSeekFailure(command, base, terminal);
    }
    if (terminal?.type === "ended") {
      return this.finishSeekAfterEnded(command, base, outcome, terminal);
    }
    if (outcome.kind === "failed") {
      return this.finishSeekFailure(command, base, outcome.error);
    }
    const result = normalizeSeekResult(base, outcome.value);
    this.applySeekResult(command, result);
    return this.publishSeekResult(result, command);
  }

  private finishTerminalSeekFailure(
    command: PlaybackCommandContext,
    base: SeekResultBase,
    terminal: Extract<TerminalSignal, { readonly type: "error" }>,
  ): SeekResult {
    this.restoreTerminal(terminal);
    const result = {
      ...base,
      confirmedTime: this.snapshot.currentTime,
      error: terminal.error,
      status: "failed" as const,
    };
    return this.publishSeekResult(result, command);
  }

  private finishSeekAfterEnded(
    command: PlaybackCommandContext,
    base: SeekResultBase,
    outcome: OperationOutcome<SeekResult>,
    terminal: Extract<TerminalSignal, { readonly type: "ended" }>,
  ): SeekResult {
    this.restoreTerminal(terminal);
    if (outcome.kind !== "completed") {
      const result = controlledSeekResult(
        base,
        this.snapshot.currentTime,
        "superseded",
      );
      return this.publishSeekResult(result, command);
    }
    const result = normalizeSeekResult(base, outcome.value);
    if (result.status !== "completed") {
      const superseded = controlledSeekResult(
        base,
        this.snapshot.currentTime,
        "superseded",
      );
      return this.publishSeekResult(superseded, command);
    }
    if (hasLeftMediaEnd(result.confirmedTime, this.snapshot.duration)) {
      this.applySeekResult(command, result);
    } else {
      this.applyEndedSeekResult(command, result);
    }
    return this.publishSeekResult(result, command);
  }

  private finishSeekFailure(
    command: PlaybackCommandContext,
    base: SeekResultBase,
    cause: unknown,
  ): SeekResult {
    if (!this.isCurrentCommand(command)) {
      return this.publishSeekResult(
        controlledSeekResult(base, this.snapshot.currentTime, "superseded"),
        command,
      );
    }
    const error = normalizeError(cause);
    const status = completionStatus(error);
    const result = {
      ...base,
      confirmedTime: this.snapshot.currentTime,
      error,
      status,
    };
    this.applySeekResult(command, result);
    return this.publishSeekResult(result, command);
  }

  private failSeekSnapshotRead(
    requestedTime: number,
    identity: CommandIdentity,
    cause: unknown,
  ): SeekResult {
    const command = this.requireCommand(identity);
    const currentTime = finiteTime(this.snapshot.currentTime);
    const base = seekResultBase(identity.requestId, requestedTime, currentTime);
    const error = normalizeError(cause);
    const result = {
      ...base,
      confirmedTime: currentTime,
      error,
      status: "failed" as const,
    };
    this.acceptCommand(identity.requestId, false);
    this.applySeekResult(command, result);
    return this.publishSeekResult(result, command);
  }

  private applySeekResult(
    command: PlaybackCommandContext,
    result: SeekResult,
  ): void {
    const error = seekResultError(result);
    const state = this.seekResultState(result);
    const currentTime =
      result.status === "completed"
        ? result.confirmedTime
        : this.snapshot.currentTime;
    this.updateSnapshot({
      ...this.snapshot,
      currentTime,
      error,
      requestId: command.requestId,
      state,
    });
  }

  private applyEndedSeekResult(
    command: PlaybackCommandContext,
    result: SeekResult,
  ): void {
    this.updateSnapshot({
      ...this.snapshot,
      currentTime: result.confirmedTime,
      error: null,
      requestId: command.requestId,
      state: "ended",
    });
  }

  private seekResultState(result: SeekResult): PlaybackState {
    if (result.status === "failed") {
      return "error";
    }
    if (result.status !== "completed" || this.seekResumeState !== "ended") {
      return this.seekResumeState;
    }
    return hasLeftMediaEnd(result.confirmedTime, this.snapshot.duration)
      ? "paused"
      : "ended";
  }

  private beginSeek(requestId: number, resumeState?: PlaybackState): void {
    if (resumeState !== undefined) {
      this.seekResumeState = resumeState;
    } else if (this.snapshot.state !== "seeking") {
      this.seekResumeState = seekResumeState(this.snapshot.state);
    }
    this.updateSnapshot({
      ...this.snapshot,
      error: null,
      requestId,
      state: "seeking",
    });
  }

  private exitSeeking(requestId: number): void {
    if (this.snapshot.state === "seeking") {
      this.updateSnapshot({
        ...this.snapshot,
        requestId,
        state: this.seekResumeState,
      });
    }
  }

  private rejectTrackSelection(
    kind: TrackKind,
    trackId: string | null,
    identity: CommandIdentity,
  ): TrackSelectionResult | null {
    if (this.disposed) {
      return this.publishTrackResult(
        trackResult(identity.requestId, kind, null, null, "canceled"),
        identity,
      );
    }
    if (!isCommandState(this.snapshot.state) || identity.sourceId === null) {
      return this.publishUnsupportedTrack(kind, identity);
    }
    if (
      this.tracksFacet === undefined ||
      this.snapshot.capabilities.tracks !== "available"
    ) {
      return this.publishUnsupportedTrack(kind, identity);
    }
    const target = this.trackTarget(kind, trackId);
    if (target.kind === "unsupported") {
      return this.publishUnsupportedTrack(kind, identity, target.error);
    }
    return null;
  }

  private trackTarget(
    kind: TrackKind,
    trackId: string | null,
  ):
    | { readonly kind: "supported" }
    | { readonly error?: PlaybackError; readonly kind: "unsupported" } {
    if (trackId === null) {
      return kind === "subtitle"
        ? { kind: "supported" }
        : { kind: "unsupported" };
    }
    const track = this.getTracks(kind).find(
      (candidate) => candidate.id === trackId && candidate.kind === kind,
    );
    if (track === undefined || track.available !== true) {
      return { kind: "unsupported" };
    }
    if (track.capability === "seamless" || track.capability === "reload") {
      return { kind: "supported" };
    }
    const message = track.unsupportedReason ?? "当前播放路径不支持切换此轨道";
    return { error: { category: "unsupported", message }, kind: "unsupported" };
  }

  private publishUnsupportedTrack(
    kind: TrackKind,
    identity: CommandIdentity,
    error?: PlaybackError,
  ): TrackSelectionResult {
    const state = isTrackKind(kind) ? this.getTrackSelection(kind) : null;
    const result = trackResult(
      identity.requestId,
      kind,
      state?.selectedTrackId ?? null,
      state?.effectiveTrackId ?? null,
      "unsupported",
      error,
    );
    return this.publishTrackResult(result, identity);
  }

  private async submitTrackSelection(
    kind: TrackKind,
    trackId: string | null,
    command: PlaybackCommandContext,
    previous: VersionedTrackSelectionState,
  ): Promise<TrackSelectionResult> {
    const intent = {
      ...previous,
      requestId: command.requestId,
      selectedTrackId: trackId,
    };
    this.acceptTrackSelectionCommand(kind, intent);
    const facet = this.tracksFacet as TrackFacet;
    const invoked = invokeSynchronously(() =>
      facet.selectTrack(kind, trackId, command),
    );
    if (invoked.kind === "failed") {
      return this.finishTrackFailure(command, previous, invoked.error);
    }
    const operation = this.monitorOperation(command.requestId, invoked.value);
    this.trackSelectionIntents.set(kind, {
      requestId: command.requestId,
      state: intent,
    });
    this.applyTrackSelection(intent, command.requestId);
    const outcome = await operation;
    return this.finishTrackSelection(command, previous, intent, outcome);
  }

  private finishTrackSelection(
    command: PlaybackCommandContext,
    previous: VersionedTrackSelectionState,
    intent: VersionedTrackSelectionState,
    outcome: OperationOutcome<void>,
  ): TrackSelectionResult {
    if (outcome.kind === "controlled") {
      return this.publishTrackResult(
        trackResultFromState(command.requestId, intent, outcome.status),
        command,
      );
    }
    if (!this.isCurrentCommand(command)) {
      return this.publishTrackResult(
        trackResultFromState(command.requestId, intent, "superseded"),
        command,
      );
    }
    if (outcome.kind === "failed") {
      return this.finishTrackFailure(command, previous, outcome.error);
    }
    return this.finishTrackSuccess(command, previous, intent.selectedTrackId);
  }

  private finishTrackSuccess(
    command: PlaybackCommandContext,
    previous: VersionedTrackSelectionState,
    targetTrackId: string | null,
  ): TrackSelectionResult {
    const confirmed = this.readFacetTrackSelection(previous.kind);
    if (confirmed.kind === "failed") {
      return this.finishTrackFailure(command, previous, confirmed.error);
    }
    if (
      confirmed.value.requestId !== command.requestId ||
      confirmed.value.effectiveTrackId !== targetTrackId
    ) {
      return this.finishUnconfirmedTrack(command, previous, confirmed.value);
    }
    const converged = convergeTrackSelection(confirmed.value);
    this.clearTrackIntent(previous.kind, command.requestId);
    this.applyTrackSelection(converged, command.requestId);
    return this.publishTrackResult(
      trackResultFromState(command.requestId, converged, "completed"),
      command,
    );
  }

  private finishUnconfirmedTrack(
    command: PlaybackCommandContext,
    previous: VersionedTrackSelectionState,
    confirmed: VersionedTrackSelectionState,
  ): TrackSelectionResult {
    const state =
      confirmed.requestId >= command.requestId ? confirmed : previous;
    const rolledBack = convergeTrackSelection(state);
    const error = {
      category: "unsupported" as const,
      message: "后端未确认目标轨道",
    };
    this.clearTrackIntent(previous.kind, command.requestId);
    this.applyTrackSelection(rolledBack, command.requestId);
    return this.publishTrackResult(
      trackResultFromState(command.requestId, rolledBack, "unsupported", error),
      command,
    );
  }

  private finishTrackFailure(
    command: PlaybackCommandContext,
    previous: VersionedTrackSelectionState,
    cause: unknown,
  ): TrackSelectionResult {
    const error = normalizeError(cause);
    const actual = this.readFacetTrackSelection(previous.kind);
    const state = this.trackFailureState(actual, previous, command.requestId);
    const rolledBack = convergeTrackSelection(state);
    this.clearTrackIntent(previous.kind, command.requestId);
    this.applyTrackSelection(rolledBack, command.requestId);
    return this.publishTrackResult(
      trackResultFromState(
        command.requestId,
        rolledBack,
        completionStatus(error),
        error,
      ),
      command,
    );
  }

  private finishTrackReadFailure(
    kind: TrackKind,
    identity: CommandIdentity,
    cause: unknown,
  ): TrackSelectionResult {
    const error = normalizeError(cause);
    const cached = this.trackSelections.get(kind);
    const state =
      cached !== undefined && sameSource(cached, this.snapshot) ? cached : null;
    const result = trackResult(
      identity.requestId,
      kind,
      state?.selectedTrackId ?? null,
      state?.effectiveTrackId ?? null,
      completionStatus(error),
      error,
    );
    return this.publishTrackResult(result, identity);
  }

  private trackFailureState(
    actual: TrackSelectionReadOutcome,
    previous: VersionedTrackSelectionState,
    requestId: number,
  ): VersionedTrackSelectionState {
    if (actual.kind === "completed" && actual.value.requestId >= requestId) {
      return actual.value;
    }
    return previous;
  }

  private currentTrackSelection(
    kind: TrackKind,
    fallback: VersionedTrackSelectionState,
  ): VersionedTrackSelectionState {
    const intent = this.trackSelectionIntents.get(kind);
    if (
      intent === undefined ||
      intent.requestId !== this.latestCommandRequestId
    ) {
      return fallback;
    }
    return sameSource(intent.state, fallback) ? intent.state : fallback;
  }

  private readTrackSelection(kind: TrackKind): TrackSelectionReadOutcome {
    const outcome = this.readFacetTrackSelection(kind);
    if (outcome.kind === "failed") {
      return outcome;
    }
    return {
      kind: "completed",
      value: this.acceptTrackSelectionState(outcome.value),
    };
  }

  private readFacetTrackSelection(kind: TrackKind): TrackSelectionReadOutcome {
    const facet = this.tracksFacet;
    if (facet === undefined) {
      return {
        error: new PlaybackBackendError("unsupported", "后端不支持轨道选择"),
        kind: "failed",
      };
    }
    const outcome = invokeSynchronously(() => facet.getSelectionState(kind));
    if (outcome.kind === "failed") {
      return outcome;
    }
    const tracks = this.getTracks(kind);
    if (!isTrackSelectionState(outcome.value, kind, this.snapshot, tracks)) {
      return { error: new Error("后端返回了无效的轨道状态"), kind: "failed" };
    }
    return {
      kind: "completed",
      value: normalizeTrackSelectionState(outcome.value),
    };
  }

  private acceptTrackSelectionState(
    state: VersionedTrackSelectionState,
  ): VersionedTrackSelectionState {
    const latest = this.trackSelectionRequestIds.get(state.kind);
    if (
      latest !== undefined &&
      sameSource(latest, state) &&
      state.requestId < latest.requestId
    ) {
      const cached = this.trackSelections.get(state.kind);
      return cached !== undefined && sameSource(cached, state)
        ? cached
        : latest;
    }
    this.rememberTrackSelectionRequest(state);
    this.trackSelections.set(state.kind, state);
    return state;
  }

  private clearTrackIntent(kind: TrackKind, requestId: number): void {
    if (this.trackSelectionIntents.get(kind)?.requestId === requestId) {
      this.trackSelectionIntents.delete(kind);
    }
  }

  private acceptTrackSelectionCommand(
    kind: TrackKind,
    state: VersionedTrackSelectionState,
  ): void {
    this.acceptCommand(state.requestId);
    this.trackSelectionIntents.delete(kind);
    this.rememberTrackSelectionRequest(state);
  }

  private rememberTrackSelectionRequest(
    state: VersionedTrackSelectionState,
  ): void {
    const latest = this.trackSelectionRequestIds.get(state.kind);
    if (
      latest === undefined ||
      !sameSource(latest, state) ||
      state.requestId >= latest.requestId
    ) {
      this.trackSelectionRequestIds.set(state.kind, state);
      this.nextRequestId = Math.max(this.nextRequestId, state.requestId);
    }
  }

  private applyTrackSelection(
    state: VersionedTrackSelectionState,
    requestId: number,
  ): void {
    this.rememberTrackSelectionRequest(state);
    this.trackSelections.set(state.kind, state);
    this.publish({
      effectiveTrackId: state.effectiveTrackId,
      kind: state.kind,
      requestId,
      selectedTrackId: state.selectedTrackId,
      sourceEpoch: state.sourceEpoch,
      sourceId: state.sourceId,
      type: "trackSelectionChanged",
    });
  }

  private publishTrackResult(
    result: TrackSelectionResult,
    identity: CommandIdentity,
  ): TrackSelectionResult {
    const snapshot = this.completionSnapshot(identity);
    this.publish({
      requestId: result.requestId,
      result,
      snapshot,
      sourceEpoch: identity.sourceEpoch,
      sourceId: identity.sourceId,
      type: "trackSelectionCompleted",
    });
    this.publish({
      requestId: result.requestId,
      result,
      snapshot,
      sourceEpoch: identity.sourceEpoch,
      sourceId: identity.sourceId,
      type: "commandCompleted",
    });
    this.completionSnapshots.delete(result.requestId);
    return result;
  }

  private rejectStateCommand(
    identity: CommandIdentity,
  ): PlaybackCommandResult | null {
    if (this.disposed) {
      return this.publishCommandResult(
        this.result(identity.requestId, "canceled"),
        identity,
      );
    }
    if (!isCommandState(this.snapshot.state) || identity.sourceId === null) {
      return this.publishCommandResult(
        this.result(identity.requestId, "unsupported"),
        identity,
      );
    }
    return null;
  }

  private rejectSeek(
    requestedTime: number,
    identity: CommandIdentity,
  ): SeekResult | null {
    if (this.disposed) {
      return this.publishSeekResult(
        this.basicSeekResult(requestedTime, identity.requestId, "canceled"),
        identity,
      );
    }
    if (!isCommandState(this.snapshot.state) || identity.sourceId === null) {
      return this.publishSeekResult(
        this.basicSeekResult(requestedTime, identity.requestId, "unsupported"),
        identity,
      );
    }
    if (
      !Number.isFinite(requestedTime) ||
      this.snapshot.capabilities.seek === "unavailable"
    ) {
      return this.unsupportedSeek(requestedTime, identity);
    }
    return null;
  }

  private unsupportedSeek(
    requestedTime: number,
    identity: CommandIdentity,
  ): SeekResult {
    return this.publishSeekResult(
      this.basicSeekResult(requestedTime, identity.requestId, "unsupported"),
      identity,
    );
  }

  private basicSeekResult(
    requestedTime: number,
    requestId: number,
    status: "canceled" | "superseded" | "unsupported",
  ): SeekResult {
    const currentTime = finiteTime(this.snapshot.currentTime);
    return {
      clamped: !Number.isFinite(requestedTime) || requestedTime !== currentTime,
      confirmedTime: currentTime,
      requestId,
      requestedTime,
      status,
      targetTime: currentTime,
    };
  }

  private runOperation<T>(
    requestId: number,
    operation: () => Promise<T>,
  ): Promise<OperationOutcome<T>> {
    const control = createPendingCommand();
    this.pending.set(requestId, control);
    const controlled = control.promise.then<OperationOutcome<T>>((status) => ({
      kind: "controlled",
      status,
    }));
    const invoked = Promise.resolve().then(
      async (): Promise<OperationOutcome<T>> => {
        if (control.isSettled()) {
          return controlled;
        }
        try {
          return { kind: "completed", value: await operation() };
        } catch (error: unknown) {
          return { error, kind: "failed" };
        }
      },
    );
    return Promise.race([invoked, controlled]).finally(() => {
      if (this.pending.get(requestId) === control) {
        this.pending.delete(requestId);
      }
    });
  }

  private monitorOperation<T>(
    requestId: number,
    operation: Promise<T>,
  ): Promise<OperationOutcome<T>> {
    const control = createPendingCommand();
    this.pending.set(requestId, control);
    const controlled = control.promise.then<OperationOutcome<T>>((status) => ({
      kind: "controlled",
      status,
    }));
    const invoked = operation.then<OperationOutcome<T>, OperationOutcome<T>>(
      (value) => ({ kind: "completed", value }),
      (error: unknown) => ({ error, kind: "failed" }),
    );
    return Promise.race([invoked, controlled]).finally(() => {
      if (this.pending.get(requestId) === control) {
        this.pending.delete(requestId);
      }
    });
  }

  private handleBackendEvent(event: PlaybackBackendEvent): void {
    if (!this.acceptBackendEvent(event)) {
      return;
    }
    if (event.type === "snapshotChanged") {
      const previousState = this.snapshot.state;
      this.applyBackendSnapshot(event);
      this.handleAbLoopSnapshot(event.snapshot, previousState);
      return;
    }
    if (event.type === "capabilitiesChanged") {
      this.updateSnapshot({
        ...this.snapshot,
        capabilities: event.capabilities,
      });
      this.publish(event);
      return;
    }
    if (event.type === "ended") {
      const terminal = this.recordTerminalSignal(event);
      this.restoreTerminal(terminal);
      return;
    }
    if (
      (event.error.category === "media" || event.error.category === "decode") &&
      this.abLoopState.a !== null
    ) {
      this.applyAbLoopState(
        createAbLoopState(),
        Math.max(this.snapshot.requestId, event.requestId),
      );
    }
    const terminal = this.recordTerminalSignal(event);
    this.restoreTerminal(terminal);
    this.publish(event);
  }

  private recordTerminalSignal(
    event: Extract<PlaybackBackendEvent, { readonly type: "ended" | "error" }>,
  ): TerminalSignal {
    this.terminalRevision += 1;
    const current = this.terminalSignal;
    if (
      event.type === "ended" &&
      current?.type === "error" &&
      sameTerminalSource(current, event)
    ) {
      this.terminalSignal = { ...current, revision: this.terminalRevision };
    } else if (event.type === "ended") {
      this.terminalSignal = {
        ...event,
        revision: this.terminalRevision,
        type: "ended",
      };
    } else {
      this.terminalSignal = {
        ...event,
        revision: this.terminalRevision,
        type: "error",
      };
    }
    return this.terminalSignal;
  }

  private restoreTerminal(terminal: TerminalSignal): void {
    if (terminal.type === "error") {
      if (
        this.snapshot.state !== "error" ||
        this.snapshot.error !== terminal.error ||
        this.snapshot.requestId !== terminal.requestId
      ) {
        this.updateSnapshot({
          ...this.snapshot,
          error: terminal.error,
          requestId: terminal.requestId,
          state: "error",
        });
      }
      return;
    }
    if (
      this.snapshot.state !== "ended" ||
      this.snapshot.error !== null ||
      this.snapshot.requestId !== terminal.requestId
    ) {
      this.updateSnapshot({
        ...this.snapshot,
        error: null,
        requestId: terminal.requestId,
        state: "ended",
      });
    }
  }

  private terminalSignalSince(
    command: PlaybackCommandContext,
    startedTerminalRevision: number,
  ): TerminalSignal | null {
    const terminal = this.terminalSignal;
    if (terminal === null || terminal.revision <= startedTerminalRevision) {
      return null;
    }
    if (
      terminal.sourceEpoch !== command.sourceEpoch ||
      terminal.sourceId !== command.sourceId
    ) {
      return null;
    }
    return terminal;
  }

  private acceptBackendEvent(event: PlaybackBackendEvent): boolean {
    if (this.disposed || event.sourceId !== this.snapshot.sourceId) {
      return false;
    }
    if (!isValidEventId(event.eventId) || !isValidEventId(event.requestId)) {
      return false;
    }
    if (
      event.sourceEpoch !== this.snapshot.sourceEpoch ||
      !this.isCurrentBackendRequest(event)
    ) {
      return false;
    }
    if (event.eventId <= this.latestEventId) {
      return false;
    }
    this.latestEventId = event.eventId;
    return true;
  }

  private isCurrentBackendRequest(event: PlaybackBackendEvent): boolean {
    if (event.requestId !== this.latestBackendRequestId) {
      return false;
    }
    return (
      event.type !== "snapshotChanged" ||
      event.snapshot.requestId === event.requestId
    );
  }

  private applyBackendSnapshot(
    event: Extract<PlaybackBackendEvent, { readonly type: "snapshotChanged" }>,
  ): void {
    const requestId = Math.max(
      this.snapshot.requestId,
      event.snapshot.requestId,
    );
    this.updateSnapshot({
      ...event.snapshot,
      requestId,
      sourceEpoch: event.sourceEpoch,
      sourceId: event.sourceId,
    });
  }

  private interruptFrameCommands(requestId: number): void {
    this.frameInterruptRequestId = Math.max(
      this.frameInterruptRequestId,
      requestId,
    );
  }

  private frameInterruptionStatus(
    identity: FrameCommandIdentity,
  ): "canceled" | "superseded" | null {
    if (this.disposed) {
      return "canceled";
    }
    const wrongSource =
      identity.sourceEpoch !== this.snapshot.sourceEpoch ||
      identity.sourceId !== this.snapshot.sourceId;
    if (wrongSource || identity.requestId <= this.frameInterruptRequestId) {
      return "superseded";
    }
    return null;
  }

  private applyFrameResult(
    command: PlaybackCommandContext,
    result: FrameStepResult,
  ): void {
    if (
      !this.isCurrentCommand(command) ||
      result.status === "canceled" ||
      result.status === "superseded"
    ) {
      return;
    }
    const currentTime =
      result.status === "completed" ||
      (result.status === "failed" && result.precision === "exact-verified")
        ? result.confirmedMediaTime
        : this.snapshot.currentTime;
    const error: PlaybackError | null =
      result.status === "failed"
        ? (result.error ?? { category: "unknown", message: "逐帧失败" })
        : null;
    this.updateSnapshot({
      ...this.snapshot,
      currentTime,
      error,
      requestId: command.requestId,
      state: "paused",
    });
  }

  private publishFrameResult(
    result: FrameStepResult,
    identity: FrameCommandIdentity,
  ): FrameStepResult {
    if (
      identity.sourceEpoch === this.snapshot.sourceEpoch &&
      identity.sourceId === this.snapshot.sourceId
    ) {
      this.lastFrameStepResult = result;
    }
    const snapshot = this.completionSnapshot(identity);
    const context = {
      requestId: result.requestId,
      result,
      snapshot,
      sourceEpoch: identity.sourceEpoch,
      sourceId: identity.sourceId,
    };
    this.publish({ ...context, type: "frameStepCompleted" });
    this.publish({ ...context, type: "commandCompleted" });
    this.completionSnapshots.delete(result.requestId);
    return result;
  }

  private acceptCommand(requestId: number, updatesBackend = true): void {
    this.settleAll("superseded");
    this.latestCommandRequestId = requestId;
    if (updatesBackend) this.latestBackendRequestId = requestId;
    this.completionSnapshots.set(requestId, { ...this.snapshot, requestId });
  }

  private createLoadCommand(
    source: PlaybackSource,
    requestId: number,
  ): PlaybackCommandContext {
    this.nextSourceEpoch += 1;
    return {
      requestId,
      sourceEpoch: this.nextSourceEpoch,
      sourceId: source.id,
    };
  }

  private identity(requestId: number): CommandIdentity {
    return {
      requestId,
      sourceEpoch: this.snapshot.sourceEpoch,
      sourceId: this.snapshot.sourceId,
    };
  }

  private currentPreviewCommand(): PlaybackCommandContext {
    return {
      requestId: this.snapshot.requestId,
      sourceEpoch: this.snapshot.sourceEpoch,
      sourceId: this.snapshot.sourceId ?? "",
    };
  }

  private requireCommand(identity: CommandIdentity): PlaybackCommandContext {
    if (identity.sourceId === null) {
      throw new Error("播放源上下文缺失");
    }
    return { ...identity, sourceId: identity.sourceId };
  }

  private isCurrentCommand(command: PlaybackCommandContext): boolean {
    return (
      !this.disposed &&
      command.requestId === this.latestCommandRequestId &&
      command.sourceEpoch === this.snapshot.sourceEpoch &&
      command.sourceId === this.snapshot.sourceId
    );
  }

  private result(
    requestId: number,
    status: PlaybackCompletionStatus,
    error?: PlaybackError,
  ): PlaybackCommandResult {
    return error === undefined
      ? { requestId, status }
      : { error, requestId, status };
  }

  private publishCommandResult(
    result: PlaybackCommandResult,
    identity: CommandIdentity,
  ): PlaybackCommandResult {
    this.publishCompletion(result, identity);
    return result;
  }

  private publishSeekResult(
    result: SeekResult,
    identity: CommandIdentity,
  ): SeekResult {
    this.publishCompletion(result, identity);
    return result;
  }

  private completeSeekEvent(
    result: SeekResult,
    reason: SeekReason,
    identity: CommandIdentity,
  ): SeekResult {
    this.publishSeekCompleted(result, reason, identity);
    return result;
  }

  private publishSeekCompleted(
    result: SeekResult,
    reason: SeekReason,
    identity: CommandIdentity,
  ): void {
    this.publish({
      reason,
      requestId: result.requestId,
      result,
      snapshot: this.completionSnapshot(identity),
      sourceEpoch: identity.sourceEpoch,
      sourceId: identity.sourceId,
      type: "seekCompleted",
    });
  }

  private publishCompletion(
    result: PlaybackCommandResult | SeekResult,
    identity: CommandIdentity,
  ): void {
    this.publish({
      requestId: result.requestId,
      result,
      snapshot: this.completionSnapshot(identity),
      sourceEpoch: identity.sourceEpoch,
      sourceId: identity.sourceId,
      type: "commandCompleted",
    });
    this.completionSnapshots.delete(result.requestId);
  }

  private completionSnapshot(identity: CommandIdentity): PlaybackSnapshot {
    const snapshot = this.completionSnapshots.get(identity.requestId);
    if (snapshot !== undefined) {
      return snapshot;
    }
    return { ...this.snapshot, requestId: identity.requestId };
  }

  private updateSnapshot(snapshot: PlaybackSnapshot): void {
    const normalized = {
      ...snapshot,
      capabilities: normalizeCapabilities(snapshot.capabilities),
    };
    this.snapshot = normalized;
    if (normalized.requestId === this.latestCommandRequestId) {
      this.completionSnapshots.set(normalized.requestId, normalized);
    }
    this.publish({
      requestId: normalized.requestId,
      snapshot: normalized,
      type: "snapshotChanged",
    });
  }

  private publish(event: PlaybackEvent): void {
    const publishVersion = ++this.publishVersion;
    const listeners = [...this.listeners];
    for (const listener of listeners) {
      safelyInvoke(() => {
        listener(event);
      });
      if (publishVersion !== this.publishVersion) {
        return;
      }
    }
  }

  private settleAll(status: PlaybackCompletionStatus): void {
    for (const command of this.pending.values()) {
      command.settle(status);
    }
  }

  private allocateRequestId(): number {
    this.nextRequestId += 1;
    return this.nextRequestId;
  }
}

function isTrackKind(kind: unknown): kind is TrackKind {
  return kind === "audio" || kind === "subtitle";
}

function isTrackSelectionState(
  state: unknown,
  kind: TrackKind,
  snapshot: PlaybackSnapshot,
  tracks: readonly PlaybackTrack[],
): state is TrackSelectionState {
  if (!isTrackSelectionShape(state, kind, snapshot)) {
    return false;
  }
  return (
    isValidStateTrackId(state.selectedTrackId, kind, tracks) &&
    isValidStateTrackId(state.effectiveTrackId, kind, tracks)
  );
}

function isTrackSelectionShape(
  state: unknown,
  kind: TrackKind,
  snapshot: PlaybackSnapshot,
): state is TrackSelectionState {
  if (typeof state !== "object" || state === null) {
    return false;
  }
  const candidate = state as Partial<TrackSelectionState>;
  return (
    candidate.kind === kind &&
    (candidate.requestId === undefined ||
      isValidEventId(candidate.requestId)) &&
    candidate.sourceEpoch === snapshot.sourceEpoch &&
    candidate.sourceId === snapshot.sourceId &&
    isValidEventId(candidate.sourceEpoch) &&
    typeof candidate.sourceId === "string" &&
    isTrackId(candidate.selectedTrackId) &&
    isTrackId(candidate.effectiveTrackId)
  );
}

function isValidStateTrackId(
  trackId: string | null,
  kind: TrackKind,
  tracks: readonly PlaybackTrack[],
): boolean {
  if (trackId === null) {
    return true;
  }
  return tracks.some(
    (track) =>
      track.id === trackId && track.kind === kind && track.available === true,
  );
}

function isTrackId(trackId: unknown): trackId is string | null {
  return trackId === null || typeof trackId === "string";
}

function sameSource(
  left: Pick<TrackSelectionState, "sourceEpoch" | "sourceId">,
  right: Pick<
    PlaybackSnapshot | TrackSelectionState,
    "sourceEpoch" | "sourceId"
  >,
): boolean {
  return (
    left.sourceEpoch === right.sourceEpoch && left.sourceId === right.sourceId
  );
}

function normalizeTrackSelectionState(
  state: TrackSelectionState,
): VersionedTrackSelectionState {
  return { ...state, requestId: state.requestId ?? 0 };
}

function convergeTrackSelection(
  state: VersionedTrackSelectionState,
): VersionedTrackSelectionState {
  return { ...state, selectedTrackId: state.effectiveTrackId };
}

function trackResultFromState(
  requestId: number,
  state: VersionedTrackSelectionState,
  status: PlaybackCompletionStatus,
  error?: PlaybackError,
): TrackSelectionResult {
  return trackResult(
    requestId,
    state.kind,
    state.selectedTrackId,
    state.effectiveTrackId,
    status,
    error,
  );
}

function trackResult(
  requestId: number,
  kind: TrackKind,
  selectedTrackId: string | null,
  effectiveTrackId: string | null,
  status: PlaybackCompletionStatus,
  error?: PlaybackError,
): TrackSelectionResult {
  const result = { effectiveTrackId, kind, requestId, selectedTrackId, status };
  return error === undefined ? result : { ...result, error };
}

function normalizeCapabilities(
  capabilities: PlaybackCapabilities,
): PlaybackCapabilities {
  return capabilities.tracks === undefined
    ? { ...capabilities, tracks: "unavailable" }
    : capabilities;
}

function createInitialSnapshot(): PlaybackSnapshot {
  return {
    buffered: [],
    capabilities: EMPTY_CAPABILITIES,
    currentTime: 0,
    duration: 0,
    error: null,
    playbackRate: 1,
    requestId: 0,
    seekable: [],
    sourceEpoch: 0,
    sourceId: null,
    state: "idle",
  };
}

function loadingSnapshot(
  current: PlaybackSnapshot,
  source: PlaybackSource,
  command: PlaybackCommandContext,
): PlaybackSnapshot {
  return {
    ...current,
    buffered: [],
    capabilities: EMPTY_CAPABILITIES,
    currentTime: 0,
    duration: source.metadata?.duration ?? 0,
    error: null,
    requestId: command.requestId,
    seekable: [],
    sourceEpoch: command.sourceEpoch,
    sourceId: command.sourceId,
    state: "loading",
  };
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

function createPendingCommand(): PendingCommand {
  let settlePromise!: (status: PlaybackCompletionStatus) => void;
  let settled = false;
  const promise = new Promise<PlaybackCompletionStatus>((resolve) => {
    settlePromise = resolve;
  });
  return {
    promise,
    isSettled: () => settled,
    settle: (status) => {
      if (!settled) {
        settled = true;
        settlePromise(status);
      }
    },
  };
}

function normalizeError(cause: unknown): PlaybackError {
  if (cause instanceof PlaybackBackendError) {
    return cause.code === undefined
      ? { category: cause.category, message: cause.message }
      : { category: cause.category, code: cause.code, message: cause.message };
  }
  if (cause instanceof Error) {
    return { category: "unknown", message: cause.message };
  }
  return { category: "unknown", message: "未知播放错误" };
}

function completionStatus(error: PlaybackError): "failed" | "unsupported" {
  return error.category === "unsupported" ? "unsupported" : "failed";
}

function seekResultBase(
  requestId: number,
  requestedTime: number,
  targetTime: number,
): SeekResultBase {
  return {
    clamped: requestedTime !== targetTime,
    requestId,
    requestedTime,
    targetTime,
  };
}

function createSeekRequest(
  command: PlaybackCommandContext,
  reason: SeekReason,
  requestedTime: number,
  targetTime: number,
): SeekRequest {
  return {
    ...command,
    boundaryPolicy: "clamp",
    reason,
    requestedTime,
    targetTime,
  };
}

function normalizeSeekResult(
  base: SeekResultBase,
  result: SeekResult,
): SeekResult {
  const normalized = {
    ...result,
    requestId: base.requestId,
    requestedTime: base.requestedTime,
  };
  if (normalized.status !== "failed" || normalized.error !== undefined) {
    return normalized;
  }
  return {
    ...normalized,
    error: { category: "unknown", message: "Seek 失败" },
  };
}

function controlledSeekResult(
  base: SeekResultBase,
  confirmedTime: number,
  status: PlaybackCompletionStatus,
): SeekResult {
  return { ...base, confirmedTime, status };
}

function seekResultError(result: SeekResult): PlaybackError | null {
  if (
    result.status === "completed" ||
    result.status === "superseded" ||
    result.status === "canceled"
  ) {
    return result.error ?? null;
  }
  if (result.status === "unsupported") {
    return (
      result.error ?? { category: "unsupported", message: "当前源不支持 Seek" }
    );
  }
  return result.error ?? { category: "unknown", message: "Seek 失败" };
}

function hasLeftMediaEnd(confirmedTime: number, duration: number): boolean {
  return (
    Number.isFinite(confirmedTime) &&
    Number.isFinite(duration) &&
    duration > 0 &&
    confirmedTime < duration
  );
}

function sameTerminalSource(
  left: Pick<TerminalSignal, "sourceEpoch" | "sourceId">,
  right: Pick<TerminalSignal, "sourceEpoch" | "sourceId">,
): boolean {
  return (
    left.sourceEpoch === right.sourceEpoch && left.sourceId === right.sourceId
  );
}

function sameCommandSource(
  identity: CommandIdentity,
  snapshot: PlaybackSnapshot,
): boolean {
  return (
    identity.sourceEpoch === snapshot.sourceEpoch &&
    identity.sourceId === snapshot.sourceId
  );
}

function seekResumeState(state: PlaybackState): PlaybackState {
  if (state === "playing" || state === "paused" || state === "ended") {
    return state;
  }
  return "ready";
}

function isCommandState(state: PlaybackState): boolean {
  return (
    state !== "idle" &&
    state !== "loading" &&
    state !== "error" &&
    state !== "disposed"
  );
}

function stopTarget(snapshot: PlaybackSnapshot): number | null {
  if (snapshot.capabilities.seek === "unavailable") return null;
  return normalizeRanges(snapshot.seekable)[0]?.start ?? null;
}

function isStoppedSnapshot(
  snapshot: PlaybackSnapshot,
  targetTime: number | null,
): boolean {
  const stoppedState =
    snapshot.state !== "playing" && snapshot.state !== "seeking";
  return (
    stoppedState && (targetTime === null || snapshot.currentTime === targetTime)
  );
}

function finiteTime(value: number): number {
  return Number.isFinite(value) ? value : 0;
}

function sameSeekTier(left: SeekTier | null, right: SeekTier): boolean {
  if (left === null || left.kind !== right.kind) {
    return false;
  }
  return (
    left.kind === "frame" ||
    (right.kind === "seconds" && left.value === right.value)
  );
}

function isValidEventId(eventId: number): boolean {
  return Number.isFinite(eventId) && Number.isInteger(eventId) && eventId >= 0;
}

function invokeSynchronously<T>(operation: () => T): SynchronousOutcome<T> {
  try {
    return { kind: "completed", value: operation() };
  } catch (error: unknown) {
    return { error, kind: "failed" };
  }
}

function safelyInvoke(operation: () => void): void {
  try {
    operation();
  } catch {
    return;
  }
}
