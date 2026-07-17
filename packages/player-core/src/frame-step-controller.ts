import {
  clampToRanges,
  directionSign,
  hasTargetIdentity,
  isPositiveDuration,
  normalizeRanges,
  verifyExactFrame,
} from "./seek-algorithms";
import type {
  AdjacentFrameTarget,
  FramePresentationFacet,
  FrameStepDirection,
  FrameStepPrecision,
  FrameStepResult,
  PlaybackBackend,
  PlaybackCommandContext,
  PlaybackCompletionStatus,
  PlaybackError,
  PlaybackSnapshot,
  PresentedFrame,
  SeekRequest,
  SeekResult,
  TimeRange,
} from "./types";

const MAX_CORRECTIONS = 2;

export interface FrameCommandIdentity {
  readonly requestId: number;
  readonly sourceEpoch: number;
  readonly sourceId: string | null;
}

export type FrameOperationOutcome<T> =
  | { readonly kind: "completed"; readonly value: T }
  | { readonly error: unknown; readonly kind: "failed" }
  | { readonly kind: "controlled"; readonly status: PlaybackCompletionStatus };

export interface FrameStepControllerHost {
  allocateRequestId(): number;
  applyFrameResult(
    command: PlaybackCommandContext,
    result: FrameStepResult,
  ): void;
  beginFrameCommand(command: PlaybackCommandContext): void;
  getIdentity(requestId: number): FrameCommandIdentity;
  getInterruptionStatus(
    identity: FrameCommandIdentity,
  ): "canceled" | "superseded" | null;
  getSnapshot(): PlaybackSnapshot;
  normalizeError(cause: unknown): PlaybackError;
  publishFrameResult(
    result: FrameStepResult,
    identity: FrameCommandIdentity,
  ): FrameStepResult;
  runOperation<T>(
    requestId: number,
    operation: () => Promise<T>,
  ): Promise<FrameOperationOutcome<T>>;
}

interface FrameContext {
  readonly command: PlaybackCommandContext;
  readonly direction: FrameStepDirection;
  readonly ranges: readonly TimeRange[];
  readonly snapshot: PlaybackSnapshot;
}

interface FramePlanBase {
  readonly frameDuration: number | null;
  readonly startFrame: PresentedFrame | null;
  readonly startMediaTime: number;
  readonly targetMediaTime: number;
}

interface ExactFramePlan extends FramePlanBase {
  readonly precision: "exact-verified";
  readonly target: AdjacentFrameTarget;
}

interface ApproximateFramePlan extends FramePlanBase {
  readonly frameDuration: number;
  readonly precision: "approximate";
}

type FramePlan =
  | ExactFramePlan
  | ApproximateFramePlan
  | (FramePlanBase & { readonly precision: "unsupported" });

export class FrameStepController {
  private readonly backend: PlaybackBackend;
  private readonly facet: FramePresentationFacet | undefined;
  private readonly host: FrameStepControllerHost;
  private queue: Promise<void> = Promise.resolve();

  constructor(
    backend: PlaybackBackend,
    facet: FramePresentationFacet | undefined,
    host: FrameStepControllerHost,
  ) {
    this.backend = backend;
    this.facet = facet;
    this.host = host;
  }

  step(direction: FrameStepDirection): Promise<FrameStepResult> {
    const identity = this.host.getIdentity(this.host.allocateRequestId());
    const result = this.queue.then(() => this.execute(identity, direction));
    this.queue = result.then(
      () => undefined,
      () => undefined,
    );
    return result;
  }

  private async execute(
    identity: FrameCommandIdentity,
    direction: FrameStepDirection,
  ): Promise<FrameStepResult> {
    const rejected = this.rejectedResult(identity, direction);
    if (rejected !== null) {
      return rejected;
    }
    const snapshot = this.host.getSnapshot();
    if (!isFrameStepState(snapshot.state)) {
      return this.publishBasic(
        identity,
        direction,
        "unsupported",
        "unsupported",
        snapshot.currentTime,
      );
    }
    const command = requireCommand(identity);
    this.host.beginFrameCommand(command);
    const paused = await this.pauseIfRequired(command, direction, snapshot);
    return paused ?? this.executePaused(command, direction);
  }

