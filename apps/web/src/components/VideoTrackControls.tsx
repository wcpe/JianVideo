import { useRef } from 'react';
import type { CSSProperties } from 'react';
import type { PlaybackTrack, TrackKind, TrackSelectionState } from '@jianvideo/player-core';
import { ActionIcon, Box, Menu, Text, VisuallyHidden } from '@mantine/core';
import { IconCheck, IconMusic, IconSubtitles, IconUpload } from '@tabler/icons-react';
import type { WebPlaybackTrack } from '@/api/subtitle';
import type { SubtitleEntry } from '@/types';
import {
  BACKGROUND_OPACITIES,
  FONT_SIZES,
  SUBTITLE_COLORS,
  VERTICAL_POSITIONS,
  saveSubtitlePreferences,
  type SubtitlePreferences,
} from './VideoTrackControls.helpers';

export type { SubtitlePreferences } from './VideoTrackControls.helpers';

export interface TrackSelections {
  readonly audio: TrackSelectionState;
  readonly subtitle: TrackSelectionState;
}

interface VideoTrackControlsProps {
  readonly tracks: readonly PlaybackTrack[];
  readonly selections: TrackSelections;
  readonly preferences: SubtitlePreferences;
  readonly onPreferencesChange: (preferences: SubtitlePreferences) => void;
  readonly onSelect: (kind: TrackKind, trackId: string | null) => void;
  readonly onUpload: (file: File) => void;
  readonly onDelete: (trackId: string) => void;
}

interface SubtitleOverlayProps {
  readonly currentTime: number;
  readonly cues: readonly SubtitleEntry[];
  readonly preferences: SubtitlePreferences;
}

export function SubtitleOverlay({ currentTime, cues, preferences }: SubtitleOverlayProps) {
  const cue = cues.find(
    (candidate) => currentTime >= candidate.start && currentTime < candidate.end,
  );
  if (!cue) return null;
  return (
    <Box data-testid="subtitle-overlay" style={overlayStyle(preferences)}>
      <Text component="span" ta="center" style={subtitleTextStyle(preferences)}>
        {cue.text}
      </Text>
    </Box>
  );
}

export default function VideoTrackControls(props: VideoTrackControlsProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const subtitles = props.tracks.filter((track) => track.kind === 'subtitle');
  const audio = props.tracks.filter((track) => track.kind === 'audio');
  return (
    <>
      <SubtitleMenu {...props} inputRef={inputRef} tracks={subtitles} />
      <AudioMenu onSelect={props.onSelect} selection={props.selections.audio} tracks={audio} />
      <UploadInput inputRef={inputRef} onUpload={props.onUpload} />
    </>
  );
}

function SubtitleMenu(
  props: VideoTrackControlsProps & {
    readonly inputRef: React.RefObject<HTMLInputElement | null>;
    readonly tracks: readonly PlaybackTrack[];
  },
) {
  const update = (patch: Partial<SubtitlePreferences>) => updatePreferences(props, patch);
  return (
    <Menu position="top" withinPortal>
      <TrackMenuTarget label="字幕轨道" icon={<IconSubtitles size={18} />} />
      <Menu.Dropdown>
        <Menu.Label>字幕</Menu.Label>
        <Menu.Item
          leftSection={
            props.selections.subtitle.effectiveTrackId === null ? (
              <IconCheck size={14} />
            ) : undefined
          }
          onClick={() => props.onSelect('subtitle', null)}
        >
          关闭字幕
          {props.selections.subtitle.effectiveTrackId === null && (
            <VisuallyHidden> 当前播放</VisuallyHidden>
          )}
        </Menu.Item>
        <TrackItems
          kind="subtitle"
          tracks={props.tracks}
          selection={props.selections.subtitle}
          onSelect={props.onSelect}
        />
        <Menu.Divider />
        <Menu.Item
          leftSection={<IconUpload size={14} />}
          onClick={() => props.inputRef.current?.click()}
        >
          上传字幕
        </Menu.Item>
        <UploadedDeleteItems tracks={props.tracks} onDelete={props.onDelete} />
        <PreferenceItems preferences={props.preferences} onChange={update} />
      </Menu.Dropdown>
    </Menu>
  );
}

