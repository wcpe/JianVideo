import { useRef, useEffect, useState, useCallback } from 'react';
import { createPreviewFacet, PlaybackCore, SEEK_TIERS } from '@jianvideo/player-core';
import type {
  AbLoopState,
  FrameStepDirection,
  PlaybackCommandContext,
  PlaybackQuality,
  PlaybackQualityState,
  PlaybackSnapshot,
  PlaybackSource,
  PlaybackTrack,
  PreparedPreviewTrack,
  PreviewHit,
  SeekTier,
  TrackKind,
} from '@jianvideo/player-core';
import { Text, Box, ActionIcon, Slider, Menu, Switch } from '@mantine/core';
import {
  IconPlayerPlay,
  IconPlayerPause,
  IconVolume,
  IconVolumeOff,
  IconMaximize,
  IconMinimize,
  IconPictureInPicture,
  IconPlayerSkipBack,
  IconPlayerSkipForward,
  IconPlayerTrackNext,
  IconPlayerTrackPrev,
  IconAdjustments,
} from '@tabler/icons-react';
import type { SubtitleEntry, PlaybackDescriptor } from '@/types';
import type { TrackResponse } from '@/api/subtitle';
import { deleteSubtitle, uploadSubtitle } from '@/api/subtitle';
import VideoTrackControls, {
  SubtitleOverlay,
  type TrackSelections,
} from '@/components/VideoTrackControls';
import {
  loadSubtitlePreferences,
  type SubtitlePreferences,
} from '@/components/VideoTrackControls.helpers';
import { isCodecSupported } from '@/utils/codec-capability';
import { notifyError, notifySuccess } from '@/utils/notify';
import { useAuthStore } from '@/stores/auth';
import { loadVolumePref, clampVolume, saveVolumePref } from '@/components/VideoPlayer.helpers';
import { WebPlaybackBackend, type WebPlaybackSourcePayload } from '@/player/WebPlaybackBackend';
import {
  createBinaryFrameMarkerResolver,
  type ResolvePresentedFrameIdentity,
  type WebBinaryFrameMarker,
  type WebFrameTimelineEntry,
} from '@/player/WebFramePresentationFacet';

interface VideoPlayerProps {
  /** 流 URL（支持 master.m3u8 触发 ABR 模式）。传入 descriptor 时由其覆盖。 */
  url: string;
  /**
   * 播放描述符（FR-52）：按目标编码 + 路径分发到对应内核。
   * 缺省时保持现有 url/isABR/streamType 行为不变（现有调用方零改动）。
   */
  descriptor?: PlaybackDescriptor;
  /** 可选视频海报；只传给原生 video，不参与播放状态机。 */
  poster?: string;
  /** 后端准备的稳定帧时间线；仅用于取得相邻目标。 */
  frameTimeline?: readonly WebFrameTimelineEntry[];
  /** 显式二进制画面 marker；仅从实际呈现像素读取稳定身份。 */
  frameMarker?: WebBinaryFrameMarker;
  /** 独立的已呈现帧身份源；缺省时逐帧自动降级为近似。 */
  resolvePresentedFrameIdentity?: ResolvePresentedFrameIdentity;
  /** 名义帧率；缺省且无时间线时按 30fps 近似。 */
  nominalFrameRate?: number;
  /** 当前源的已准备时间轴预览轨道。 */
  previewTrack?: PreparedPreviewTrack;
  /** 精灵资源名到可加载 URL 的映射。 */
  previewSpriteUrls?: Readonly<Record<string, string>>;
  /** 统一轨道 API 对应的媒体 ID。 */
  mediaId?: number;
  /** 播放前预载的统一轨道清单。 */
  trackResponse?: TrackResponse;
  /** 上传或删除后刷新统一轨道清单。 */
  onTrackManifestRefresh?: () => Promise<TrackResponse>;
  /** 自动播放 */
  autoPlay?: boolean;
  /** 当前播放路径发生不可恢复错误时通知调用方切换 fallback。 */
  onPlaybackError?: () => void;
  /** 解析后的字幕条目列表 */
  subtitleEntries?: SubtitleEntry[];
  /** 是否显示字幕 */
  subtitleVisible?: boolean;
  /**
   * 显式指定是否走 ABR（hls.js）模式。
   * 缺省时按 URL 是否以 master.m3u8 结尾推断。
   */
  isABR?: boolean;
  /**
   * 显式声明流类型为 mp4 时使用浏览器原生 video 标签直接加载。
   */
  streamType?: 'mpegts' | 'mp4';
  /**
   * 填充模式（FR-103）：为真时视频区放弃固定 16:9 比例，改为 flex 填充父容器剩余高度
   * （video 以 object-fit:contain letterbox 黑边铺满），供播放页全屏沉浸布局使用。
   * 缺省 false 时维持固定 16:9（灯箱 / 分享等用法零改动）。
   */
  fill?: boolean;
  /**
   * 续播起始位置（秒，FR-44）。大于 1 时在媒体可定位后 seek 到该位置一次。
   */
  initialPosition?: number;
  /**
   * 定期上报当前播放位置（秒，FR-44）。约每 10s 触发一次，暂停时补报一次。
   */
  onPositionReport?: (position: number) => void;
  /**
   * 播放接近结束时回调一次（FR-44），用于标记「已看」。
   */
  onEnded?: () => void;
}

// 播放位置上报节流间隔（秒，FR-44）
const POSITION_REPORT_INTERVAL = 10;
// 「看完」判定：剩余时长小于该秒数即视为已看完（FR-44）
const WATCHED_REMAINING_THRESHOLD = 15;
// 默认定位档位与核心初始档保持一致。
const DEFAULT_SEEK_TIER = { kind: 'seconds', value: 5 } as const satisfies SeekTier;
// 键盘音量调节步长（FR-104）
const VOLUME_STEP = 0.1;
// 控件自动隐藏延迟（毫秒，FR-104）：播放中鼠标静止超过该时长则淡出控件
const CONTROLS_HIDE_DELAY = 3000;
// 可选倍速档位（FR-104）
const PLAYBACK_RATES = [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2] as const;
const INITIAL_QUALITY_STATE: PlaybackQualityState = {
  actualQuality: null,
  dataSaver: false,
  dataSaverBlocked: false,
  manualQuality: null,
  playbackRate: 1,
  qualities: [],
  qualityMode: 'auto',
};
const INITIAL_AB_LOOP_STATE: AbLoopState = { a: null, b: null, enabled: false };
// 字幕字号档位（FR-104）：映射到字幕文本 fontSize
const SUBTITLE_SCALES = {
  small: 'var(--mantine-font-size-xs)',
  medium: 'var(--mantine-font-size-sm)',
  large: 'var(--mantine-font-size-lg)',
} as const;
type SubtitleScale = keyof typeof SUBTITLE_SCALES;

const PREVIEW_LONG_PRESS_MS = 400;
const PREVIEW_VERTICAL_CANCEL_PX = 12;
const PREVIEW_FALLBACK_MARGIN_PERCENT = 5;

type PreviewState = {
  readonly hit: PreviewHit | null;
  readonly imageUrl: string | null;
  readonly leftPercent: number;
  readonly mediaTime: number;
};

type PreviewPointerSession = {
  active: boolean;
  canceled: boolean;
  clientX: number;
  pointerId: number;
  startY: number;
  timer: ReturnType<typeof setTimeout> | null;
};

function createPointerSession(): PreviewPointerSession {
  return { active: false, canceled: false, clientX: 0, pointerId: -1, startY: 0, timer: null };
}

function commandFromSnapshot(snapshot: PlaybackSnapshot): PlaybackCommandContext | null {
  return snapshot.sourceId === null
    ? null
    : {
        requestId: snapshot.requestId,
        sourceEpoch: snapshot.sourceEpoch,
        sourceId: snapshot.sourceId,
      };
}

function mediaTimeAtClientX(element: HTMLElement, clientX: number, duration: number): number {
  const rect = element.getBoundingClientRect();
  if (rect.width <= 0 || duration <= 0) return 0;
  const fraction = Math.min(1, Math.max(0, (clientX - rect.left) / rect.width));
  return fraction * duration;
}

function previewLeftPercent(
  element: HTMLElement,
  rawPercent: number,
  spriteWidth: number,
  hasImage: boolean,
): number {
  const trackWidth = element.getBoundingClientRect().width;
  const spriteMargin = hasImage && trackWidth > 0 ? (spriteWidth / 2 / trackWidth) * 100 : 0;
  const marginPercent = Math.min(50, Math.max(PREVIEW_FALLBACK_MARGIN_PERCENT, spriteMargin));
  return Math.min(100 - marginPercent, Math.max(marginPercent, rawPercent));
}