  private rejectedResult(
    identity: FrameCommandIdentity,
    direction: FrameStepDirection,
  ): FrameStepResult | null {
    const status = this.host.getInterruptionStatus(identity);
    if (status !== null) {
      return this.publishBasic(identity, direction, status, "unsupported");
    }
    if (identity.sourceId === null) {
      return this.publishBasic(
        identity,
        direction,
        "unsupported",
        "unsupported",
      );
    }
    return null;
  }

  private async pauseIfRequired(
    command: PlaybackCommandContext,
    direction: FrameStepDirection,
    snapshot: PlaybackSnapshot,
  ): Promise<FrameStepResult | null> {
    if (snapshot.state === "paused") {
      return null;
    }
    const outcome = await this.host.runOperation(command.requestId, () =>
      this.backend.pause(command),
    );
    if (outcome.kind !== "completed") {
      return this.finishOperationFailure(
        command,
        direction,
        snapshot.currentTime,
        outcome,
      );
    }
    const confirmation = this.readSnapshot();
    if (confirmation instanceof Error) {
      return this.publishFailure(command, direction, confirmation);
    }
    return isPauseConfirmed(confirmation, command)
      ? null
      : this.publishFailure(command, direction, new Error("逐帧暂停未确认"));
  }

  private async executePaused(
    command: PlaybackCommandContext,
    direction: FrameStepDirection,
  ): Promise<FrameStepResult> {
    const interrupted = this.host.getInterruptionStatus(command);
    if (interrupted !== null) {
      return this.publishBasic(command, direction, interrupted, "unsupported");
    }
    const current = this.host.getSnapshot();
    if (!isFrameStepState(current.state)) {
      return this.publishBasic(
        command,
        direction,
        "unsupported",
        "unsupported",
        current.currentTime,
      );
    }
    const snapshot = this.readSnapshot();
    if (snapshot instanceof Error) {
      return this.publishFailure(command, direction, snapshot);
    }
    const context = this.createContext(command, direction, snapshot);
    if (context === null) {
      return this.publishBasic(
        command,
        direction,
        "unsupported",
        "unsupported",
        snapshot.currentTime,
      );
    }
    const plan = this.createPlan(context);
    return this.executePlan(context, plan);
  }

  private createContext(
    command: PlaybackCommandContext,
    direction: FrameStepDirection,
    snapshot: PlaybackSnapshot,
  ): FrameContext | null {
    const ranges = normalizeRanges(snapshot.seekable);
    if (snapshot.capabilities.seek === "unavailable" || ranges.length === 0) {
      return null;
    }
    return { command, direction, ranges, snapshot };
  }

  private createPlan(context: FrameContext): FramePlan {
    const observedFrame = this.readCurrentFrame(context.command);
    const exactFrameAvailable =
      context.snapshot.capabilities.framePresentation === "exact";
    const startFrame =
      exactFrameAvailable &&
      observedFrame !== null &&
      isCurrentFrame(observedFrame, context.command)
        ? observedFrame
        : null;
    const startMediaTime = finiteMediaTime(
      startFrame?.mediaTime,
      context.snapshot.currentTime,
    );
    const nominal = this.readNominalDuration(context.command);
    const target = this.readExactTarget(context, startFrame);
    if (
      target !== null &&
      hasTargetIdentity(startFrame as PresentedFrame, target)
    ) {
      return this.exactPlan(startFrame as PresentedFrame, target);
    }
    if (isPositiveDuration(nominal)) {
      const targetMediaTime =
        startMediaTime + directionSign(context.direction) * nominal;
      return {
        frameDuration: nominal,
        precision: "approximate",
        startFrame,
        startMediaTime,
        targetMediaTime,
      };
    }
    return {
      frameDuration: null,
      precision: "unsupported",
      startFrame,
      startMediaTime,
      targetMediaTime: startMediaTime,
    };
  }