function AudioMenu({
  tracks,
  selection,
  onSelect,
}: {
  readonly tracks: readonly PlaybackTrack[];
  readonly selection: TrackSelectionState;
  readonly onSelect: VideoTrackControlsProps['onSelect'];
}) {
  const [onlyTrack] = tracks;
  const showUnsupportedTrack = tracks.length === 1 && onlyTrack?.capability === 'unsupported';
  if (!showUnsupportedTrack && tracks.filter((track) => track.available === true).length < 2) {
    return null;
  }
  return (
    <Menu position="top" withinPortal>
      <TrackMenuTarget label="音轨" icon={<IconMusic size={18} />} />
      <Menu.Dropdown>
        <Menu.Label>音轨</Menu.Label>
        {tracks.length === 0 && <Menu.Item disabled>暂无音轨信息</Menu.Item>}
        <TrackItems kind="audio" tracks={tracks} selection={selection} onSelect={onSelect} />
      </Menu.Dropdown>
    </Menu>
  );
}

function TrackMenuTarget({
  label,
  icon,
}: {
  readonly label: string;
  readonly icon: React.ReactNode;
}) {
  return (
    <Menu.Target>
      <ActionIcon variant="subtle" color="gray" aria-label={label}>
        {icon}
      </ActionIcon>
    </Menu.Target>
  );
}

function TrackItems({
  kind,
  tracks,
  selection,
  onSelect,
}: {
  readonly kind: TrackKind;
  readonly tracks: readonly PlaybackTrack[];
  readonly selection: TrackSelectionState;
  readonly onSelect: VideoTrackControlsProps['onSelect'];
}) {
  return tracks.map((track) => (
    <Menu.Item
      key={track.id}
      disabled={track.available !== true || track.capability === 'unsupported'}
      leftSection={effectiveIcon(track, selection)}
      onClick={() => onSelect(kind, track.id)}
    >
      {trackLabel(track, selection)}
      {selection.effectiveTrackId === track.id && <VisuallyHidden> 当前播放</VisuallyHidden>}
    </Menu.Item>
  ));
}

function UploadedDeleteItems({
  tracks,
  onDelete,
}: {
  readonly tracks: readonly PlaybackTrack[];
  readonly onDelete: VideoTrackControlsProps['onDelete'];
}) {
  return tracks
    .filter((track) => track.source === 'uploaded')
    .map((track) => (
      <Menu.Item key={`delete-${track.id}`} color="red" onClick={() => onDelete(track.id)}>
        删除 {track.label}
      </Menu.Item>
    ));
}

function UploadInput({
  inputRef,
  onUpload,
}: {
  readonly inputRef: React.RefObject<HTMLInputElement | null>;
  readonly onUpload: VideoTrackControlsProps['onUpload'];
}) {
  return (
    <input
      ref={inputRef}
      hidden
      type="file"
      aria-label="上传字幕文件"
      accept=".srt,.ass,.ssa,.vtt"
      onChange={(event) => handleUploadChange(event, onUpload)}
    />
  );
}

function PreferenceItems({
  preferences,
  onChange,
}: {
  readonly preferences: SubtitlePreferences;
  readonly onChange: (patch: Partial<SubtitlePreferences>) => void;
}) {
  return (
    <>
      <Menu.Label>字幕样式</Menu.Label>
      <FontSizeItems preferences={preferences} onChange={onChange} />
      <ColorItems onChange={onChange} />
      <OpacityItems onChange={onChange} />
      <PositionItems onChange={onChange} />
    </>
  );
}

function FontSizeItems({
  preferences,
  onChange,
}: {
  readonly preferences: SubtitlePreferences;
  readonly onChange: (patch: Partial<SubtitlePreferences>) => void;
}) {
  return FONT_SIZES.map((value) => (
    <Menu.Item key={value} onClick={() => onChange({ fontSize: value })}>
      字号 {fontSizeLabel(value)}
      {preferences.fontSize === value ? ' ✓' : ''}
    </Menu.Item>
  ));
}

