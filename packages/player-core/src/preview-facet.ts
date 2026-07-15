import type {
  PlaybackCommandContext,
  PreparedPreviewCue,
  PreparedPreviewTrack,
  PreviewFacet,
  PreviewHit,
  PreviewTrackState,
} from './types';

const EMPTY_STATE: PreviewTrackState = {
  generationId: null,
  mediaId: null,
  profileId: null,
  requestId: 0,
  sourceEpoch: 0,
  sourceId: null,
  status: 'empty',
};

export class PreviewTrackValidationError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'PreviewTrackValidationError';
  }
}

export class PreparedPreviewFacet implements PreviewFacet {
  private state: PreviewTrackState = EMPTY_STATE;
  private track: PreparedPreviewTrack | null = null;

  setTrack(track: PreparedPreviewTrack | null, command: PlaybackCommandContext): PreviewTrackState {
    if (isStaleCommand(command, this.state)) {
      return this.getState();
    }
    const prepared = track === null ? null : validateAndCopyTrack(track);
    this.track = prepared;
    this.state = prepared === null ? emptyState(command) : readyState(prepared, command);
    return this.getState();
  }

  hitTest(mediaTime: number, command: PlaybackCommandContext): PreviewHit | null {
    const track = this.track;
    if (!Number.isFinite(mediaTime) || track === null || !matchesContext(command, this.state)) {
      return null;
    }
    const cue = findCue(track.cues, mediaTime);
    return cue === null ? null : createHit(cue, track);
  }

  getState(): PreviewTrackState {
    return Object.freeze({ ...this.state });
  }
}

export function createPreviewFacet(): PreviewFacet {
  return new PreparedPreviewFacet();
}

function validateAndCopyTrack(track: PreparedPreviewTrack): PreparedPreviewTrack {
  validateTrackFields(track);
  const cues: PreparedPreviewCue[] = [];
  let previousEnd = 0;
  for (const cue of track.cues) {
    validateCue(cue, previousEnd);
    cues.push(copyCue(cue));
    previousEnd = cue.endTime;
  }
  return { ...track, cues };
}

function validateTrackFields(track: PreparedPreviewTrack): void {
  const fields = [track.generationId, track.mediaId, track.profileId, track.sourceFingerprint];
  if (fields.some((field) => !isNonEmpty(field))) {
    throw new PreviewTrackValidationError('预览轨道字段不能为空');
  }
  if (track.cues.length === 0) {
    throw new PreviewTrackValidationError('预览轨道片段不能为空');
  }
}

function validateCue(cue: PreparedPreviewCue, previousEnd: number): void {
  const validTimes = Number.isFinite(cue.startTime) && Number.isFinite(cue.endTime);
  if (!validTimes || cue.startTime < 0 || cue.endTime <= cue.startTime) {
    throw new PreviewTrackValidationError('预览片段时间范围无效');
  }
  if (cue.startTime < previousEnd) {
    throw new PreviewTrackValidationError('预览片段必须严格有序且不重叠');
  }
  validateSprite(cue);
}

function validateSprite(cue: PreparedPreviewCue): void {
  const { assetId, height, width, x, y } = cue.sprite;
  const invalidSize = [height, width].some((value) => !Number.isFinite(value) || value <= 0);
  const invalidPosition = [x, y].some((value) => !Number.isFinite(value) || value < 0);
  if (!isNonEmpty(assetId) || invalidSize || invalidPosition) {
    throw new PreviewTrackValidationError('预览精灵字段无效');
  }
}

function copyCue(cue: PreparedPreviewCue): PreparedPreviewCue {
  return { ...cue, sprite: { ...cue.sprite } };
}

function isNonEmpty(value: string): boolean {
  return typeof value === 'string' && value.trim().length > 0;
}

function isStaleCommand(command: PlaybackCommandContext, state: PreviewTrackState): boolean {
  if (command.sourceEpoch !== state.sourceEpoch) {
    return command.sourceEpoch < state.sourceEpoch;
  }
  if (state.sourceId !== null && command.sourceId !== state.sourceId) {
    return true;
  }
  return command.requestId < state.requestId;
}

function matchesContext(command: PlaybackCommandContext, state: PreviewTrackState): boolean {
  return (
    command.sourceId === state.sourceId &&
    command.sourceEpoch === state.sourceEpoch &&
    command.requestId >= state.requestId
  );
}

function findCue(cues: readonly PreparedPreviewCue[], mediaTime: number): PreparedPreviewCue | null {
  let low = 0;
  let high = cues.length - 1;
  while (low <= high) {
    const middle = Math.floor((low + high) / 2);
    const cue = cues[middle];
    if (cue === undefined) {
      return null;
    }
    if (mediaTime < cue.startTime) {
      high = middle - 1;
    } else if (mediaTime >= cue.endTime) {
      low = middle + 1;
    } else {
      return cue;
    }
  }
  return null;
}

function createHit(cue: PreparedPreviewCue, track: PreparedPreviewTrack): PreviewHit {
  return {
    ...copyCue(cue),
    generationId: track.generationId,
    profileId: track.profileId,
  };
}

function emptyState(command: PlaybackCommandContext): PreviewTrackState {
  return {
    ...EMPTY_STATE,
    requestId: command.requestId,
    sourceEpoch: command.sourceEpoch,
    sourceId: command.sourceId,
  };
}

function readyState(track: PreparedPreviewTrack, command: PlaybackCommandContext): PreviewTrackState {
  return {
    generationId: track.generationId,
    mediaId: track.mediaId,
    profileId: track.profileId,
    requestId: command.requestId,
    sourceEpoch: command.sourceEpoch,
    sourceId: command.sourceId,
    status: 'ready',
  };
}