  private exactPlan(
    startFrame: PresentedFrame,
    target: AdjacentFrameTarget,
  ): ExactFramePlan {
    return {
      frameDuration: target.frameDuration,
      precision: "exact-verified",
      startFrame,
      startMediaTime: startFrame.mediaTime,
      target,
      targetMediaTime: target.mediaTime,
    };
  }

  private readExactTarget(
    context: FrameContext,
    startFrame: PresentedFrame | null,
  ): AdjacentFrameTarget | null {
    if (
      this.facet === undefined ||
      context.snapshot.capabilities.framePresentation !== "exact" ||
      startFrame === null
    ) {
      return null;
    }
    if (!isCurrentFrame(startFrame, context.command)) {
      return null;
    }
    const outcome = invokeSynchronously(() =>
      this.facet?.getAdjacentFrameTarget(
        startFrame,
        context.direction,
        context.command,
      ),
    );
    const target = outcome.kind === "completed" ? outcome.value : null;
    return target !== undefined && isValidTarget(target) ? target : null;
  }

  private executePlan(
    context: FrameContext,
    plan: FramePlan,
  ): Promise<FrameStepResult> {
    if (plan.precision === "unsupported") {
      return Promise.resolve(
        this.publishPlanResult(
          context,
          plan,
          "unsupported",
          plan.startMediaTime,
          0,
          null,
        ),
      );
    }
    const targetTime = clampToRanges(plan.targetMediaTime, context.ranges);
    if (
      targetTime === plan.startMediaTime &&
      targetTime !== plan.targetMediaTime
    ) {
      return Promise.resolve(
        this.publishPlanResult(
          context,
          plan,
          "completed",
          targetTime,
          0,
          null,
          true,
        ),
      );
    }
    if (targetTime === plan.startMediaTime && isBoundary(context, targetTime)) {
      return Promise.resolve(
        this.publishPlanResult(
          context,
          plan,
          "completed",
          targetTime,
          0,
          null,
          true,
        ),
      );
    }
    return plan.precision === "exact-verified"
      ? this.executeExact(context, plan, targetTime)
      : this.executeApproximate(context, plan, targetTime);
  }

  private async executeApproximate(
    context: FrameContext,
    plan: ApproximateFramePlan,
    targetTime: number,
  ): Promise<FrameStepResult> {
    const outcome = await this.seek(
      context.command,
      plan.targetMediaTime,
      targetTime,
    );
    if (outcome.kind !== "completed") {
      return this.finishPlanOperation(context, plan, outcome, 0);
    }
    const result = outcome.value;
    const confirmed =
      result.status === "completed"
        ? result.confirmedTime
        : plan.startMediaTime;
    return this.publishPlanResult(
      context,
      plan,
      result.status,
      confirmed,
      0,
      null,
      result.clamped,
      result.error,
    );
  }

  private async executeExact(
    context: FrameContext,
    plan: ExactFramePlan,
    targetTime: number,
  ): Promise<FrameStepResult> {
    let lastResult: ExactFrameResult | null = null;
    let seekTargetTime = targetTime;
    for (let attempt = 0; attempt <= MAX_CORRECTIONS; attempt += 1) {
      const result = await this.exactAttempt(context, plan, seekTargetTime);
      if (result.kind !== "frame") {
        return this.finishExactAttempt(context, plan, result, attempt);
      }
      lastResult = result;
      if (result.valid) {
        return this.publishExactSuccess(context, plan, result, attempt);
      }
      seekTargetTime = this.correctionTarget(context, plan, result.frame);
    }
    return this.publishVerificationFailure(context, plan, lastResult);
  }