function loadImage(url: string, cache: Map<string, Promise<void>>): Promise<void> {
  const cached = cache.get(url);
  if (cached) return cached;
  const loading = new Promise<void>((resolve, reject) => {
    const image = new Image();
    image.onload = () => resolve();
    image.onerror = () => reject(new Error('预览图片加载失败'));
    image.src = url;
  });
  cache.set(url, loading);
  return loading;
}

/** 解析后的有效播放输入（FR-52）。 */
interface ResolvedPlayback {
  url: string;
  /** 是否 ABR（hls.js）；undefined 时由 url 是否 master.m3u8 推断 */
  isABR?: boolean;
  streamType: 'mpegts' | 'mp4';
  /** 目标编码客户端不支持且无回退源：不初始化内核，展示提示 */
  unsupported: boolean;
}

/**
 * 解析播放描述符为有效播放输入（纯函数，FR-52）。
 *
 * - 无描述符：沿用现有 url/isABR/streamType 入参，行为不变。
 * - `ts` / `mp4` 路径：直接映射到现有 mpegts.js / 原生 video 分支。
 * - `fmp4` 路径：先 isCodecSupported 校验客户端能力——支持则走 hls.js（fMP4，按 ABR 模式
 *   加载 index.m3u8）；不支持则回退 fallbackUrl（按 TS 路径），无回退源则标记 unsupported。
 */
function resolveDescriptor(
  descriptor: PlaybackDescriptor | undefined,
  fallback: { url: string; isABR?: boolean; streamType: 'mpegts' | 'mp4' },
): ResolvedPlayback {
  if (!descriptor) {
    return {
      url: fallback.url,
      isABR: fallback.isABR,
      streamType: fallback.streamType,
      unsupported: false,
    };
  }
  if (descriptor.path === 'mp4') {
    return { url: descriptor.url, isABR: false, streamType: 'mp4', unsupported: false };
  }
  if (descriptor.path === 'fmp4') {
    if (isCodecSupported(descriptor.codec)) {
      // hls.js 原生支持 fMP4 分片，按 ABR 模式加载 HLS-fMP4 清单
      return { url: descriptor.url, isABR: true, streamType: 'mpegts', unsupported: false };
    }
    // 客户端不支持目标编码：回退 H.264/TS 源；无回退源则标记不可播
    if (descriptor.fallbackUrl) {
      return {
        url: descriptor.fallbackUrl,
        isABR: undefined,
        streamType: 'mpegts',
        unsupported: false,
      };
    }
    return { url: descriptor.url, isABR: undefined, streamType: 'mpegts', unsupported: true };
  }
  // ts 路径：等价现有 mpegts.js / hls.js-ABR 分支（由 url 是否 master.m3u8 推断）
  return { url: descriptor.url, isABR: undefined, streamType: 'mpegts', unsupported: false };
}

function createPlaybackSource(
  url: string,
  streamType: ResolvedPlayback['streamType'],
  unsupported: boolean,
  isABR: boolean,
  mediaId?: number,
  trackResponse?: TrackResponse,
  frameTimeline?: readonly WebFrameTimelineEntry[],
  nominalFrameRate?: number,
  resolvePresentedFrameIdentity?: ResolvePresentedFrameIdentity,
): PlaybackSource {
  const kind: WebPlaybackSourcePayload['kind'] = unsupported
    ? 'unsupported'
    : streamType === 'mp4'
      ? 'native'
      : isABR
        ? 'hls'
        : 'mpegts';
  const mode = kind === 'hls' ? 'adaptive' : kind === 'mpegts' ? 'stream' : 'direct';
  return {
    id: `${kind}:${url}`,
    mode,
    payload: {
      kind,
      url,
      ...(mediaId === undefined ? {} : { mediaId }),
      ...(trackResponse === undefined ? {} : { trackResponse }),
      ...(frameTimeline === undefined ? {} : { frameTimeline }),
      ...(nominalFrameRate === undefined ? {} : { nominalFrameRate }),
      ...(resolvePresentedFrameIdentity === undefined ? {} : { resolvePresentedFrameIdentity }),
    } satisfies WebPlaybackSourcePayload,
  };
}

function seekTierLabel(tier: SeekTier): string {
  return tier.kind === 'frame' ? '1 帧' : `${tier.value} 秒`;
}

function bufferedPercent(snapshot: PlaybackSnapshot): number {
  const bufferedEnd = snapshot.buffered.at(-1)?.end ?? 0;
  if (snapshot.duration <= 0) return 0;
  return Math.min(100, (bufferedEnd / snapshot.duration) * 100);
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : '请稍后重试';
}

function hasSameTracks(left: TrackResponse | undefined, right: TrackResponse): boolean {
  if (left === right) return true;
  if (!left || left.tracks.length !== right.tracks.length) return false;
  const rightTracks = new Set(right.tracks.map((track) => `${track.kind}:${track.id}`));
  return left.tracks.every((track) => rightTracks.has(`${track.kind}:${track.id}`));
}

function isSameSource(left: PlaybackCommandContext, right: PlaybackCommandContext | null): boolean {
  return Boolean(
    right && left.sourceEpoch === right.sourceEpoch && left.sourceId === right.sourceId,
  );
}