function ColorItems({ onChange }: Pick<Parameters<typeof PreferenceItems>[0], 'onChange'>) {
  return SUBTITLE_COLORS.map((value) => (
    <Menu.Item key={value} onClick={() => onChange({ color: value })}>
      文字颜色 {value}
    </Menu.Item>
  ));
}

function OpacityItems({ onChange }: Pick<Parameters<typeof PreferenceItems>[0], 'onChange'>) {
  return BACKGROUND_OPACITIES.map((value) => (
    <Menu.Item key={value} onClick={() => onChange({ backgroundOpacity: value })}>
      背景透明度 {Math.round((1 - value) * 100)}%
    </Menu.Item>
  ));
}

function PositionItems({ onChange }: Pick<Parameters<typeof PreferenceItems>[0], 'onChange'>) {
  return VERTICAL_POSITIONS.map((value) => (
    <Menu.Item key={value} onClick={() => onChange({ verticalPosition: value })}>
      垂直位置 {value}%
    </Menu.Item>
  ));
}

function updatePreferences(
  props: VideoTrackControlsProps,
  patch: Partial<SubtitlePreferences>,
): void {
  const next = { ...props.preferences, ...patch };
  saveSubtitlePreferences(next);
  props.onPreferencesChange(next);
}

function handleUploadChange(
  event: React.ChangeEvent<HTMLInputElement>,
  onUpload: (file: File) => void,
) {
  const file = event.currentTarget.files?.[0];
  if (file) onUpload(file);
  event.currentTarget.value = '';
}

function effectiveIcon(track: PlaybackTrack, selection: TrackSelectionState) {
  return selection.effectiveTrackId === track.id ? <IconCheck size={14} /> : undefined;
}

function trackLabel(track: PlaybackTrack, selection: TrackSelectionState): string {
  const switching =
    selection.selectedTrackId === track.id && selection.effectiveTrackId !== track.id;
  const details = trackDetails(track as WebPlaybackTrack);
  const reason = track.capability === 'unsupported' ? track.unsupportedReason : undefined;
  return [track.label, details, switching ? '切换中' : '', reason].filter(Boolean).join(' · ');
}

function trackDetails(track: WebPlaybackTrack): string {
  const channels = track.channels ? `${track.channels} 声道` : track.channelLayout;
  return [
    track.title,
    track.language,
    track.codec ?? track.format,
    channels,
    track.default ? '默认' : undefined,
    track.forced ? '强制' : undefined,
  ]
    .filter(Boolean)
    .join(' · ');
}

function overlayStyle(preferences: SubtitlePreferences): CSSProperties {
  return {
    position: 'absolute',
    insetInline: 0,
    bottom: `${preferences.verticalPosition}%`,
    display: 'flex',
    justifyContent: 'center',
    pointerEvents: 'none',
    padding: '0 1rem',
  };
}

function subtitleTextStyle(preferences: SubtitlePreferences): CSSProperties {
  return {
    display: 'inline-block',
    maxWidth: '80%',
    padding: '0.25rem 0.75rem',
    borderRadius: 'var(--mantine-radius-sm)',
    backgroundColor: `rgba(0,0,0,${preferences.backgroundOpacity})`,
    color: preferences.color,
    fontSize: fontSize(preferences.fontSize),
    whiteSpace: 'pre-line',
  };
}

function fontSizeLabel(value: SubtitlePreferences['fontSize']): string {
  return value === 'small' ? '小' : value === 'medium' ? '中' : '大';
}

function fontSize(value: SubtitlePreferences['fontSize']): string {
  if (value === 'small') return 'var(--mantine-font-size-xs)';
  if (value === 'large') return 'var(--mantine-font-size-lg)';
  return 'var(--mantine-font-size-sm)';
}