  private async exactAttempt(
    context: FrameContext,
    plan: ExactFramePlan,
    targetTime: number,
  ): Promise<ExactAttemptResult> {
    const seekOutcome = await this.seek(
      context.command,
      targetTime,
      targetTime,
    );
    if (
      seekOutcome.kind !== "completed" ||
      seekOutcome.value.status !== "completed"
    ) {
      return { kind: "seek", outcome: seekOutcome };
    }
    const facet = this.facet;
    if (facet === undefined) {
      return {
        kind: "wait",
        outcome: { error: new Error("缺少帧呈现分面"), kind: "failed" },
      };
    }
    const frameOutcome = await this.host.runOperation(
      context.command.requestId,
      () => facet.waitForPresentedFrame(context.command),
    );
    if (frameOutcome.kind !== "completed") {
      return { kind: "wait", outcome: frameOutcome };
    }
    if (!isCurrentFrame(frameOutcome.value, context.command)) {
      return {
        kind: "wait",
        outcome: { kind: "controlled", status: "superseded" },
      };
    }
    return this.verifyPresentedFrame(context, plan, frameOutcome.value);
  }

  private verifyPresentedFrame(
    context: FrameContext,
    plan: ExactFramePlan,
    frame: PresentedFrame,
  ): ExactFrameResult {
    const approximate = verifyApproximateFrame(plan, frame, context.direction);
    if (frame.sampleSource !== "video-frame-callback") {
      return { frame, kind: "frame", precision: "approximate", ...approximate };
    }
    const exact = verifyExactFrame(
      plan.startFrame as PresentedFrame,
      plan.target,
      frame,
      context.direction,
    );
    if (!exact.identityAvailable) {
      return { frame, kind: "frame", precision: "approximate", ...approximate };
    }
    return {
      frame,
      kind: "frame",
      precision: "exact-verified",
      timestampError: exact.timestampError,
      valid: exact.valid,
    };
  }

  private correctionTarget(
    context: FrameContext,
    plan: ExactFramePlan,
    frame: PresentedFrame,
  ): number {
    const correction = correctionDirection(
      plan.target,
      frame,
      context.direction,
    );
    const requestedTime =
      plan.targetMediaTime + correction * plan.target.frameDuration * 0.5;
    return clampToRanges(requestedTime, context.ranges);
  }

  private finishExactAttempt(
    context: FrameContext,
    plan: ExactFramePlan,
    result: ExactOperationResult,
    correctionCount: number,
  ): FrameStepResult {
    if (result.outcome.kind !== "completed") {
      return this.finishPlanOperation(
        context,
        plan,
        result.outcome,
        correctionCount,
      );
    }
    const seekResult = result.outcome.value;
    return this.publishPlanResult(
      context,
      plan,
      seekResult.status,
      seekResult.confirmedTime,
      correctionCount,
      null,
      seekResult.clamped,
      seekResult.error,
    );
  }

  private publishExactSuccess(
    context: FrameContext,
    plan: ExactFramePlan,
    result: ExactFrameResult,
    correctionCount: number,
  ): FrameStepResult {
    const completed = this.resultFromPlan(
      context,
      plan,
      "completed",
      result.frame.mediaTime,
      correctionCount,
      result.timestampError,
    );
    const normalized = {
      ...completed,
      ...frameIdentity("confirmed", result.frame),
      precision: result.precision,
    };
    return this.applyAndPublish(context.command, normalized);
  }

  private publishVerificationFailure(
    context: FrameContext,
    plan: ExactFramePlan,
    lastResult: ExactFrameResult | null,
  ): FrameStepResult {
    const error: PlaybackError = {
      category: "media",
      code: "FRAME_STEP_VERIFICATION_FAILED",
      message: "逐帧校验失败",
    };
    const confirmed = lastResult?.frame.mediaTime ?? plan.startMediaTime;
    const timestampError = lastResult?.timestampError ?? null;
    const result = this.resultFromPlan(
      context,
      plan,
      "failed",
      confirmed,
      MAX_CORRECTIONS,
      timestampError,
      error,
    );
    const normalized = normalizeVerificationFailure(result, lastResult);
    return this.applyAndPublish(context.command, normalized);
  }