export default function VideoPlayer({

  url,
  descriptor,
  poster,
  frameTimeline,
  frameMarker,
  nominalFrameRate,
  previewTrack,
  previewSpriteUrls,
  resolvePresentedFrameIdentity,
  mediaId,
  trackResponse,
  onTrackManifestRefresh,
  autoPlay = true,
  subtitleEntries,
  subtitleVisible = false,
  isABR: isABRProp,
  streamType = 'mpegts',
  fill = false,
  initialPosition,
  onPositionReport,
  onEnded,
  onPlaybackError,
}: VideoPlayerProps) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const coreRef = useRef<PlaybackCore | null>(null);
  const backendRef = useRef<WebPlaybackBackend | null>(null);
  const previewImageCacheRef = useRef(new Map<string, Promise<void>>());
  const previewRequestRef = useRef(0);
  const previewPointerRef = useRef<PreviewPointerSession>(createPointerSession());
  const suppressSliderSeekRef = useRef(false);
  const seekPreviewRequestRef = useRef(0);
  const previewTrackRef = useRef(previewTrack);
  const previewSpriteUrlsRef = useRef(previewSpriteUrls);
  const trackResponseRef = useRef(trackResponse);
  const trackResponsePropRef = useRef(trackResponse);
  const pendingTrackResponseRef = useRef<TrackResponse | null>(null);
  const manifestGenerationRef = useRef(0);
  const manifestContextRef = useRef<string | null>(null);
  const mediaIdRef = useRef(mediaId);
  previewTrackRef.current = previewTrack;
  trackResponsePropRef.current = trackResponse;
  mediaIdRef.current = mediaId;
  previewSpriteUrlsRef.current = previewSpriteUrls;
  // FR-44：续播 / 上报 / 看完状态。用 ref 持有最新回调与一次性标志，避免重建监听器。
  const initialPositionRef = useRef(initialPosition);
  const hasSeekedRef = useRef(false);
  const restoreGenerationRef = useRef(0);
  const restoreInFlightRef = useRef<number | null>(null);
  const restoreRetryPendingRef = useRef(false);
  const seekableAvailableRef = useRef(false);
  const retryInitialSeekRef = useRef<() => void>(() => undefined);
  const onPositionReportRef = useRef(onPositionReport);
  const onEndedRef = useRef(onEnded);
  const onPlaybackErrorRef = useRef(onPlaybackError);
  const lastReportRef = useRef(0);
  const endedReportedRef = useRef(false);
  initialPositionRef.current = initialPosition;
  onPositionReportRef.current = onPositionReport;
  onEndedRef.current = onEnded;
  onPlaybackErrorRef.current = onPlaybackError;
  const [isPlaying, setIsPlaying] = useState(false);
  const [autoPlayBlocked, setAutoPlayBlocked] = useState(false);
  // FR-102：静音自动播。浏览器允许 muted autoplay，进页即出画面；autoMuted 为真时
  // 在角落展示「点击取消静音」入口，用户主动调音量 / 切静音时清除该标记。
  const [autoMuted, setAutoMuted] = useState(false);
  const [currentTime, setCurrentTime] = useState(0);
  const [seekPreviewTime, setSeekPreviewTime] = useState<number | null>(null);
  const [duration, setDuration] = useState(0);
  const [volume, setVolume] = useState(1);
  const [isMuted, setIsMuted] = useState(false);
  const [bufferedProgress, setBufferedProgress] = useState(0);
  const [subtitleText, setSubtitleText] = useState('');
  const [trackCues, setTrackCues] = useState<readonly SubtitleEntry[]>([]);
  const [tracks, setTracks] = useState<readonly PlaybackTrack[]>([]);
  const [trackSelections, setTrackSelections] = useState<TrackSelections | null>(null);
  const [subtitlePreferences, setSubtitlePreferences] = useState<SubtitlePreferences>(() =>
    loadSubtitlePreferences(),
  );
  const [abrLevel, setAbrLevel] = useState<string | null>(null);
  const [qualityState, setQualityState] = useState<PlaybackQualityState>(INITIAL_QUALITY_STATE);
  const [abLoopState, setAbLoopState] = useState<AbLoopState>(INITIAL_AB_LOOP_STATE);
  const abLoopStateRef = useRef(abLoopState);
  abLoopStateRef.current = abLoopState;
  // 末端缓冲等待（FR-18）：mpegts.js ERROR 或 video stalled 时进入等待，1s 后自动重载
  const [isWaiting, setIsWaiting] = useState(false);
  const [seekTier, setSeekTier] = useState<SeekTier>(DEFAULT_SEEK_TIER);
  const [framePresentationCapability, setFramePresentationCapability] = useState<
    PlaybackSnapshot['capabilities']['framePresentation'] | null
  >(null);
  const [approximateFrameStep, setApproximateFrameStep] = useState(false);
  const [lastFrameStepResult, setLastFrameStepResult] = useState('pending');
  // FR-104：播放器内核与控件增强
  const containerRef = useRef<HTMLDivElement>(null);
  // 全屏态：由 fullscreenchange 同步
  const [isFullscreen, setIsFullscreen] = useState(false);
  // 倍速：映射到 video.playbackRate
  const [rate, setRate] = useState(1);
  // 进度条 hover 时间预览：null 表示未 hover
  const [hoverPct, setHoverPct] = useState<number | null>(null);
  const [timelinePreview, setTimelinePreview] = useState<PreviewState | null>(null);
  // 控件可见性：播放中鼠标静止数秒后淡出
  const [controlsVisible, setControlsVisible] = useState(true);
  // 字幕字号档（FR-104）
  const [subtitleScale, setSubtitleScale] = useState<SubtitleScale>('medium');
  // 画中画能力探测（FR-104）：浏览器不支持时隐藏 PiP 按钮
  const pipSupported = typeof document !== 'undefined' && Boolean(document.pictureInPictureEnabled);
  // 控件自动隐藏定时器句柄
  const hideTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // FR-52：解析播放描述符为有效播放输入（url / 是否 ABR / 流类型）。
  // 缺省描述符时直接沿用现有入参，行为不变；fmp4 路径前先做客户端能力校验，
  // 不支持时回退 fallbackUrl（按 TS 路径加载），无回退源则标记为不可播。
  const resolved = resolveDescriptor(descriptor, { url, isABR: isABRProp, streamType });

  // 判断是否为 ABR 模式
  const isABR = resolved.isABR ?? resolved.url.endsWith('master.m3u8');
  const expectedSourceId = createPlaybackSource(
    resolved.url,
    resolved.streamType,
    resolved.unsupported,
    isABR,
  ).id;
  const manifestContext = `${mediaId ?? ''}:${expectedSourceId}`;
  if (manifestContextRef.current !== manifestContext) {
    manifestContextRef.current = manifestContext;
    manifestGenerationRef.current = 0;
    pendingTrackResponseRef.current = null;
    trackResponseRef.current = trackResponse;
  }

  const seekToInitialOnce = useCallback(async () => {
    const core = coreRef.current;
    const video = videoRef.current;
    if (!core || !video || hasSeekedRef.current || restoreInFlightRef.current !== null) return;
    const position = initialPositionRef.current;
    if (!position || position <= 1 || !isFinite(video.duration) || video.duration <= 0) return;
    if (video.duration - position <= WATCHED_REMAINING_THRESHOLD) {
      hasSeekedRef.current = true;
      restoreRetryPendingRef.current = false;
      return;
    }
    const generation = restoreGenerationRef.current;
    restoreInFlightRef.current = generation;
    restoreRetryPendingRef.current = false;
    try {
      const result = await core.seek(position, 'restore');
      if (generation === restoreGenerationRef.current && result.status === 'completed') {
        hasSeekedRef.current = true;
      }
    } finally {
      if (restoreInFlightRef.current === generation) restoreInFlightRef.current = null;
      if (
        generation === restoreGenerationRef.current &&
        restoreRetryPendingRef.current &&
        !hasSeekedRef.current
      ) {
        retryInitialSeekRef.current();
      }
    }
  }, []);
  retryInitialSeekRef.current = () => {
    void seekToInitialOnce();
  };

  const syncPreviewFacet = useCallback((snapshot: PlaybackSnapshot) => {
    const command = commandFromSnapshot(snapshot);
    const core = coreRef.current;
    if (!command || !core) return;
    const state = core.getPreviewState();
    const track = previewTrackRef.current;
    const sourceChanged =
      state === null || state.sourceEpoch !== command.sourceEpoch || state.sourceId !== command.sourceId;
    const trackChanged = track?.generationId !== state?.generationId;
    if (sourceChanged) core.setPreviewTrack(null, command);
    if (sourceChanged || trackChanged) core.setPreviewTrack(track ?? null, command);
  }, []);

  const syncCoreSnapshot = useCallback(
    (snapshot: PlaybackSnapshot) => {
      syncPreviewFacet(snapshot);
      const seekableAvailable = snapshot.seekable.length > 0;
      const becameSeekable = !seekableAvailableRef.current && seekableAvailable;
      seekableAvailableRef.current = seekableAvailable;
      setIsPlaying(snapshot.state === 'playing');
      setCurrentTime(snapshot.currentTime);
      setDuration(snapshot.duration);
      setBufferedProgress(bufferedPercent(snapshot));
      setFramePresentationCapability(snapshot.capabilities.framePresentation);
      if (becameSeekable) {
        restoreRetryPendingRef.current = true;
        void seekToInitialOnce();
      }
    },
    [seekToInitialOnce, syncPreviewFacet],
  );

  const syncTrackState = useCallback((core: PlaybackCore) => {
    const audio = core.getTrackSelection('audio');
    const subtitle = core.getTrackSelection('subtitle');
    setTracks([...core.getTracks('subtitle'), ...core.getTracks('audio')]);
    if (audio && subtitle) setTrackSelections({ audio, subtitle });
  }, []);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    const backend = new WebPlaybackBackend(video, {
      getHlsRequestHeaders: () => {
        const headers: Record<string, string> = {};
        const token = useAuthStore.getState().token as string | undefined;
        if (token) headers.Authorization = `Bearer ${token}`;
        return headers;
      },
      onAbrLevelChange: setAbrLevel,
      onPlaybackError: () => onPlaybackErrorRef.current?.(),
      onWaitingChange: setIsWaiting,
    });
    const core = new PlaybackCore({
      backend,
      facets: {
        framePresentation: backend.framePresentation,
        loadControl: backend.loadControl,
        preview: createPreviewFacet(),
        quality: backend.quality,
        tracks: backend.tracks,
      },
      initialSeekTier: DEFAULT_SEEK_TIER,
    });
    backendRef.current = backend;
    coreRef.current = core;
    setSeekTier(core.getSeekTier() ?? DEFAULT_SEEK_TIER);
    syncCoreSnapshot(core.getSnapshot());
    const unsubscribeCues = backend.tracks.subscribeCues(setTrackCues);
    const unsubscribe = core.subscribe((event) => {
      if (event.type === 'snapshotChanged') syncCoreSnapshot(event.snapshot);
      if (event.type === 'trackSelectionChanged' || event.type === 'trackSelectionCompleted') {
        syncTrackState(core);
      }
      if (event.type === 'seekTierChanged') setSeekTier(event.tier);
      if (event.type === 'qualityStateChanged') {
        setQualityState(event.state);
        setRate(event.state.playbackRate);
      }
      if (event.type === 'abLoopChanged') setAbLoopState(event.state);
      if (event.type === 'frameStepCompleted') {
        const snapshot = core.getSnapshot();
        const isCurrentSource =
          event.sourceEpoch === snapshot.sourceEpoch && event.sourceId === snapshot.sourceId;
        if (!isCurrentSource) return;
        setLastFrameStepResult(
          [
            event.result.status,
            event.result.precision,
            event.result.confirmedSourceFrameIndex ?? event.result.confirmedStableFrameId ?? 'unknown',
            event.result.error?.code ?? 'ok',
          ].join(':'),
        );
        if (event.result.precision === 'approximate') setApproximateFrameStep(true);
        if (event.result.precision === 'exact-verified' && event.result.status === 'completed') {
          setApproximateFrameStep(false);
        }
      }
    });
    return () => {
      unsubscribe();
      unsubscribeCues();
      core.dispose();
      backendRef.current = null;
      coreRef.current = null;
    };
  }, [syncCoreSnapshot, syncTrackState]);

  useEffect(() => {
    const core = coreRef.current;
    if (core) syncPreviewFacet(core.getSnapshot());
    previewRequestRef.current += 1;
    setTimelinePreview(null);
  }, [previewTrack, previewSpriteUrls, syncPreviewFacet]);

  useEffect(() => {
    const backend = backendRef.current;
    const core = coreRef.current;
    if (!backend || !core || !trackResponse) return;
    const pendingResponse = pendingTrackResponseRef.current;
    if (pendingResponse && !hasSameTracks(trackResponse, pendingResponse)) return;
    pendingTrackResponseRef.current = null;
    trackResponseRef.current = trackResponse;
    const command = commandFromSnapshot(core.getSnapshot());
    if (!command || command.sourceId !== expectedSourceId) return;
    backend.tracks.updateResponse(trackResponse, command);
    syncTrackState(core);
  }, [expectedSourceId, syncTrackState, trackResponse]);

  // FR-102：自动播放仍由壳层先静音，但播放命令统一进入 PlaybackCore。
  const attemptAutoPlay = useCallback(async (core: PlaybackCore) => {
    const video = videoRef.current;
    if (!video) return;
    video.muted = true;
    setIsMuted(true);
    setAutoMuted(true);
    const result = await core.play();
    if (result.status === 'failed') setAutoPlayBlocked(true);
  }, []);

  useEffect(() => {
    const core = coreRef.current;
    if (!core) return;
    const markerResolver =
      resolvePresentedFrameIdentity ??
      (frameMarker && videoRef.current
        ? createBinaryFrameMarkerResolver(videoRef.current, frameMarker)
        : undefined);
    const source = createPlaybackSource(
      resolved.url,
      resolved.streamType,
      resolved.unsupported,
      isABR,
      mediaId,
      trackResponseRef.current,
      frameTimeline,
      nominalFrameRate,
      markerResolver,
    );
    let current = true;
    restoreGenerationRef.current += 1;
    restoreInFlightRef.current = null;
    restoreRetryPendingRef.current = false;
    seekableAvailableRef.current = false;
    hasSeekedRef.current = false;
    setAutoPlayBlocked(false);
    setFramePresentationCapability(null);
    setApproximateFrameStep(false);
    setLastFrameStepResult('pending');
    previewRequestRef.current += 1;
    setTimelinePreview(null);
    void core.load(source).then(async (result) => {
      if (!current || result.status !== 'completed') return;
      syncTrackState(core);
      setQualityState(core.getQualityState());
      setAbLoopState(core.getAbLoopState());
      setRate(core.getQualityState().playbackRate);
      await seekToInitialOnce();
      if (current && autoPlay && !resolved.unsupported) await attemptAutoPlay(core);
    });
    return () => {
      current = false;
    };
  }, [
    autoPlay,
    attemptAutoPlay,
    frameMarker,
    frameTimeline,
    isABR,
    mediaId,
    nominalFrameRate,
    resolvePresentedFrameIdentity,
    resolved.streamType,
    resolved.unsupported,
    resolved.url,
    seekToInitialOnce,
    syncTrackState,
  ]);

  // FR-44：URL 变化（切换媒体）时重置上报 / 看完的一次性标志。
  useEffect(() => {
    setAbrLevel(null);
    lastReportRef.current = 0;
    endedReportedRef.current = false;
  }, [resolved.url]);

  const reportEndedOnce = useCallback(() => {
    const ended = onEndedRef.current;
    if (!ended || endedReportedRef.current) return;
    endedReportedRef.current = true;
    ended();
  }, []);

  const reportPausedPosition = useCallback((video: HTMLVideoElement) => {
    const report = onPositionReportRef.current;
    if (!report || video.currentTime <= 0) return;
    lastReportRef.current = video.currentTime;
    report(video.currentTime);
  }, []);

  const reportWatchProgress = useCallback(
    (video: HTMLVideoElement) => {
      const report = onPositionReportRef.current;
      if (report && video.currentTime - lastReportRef.current >= POSITION_REPORT_INTERVAL) {
        lastReportRef.current = video.currentTime;
        report(video.currentTime);
      }
      const loop = abLoopStateRef.current;
      const reachedLoopEnd = loop.enabled && loop.b !== null && video.currentTime >= loop.b;
      if (
        !reachedLoopEnd &&
        isFinite(video.duration) &&
        video.duration > 0 &&
        video.duration - video.currentTime <= WATCHED_REMAINING_THRESHOLD
      ) {
        reportEndedOnce();
      }
    },
    [reportEndedOnce],
  );

  // 观看上报与音量仍属于 Web 壳层；播放状态、时间和缓冲只订阅 core 快照。
  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    const onTime = () => reportWatchProgress(video);
    const onDuration = () => {
      void seekToInitialOnce();
    };
    const onPause = () => reportPausedPosition(video);
    const onNativeEnded = () => {
      const loop = abLoopStateRef.current;
      if (loop.enabled) return;
      reportEndedOnce();
    };
    const onVolume = () => {
      setVolume(video.volume);
      setIsMuted(video.muted);
    };
    video.addEventListener('timeupdate', onTime);
    video.addEventListener('durationchange', onDuration);
    video.addEventListener('loadedmetadata', onDuration);
    video.addEventListener('pause', onPause);
    video.addEventListener('ended', onNativeEnded);
    video.addEventListener('volumechange', onVolume);
    return () => {
      video.removeEventListener('timeupdate', onTime);
      video.removeEventListener('durationchange', onDuration);
      video.removeEventListener('loadedmetadata', onDuration);
      video.removeEventListener('pause', onPause);
      video.removeEventListener('ended', onNativeEnded);
      video.removeEventListener('volumechange', onVolume);
    };
  }, [reportEndedOnce, reportPausedPosition, reportWatchProgress, seekToInitialOnce]);

  // 字幕同步
  useEffect(() => {
    if (!subtitleVisible || !subtitleEntries?.length) {
      setSubtitleText('');
      return;
    }
    const entry = subtitleEntries.find((e) => currentTime >= e.start && currentTime < e.end);
    setSubtitleText(entry?.text ?? '');
  }, [currentTime, subtitleEntries, subtitleVisible]);

  // FR-104：挂载时恢复记忆的音量档（与 FR-102 协调）。
  // 首次仍走静音自动播（attemptAutoPlay 置 muted），这里只把滑块/音量值恢复为上次偏好，
  // 取消静音后即按记忆音量出声；若上次本就是手动静音则记忆静音态。
  useEffect(() => {
    const pref = loadVolumePref();
    if (!pref) return;
    const v = videoRef.current;
    setVolume(pref.volume);
    if (v) v.volume = pref.volume;
    // 上次为手动静音 → 保留静音态（autoPlay 关闭时直接体现）；不强行解除 FR-102 自动静音
    if (pref.muted && !autoPlay) {
      setIsMuted(true);
      if (v) v.muted = true;
    }
  }, [autoPlay]);

  // FR-104：全屏态同步——监听 fullscreenchange，按当前全屏元素是否为本容器更新按钮态
  useEffect(() => {
    const onFsChange = () => setIsFullscreen(document.fullscreenElement === containerRef.current);
    document.addEventListener('fullscreenchange', onFsChange);
    return () => document.removeEventListener('fullscreenchange', onFsChange);
  }, []);

  const togglePlay = () => {
    const core = coreRef.current;
    if (!core || qualityState.dataSaverBlocked) return;
    if (isPlaying) {
      void core.pause();
      return;
    }
    setAutoPlayBlocked(false);
    void core.play();
  };

  const hideTimelinePreview = useCallback(() => {
    previewRequestRef.current += 1;
    setTimelinePreview(null);
  }, []);

  const clearTimelinePreviewImage = useCallback(() => {
    setTimelinePreview((current) => (current ? { ...current, imageUrl: null } : null));
  }, []);

  const showTimelinePreview = useCallback((element: HTMLElement, clientX: number) => {
    const core = coreRef.current;
    if (!core) return;
    const snapshot = core.getSnapshot();
    const command = commandFromSnapshot(snapshot);
    if (!command || snapshot.duration <= 0) return;
    const mediaTime = mediaTimeAtClientX(element, clientX, snapshot.duration);
    const hit = core.hitTestPreview(mediaTime, command);
    const imageUrl = hit ? previewSpriteUrlsRef.current?.[hit.sprite.assetId] : undefined;
    const rawPercent = (mediaTime / snapshot.duration) * 100;
    const spriteWidth = hit?.sprite.width ?? 0;
    const leftPercent = previewLeftPercent(element, rawPercent, spriteWidth, Boolean(imageUrl));
    const request = ++previewRequestRef.current;
    setTimelinePreview({ hit, imageUrl: null, leftPercent, mediaTime });
    if (!hit || !imageUrl) return;
    void loadImage(imageUrl, previewImageCacheRef.current).then(
      () => {
        if (previewRequestRef.current === request) {
          setTimelinePreview({ hit, imageUrl, leftPercent, mediaTime });
        }
      },
      () => undefined,
    );
  }, []);

  const clearPreviewPointerTimer = () => {
    const session = previewPointerRef.current;
    if (session.timer) clearTimeout(session.timer);
    session.timer = null;
  };

  const resetPreviewPointer = (hide = true) => {
    clearPreviewPointerTimer();
    previewPointerRef.current = createPointerSession();
    if (hide) hideTimelinePreview();
  };

  const handlePreviewPointerDownCapture = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.pointerType !== 'mouse') suppressSliderSeekRef.current = true;
  };

  const handlePreviewPointerDown = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.pointerType === 'mouse') return;
    resetPreviewPointer(false);
    const session = previewPointerRef.current;
    session.clientX = event.clientX;
    session.pointerId = event.pointerId;
    session.startY = event.clientY;
    const element = event.currentTarget;
    session.timer = setTimeout(() => {
      if (session.canceled) return;
      session.active = true;
      showTimelinePreview(element, session.clientX);
    }, PREVIEW_LONG_PRESS_MS);
  };

  const handlePreviewPointerMove = (event: React.PointerEvent<HTMLDivElement>) => {
    const session = previewPointerRef.current;
    if (event.pointerId !== session.pointerId) return;
    session.clientX = event.clientX;
    if (Math.abs(event.clientY - session.startY) > PREVIEW_VERTICAL_CANCEL_PX) {
      session.canceled = true;
      clearPreviewPointerTimer();
      if (session.active) hideTimelinePreview();
      return;
    }
    if (!session.active) return;
    event.preventDefault();
    showTimelinePreview(event.currentTarget, event.clientX);
  };

  const handlePreviewPointerUp = (event: React.PointerEvent<HTMLDivElement>) => {
    const session = previewPointerRef.current;
    if (event.pointerId !== session.pointerId) return;
    const core = coreRef.current;
    if (session.active && !session.canceled && core) {
      const snapshot = core.getSnapshot();
      const mediaTime = mediaTimeAtClientX(event.currentTarget, event.clientX, snapshot.duration);
      void core.seek(mediaTime, 'user');
    }
    resetPreviewPointer();
    suppressSliderSeekRef.current = false;
  };

  const handlePreviewPointerCancel = () => {
    resetPreviewPointer();
    suppressSliderSeekRef.current = false;
  };

  const handleSeek = (val: number) => {
    if (suppressSliderSeekRef.current || duration <= 0) return;
    const targetTime = (val / 100) * duration;
    const request = ++seekPreviewRequestRef.current;
    setSeekPreviewTime(targetTime);
    const core = coreRef.current;
    if (!core) {
      setSeekPreviewTime(null);
      return;
    }
    void core.seek(targetTime, 'user').finally(() => {
      if (seekPreviewRequestRef.current === request) setSeekPreviewTime(null);
    });
  };
  const handleVolume = (val: number) => {
    const v = videoRef.current;
    if (!v) return;
    // 夹取到 [0,1]：滑块域为 [0,1]，但仍兜底任何越界来源，杜绝 v.volume 触发 IndexSizeError
    const next = clampVolume(val);
    v.volume = next;
    v.muted = next === 0;
    setVolume(next);
    setIsMuted(next === 0);
    setAutoMuted(false);
    // FR-104：用户主动调音量 → 记忆其偏好
    saveVolumePref({ volume: next, muted: next === 0 });
  };
  const toggleMute = () => {
    const v = videoRef.current;
    if (!v) return;
    const next = !isMuted;
    v.muted = next;
    setIsMuted(next);
    setAutoMuted(false);
    saveVolumePref({ volume, muted: next });
  };
  // FR-102：取消静音自动播的静音态，恢复有声并撤下「点击取消静音」入口
  // FR-104：恢复有声后记忆当前音量为非静音偏好
  const unmute = () => {
    const v = videoRef.current;
    if (!v) return;
    v.muted = false;
    setIsMuted(false);
    setAutoMuted(false);
    saveVolumePref({ volume, muted: false });
  };

  const seekByTier = (direction: FrameStepDirection) => {
    const core = coreRef.current;
    if (core) void core.seekByTier(direction);
  };

  const stepFrame = (direction: FrameStepDirection) => {
    const core = coreRef.current;
    if (core) void core.stepFrame(direction);
  };

  const changeSeekTier = (tier: SeekTier) => {
    const core = coreRef.current;
    if (core) void core.setSeekTier(tier);
  };

  const isCurrentTrackOperation = (core: PlaybackCore, operationMediaId: number): boolean =>
    coreRef.current === core && mediaIdRef.current === operationMediaId;

  const isCurrentManifestRefresh = (
    core: PlaybackCore,
    backend: WebPlaybackBackend,
    operationMediaId: number,
    generation: number,
    source: PlaybackCommandContext,
  ): boolean =>
    manifestGenerationRef.current === generation &&
    backendRef.current === backend &&
    isCurrentTrackOperation(core, operationMediaId) &&
    isSameSource(source, commandFromSnapshot(core.getSnapshot()));

  const selectTrack = async (
    kind: TrackKind,
    trackId: string | null,
    failureTitle = kind === 'subtitle' ? '切换字幕失败' : '切换音轨失败',
  ): Promise<boolean> => {
    const core = coreRef.current;
    if (!core) return false;
    const result = await core.selectTrack(kind, trackId);
    if (result.status === 'failed' || result.status === 'unsupported') {
      notifyError(result.error?.message ?? '请稍后重试', failureTitle);
      return false;
    }
    return result.status === 'completed';
  };

  const refreshTracks = async (
    core: PlaybackCore,
    operationMediaId: number,
  ): Promise<TrackResponse | null> => {
    const generation = ++manifestGenerationRef.current;
    const backend = backendRef.current;
    const source = commandFromSnapshot(core.getSnapshot());
    if (!backend || !source || !onTrackManifestRefresh) return null;
    let response: TrackResponse;
    try {
      response = await onTrackManifestRefresh();
    } catch (error: unknown) {
      if (!isCurrentManifestRefresh(core, backend, operationMediaId, generation, source))
        return null;
      throw error;
    }
    if (!isCurrentManifestRefresh(core, backend, operationMediaId, generation, source)) return null;
    const command = commandFromSnapshot(core.getSnapshot());
    if (!command) return null;
    trackResponseRef.current = response;
    pendingTrackResponseRef.current = hasSameTracks(trackResponsePropRef.current, response)
      ? null
      : response;
    backend.tracks.updateResponse(response, command);
    syncTrackState(core);
    return response;
  };

  const handleSubtitleUpload = async (file: File) => {
    const core = coreRef.current;
    const operationMediaId = mediaId;
    if (!core || operationMediaId === undefined) return;
    let uploaded;
    try {
      uploaded = await uploadSubtitle(operationMediaId, file);
    } catch (error: unknown) {
      if (isCurrentTrackOperation(core, operationMediaId))
        notifyError(errorMessage(error), '上传字幕失败');
      return;
    }
    if (!isCurrentTrackOperation(core, operationMediaId)) return;
    let response: TrackResponse | null;
    try {
      response = await refreshTracks(core, operationMediaId);
    } catch (error: unknown) {
      if (isCurrentTrackOperation(core, operationMediaId))
        notifyError(errorMessage(error), '已上传但刷新失败');
      return;
    }
    if (response && isCurrentTrackOperation(core, operationMediaId))
      await selectTrack('subtitle', uploaded.id);
  };

  const handleSubtitleDelete = async (trackId: string) => {
    const core = coreRef.current;
    const operationMediaId = mediaId;
    if (!core || operationMediaId === undefined) return;
    const selection = core.getTrackSelection('subtitle');
    const uploaded = tracks.find((track) => track.id === trackId && track.source === 'uploaded');
    const shouldClose = Boolean(
      uploaded &&
        selection &&
        (selection.selectedTrackId === trackId || selection.effectiveTrackId === trackId),
    );
    const restoreTrackId = selection?.effectiveTrackId ?? selection?.selectedTrackId ?? null;
    if (shouldClose && !(await selectTrack('subtitle', null, '关闭字幕失败'))) return;
    try {
      await deleteSubtitle(operationMediaId, trackId);
    } catch (error: unknown) {
      if (!isCurrentTrackOperation(core, operationMediaId)) return;
      notifyError(errorMessage(error), '删除字幕失败');
      if (shouldClose && restoreTrackId)
        await selectTrack('subtitle', restoreTrackId, '恢复字幕失败');
      return;
    }
    if (!isCurrentTrackOperation(core, operationMediaId)) return;
    try {
      await refreshTracks(core, operationMediaId);
    } catch (error: unknown) {
      if (isCurrentTrackOperation(core, operationMediaId))
        notifyError(errorMessage(error), '字幕已删除但刷新失败');
    }
  };

  const changeRate = async (next: number) => {
    const core = coreRef.current;
    if (!core) return;
    const result = await core.setPlaybackRate(next);
    if (result.status === 'failed' || result.status === 'unsupported') {
      notifyError(result.error?.message ?? '当前播放路径不支持该倍速', '切换倍速失败');
    }
  };

  const changeQuality = async (quality: PlaybackQuality | null) => {
    const core = coreRef.current;
    if (!core) return;
    const disabledSaver = qualityState.dataSaver && (quality?.height ?? 0) > 480;
    const selection = quality === null
      ? ({ mode: 'auto' } as const)
      : {
          mode: 'manual' as const,
          quality: {
            ...(quality.bandwidth === undefined ? {} : { bandwidth: quality.bandwidth }),
            height: quality.height!,
          },
        };
    const result = await core.selectQuality(selection);
    if (result.status === 'failed' || result.status === 'unsupported') {
      notifyError(result.error?.message ?? '目标清晰度当前不可用', '切换清晰度失败');
      return;
    }
    if (disabledSaver) notifySuccess('已关闭省流量并切换到高画质');
  };

  const changeDataSaver = async (enabled: boolean) => {
    const core = coreRef.current;
    if (!core) return;
    const result = await core.setDataSaver(enabled);
    if (result.status === 'failed' || result.status === 'unsupported') {
      notifyError(result.error?.message ?? '省流量切换失败');
    }
  };

  const setAbPoint = async (point: 'a' | 'b') => {
    const core = coreRef.current;
    if (!core) return;
    const result = point === 'a' ? await core.setAbLoopA() : await core.setAbLoopB();
    if (result.status === 'failed' || result.status === 'unsupported') {
      notifyError(result.error?.message ?? 'A-B 区间无效', '设置 A-B 失败');
    }
  };

  const clearAbLoop = () => {
    const core = coreRef.current;
    if (core) void core.clearAbLoop();
  };

  // FR-104：键盘音量调节（夹取到 [0,1]，并解除静音）
  const nudgeVolume = (delta: number) => {
    const next = Math.min(1, Math.max(0, (isMuted ? 0 : volume) + delta));
    handleVolume(next);
  };

  // FR-104：全屏切换——对最外层容器调原生 Fullscreen API（不支持时静默）
  const toggleFullscreen = () => {
    const el = containerRef.current;
    if (!el) return;
    if (document.fullscreenElement === el) {
      void document.exitFullscreen?.();
    } else {
      void el.requestFullscreen?.();
    }
  };

  // FR-104：画中画切换（仅在浏览器支持时可达）
  const togglePip = () => {
    const v = videoRef.current;
    if (!v) return;
    if (document.pictureInPictureElement === v) {
      void document.exitPictureInPicture?.();
    } else {
      void v.requestPictureInPicture?.()?.catch?.(() => {
        /* 静默：部分流不支持 PiP */
      });
    }
  };

  // FR-104：按百分比跳转（数字键 0-9）也统一进入 core。
  const seekToPercent = (pct: number) => {
    const core = coreRef.current;
    if (core && duration > 0) void core.seek((pct / 100) * duration, 'user');
  };

  // FR-104：控件自动隐藏——鼠标移动时显示并重置定时器，播放中静止超时淡出
  const showControlsTemporarily = useCallback(() => {
    setControlsVisible(true);
    if (hideTimerRef.current) clearTimeout(hideTimerRef.current);
    hideTimerRef.current = setTimeout(() => {
      // 仅在播放中淡出；暂停时常驻可见
      if (videoRef.current && !videoRef.current.paused) setControlsVisible(false);
    }, CONTROLS_HIDE_DELAY);
  }, []);

  useEffect(
    () => () => {
      if (hideTimerRef.current) clearTimeout(hideTimerRef.current);
      const timer = previewPointerRef.current.timer;
      if (timer) clearTimeout(timer);
    },
    [],
  );

  // FR-104：键盘快捷键。仅在焦点不在输入控件时生效，避免干扰页面其它输入。
  const handleKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    const target = e.target as HTMLElement | null;
    const tag = target?.tagName;
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target?.isContentEditable)
      return;
    // 数字键 0-9：跳转到对应百分比
    if (/^[0-9]$/.test(e.key)) {
      e.preventDefault();
      seekToPercent(parseInt(e.key, 10) * 10);
      return;
    }
    switch (e.key) {
      case ' ':
      case 'Spacebar':
        e.preventDefault();
        togglePlay();
        break;
      case 'ArrowRight':
        e.preventDefault();
        seekByTier('next');
        break;
      case 'ArrowLeft':
        e.preventDefault();
        seekByTier('previous');
        break;
      case ',':
        e.preventDefault();
        stepFrame('previous');
        break;
      case '.':
        e.preventDefault();
        stepFrame('next');
        break;
      case 'ArrowUp':
        e.preventDefault();
        nudgeVolume(VOLUME_STEP);
        break;
      case 'ArrowDown':
        e.preventDefault();
        nudgeVolume(-VOLUME_STEP);
        break;
      case 'f':
      case 'F':
        e.preventDefault();
        toggleFullscreen();
        break;
      case 'm':
      case 'M':
        e.preventDefault();
        toggleMute();
        break;
      default:
        break;
    }
  };

  const fmt = (s: number, precise = false) => {
    if (!Number.isFinite(s)) return precise ? '0:00.000' : '0:00';
    if (!precise) {
      const totalSeconds = Math.floor(Math.max(0, s));
      return `${Math.floor(totalSeconds / 60)}:${(totalSeconds % 60).toString().padStart(2, '0')}`;
    }
    const totalMilliseconds = Math.round(Math.max(0, s) * 1000);
    const minutes = Math.floor(totalMilliseconds / 60000);
    const seconds = Math.floor((totalMilliseconds % 60000) / 1000);
    const milliseconds = totalMilliseconds % 1000;
    return `${minutes}:${seconds.toString().padStart(2, '0')}.${milliseconds
      .toString()
      .padStart(3, '0')}`;
  };
  const displayedCurrentTime = seekPreviewTime ?? currentTime;
  const showPreciseCurrentTime = seekPreviewTime !== null || !isPlaying;
  const playPct = duration > 0 ? (displayedCurrentTime / duration) * 100 : 0;
  const actualQualityLabel = qualityState.actualQuality?.label ?? abrLevel;
  const qualityLabel =
    qualityState.qualityMode === 'manual' && qualityState.manualQuality
      ? `${qualityState.manualQuality.label}（手动）`
      : actualQualityLabel
        ? `自动（当前 ${actualQualityLabel}）`
        : '自动';
  const abLoopLabel =
    abLoopState.a === null
      ? 'A/B 未设置'
      : `A ${fmt(abLoopState.a, true)} · ${abLoopState.b === null ? 'B 未设置' : `B ${fmt(abLoopState.b, true)}`}`;

  // FR-104：hover 进度条时的时间气泡定位（百分比）
  const hoverTime = hoverPct !== null && duration > 0 ? (hoverPct / 100) * duration : 0;
  // 控件区透明度：播放中静止淡出，其余常显
  const controlsOpacity = controlsVisible ? 1 : 0;

  return (
    <Box
      ref={containerRef}
      data-testid="video-player-root"
      data-frame-presentation={framePresentationCapability ?? 'pending'}
      data-frame-step-result={lastFrameStepResult}
      tabIndex={0}
      onKeyDown={handleKeyDown}
      onMouseMove={showControlsTemporarily}
      style={{
        display: 'flex',
        flexDirection: 'column',
        width: '100%',
        backgroundColor: 'black',
        borderRadius: 'var(--mantine-radius-lg)',
        overflow: 'hidden',
        outline: 'none',
        // FR-103 填充模式：撑满父容器剩余高度（播放页全屏沉浸用）
        ...(fill ? { flex: 1, height: '100%', minHeight: 0 } : {}),
      }}
    >
      <Box
        onDoubleClick={toggleFullscreen}
        style={{
          position: 'relative',
          width: '100%',
          backgroundColor: 'black',
          // FR-103：填充模式去掉固定 16:9，改为 flex 填充剩余高度；缺省维持 16:9
          ...(fill ? { flex: 1, minHeight: 0 } : { aspectRatio: '16/9' }),
        }}
      >
        <video
          ref={videoRef}
          poster={poster}
          style={{ width: '100%', height: '100%', backgroundColor: 'black', objectFit: 'contain' }}
          playsInline
        />
        {/* FR-52：目标编码不受支持且无回退源时的提示（不抛 Network Error） */}
        {resolved.unsupported && (
          <Box
            role="alert"
            style={{
              position: 'absolute',
              inset: 0,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              backgroundColor: 'rgba(0,0,0,0.6)',
              padding: '0 1rem',
            }}
          >
            <Text c="white" size="sm" ta="center">
              当前浏览器不支持该视频编码，无法播放
            </Text>
          </Box>
        )}
        {qualityState.dataSaverBlocked && (
          <Box
            role="alert"
            style={{
              position: 'absolute',
              inset: 0,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              backgroundColor: 'rgba(0,0,0,0.7)',
              padding: '0 1rem',
            }}
          >
            <Text c="white" size="sm" ta="center">
              当前视频无 480p 或更低档位，请关闭省流量后播放
            </Text>
          </Box>
        )}
        {autoPlayBlocked && !isPlaying && !qualityState.dataSaverBlocked && (
          <Box
            style={{
              position: 'absolute',
              inset: 0,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              backgroundColor: 'rgba(0,0,0,0.5)',
              cursor: 'pointer',
            }}
            onClick={togglePlay}
          >
            <Text c="white" size="lg">
              点击播放
            </Text>
          </Box>
        )}
        {/* FR-102：静音自动播时角落提供取消静音入口，点击恢复有声 */}
        {autoMuted && isMuted && !autoPlayBlocked && (
          <Box
            component="button"
            type="button"
            onClick={unmute}
            aria-label="点击取消静音"
            style={{
              position: 'absolute',
              top: 12,
              right: 12,
              display: 'flex',
              alignItems: 'center',
              gap: 6,
              padding: '6px 12px',
              border: 'none',
              borderRadius: 'var(--mantine-radius-xl)',
              backgroundColor: 'rgba(0,0,0,0.6)',
              color: 'white',
              cursor: 'pointer',
              fontSize: 'var(--mantine-font-size-sm)',
            }}
          >
            <IconVolumeOff size={16} />
            <Text component="span" c="white" size="sm">
              点击取消静音
            </Text>
          </Box>
        )}
        {isWaiting && !autoPlayBlocked && (
          <Box
            aria-label="缓冲等待"
            role="status"
            style={{
              position: 'absolute',
              inset: 0,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              backgroundColor: 'rgba(0,0,0,0.45)',
              pointerEvents: 'none',
            }}
          >
            <Text c="white" size="sm">
              等待新数据…
            </Text>
          </Box>
        )}
        {trackResponse && (
          <SubtitleOverlay
            currentTime={currentTime}
            cues={trackCues}
            preferences={subtitlePreferences}
          />
        )}
        {!trackResponse && subtitleVisible && subtitleText && (
          <Box
            style={{
              position: 'absolute',
              insetInline: 0,
              bottom: '8%',
              display: 'flex',
              justifyContent: 'center',
              pointerEvents: 'none',
              padding: '0 1rem',
            }}
          >
            {/* FR-104：字幕字号档（SUBTITLE_SCALES）映射到 fontSize */}
            <Text
              component="span"
              c="white"
              ta="center"
              style={{
                display: 'inline-block',
                padding: '0.25rem 0.75rem',
                borderRadius: 'var(--mantine-radius-sm)',
                backgroundColor: 'rgba(0,0,0,0.6)',
                maxWidth: '80%',
                fontSize: SUBTITLE_SCALES[subtitleScale],
              }}
            >
              {subtitleText}
            </Text>
          </Box>
        )}
      </Box>

      {/* 控件栏（FR-104）：播放中鼠标静止数秒淡出，移动 / 暂停时重现 */}
      <Box
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 'var(--mantine-spacing-sm)',
          padding: '0.5rem 1rem',
          backgroundColor: 'var(--mantine-color-default)',
          opacity: controlsOpacity,
          transition: 'opacity 0.3s ease',
        }}
      >
        <ActionIcon
          variant="subtle"
          color="gray"
          onClick={togglePlay}
          disabled={qualityState.dataSaverBlocked}
          aria-label={isPlaying ? '暂停' : '播放'}
        >
          {isPlaying ? <IconPlayerPause size={22} /> : <IconPlayerPlay size={22} />}
        </ActionIcon>

        <ActionIcon
          variant="subtle"
          color="gray"
          onClick={() => seekByTier('previous')}
          aria-label={`后退 ${seekTierLabel(seekTier)}`}
        >
          <IconPlayerSkipBack size={20} />
        </ActionIcon>
        <ActionIcon
          variant="subtle"
          color="gray"
          onClick={() => seekByTier('next')}
          aria-label={`前进 ${seekTierLabel(seekTier)}`}
        >
          <IconPlayerSkipForward size={20} />
        </ActionIcon>
        <Menu position="top" withinPortal>
          <Menu.Target>
            <ActionIcon
              variant="subtle"
              color="gray"
              aria-label={`定位档位：${seekTierLabel(seekTier)}`}
              style={{ width: 'auto', paddingInline: 6 }}
            >
              <Text size="xs" fw={600}>
                {seekTierLabel(seekTier)}
              </Text>
            </ActionIcon>
          </Menu.Target>
          <Menu.Dropdown>
            <Menu.Label>定位档位</Menu.Label>
            {SEEK_TIERS.map((tier) => (
              <Menu.Item
                key={seekTierLabel(tier)}
                onClick={() => changeSeekTier(tier)}
                fw={seekTierLabel(tier) === seekTierLabel(seekTier) ? 700 : 400}
              >
                {seekTierLabel(tier)}
              </Menu.Item>
            ))}
          </Menu.Dropdown>
        </Menu>
        <ActionIcon
          variant="subtle"
          color="gray"
          onClick={() => stepFrame('previous')}
          aria-label="前一帧"
        >
          <IconPlayerTrackPrev size={20} />
        </ActionIcon>
        <ActionIcon
          variant="subtle"
          color="gray"
          onClick={() => stepFrame('next')}
          aria-label="后一帧"
        >
          <IconPlayerTrackNext size={20} />
        </ActionIcon>

        <Box
          style={{
            flex: 1,
            display: 'flex',
            alignItems: 'center',
            gap: 'var(--mantine-spacing-xs)',
          }}
        >
          <Text
            data-testid="video-current-time"
            size="xs"
            c="dimmed"
            style={{ fontVariantNumeric: 'tabular-nums' }}
          >
            {fmt(displayedCurrentTime, showPreciseCurrentTime)}
          </Text>
          {/* FR-104：进度条加大热区（容器高度抬升）+ hover 时间气泡；FR-93 修偏配色由蓝转品牌紫 */}
          <Box
            data-testid="video-progress-preview"
            style={{
              flex: 1,
              position: 'relative',
              height: 16,
              display: 'flex',
              alignItems: 'center',
              touchAction: timelinePreview ? 'none' : 'auto',
            }}
            onMouseMove={(event) => {
              const rect = event.currentTarget.getBoundingClientRect();
              if (rect.width <= 0) return;
              const percent = Math.min(
                100,
                Math.max(0, ((event.clientX - rect.left) / rect.width) * 100),
              );
              setHoverPct(percent);
              showTimelinePreview(event.currentTarget, event.clientX);
            }}
            onMouseLeave={() => {
              setHoverPct(null);
              hideTimelinePreview();
            }}
            onPointerDownCapture={handlePreviewPointerDownCapture}
            onPointerDown={handlePreviewPointerDown}
            onPointerMove={handlePreviewPointerMove}
            onPointerUp={handlePreviewPointerUp}
            onPointerCancel={handlePreviewPointerCancel}
          >
            <Slider
              value={Math.min(bufferedProgress, 100)}
              size={4}
              color="gray"
              radius="xl"
              thumbSize={0}
              style={{ position: 'absolute', width: '100%', pointerEvents: 'none' }}
            />
            <Slider
              aria-label="播放进度"
              thumbLabel="播放进度"
              value={playPct}
              onChange={handleSeek}
              size={4}
              color="purple"
              radius="xl"
              thumbSize={12}
              style={{ position: 'absolute', width: '100%' }}
            />
            {timelinePreview && (
              <Box
                data-testid="timeline-preview-overlay"
                role="status"
                style={{
                  position: 'absolute',
                  bottom: '100%',
                  left: `${timelinePreview.leftPercent}%`,
                  transform: 'translateX(-50%)',
                  marginBottom: 6,
                  padding: 4,
                  borderRadius: 'var(--mantine-radius-sm)',
                  backgroundColor: 'rgba(0,0,0,0.85)',
                  color: 'white',
                  whiteSpace: 'nowrap',
                  pointerEvents: 'none',
                }}
              >
                {timelinePreview.imageUrl && timelinePreview.hit && (
                  <Box
                    data-testid="timeline-preview-sprite"
                    style={{
                      height: timelinePreview.hit.sprite.height,
                      overflow: 'hidden',
                      width: timelinePreview.hit.sprite.width,
                    }}
                  >
                    <img
                      alt=""
                      src={timelinePreview.imageUrl}
                      onError={clearTimelinePreviewImage}
                      style={{
                        display: 'block',
                        maxWidth: 'none',
                        transform: `translate(-${timelinePreview.hit.sprite.x}px, -${timelinePreview.hit.sprite.y}px)`,
                      }}
                    />
                  </Box>
                )}
                <Text c="white" size="xs" ta="center">
                  {fmt(timelinePreview.mediaTime)}
                </Text>
              </Box>
            )}
            {!timelinePreview && hoverPct !== null && (
              <Box
                style={{
                  position: 'absolute',
                  bottom: '100%',
                  left: `${hoverPct}%`,
                  transform: 'translateX(-50%)',
                  marginBottom: 6,
                  padding: '2px 6px',
                  borderRadius: 'var(--mantine-radius-sm)',
                  backgroundColor: 'rgba(0,0,0,0.8)',
                  color: 'white',
                  fontSize: 'var(--mantine-font-size-xs)',
                  whiteSpace: 'nowrap',
                  pointerEvents: 'none',
                }}
              >
                {fmt(hoverTime)}
              </Box>
            )}
          </Box>
          <Text size="xs" c="dimmed" style={{ fontVariantNumeric: 'tabular-nums' }}>
            {fmt(duration)}
          </Text>
        </Box>

        {isABR && (
          <Menu position="top" withinPortal>
            <Menu.Target>
              <ActionIcon
                variant="subtle"
                color="gray"
                aria-label="清晰度"
                style={{ width: 'auto', paddingInline: 6 }}
              >
                <Text size="xs" fw={600}>
                  {qualityLabel}
                </Text>
              </ActionIcon>
            </Menu.Target>
            <Menu.Dropdown>
              <Menu.Label>清晰度</Menu.Label>
              <Menu.Item onClick={() => void changeQuality(null)} fw={qualityState.qualityMode === 'auto' ? 700 : 400}>
                自动
              </Menu.Item>
              {qualityState.qualities.map((quality) => (
                <Menu.Item
                  key={quality.id}
                  onClick={() => void changeQuality(quality)}
                  fw={qualityState.manualQuality?.id === quality.id ? 700 : 400}
                >
                  {quality.label}
                </Menu.Item>
              ))}
            </Menu.Dropdown>
          </Menu>
        )}

        {isABR && (
          <Switch
            size="xs"
            label="省流量"
            checked={qualityState.dataSaver}
            onChange={(event) => void changeDataSaver(event.currentTarget.checked)}
          />
        )}

        <Menu position="top" withinPortal>
          <Menu.Target>
            <ActionIcon
              variant="subtle"
              color="gray"
              aria-label="A-B 循环"
              style={{ width: 'auto', paddingInline: 6 }}
            >
              <Text size="xs" fw={600}>
                A-B
              </Text>
            </ActionIcon>
          </Menu.Target>
          <Menu.Dropdown>
            <Menu.Label>A-B 循环</Menu.Label>
            <Menu.Item onClick={() => void setAbPoint('a')}>设置 A 点</Menu.Item>
            <Menu.Item onClick={() => void setAbPoint('b')}>设置 B 点</Menu.Item>
            <Menu.Item onClick={clearAbLoop}>清除 A-B</Menu.Item>
          </Menu.Dropdown>
        </Menu>
        <Text size="xs" c={abLoopState.enabled ? 'green' : 'dimmed'}>
          {abLoopLabel}
        </Text>

        {/* FR-104：倍速菜单 */}
        <Menu position="top" withinPortal>
          <Menu.Target>
            <ActionIcon
              variant="subtle"
              color="gray"
              aria-label="播放速度"
              style={{ width: 'auto', paddingInline: 6 }}
            >
              <Text size="xs" fw={600}>
                {rate}×
              </Text>
            </ActionIcon>
          </Menu.Target>
          <Menu.Dropdown>
            <Menu.Label>播放速度</Menu.Label>
            {PLAYBACK_RATES.map((r) => (
              <Menu.Item key={r} onClick={() => void changeRate(r)} fw={r === rate ? 700 : 400}>
                {r}×
              </Menu.Item>
            ))}
          </Menu.Dropdown>
        </Menu>

        {/* FR-104：字幕字号档（仅在有字幕时展示） */}
        {!trackResponse && subtitleVisible && (
          <Menu position="top" withinPortal>
            <Menu.Target>
              <ActionIcon variant="subtle" color="gray" aria-label="字幕字号">
                <IconAdjustments size={18} />
              </ActionIcon>
            </Menu.Target>
            <Menu.Dropdown>
              <Menu.Label>字幕字号</Menu.Label>
              <Menu.Item
                onClick={() => setSubtitleScale('small')}
                fw={subtitleScale === 'small' ? 700 : 400}
              >
                小
              </Menu.Item>
              <Menu.Item
                onClick={() => setSubtitleScale('medium')}
                fw={subtitleScale === 'medium' ? 700 : 400}
              >
                中
              </Menu.Item>
              <Menu.Item
                onClick={() => setSubtitleScale('large')}
                fw={subtitleScale === 'large' ? 700 : 400}
              >
                大
              </Menu.Item>
            </Menu.Dropdown>
          </Menu>
        )}

        {trackResponse && trackSelections && (
          <VideoTrackControls
            tracks={tracks}
            selections={trackSelections}
            preferences={subtitlePreferences}
            onPreferencesChange={setSubtitlePreferences}
            onSelect={(kind, trackId) => void selectTrack(kind, trackId)}
            onUpload={(file) => void handleSubtitleUpload(file)}
            onDelete={(trackId) => void handleSubtitleDelete(trackId)}
          />
        )}

        <Box style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
          {/* FR-104：音量图标语义灰（修偏 color="white" 亮色主题不可见）；音量条由蓝转品牌紫 */}
          <ActionIcon
            variant="subtle"
            color="gray"
            onClick={toggleMute}
            aria-label={isMuted ? '取消静音' : '静音'}
          >
            {isMuted || volume === 0 ? <IconVolumeOff size={18} /> : <IconVolume size={18} />}
          </ActionIcon>
          <Slider
            value={isMuted ? 0 : volume}
            onChange={handleVolume}
            min={0}
            max={1}
            step={0.05}
            size={4}
            color="purple"
            radius="xl"
            showLabelOnHover={false}
            aria-label="音量"
            style={{ width: '5rem' }}
          />
        </Box>

        {/* FR-104：画中画（浏览器不支持则隐藏） */}
        {pipSupported && (
          <ActionIcon variant="subtle" color="gray" onClick={togglePip} aria-label="画中画">
            <IconPictureInPicture size={18} />
          </ActionIcon>
        )}

        {/* FR-104：全屏切换 */}
        <ActionIcon
          variant="subtle"
          color="gray"
          onClick={toggleFullscreen}
          aria-label={isFullscreen ? '退出全屏' : '全屏'}
        >
          {isFullscreen ? <IconMinimize size={18} /> : <IconMaximize size={18} />}
        </ActionIcon>

        {(framePresentationCapability === 'approximate' || approximateFrameStep) && (
          <Text size="xs" c="yellow" fw={500} role="status">
            近似逐帧
          </Text>
        )}

      </Box>
    </Box>
  );
}
