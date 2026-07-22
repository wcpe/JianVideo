import type { PlaybackTrack, TrackSelectionState } from '@jianvideo/player-core';
import type { SubtitleTrack } from '@/types';
import client from './client';

const useMock = import.meta.env.VITE_USE_MOCK === 'true';

type TrackKind = 'audio' | 'subtitle';
type TrackSource = 'sidecar' | 'uploaded' | 'embedded' | 'derived';
type TrackCapability = 'seamless' | 'reload' | 'unsupported';

interface TrackDto {
  id: string;
  kind: TrackKind;
  label: string;
  source?: TrackSource;
  format?: string;
  codec?: string;
  language?: string;
  title?: string;
  channels?: number;
  channel_layout?: string;
  is_default?: boolean;
  is_forced?: boolean;
  available?: boolean;
  capability?: TrackCapability;
  unsupported_reason?: string;
  stream_index?: number;
}

interface SelectionDto {
  selected_track_id: string | null;
  effective_track_id: string | null;
}

interface CapabilityDto {
  available: boolean;
  capability: string;
  unsupported_reason?: string;
}

interface TrackResponseDto {
  tracks: TrackDto[];
  selection: Record<TrackKind, SelectionDto>;
  sources: Record<string, CapabilityDto>;
  backend: Record<string, CapabilityDto>;
}

export interface WebPlaybackTrack extends PlaybackTrack {
  readonly channels?: number;
  readonly channelLayout?: string;
}

export interface WebTrackCapability {
  readonly available: boolean;
  readonly capability: string;
  readonly unsupportedReason?: string;
}

export interface WebTrackSelection {
  readonly selectedTrackId: string | null;
  readonly effectiveTrackId: string | null;
}

export interface TrackResponse {
  readonly tracks: readonly WebPlaybackTrack[];
  readonly selection: Readonly<Record<TrackKind, WebTrackSelection>>;
  readonly sources: Readonly<Record<string, WebTrackCapability>>;
  readonly backend: Readonly<Record<string, WebTrackCapability>>;
}

export async function realGetTracks(mediaId: number, signal?: AbortSignal): Promise<TrackResponse> {
  const response = await client.get<TrackResponseDto>(`/api/play/${mediaId}/tracks`, { signal });
  return mapResponse(response.data);
}

export async function realGetTrackContent(
  mediaId: number,
  trackId: string,
  signal?: AbortSignal,
): Promise<string> {
  const encodedTrackId = encodeURIComponent(trackId);
  const response = await client.get<string>(
    `/api/play/${mediaId}/subtitles/${encodedTrackId}/content`,
    { responseType: 'text', signal },
  );
  return response.data;
}

export async function realUploadSubtitle(mediaId: number, file: File): Promise<WebPlaybackTrack> {
  const form = new FormData();
  form.append('file', file);
  const response = await client.post<TrackDto>(`/api/play/${mediaId}/subtitles`, form);
  return mapTrack(response.data);
}

export async function realDeleteSubtitle(mediaId: number, trackId: string): Promise<void> {
  await client.delete(`/api/play/${mediaId}/subtitles/${encodeURIComponent(trackId)}`);
}

const mockResponse: TrackResponse = {
  tracks: [
    {
      available: true,
      capability: 'seamless',
      format: 'vtt',
      id: 'mock-subtitle-1',
      kind: 'subtitle',
      label: '测试字幕',
      source: 'sidecar',
    },
  ],
  selection: {
    audio: { effectiveTrackId: null, selectedTrackId: null },
    subtitle: { effectiveTrackId: null, selectedTrackId: null },
  },
  sources: {},
  backend: {},
};

const mockVtt = `WEBVTT

00:00:01.000 --> 00:00:03.000
这是第一条测试字幕

00:00:04.000 --> 00:00:06.000
这是第二条测试字幕
`;