  private finishPlanOperation(
    context: FrameContext,
    plan: FramePlan,
    outcome: FrameOperationOutcome<unknown>,
    correctionCount: number,
  ): FrameStepResult {
    if (outcome.kind === "controlled") {
      return this.publishPlanResult(
        context,
        plan,
        outcome.status,
        plan.startMediaTime,
        correctionCount,
        null,
      );
    }
    const error =
      outcome.kind === "failed"
        ? this.host.normalizeError(outcome.error)
        : undefined;
    const status = error?.category === "unsupported" ? "unsupported" : "failed";
    return this.publishPlanResult(
      context,
      plan,
      status,
      plan.startMediaTime,
      correctionCount,
      null,
      false,
      error,
    );
  }

  private publishPlanResult(
    context: FrameContext,
    plan: FramePlan,
    status: PlaybackCompletionStatus,
    confirmedMediaTime: number,
    correctionCount: number,
    timestampError: number | null,
    clamped = plan.targetMediaTime !== confirmedMediaTime,
    error?: PlaybackError,
  ): FrameStepResult {
    const result = this.resultFromPlan(
      context,
      plan,
      status,
      confirmedMediaTime,
      correctionCount,
      timestampError,
      error,
      clamped,
    );
    return this.applyAndPublish(context.command, result);
  }

  private resultFromPlan(
    context: FrameContext,
    plan: FramePlan,
    status: PlaybackCompletionStatus,
    confirmedMediaTime: number,
    correctionCount: number,
    timestampError: number | null,
    error?: PlaybackError,
    clamped = plan.targetMediaTime !== confirmedMediaTime,
  ): FrameStepResult {
    return createFrameResult({
      clamped,
      confirmedMediaTime,
      context,
      correctionCount,
      plan,
      status,
      timestampError,
      ...(error === undefined ? {} : { error }),
    });
  }

  private applyAndPublish(
    command: PlaybackCommandContext,
    result: FrameStepResult,
  ): FrameStepResult {
    this.host.applyFrameResult(command, result);
    return this.host.publishFrameResult(result, command);
  }

  private seek(
    command: PlaybackCommandContext,
    requestedTime: number,
    targetTime: number,
  ): Promise<FrameOperationOutcome<SeekResult>> {
    const request: SeekRequest = {
      ...command,
      boundaryPolicy: "clamp",
      reason: "step",
      requestedTime,
      targetTime,
    };
    return this.host.runOperation(command.requestId, () =>
      this.backend.seek(request),
    );
  }

  private readSnapshot(): PlaybackSnapshot | Error {
    const outcome = invokeSynchronously(() => this.backend.getSnapshot());
    return outcome.kind === "completed"
      ? outcome.value
      : errorFromUnknown(outcome.error);
  }

  private readCurrentFrame(
    command: PlaybackCommandContext,
  ): PresentedFrame | null {
    if (this.facet === undefined) {
      return null;
    }
    const outcome = invokeSynchronously(() =>
      this.facet?.getCurrentPresentedFrame(command),
    );
    return outcome.kind === "completed" ? (outcome.value ?? null) : null;
  }

  private readNominalDuration(command: PlaybackCommandContext): number | null {
    if (this.facet === undefined) {
      return null;
    }
    const outcome = invokeSynchronously(() =>
      this.facet?.getNominalFrameDuration(command),
    );
    return outcome.kind === "completed" && isPositiveDuration(outcome.value)
      ? outcome.value
      : null;
  }

  private publishFailure(
    identity: FrameCommandIdentity,
    direction: FrameStepDirection,
    cause: unknown,
  ): FrameStepResult {
    const error = this.host.normalizeError(cause);
    const status = error.category === "unsupported" ? "unsupported" : "failed";
    const result = basicFrameResult(
      identity,
      direction,
      status,
      "unsupported",
      0,
      error,
    );
    if (identity.sourceId !== null) {
      this.host.applyFrameResult(requireCommand(identity), result);
    }
    return this.host.publishFrameResult(result, identity);
  }

