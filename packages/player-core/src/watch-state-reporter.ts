import type {
  WatchStatePlaybackContext,
  WatchStateReport,
  WatchStateReportInput,
  WatchStateSendResult,
  WatchStateSnapshot,
  WatchStateTransport,
} from './types';

interface WatchStateReporterOptions {
  readonly getPlaybackState: () => WatchStatePlaybackContext;
  readonly initialState: WatchStateSnapshot;
  readonly sessionId: string;
  readonly transport: WatchStateTransport;
}

interface PendingReport {
  readonly input: WatchStateReportInput;
  readonly keepalive: boolean;
  readonly retried: boolean;
}

export class WatchStateReporter {
  private closed = false;
  private disposed = false;
  private inFlight: Promise<void> | null = null;
  private nextEventSeq = 1;
  private readonly options: WatchStateReporterOptions;
  private pending: PendingReport | null = null;
  private state: WatchStateSnapshot;

  constructor(options: WatchStateReporterOptions) {
    this.options = options;
    this.state = options.initialState;
  }

  report(input: WatchStateReportInput, options?: { readonly keepalive?: boolean }): void {
    if (this.closed || this.disposed) return;
    this.enqueue({ input, keepalive: options?.keepalive === true, retried: false });
  }

  close(input?: WatchStateReportInput): void {
    if (this.closed || this.disposed) return;
    this.closed = true;
    this.pending = null;
    if (input !== undefined && this.inFlight === null) {
      this.start({ input, keepalive: true, retried: false });
    }
  }

  dispose(): void {
    this.disposed = true;
    this.closed = true;
    this.pending = null;
  }

  getState(): WatchStateSnapshot {
    return this.state;
  }

  async idle(): Promise<void> {
    while (this.inFlight !== null) {
      await this.inFlight;
    }
  }

  private enqueue(report: PendingReport): void {
    if (this.inFlight === null) {
      this.start(report);
      return;
    }
    this.pending = mergePending(this.pending, report);
  }

  private start(report: PendingReport): void {
    const event = this.createEvent(report.input);
    const operation = this.send(event, report);
    this.inFlight = operation;
  }

  private async send(event: WatchStateReport, report: PendingReport): Promise<void> {
    try {
      const options = report.keepalive ? { keepalive: true } : undefined;
      const result = await this.options.transport.send(event, options);
      if (!this.disposed) this.acceptResult(result);
      if (this.shouldRetry(result, report)) {
        this.pending = mergePending(this.pending, this.retryReport(report));
      }
    } catch {
      // 网络失败不建立离线队列，后续新事件仍可继续上报。
    } finally {
      this.inFlight = null;
      if (!this.closed && !this.disposed && this.pending !== null) {
        const next = this.pending;
        this.pending = null;
        this.start(next);
      }
    }
  }

  private createEvent(input: WatchStateReportInput): WatchStateReport {
    return {
      ...input,
      eventSeq: this.nextEventSeq++,
      expectedRevision: this.state.revision,
      sessionId: this.options.sessionId,
    };
  }

  private acceptResult(result: WatchStateSendResult): void {
    this.state = result.current;
  }

  private shouldRetry(result: WatchStateSendResult, report: PendingReport): boolean {
    if (
      result.kind !== 'conflict' ||
      report.retried ||
      this.closed ||
      this.disposed ||
      this.pending !== null
    ) {
      return false;
    }
    const playback = this.options.getPlaybackState();
    return playback.foreground && (playback.playing || isActiveUserSeek(report.input));
  }

  private retryReport(report: PendingReport): PendingReport {
    const playback = this.options.getPlaybackState();
    return {
      input: {
        ...(playback.durationSeconds === undefined
          ? {}
          : { durationSeconds: playback.durationSeconds }),
        eventType: report.input.eventType,
        positionSeconds: playback.positionSeconds,
        reason: report.input.reason,
      },
      keepalive: report.keepalive,
      retried: true,
    };
  }
}

function isActiveUserSeek(input: WatchStateReportInput): boolean {
  return input.eventType === 'seek' && input.reason === 'user';
}

function mergePending(current: PendingReport | null, next: PendingReport): PendingReport {
  if (current === null || next.input.eventType === 'ended') return next;
  if (current.input.eventType === 'ended') return current;
  if (next.input.eventType === 'progress' && current.input.eventType !== 'progress') return current;
  return next;
}