export function getTracks(mediaId: number, signal?: AbortSignal): Promise<TrackResponse> {
  return useMock ? Promise.resolve(mockResponse) : realGetTracks(mediaId, signal);
}

export function getTrackContent(
  mediaId: number,
  trackId: string,
  signal?: AbortSignal,
): Promise<string> {
  return useMock ? Promise.resolve(mockVtt) : realGetTrackContent(mediaId, trackId, signal);
}

export function uploadSubtitle(mediaId: number, file: File): Promise<WebPlaybackTrack> {
  return useMock
    ? Promise.resolve(mockResponse.tracks[0] as WebPlaybackTrack)
    : realUploadSubtitle(mediaId, file);
}

export function deleteSubtitle(mediaId: number, trackId: string): Promise<void> {
  return useMock ? Promise.resolve() : realDeleteSubtitle(mediaId, trackId);
}

export async function realGetSubtitles(mediaId: number): Promise<SubtitleTrack[]> {
  return legacySubtitles(await realGetTracks(mediaId), mediaId);
}

export async function getSubtitles(mediaId: number): Promise<SubtitleTrack[]> {
  return legacySubtitles(await getTracks(mediaId), mediaId);
}

export async function realGetSubtitleContent(mediaId: number, index: number): Promise<string> {
  const track = (await realGetTracks(mediaId)).tracks.filter((item) => item.kind === 'subtitle')[
    index
  ];
  if (!track) throw new Error('字幕轨道不存在');
  return realGetTrackContent(mediaId, track.id);
}

export async function getSubtitleContent(mediaId: number, index: number): Promise<string> {
  const track = (await getTracks(mediaId)).tracks.filter((item) => item.kind === 'subtitle')[index];
  if (!track) throw new Error('字幕轨道不存在');
  return getTrackContent(mediaId, track.id);
}

export function selectionState(
  response: TrackResponse,
  kind: TrackKind,
  sourceEpoch: number,
  sourceId: string,
): TrackSelectionState {
  return { ...response.selection[kind], kind, sourceEpoch, sourceId };
}

function mapResponse(dto: TrackResponseDto): TrackResponse {
  return {
    tracks: dto.tracks.map(mapTrack),
    selection: {
      audio: mapSelection(dto.selection.audio),
      subtitle: mapSelection(dto.selection.subtitle),
    },
    sources: mapCapabilities(dto.sources),
    backend: mapCapabilities(dto.backend),
  };
}

function mapTrack(dto: TrackDto): WebPlaybackTrack {
  return {
    id: String(dto.id),
    kind: dto.kind,
    label: dto.label,
    available: dto.available,
    capability: dto.capability,
    codec: dto.codec,
    default: dto.is_default,
    forced: dto.is_forced,
    format: dto.format,
    language: dto.language,
    source: dto.source,
    streamIndex: dto.stream_index,
    title: dto.title,
    unsupportedReason: dto.unsupported_reason,
    channels: dto.channels,
    channelLayout: dto.channel_layout,
  };
}

function mapSelection(dto: SelectionDto): WebTrackSelection {
  return {
    selectedTrackId: dto.selected_track_id === null ? null : String(dto.selected_track_id),
    effectiveTrackId: dto.effective_track_id === null ? null : String(dto.effective_track_id),
  };
}

function mapCapabilities(
  values: Record<string, CapabilityDto>,
): Record<string, WebTrackCapability> {
  return Object.fromEntries(
    Object.entries(values).map(([key, value]) => [
      key,
      {
        available: value.available,
        capability: value.capability,
        unsupportedReason: value.unsupported_reason,
      },
    ]),
  );
}

function legacySubtitles(response: TrackResponse, mediaId: number): SubtitleTrack[] {
  return response.tracks
    .filter((track) => track.kind === 'subtitle')
    .map((track, index) => ({
      index,
      file_name: track.label,
      format: track.format ?? '',
      url: `/api/play/${mediaId}/subtitles/${encodeURIComponent(track.id)}/content`,
    }));
}