  private finishOperationFailure(
    command: PlaybackCommandContext,
    direction: FrameStepDirection,
    mediaTime: number,
    outcome: FrameOperationOutcome<unknown>,
  ): FrameStepResult {
    if (outcome.kind === "controlled") {
      return this.publishBasic(
        command,
        direction,
        outcome.status,
        "unsupported",
        mediaTime,
      );
    }
    return this.publishFailure(
      command,
      direction,
      outcome.kind === "failed" ? outcome.error : new Error("逐帧暂停失败"),
    );
  }

  private publishBasic(
    identity: FrameCommandIdentity,
    direction: FrameStepDirection,
    status: PlaybackCompletionStatus,
    precision: FrameStepPrecision,
    mediaTime = 0,
  ): FrameStepResult {
    return this.host.publishFrameResult(
      basicFrameResult(identity, direction, status, precision, mediaTime),
      identity,
    );
  }
}

type ExactAttemptResult = ExactFrameResult | ExactOperationResult;

interface ExactFrameResult {
  readonly frame: PresentedFrame;
  readonly kind: "frame";
  readonly precision: "approximate" | "exact-verified";
  readonly timestampError: number | null;
  readonly valid: boolean;
}

type ExactOperationResult =
  | {
      readonly kind: "seek";
      readonly outcome: FrameOperationOutcome<SeekResult>;
    }
  | {
      readonly kind: "wait";
      readonly outcome: Exclude<
        FrameOperationOutcome<PresentedFrame>,
        { readonly kind: "completed" }
      >;
    };

function correctionDirection(
  target: AdjacentFrameTarget,
  actual: PresentedFrame,
  stepDirection: FrameStepDirection,
): 1 | -1 {
  if (
    target.sourceFrameIndex !== undefined &&
    actual.sourceFrameIndex !== undefined
  ) {
    if (actual.sourceFrameIndex < target.sourceFrameIndex) return 1;
    if (actual.sourceFrameIndex > target.sourceFrameIndex) return -1;
  }
  if (actual.mediaTime < target.mediaTime) return 1;
  if (actual.mediaTime > target.mediaTime) return -1;
  return directionSign(stepDirection);
}

function verifyApproximateFrame(
  plan: ExactFramePlan,
  frame: PresentedFrame,
  direction: FrameStepDirection,
): Pick<ExactFrameResult, "timestampError" | "valid"> {
  const directionDelta =
    (frame.mediaTime - plan.startMediaTime) * directionSign(direction);
  const timestampError = Math.abs(frame.mediaTime - plan.targetMediaTime);
  const validDirection = Number.isFinite(directionDelta) && directionDelta > 0;
  const withinTolerance =
    Number.isFinite(timestampError) &&
    timestampError <= plan.target.frameDuration + Number.EPSILON;
  return { timestampError, valid: validDirection && withinTolerance };
}

function normalizeVerificationFailure(
  result: FrameStepResult,
  lastResult: ExactFrameResult | null,
): FrameStepResult {
  if (lastResult === null) {
    return result;
  }
  return {
    ...result,
    ...frameIdentity("confirmed", lastResult.frame),
    precision: lastResult.precision,
  };
}

function createFrameResult(input: {
  readonly clamped: boolean;
  readonly confirmedMediaTime: number;
  readonly context: FrameContext;
  readonly correctionCount: number;
  readonly error?: PlaybackError;
  readonly plan: FramePlan;
  readonly status: PlaybackCompletionStatus;
  readonly timestampError: number | null;
}): FrameStepResult {
  const { context, plan } = input;
  return {
    clamped: input.clamped,
    confirmedMediaTime: input.confirmedMediaTime,
    correctionCount: input.correctionCount,
    direction: context.direction,
    frameDuration: plan.frameDuration,
    precision: plan.precision,
    requestId: context.command.requestId,
    startMediaTime: plan.startMediaTime,
    status: input.status,
    targetMediaTime: plan.targetMediaTime,
    timestampError: input.timestampError,
    ...frameIdentity("start", plan.startFrame),
    ...targetIdentity(plan),
    ...(input.error === undefined ? {} : { error: input.error }),
  };
}

function basicFrameResult(
  identity: FrameCommandIdentity,
  direction: FrameStepDirection,
  status: PlaybackCompletionStatus,
  precision: FrameStepPrecision,
  mediaTime: number,
  error?: PlaybackError,
): FrameStepResult {
  return {
    clamped: false,
    confirmedMediaTime: mediaTime,
    correctionCount: 0,
    direction,
    frameDuration: null,
    precision,
    requestId: identity.requestId,
    startMediaTime: mediaTime,
    status,
    targetMediaTime: mediaTime,
    timestampError: null,
    ...(error === undefined ? {} : { error }),
  };
}

function frameIdentity(
  prefix: "confirmed" | "start",
  frame: PresentedFrame | null,
): Record<string, number | string> {
  if (frame === null) {
    return {};
  }
  const indexKey = `${prefix}SourceFrameIndex`;
  const idKey = `${prefix}StableFrameId`;
  return {
    ...(frame.sourceFrameIndex === undefined
      ? {}
      : { [indexKey]: frame.sourceFrameIndex }),
    ...(frame.stableFrameId === undefined
      ? {}
      : { [idKey]: frame.stableFrameId }),
  };
}

function targetIdentity(plan: FramePlan): Record<string, number | string> {
  if (plan.precision !== "exact-verified") {
    return {};
  }
  return {
    ...(plan.target.sourceFrameIndex === undefined
      ? {}
      : { targetSourceFrameIndex: plan.target.sourceFrameIndex }),
    ...(plan.target.stableFrameId === undefined
      ? {}
      : { targetStableFrameId: plan.target.stableFrameId }),
  };
}

function isBoundary(context: FrameContext, mediaTime: number): boolean {
  const boundary =
    context.direction === "next"
      ? context.ranges.at(-1)?.end
      : context.ranges[0]?.start;
  return boundary === mediaTime;
}

function isFrameStepState(state: PlaybackSnapshot["state"]): boolean {
  return (
    state === "paused" ||
    state === "playing" ||
    state === "ready" ||
    state === "ended"
  );
}

function isPauseConfirmed(
  snapshot: PlaybackSnapshot,
  command: PlaybackCommandContext,
): boolean {
  return (
    snapshot.state === "paused" &&
    snapshot.requestId === command.requestId &&
    snapshot.sourceEpoch === command.sourceEpoch &&
    snapshot.sourceId === command.sourceId
  );
}

function isCurrentFrame(
  frame: PresentedFrame,
  command: PlaybackCommandContext,
): boolean {
  return (
    frame.sourceEpoch === command.sourceEpoch &&
    frame.sourceId === command.sourceId
  );
}

function isValidTarget(
  target: AdjacentFrameTarget | null,
): target is AdjacentFrameTarget {
  return (
    target !== null &&
    Number.isFinite(target.mediaTime) &&
    isPositiveDuration(target.frameDuration)
  );
}

function finiteMediaTime(
  frameTime: number | undefined,
  snapshotTime: number,
): number {
  if (frameTime !== undefined && Number.isFinite(frameTime)) {
    return frameTime;
  }
  return Number.isFinite(snapshotTime) ? snapshotTime : 0;
}

function requireCommand(
  identity: FrameCommandIdentity,
): PlaybackCommandContext {
  if (identity.sourceId === null) {
    throw new Error("播放源上下文缺失");
  }
  return { ...identity, sourceId: identity.sourceId };
}

function errorFromUnknown(cause: unknown): Error {
  return cause instanceof Error ? cause : new Error("逐帧读取快照失败");
}

function invokeSynchronously<T>(
  operation: () => T,
):
  | { readonly kind: "completed"; readonly value: T }
  | { readonly error: unknown; readonly kind: "failed" } {
  try {
    return { kind: "completed", value: operation() };
  } catch (error: unknown) {
    return { error, kind: "failed" };
  }
}
