import { useState, useEffect, useCallback, useRef, Suspense, lazy } from 'react';
import { useNavigate } from 'react-router-dom';
import { useClipboard } from '@mantine/hooks';
import {
  Modal,
  Group,
  Stack,
  Button,
  ActionIcon,
  Text,
  Box,
  ScrollArea,
  Divider,
  Tooltip,
  Anchor,
  Badge,
  SimpleGrid,
  UnstyledButton,
  Alert,
  Select,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import {
  IconChevronLeft,
  IconChevronRight,
  IconX,
  IconMaximize,
  IconMinimize,
  IconDownload,
  IconRotateClockwise,
  IconRotate2,
  IconPlayerPlay,
  IconPlayerPause,
  IconHeart,
  IconHeartFilled,
  IconShare,
  IconTag,
  IconLayoutSidebarRightCollapse,
  IconLayoutSidebarRightExpand,
  IconCopy,
  IconCheck,
  IconMapPin,
  IconCamera,
  IconAperture,
  IconClock,
  IconAdjustments,
  IconPhoto,
  IconMovie,
  IconAlertCircle,
  IconFileUpload,
} from '@tabler/icons-react';
// FR-102：懒加载 VideoPlayer，仅在灯箱内实际查看视频时才加载其 mpegts.js 等重内核，
// 避免图片预览场景白白拉入大体积播放内核。
const VideoPlayer = lazy(() => import('@/components/VideoPlayer'));
import ShareDialog from '@/components/ShareDialog';
import BatchActionsModals from '@/components/BatchActionsModals';
import ImageEditorPanel from '@/components/ImageEditorPanel';
import ClipExportPanel from '@/components/ClipExportPanel';
import { useBatchActions } from '@/hooks/useBatchActions';
import { isImageFile, mediaDisplayName } from '@/utils/media';
import { mediaRawUrl, mediaStreamUrl } from '@/utils/media-url';
import {
  formatSize,
  formatDuration,
  formatAperture,
  formatShutter,
  formatIso,
} from '@/utils/format';
import {
  enqueueMetadataWriteback,
  generateMediaCovers,
  getMediaCovers,
  getMediaInference,
  getMediaMetadata,
  getMediaTags,
  selectMediaCover,
  updateMediaContentRating,
} from '@/api/library';
import { confirmAIResult, listAIResults, rejectAIResult } from '@/api/ai';
import { getTask } from '@/api/tasks';
import { extractErrorCode, extractErrorMessage } from '@/utils/error';
import {
  CONTENT_RATING_OPTIONS,
  contentRatingBadgeColor,
  formatContentRatingLabel,
} from '@/utils/content-rating';
import type {
  AIResult,
  MediaCoversResponse,
  MediaFile,
  MediaInference,
  MediaMetadata,
  NormalizedEmbeddedMetadata,
  Tag,
} from '@/types';

interface MediaDetailPanelProps {
  files: MediaFile[];
  /** 选中项在 files 中的下标；null 表示面板关闭 */
  initialIndex: number | null;
  onClose: () => void;
  customImageExtensions: Record<number, string[]>;
  /** 切换收藏（FR-106）：父层负责调接口并刷新列表，未传则不显收藏按钮 */
  onToggleFavorite?: (f: MediaFile) => void;
  /** 按标签筛选（FR2-032）：点击标签 chip 时回调，父页设置 tag_id */
  onFilterByTag?: (tag: Tag) => void;
  /** 内容分级变更后通知父层刷新列表项（FR2-051） */
  onContentRatingChange?: (mediaID: number, contentRating: string) => void;
}

const ZOOM_MIN = 1;
const ZOOM_MAX = 4;
const ZOOM_STEP = 0.25;
// 双击放大目标倍率（FR-105）：1×↔2× 切换
const ZOOM_DOUBLE_CLICK = 2;
// 幻灯片自动切换间隔（毫秒，FR-105）
const SLIDESHOW_INTERVAL_MS = 3000;
// 相邻预加载半径（FR-105）：预取前后各 1 张原图，切换不闪白
const PRELOAD_RADIUS = 1;
const COVER_TASK_POLL_INTERVAL_MS = 500;
const COVER_TASK_MAX_POLLS = 120;
const COVER_CHANGED_EVENT = 'jianvideo:cover-changed';

async function waitForCoverTask(taskID: number): Promise<void> {
  for (let poll = 0; poll < COVER_TASK_MAX_POLLS; poll += 1) {
    const task = await getTask(String(taskID));
    if (task.status === 'succeeded') return;
    if (task.status === 'failed' || task.status === 'canceled') {
      throw new Error(task.error || '封面生成任务未完成');
    }
    await new Promise((resolve) => setTimeout(resolve, COVER_TASK_POLL_INTERVAL_MS));
  }
  throw new Error('封面生成任务等待超时');
}

function notifyCoverChanged(mediaID: number): void {
  window.dispatchEvent(new CustomEvent(COVER_CHANGED_EVENT, { detail: { mediaID } }));
}

// 详情区定宽 label 列宽（FR-106）：定义列表两列对齐，键值成对易读
const DETAIL_LABEL_WIDTH = 64;

/**
 * 一行「标签 / 值」详情（FR-106）：定宽 label 列 + 紧凑 value 列的定义列表行（dt/dd）。
 * 复用 FR-101 系统页定宽两列范式；可选前置 tabler 图标，值为空时不渲染。
 */
function DetailRow({
  label,
  value,
  icon,
}: {
  label: string;
  value: string | number | null | undefined;
  icon?: React.ReactNode;
}) {
  if (value === null || value === undefined || value === '') return null;
  return (
    <Box
      component="div"
      style={{ display: 'flex', gap: 'var(--mantine-spacing-sm)', alignItems: 'baseline' }}
    >
      <Text
        component="dt"
        size="sm"
        c="dimmed"
        style={{
          width: DETAIL_LABEL_WIDTH,
          flexShrink: 0,
          display: 'flex',
          alignItems: 'center',
          gap: 4,
        }}
      >
        {icon}
        {label}
      </Text>
      <Text component="dd" size="sm" style={{ margin: 0, minWidth: 0, wordBreak: 'break-word' }}>
        {value}
      </Text>
    </Box>
  );
}

/** 复制按钮（FR-106）：复制成功 2 秒内显勾选反馈 */
function CopyIconButton({ value, label }: { value: string; label: string }) {
  const clipboard = useClipboard({ timeout: 2000 });
  return (
    <Tooltip label={clipboard.copied ? '已复制' : label}>
      <ActionIcon
        variant="subtle"
        size="sm"
        color={clipboard.copied ? 'teal' : 'gray'}
        aria-label={label}
        onClick={() => clipboard.copy(value)}
      >
        {clipboard.copied ? <IconCheck size={14} /> : <IconCopy size={14} />}
      </ActionIcon>
    </Tooltip>
  );
}

function formatTime(s?: string | null): string {
  if (!s) return '';
  const d = new Date(s);
  return isNaN(d.getTime()) ? '' : d.toLocaleString();
}

// 媒体时间来源中文标注（FR-38）
const MEDIA_TIME_SOURCE_LABEL: Record<string, string> = {
  exif: 'EXIF',
  filename: '文件名',
  created: '创建时间',
  modified: '修改时间',
};

/** 是否含可展示的 EXIF 信息（FR-38） */
function hasExif(f: MediaFile): boolean {
  return !!(
    f.media_time ||
    f.camera ||
    f.lens ||
    f.aperture ||
    f.shutter ||
    (f.iso ?? 0) > 0 ||
    (f.gps_lat ?? 0) !== 0 ||
    (f.gps_lon ?? 0) !== 0
  );
}

function parseNormalizedMetadata(item: MediaMetadata): NormalizedEmbeddedMetadata | null {
  try {
    return JSON.parse(item.normalized_json) as NormalizedEmbeddedMetadata;
  } catch {
    return null;
  }
}

function streamLabel(stream: { codec_name?: string; language?: string; title?: string }): string {
  return [stream.codec_name, stream.language, stream.title].filter(Boolean).join(' · ');
}

function EmbeddedMetadataInfo({
  items,
  error,
  loading,
}: {
  items: MediaMetadata[];
  error: string | null;
  loading: boolean;
}) {
  return (
    <>
      <Divider my={4} label="文件自带元数据" labelPosition="left" />
      {loading && (
        <Text size="xs" c="dimmed">
          加载元数据中…
        </Text>
      )}
      {!loading && error && (
        <Text size="xs" c="red" role="alert">
          {error}
        </Text>
      )}
      {!loading && !error && items.length === 0 && (
        <Text size="xs" c="dimmed">
          暂无解析到的嵌入元数据
        </Text>
      )}
      {!loading &&
        !error &&
        items.map((item) => {
          const metadata = parseNormalizedMetadata(item);
          if (!metadata) {
            return (
              <Text key={item.id} size="xs" c="dimmed">
                元数据记录无法解析（来源 {item.source}）
              </Text>
            );
          }
          const video = metadata.video_streams?.[0];
          const color = video?.color;
          const videoValue = video
            ? [
                video.codec_name,
                video.width && video.height ? `${video.width}×${video.height}` : '',
                video.frame_rate,
                color?.space,
                color?.transfer,
              ]
                .filter(Boolean)
                .join(' · ')
            : '';
          return (
            <Box key={item.id} component="dl" style={{ margin: 0 }} mb="xs">
              <DetailRow
                label="解析来源"
                value={`${item.source} · ${item.tool}${item.tool_version ? ` ${item.tool_version}` : ''}${item.stale ? '（待刷新）' : ''}`}
              />
              <DetailRow
                label="解析时间"
                value={item.parsed_at ? formatTime(item.parsed_at) : ''}
              />
              <DetailRow label="容器" value={metadata.container?.format_name} />
              <DetailRow label="视频流" value={videoValue} />
              <DetailRow
                label="音频流"
                value={metadata.audio_streams?.map(streamLabel).filter(Boolean).join('；')}
              />
              <DetailRow
                label="字幕流"
                value={metadata.subtitle_streams?.map(streamLabel).filter(Boolean).join('；')}
              />
              <DetailRow label="内嵌标题" value={metadata.tags?.title} />
              <DetailRow
                label="IPTC"
                value={
                  metadata.image?.iptc
                    ? Object.values(metadata.image.iptc).filter(Boolean).join('；')
                    : ''
                }
              />
              <DetailRow
                label="XMP"
                value={
                  metadata.image?.xmp
                    ? Object.values(metadata.image.xmp).filter(Boolean).join('；')
                    : ''
                }
              />
            </Box>
          );
        })}
    </>
  );
}

/** 标签列表与按标签筛选入口（FR2-032） */
function MediaTagsSection({
  tags,
  loading,
  error,
  onFilterByTag,
}: {
  tags: Tag[];
  loading: boolean;
  error: string | null;
  onFilterByTag?: (tag: Tag) => void;
}) {
  return (
    <>
      <Divider my={4} label="标签" labelPosition="left" />
      {loading && (
        <Text size="xs" c="dimmed">
          加载标签中…
        </Text>
      )}
      {!loading && error && (
        <Text size="xs" c="red" role="alert">
          {error}
        </Text>
      )}
      {!loading && !error && tags.length === 0 && (
        <Text size="xs" c="dimmed">
          尚未打标签
        </Text>
      )}
      {!loading && !error && tags.length > 0 && (
        <Group gap={6} wrap="wrap" aria-label="媒体标签">
          {tags.map((tag) =>
            onFilterByTag ? (
              <Badge
                key={tag.id}
                component="button"
                type="button"
                variant="light"
                style={{ cursor: 'pointer' }}
                onClick={() => onFilterByTag(tag)}
                aria-label={`按标签筛选：${tag.name}`}
              >
                {tag.name}
              </Badge>
            ) : (
              <Badge key={tag.id} variant="light">
                {tag.name}
              </Badge>
            ),
          )}
        </Group>
      )}
    </>
  );
}

/** 离线影视推断展示（FR2-032） */
function InferenceSection({
  inference,
  loading,
  error,
}: {
  inference: MediaInference | null;
  loading: boolean;
  error: string | null;
}) {
  return (
    <>
      <Divider my={4} label="影视信息" labelPosition="left" />
      {loading && (
        <Text size="xs" c="dimmed">
          加载推断信息中…
        </Text>
      )}
      {!loading && error && (
        <Text size="xs" c="red" role="alert">
          {error}
        </Text>
      )}
      {!loading && !error && !inference && (
        <Text size="xs" c="dimmed">
          暂无本地推断片名
        </Text>
      )}
      {!loading && !error && inference && (
        <Box component="dl" style={{ margin: 0 }} aria-label="影视推断信息">
          <DetailRow label="片名" value={inference.title} />
          <DetailRow label="类型" value={inference.kind} />
          <DetailRow label="年份" value={inference.year > 0 ? String(inference.year) : ''} />
          <DetailRow
            label="季/集"
            value={
              inference.season > 0 || inference.episode > 0
                ? `S${String(inference.season).padStart(2, '0')}E${String(inference.episode).padStart(2, '0')}`
                : ''
            }
          />
          <DetailRow label="集标题" value={inference.episode_title} />
          <DetailRow
            label="来源"
            value={`${inference.manual ? '人工纠正' : '自动推断'} · ${inference.source}`}
          />
        </Box>
      )}
    </>
  );
}

/** AI 结果审核（FR2-012）：列表 + 确认/驳回；关闭门仅提示，不阻断详情 */
function AIResultsSection({
  results,
  loading,
  error,
  busyID,
  onConfirm,
  onReject,
}: {
  results: AIResult[];
  loading: boolean;
  error: string | null;
  busyID: number | null;
  onConfirm: (id: number) => void;
  onReject: (id: number) => void;
}) {
  return (
    <>
      <Divider my={4} label="AI 结果" labelPosition="left" />
      {loading && (
        <Text size="xs" c="dimmed">
          加载 AI 结果中…
        </Text>
      )}
      {!loading && error && (
        <Text size="xs" c="dimmed" role="status">
          {error}
        </Text>
      )}
      {!loading && !error && results.length === 0 && (
        <Text size="xs" c="dimmed">
          暂无 AI 结果
        </Text>
      )}
      {!loading &&
        !error &&
        results.map((item) => (
          <Stack key={item.id} gap={4} mb={6} aria-label={`AI 结果 ${item.id}`}>
            <Group gap="xs" wrap="wrap">
              <Badge size="sm" variant="light" color="grape">
                {item.task_type}
              </Badge>
              {item.manual ? (
                <Badge size="sm" color="teal">
                  已确认
                </Badge>
              ) : (
                <Badge size="sm" color="gray" variant="outline">
                  待审
                </Badge>
              )}
              <Text size="xs" c="dimmed">
                {item.model_id}
              </Text>
            </Group>
            <Text size="xs" lineClamp={3} style={{ wordBreak: 'break-all' }}>
              {item.payload_json || '{}'}
            </Text>
            {!item.manual && (
              <Group gap="xs">
                <Button
                  size="compact-xs"
                  variant="light"
                  color="teal"
                  loading={busyID === item.id}
                  onClick={() => onConfirm(item.id)}
                >
                  确认
                </Button>
                <Button
                  size="compact-xs"
                  variant="light"
                  color="red"
                  loading={busyID === item.id}
                  onClick={() => onReject(item.id)}
                >
                  驳回
                </Button>
              </Group>
            )}
          </Stack>
        ))}
    </>
  );
}

/**
 * 文件详情面板（FR-34）：左侧预览（图片可滚轮缩放 / 视频内嵌播放器直接播放，FR-102）、右侧元数据，
 * 支持全屏切换、←/→ 上下一项、Esc 关闭。EXIF 区块由 FR-38 在右侧补充。
 */
export default function MediaDetailPanel({
  files,
  initialIndex,
  onClose,
  customImageExtensions,
  onToggleFavorite,
  onFilterByTag,
  onContentRatingChange,
}: MediaDetailPanelProps) {
  const navigate = useNavigate();
  // 打标签复用 FR-91 批量编排（单 id 列表即可）：零新后端端点
  const batch = useBatchActions();
  // 分享弹窗开合（FR-106）：复用既有 ShareDialog（resourceType='media'）
  const [shareOpened, setShareOpened] = useState(false);
  // 内容分级本地态（FR2-051）：保存成功后回写并通知父层
  const [contentRating, setContentRating] = useState('');
  const [ratingSaving, setRatingSaving] = useState(false);
  // 图片编辑导出（FR2-038）
  const [imageEditorOpened, setImageEditorOpened] = useState(false);
  // 视频粗剪导出（FR2-039）
  const [clipExportOpened, setClipExportOpened] = useState(false);
  // 危险写回原文件二次确认（FR2-033）
  const [writebackConfirmOpened, setWritebackConfirmOpened] = useState(false);
  const [writebackSubmitting, setWritebackSubmitting] = useState(false);
  const [writebackTaskId, setWritebackTaskId] = useState<string | null>(null);
  const [writebackStatus, setWritebackStatus] = useState<string | null>(null);
  // 信息栏折叠（FR-106）：折叠后右侧详情收起、左侧预览吃满，纯图沉浸
  const [infoCollapsed, setInfoCollapsed] = useState(false);

  const opened = initialIndex !== null;
  const [idx, setIdx] = useState<number>(initialIndex ?? 0);
  const [fullscreen, setFullscreen] = useState(false);
  const [zoom, setZoom] = useState(1);
  // 平移量（FR-105）：放大后拖拽查看图片别处
  const [pan, setPan] = useState({ x: 0, y: 0 });
  // 旋转角度（FR-105）：0/90/180/270，左右各 90°
  const [rotation, setRotation] = useState(0);
  const [embeddedMetadata, setEmbeddedMetadata] = useState<MediaMetadata[]>([]);
  const [metadataLoading, setMetadataLoading] = useState(false);
  const [metadataError, setMetadataError] = useState<string | null>(null);
  const [mediaTags, setMediaTags] = useState<Tag[]>([]);
  const [tagsLoading, setTagsLoading] = useState(false);
  const [tagsError, setTagsError] = useState<string | null>(null);
  const [inference, setInference] = useState<MediaInference | null>(null);
  const [inferenceLoading, setInferenceLoading] = useState(false);
  const [inferenceError, setInferenceError] = useState<string | null>(null);
  const [aiResults, setAIResults] = useState<AIResult[]>([]);
  const [aiLoading, setAILoading] = useState(false);
  const [aiError, setAIError] = useState<string | null>(null);
  const [aiBusyID, setAIBusyID] = useState<number | null>(null);
  const [covers, setCovers] = useState<MediaCoversResponse>({ cover: null, candidates: [] });
  const [coverGenerating, setCoverGenerating] = useState(false);
  // 幻灯片自动轮播开关（FR-105）
  const [slideshow, setSlideshow] = useState(false);

  // 鼠标 / 触摸拖拽平移的过程态（FR-105）：起点坐标与起始平移量，拖拽中不触发渲染
  const dragRef = useRef<{
    active: boolean;
    startX: number;
    startY: number;
    panX: number;
    panY: number;
  }>({
    active: false,
    startX: 0,
    startY: 0,
    panX: 0,
    panY: 0,
  });
  // 双指捏合缩放的过程态（FR-105）：起始两指间距与起始缩放
  const pinchRef = useRef<{ active: boolean; startDist: number; startZoom: number }>({
    active: false,
    startDist: 0,
    startZoom: 1,
  });

  const total = files.length;

  // 重新打开（initialIndex 变化）时定位并复位查看状态
  useEffect(() => {
    if (initialIndex !== null) {
      setIdx(initialIndex);
      setZoom(1);
      setPan({ x: 0, y: 0 });
      setRotation(0);
      setSlideshow(false);
    }
  }, [initialIndex]);

  // 上/下一项环绕循环（FR-105）：到头时首↔尾循环
  const goPrev = useCallback(() => {
    setIdx((i) => (total > 0 ? (i - 1 + total) % total : i));
  }, [total]);
  const goNext = useCallback(() => {
    setIdx((i) => (total > 0 ? (i + 1) % total : i));
  }, [total]);

  // 换项复位缩放 / 平移 / 旋转（FR-105）
  useEffect(() => {
    setZoom(1);
    setPan({ x: 0, y: 0 });
    setRotation(0);
  }, [idx]);

  // 右/左旋转 90°（FR-105）
  const rotateRight = useCallback(() => setRotation((r) => (r + 90) % 360), []);
  const rotateLeft = useCallback(() => setRotation((r) => (r + 270) % 360), []);

  // 在站内地图打开（FR-106）：跳照片地图页并带经纬度定位，外部 OSM 链接保留为次要入口
  const openInSiteMap = useCallback(
    (lat: number, lon: number) => {
      onClose();
      navigate(`/map?lat=${lat}&lon=${lon}`);
    },
    [navigate, onClose],
  );

  // 键盘导航与快捷键（FR-105 在既有 ←/→/Esc 上补 +/-、F、Space、R）
  useEffect(() => {
    if (!opened) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'ArrowLeft') goPrev();
      else if (e.key === 'ArrowRight') goNext();
      else if (e.key === 'Escape') onClose();
      else if (e.key === '+' || e.key === '=') setZoom((z) => Math.min(ZOOM_MAX, z + ZOOM_STEP));
      else if (e.key === '-' || e.key === '_') setZoom((z) => Math.max(ZOOM_MIN, z - ZOOM_STEP));
      else if (e.key === 'f' || e.key === 'F') setFullscreen((v) => !v);
      else if (e.key === ' ') {
        e.preventDefault();
        goNext();
      } else if (e.key === 'r' || e.key === 'R') rotateRight();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [opened, goPrev, goNext, onClose, rotateRight]);

  // 幻灯片自动轮播（FR-105）：开启时按固定间隔自动切下一张
  useEffect(() => {
    if (!opened || !slideshow) return;
    const timer = window.setInterval(goNext, SLIDESHOW_INTERVAL_MS);
    return () => window.clearInterval(timer);
  }, [opened, slideshow, goNext]);

  // 相邻原图预加载（FR-105）：预取当前项前后各 PRELOAD_RADIUS 张，切换不闪白
  useEffect(() => {
    if (!opened || total === 0) return;
    for (let d = 1; d <= PRELOAD_RADIUS; d++) {
      for (const j of [(idx - d + total) % total, (idx + d) % total]) {
        const neighbor = files[j];
        if (neighbor && isImageFile(neighbor, customImageExtensions)) {
          // 用 createElement('img') 触发浏览器原图预取，jsdom 下亦可断言 src 赋值
          const pre = document.createElement('img');
          pre.src = mediaRawUrl(neighbor.id);
        }
      }
    }
  }, [opened, idx, total, files, customImageExtensions]);

  const file = opened && files[idx] ? files[idx] : null;

  // 切换媒体时同步内容分级本地态（FR2-051）
  useEffect(() => {
    setContentRating((file?.content_rating ?? '').trim());
  }, [file?.id, file?.content_rating]);

  const handleContentRatingChange = useCallback(
    async (next: string | null) => {
      if (!file) return;
      const value = next ?? '';
      setContentRating(value);
      setRatingSaving(true);
      try {
        await updateMediaContentRating(file.id, value);
        onContentRatingChange?.(file.id, value);
        notifications.show({
          color: 'green',
          message: `内容分级已更新为「${formatContentRatingLabel(value)}」`,
          autoClose: 2500,
        });
      } catch (err) {
        // 失败回退到服务端当前值
        setContentRating((file.content_rating ?? '').trim());
        notifications.show({
          color: 'red',
          title: '更新分级失败',
          message: extractErrorMessage(err, '更新内容分级失败'),
          autoClose: 4000,
        });
      } finally {
        setRatingSaving(false);
      }
    },
    [file, onContentRatingChange],
  );

  // FR2-032：按当前媒体加载嵌入元数据 / 标签 / 推断，失败只提示不阻断预览
  useEffect(() => {
    let active = true;
    setEmbeddedMetadata([]);
    setMetadataError(null);
    setMetadataLoading(false);
    if (!file)
      return () => {
        active = false;
      };
    setMetadataLoading(true);
    void getMediaMetadata(file.id)
      .then((items) => {
        if (!active) return;
        setEmbeddedMetadata(items);
        setMetadataError(null);
      })
      .catch(() => {
        if (!active) return;
        setEmbeddedMetadata([]);
        setMetadataError('加载元数据失败');
      })
      .finally(() => {
        if (active) setMetadataLoading(false);
      });
    return () => {
      active = false;
    };
  }, [file]);

  useEffect(() => {
    let active = true;
    setMediaTags([]);
    setTagsError(null);
    setTagsLoading(false);
    if (!file)
      return () => {
        active = false;
      };
    setTagsLoading(true);
    void getMediaTags(file.id)
      .then((items) => {
        if (!active) return;
        setMediaTags(items);
        setTagsError(null);
      })
      .catch(() => {
        if (!active) return;
        setMediaTags([]);
        setTagsError('加载标签失败');
      })
      .finally(() => {
        if (active) setTagsLoading(false);
      });
    return () => {
      active = false;
    };
  }, [file]);

  useEffect(() => {
    let active = true;
    setInference(null);
    setInferenceError(null);
    setInferenceLoading(false);
    if (!file)
      return () => {
        active = false;
      };
    setInferenceLoading(true);
    void getMediaInference(file.id)
      .then((result) => {
        if (!active) return;
        setInference(result);
        setInferenceError(null);
      })
      .catch(() => {
        if (!active) return;
        setInference(null);
        setInferenceError('加载影视信息失败');
      })
      .finally(() => {
        if (active) setInferenceLoading(false);
      });
    return () => {
      active = false;
    };
  }, [file]);

  // FR2-012：按媒体加载 AI 结果；AI 关闭时仅提示，不阻断详情
  useEffect(() => {
    let active = true;
    setAIResults([]);
    setAIError(null);
    setAILoading(false);
    setAIBusyID(null);
    if (!file)
      return () => {
        active = false;
      };
    setAILoading(true);
    void listAIResults(file.id)
      .then((items) => {
        if (!active) return;
        setAIResults(items);
        setAIError(null);
      })
      .catch((err) => {
        if (!active) return;
        setAIResults([]);
        const code = extractErrorCode(err);
        if (code === 'AI_DISABLED' || code === 'AI_UNAVAILABLE') {
          setAIError('AI 未启用');
        } else {
          setAIError(extractErrorMessage(err, '加载 AI 结果失败'));
        }
      })
      .finally(() => {
        if (active) setAILoading(false);
      });
    return () => {
      active = false;
    };
  }, [file]);

  const handleConfirmAI = useCallback(async (id: number) => {
    setAIBusyID(id);
    try {
      await confirmAIResult(id);
      setAIResults((prev) => prev.map((r) => (r.id === id ? { ...r, manual: true } : r)));
      notifications.show({ color: 'green', message: '已确认 AI 结果', autoClose: 2500 });
    } catch (err) {
      notifications.show({
        color: 'red',
        title: '确认失败',
        message: extractErrorMessage(err, '确认 AI 结果失败'),
        autoClose: 4000,
      });
    } finally {
      setAIBusyID(null);
    }
  }, []);

  const handleRejectAI = useCallback(async (id: number) => {
    setAIBusyID(id);
    try {
      await rejectAIResult(id);
      setAIResults((prev) => prev.filter((r) => r.id !== id));
      notifications.show({ color: 'green', message: '已驳回 AI 结果', autoClose: 2500 });
    } catch (err) {
      notifications.show({
        color: 'red',
        title: '驳回失败',
        message: extractErrorMessage(err, '驳回 AI 结果失败'),
        autoClose: 4000,
      });
    } finally {
      setAIBusyID(null);
    }
  }, []);

  useEffect(() => {
    let active = true;
    setCovers({ cover: null, candidates: [] });
    setCoverGenerating(false);
    if (!file)
      return () => {
        active = false;
      };
    void getMediaCovers(file.id)
      .then((result) => {
        if (active) setCovers(result);
      })
      .catch(() => {
        if (active) setCovers({ cover: null, candidates: [] });
      });
    return () => {
      active = false;
    };
  }, [file]);
  if (!file) return null;

  const isImage = isImageFile(file, customImageExtensions);

  const handleGenerateCovers = async () => {
    setCoverGenerating(true);
    try {
      const task = await generateMediaCovers(file.id, covers.candidates.length > 0);
      await waitForCoverTask(task.task_id);
      setCovers(await getMediaCovers(file.id));
      notifyCoverChanged(file.id);
    } finally {
      setCoverGenerating(false);
    }
  };

  const handleSelectCover = async (candidateID: number) => {
    const selected = await selectMediaCover(file.id, candidateID);
    setCovers((current) => ({ ...current, cover: selected }));
    notifyCoverChanged(file.id);
  };

  // FR2-033：危险写回二次确认后入队，并轮询任务状态
  const handleConfirmWriteback = async () => {
    if (!file) return;
    setWritebackSubmitting(true);
    setWritebackStatus(null);
    try {
      const accepted = await enqueueMetadataWriteback(file.id, true);
      setWritebackTaskId(accepted.task_id);
      setWritebackStatus(accepted.status || 'pending');
      setWritebackConfirmOpened(false);
      notifications.show({
        color: 'blue',
        title: '写回已入队',
        message: `任务 #${accepted.task_id} 正在将库内元数据写回原文件`,
        autoClose: 4000,
      });
      // 后台轮询终态
      void (async () => {
        for (let i = 0; i < 60; i++) {
          await new Promise((r) => setTimeout(r, 1500));
          try {
            const task = await getTask(String(accepted.task_id));
            setWritebackStatus(task.status);
            if (task.status === 'succeeded') {
              notifications.show({
                color: 'green',
                title: '写回完成',
                message: '原文件元数据已更新；可在审计页回滚快照',
                autoClose: 6000,
              });
              return;
            }
            if (task.status === 'failed' || task.status === 'canceled') {
              notifications.show({
                color: 'red',
                title: '写回失败',
                message: task.error || '任务失败；原文件与快照应保持完整',
                autoClose: 8000,
              });
              return;
            }
          } catch {
            // 轮询失败忽略，继续下一轮
          }
        }
      })();
    } catch (err) {
      notifications.show({
        color: 'red',
        title: '写回入队失败',
        message: extractErrorMessage(err, '无法入队写回任务'),
        autoClose: 6000,
      });
    } finally {
      setWritebackSubmitting(false);
    }
  };

  const handleWheel = (e: React.WheelEvent) => {
    e.preventDefault();
    setZoom((z) =>
      Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, z + (e.deltaY < 0 ? ZOOM_STEP : -ZOOM_STEP))),
    );
  };

  // 双击放大 / 复位（FR-105）：1×↔2× 切换，复位时平移归零
  const handleDoubleClick = () => {
    if (zoom > 1) {
      setZoom(1);
      setPan({ x: 0, y: 0 });
    } else {
      setZoom(ZOOM_DOUBLE_CLICK);
    }
  };

  // 鼠标按下开始拖拽平移（FR-105）：仅放大态生效，过程绑定到 window 以便拖出元素仍跟手
  const handleMouseDown = (e: React.MouseEvent) => {
    if (zoom <= 1) return;
    e.preventDefault();
    dragRef.current = {
      active: true,
      startX: e.clientX,
      startY: e.clientY,
      panX: pan.x,
      panY: pan.y,
    };
    const onMove = (ev: MouseEvent) => {
      if (!dragRef.current.active) return;
      setPan({
        x: dragRef.current.panX + (ev.clientX - dragRef.current.startX),
        y: dragRef.current.panY + (ev.clientY - dragRef.current.startY),
      });
    };
    const onUp = () => {
      dragRef.current.active = false;
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
    };
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
  };

  // 触摸开始（FR-105）：单指准备平移，双指准备捏合缩放
  const handleTouchStart = (e: React.TouchEvent) => {
    if (e.touches.length === 2) {
      const dx = e.touches[0].clientX - e.touches[1].clientX;
      const dy = e.touches[0].clientY - e.touches[1].clientY;
      pinchRef.current = { active: true, startDist: Math.hypot(dx, dy), startZoom: zoom };
    } else if (e.touches.length === 1 && zoom > 1) {
      const t = e.touches[0];
      dragRef.current = {
        active: true,
        startX: t.clientX,
        startY: t.clientY,
        panX: pan.x,
        panY: pan.y,
      };
    }
  };

  // 触摸移动（FR-105）：双指改缩放，单指改平移
  const handleTouchMove = (e: React.TouchEvent) => {
    if (pinchRef.current.active && e.touches.length === 2) {
      e.preventDefault();
      const dx = e.touches[0].clientX - e.touches[1].clientX;
      const dy = e.touches[0].clientY - e.touches[1].clientY;
      const ratio = Math.hypot(dx, dy) / (pinchRef.current.startDist || 1);
      setZoom(Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, pinchRef.current.startZoom * ratio)));
    } else if (dragRef.current.active && e.touches.length === 1) {
      e.preventDefault();
      const t = e.touches[0];
      setPan({
        x: dragRef.current.panX + (t.clientX - dragRef.current.startX),
        y: dragRef.current.panY + (t.clientY - dragRef.current.startY),
      });
    }
  };

  // 触摸结束（FR-105）：松手清理过程态，缩回 1× 时平移归零
  const handleTouchEnd = (e: React.TouchEvent) => {
    if (e.touches.length === 0) {
      dragRef.current.active = false;
      pinchRef.current.active = false;
      if (zoom <= 1) setPan({ x: 0, y: 0 });
    }
  };

  return (
    <Modal
      opened={opened}
      onClose={onClose}
      fullScreen={fullscreen}
      size="90%"
      padding="md"
      withCloseButton={false}
      title={
        <Group justify="space-between" w="100%" wrap="nowrap">
          <Group gap="xs" wrap="nowrap" style={{ minWidth: 0 }}>
            <Text fw={600} truncate>
              {mediaDisplayName(file)}
            </Text>
            {/* 位置计数（FR-105）：当前 / 总数 */}
            <Text size="sm" c="dimmed" style={{ flexShrink: 0 }}>
              {idx + 1} / {total}
            </Text>
          </Group>
          <Group gap={4} wrap="nowrap">
            <Tooltip label="上一项 (←)">
              <ActionIcon variant="subtle" onClick={goPrev} aria-label="上一项">
                <IconChevronLeft size={18} />
              </ActionIcon>
            </Tooltip>
            <Tooltip label="下一项 (→)">
              <ActionIcon variant="subtle" onClick={goNext} aria-label="下一项">
                <IconChevronRight size={18} />
              </ActionIcon>
            </Tooltip>
            {/* 图片专属：旋转与幻灯片（FR-105），视频分支不显示 */}
            {isImage && (
              <>
                <Tooltip label="向左旋转 (R 为右旋)">
                  <ActionIcon variant="subtle" onClick={rotateLeft} aria-label="向左旋转">
                    <IconRotate2 size={18} />
                  </ActionIcon>
                </Tooltip>
                <Tooltip label="向右旋转 (R)">
                  <ActionIcon variant="subtle" onClick={rotateRight} aria-label="向右旋转">
                    <IconRotateClockwise size={18} />
                  </ActionIcon>
                </Tooltip>
                <Tooltip label={slideshow ? '暂停幻灯片' : '开始幻灯片'}>
                  <ActionIcon
                    variant="subtle"
                    onClick={() => setSlideshow((v) => !v)}
                    aria-label="幻灯片切换"
                  >
                    {slideshow ? <IconPlayerPause size={18} /> : <IconPlayerPlay size={18} />}
                  </ActionIcon>
                </Tooltip>
                {/* 图片编辑导出（FR2-038） */}
                <Tooltip label="编辑导出">
                  <ActionIcon
                    variant="subtle"
                    onClick={() => setImageEditorOpened(true)}
                    aria-label="图片编辑导出"
                  >
                    <IconAdjustments size={18} />
                  </ActionIcon>
                </Tooltip>
              </>
            )}
            {/* 视频粗剪导出（FR2-039） */}
            {!isImage && (
              <Tooltip label="片段粗剪导出">
                <ActionIcon
                  variant="subtle"
                  onClick={() => setClipExportOpened(true)}
                  aria-label="片段粗剪导出"
                >
                  <IconMovie size={18} />
                </ActionIcon>
              </Tooltip>
            )}
            {/* 工具栏（FR-106）：收藏 / 打标签 / 分享，与网格操作一致，复用既有能力 */}
            {onToggleFavorite && (
              <Tooltip label={file.favorite ? '取消收藏' : '收藏'}>
                <ActionIcon
                  variant="subtle"
                  color={file.favorite ? 'red' : 'gray'}
                  onClick={() => onToggleFavorite(file)}
                  aria-label={file.favorite ? '取消收藏' : '收藏'}
                >
                  {file.favorite ? <IconHeartFilled size={18} /> : <IconHeart size={18} />}
                </ActionIcon>
              </Tooltip>
            )}
            <Tooltip label="打标签">
              <ActionIcon
                variant="subtle"
                onClick={() => batch.openAddTag([file.id])}
                aria-label="打标签"
              >
                <IconTag size={18} />
              </ActionIcon>
            </Tooltip>
            <Tooltip label="分享">
              <ActionIcon variant="subtle" onClick={() => setShareOpened(true)} aria-label="分享">
                <IconShare size={18} />
              </ActionIcon>
            </Tooltip>
            {/* 信息栏折叠（FR-106）：纯图沉浸 */}
            <Tooltip label={infoCollapsed ? '展开信息栏' : '折叠信息栏'}>
              <ActionIcon
                variant="subtle"
                onClick={() => setInfoCollapsed((v) => !v)}
                aria-label={infoCollapsed ? '展开信息栏' : '折叠信息栏'}
              >
                {infoCollapsed ? (
                  <IconLayoutSidebarRightExpand size={18} />
                ) : (
                  <IconLayoutSidebarRightCollapse size={18} />
                )}
              </ActionIcon>
            </Tooltip>
            <Tooltip label={fullscreen ? '退出全屏 (F)' : '全屏 (F)'}>
              <ActionIcon
                variant="subtle"
                onClick={() => setFullscreen((f) => !f)}
                aria-label="全屏切换"
              >
                {fullscreen ? <IconMinimize size={18} /> : <IconMaximize size={18} />}
              </ActionIcon>
            </Tooltip>
            <ActionIcon variant="subtle" color="gray" onClick={onClose} aria-label="关闭">
              <IconX size={18} />
            </ActionIcon>
          </Group>
        </Group>
      }
      styles={{ title: { width: '100%' } }}
    >
      <Group
        align="stretch"
        gap="md"
        wrap="nowrap"
        style={{ minHeight: fullscreen ? '80vh' : '60vh' }}
      >
        {/* 左侧预览 */}
        <Box
          style={{
            flex: 1,
            minWidth: 0,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            background: '#000',
            borderRadius: 8,
            overflow: 'hidden',
          }}
        >
          {isImage ? (
            <Box
              onWheel={handleWheel}
              onTouchStart={handleTouchStart}
              onTouchMove={handleTouchMove}
              onTouchEnd={handleTouchEnd}
              style={{
                width: '100%',
                height: '100%',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                overflow: 'hidden',
                touchAction: 'none',
                cursor: zoom > 1 ? (dragRef.current.active ? 'grabbing' : 'grab') : 'zoom-in',
              }}
            >
              <Box
                component="img"
                src={mediaRawUrl(file.id)}
                alt={file.file_name}
                draggable={false}
                onDoubleClick={handleDoubleClick}
                onMouseDown={handleMouseDown}
                style={{
                  maxWidth: '100%',
                  maxHeight: fullscreen ? '78vh' : '58vh',
                  objectFit: 'contain',
                  // 合成变换（FR-105）：先平移、再缩放、再旋转；拖拽中关动画跟手
                  transform: `translate(${pan.x}px, ${pan.y}px) scale(${zoom}) rotate(${rotation}deg)`,
                  transition: dragRef.current.active ? 'none' : 'transform 0.1s ease-out',
                  userSelect: 'none',
                }}
              />
            </Box>
          ) : (
            // FR-102：视频内嵌 VideoPlayer 直接播放（静音自动播），去掉「打开播放」中间步骤。
            // 灯箱内用既有 /api/play/:id/stream 流地址（与播放页降级路径一致），不引入协商逻辑。
            <Box w="100%" px="md" style={{ maxWidth: 960 }}>
              <Suspense
                fallback={
                  <Text c="dimmed" size="sm">
                    加载播放器…
                  </Text>
                }
              >
                <VideoPlayer
                  url={mediaStreamUrl(file.id)}
                  streamType="mp4"
                  poster={`/api/library/thumbnail/${file.id}`}
                  autoPlay
                />
              </Suspense>
            </Box>
          )}
        </Box>

        {/* 右侧详情（FR-106 可折叠沉浸）：折叠后不渲染、左侧预览吃满宽度 */}
        {!infoCollapsed && (
          <ScrollArea style={{ width: 300, flexShrink: 0 }} type="hover">
            <Stack gap="xs">
              <Group justify="space-between" wrap="nowrap">
                <Text fw={600} size="sm">
                  文件信息
                </Text>
                {/* 复制文件路径（FR-106） */}
                {file.file_path && <CopyIconButton value={file.file_path} label="复制路径" />}
              </Group>
              {/* 定宽两列定义列表（FR-106） */}
              <Box component="dl" style={{ margin: 0 }}>
                <DetailRow label="显示名" value={mediaDisplayName(file)} />
                <DetailRow label="文件名" value={file.file_name} />
                <DetailRow label="类型" value={file.format?.toUpperCase()} />
                <DetailRow label="大小" value={formatSize(file.file_size)} />
                {file.width > 0 && file.height > 0 && (
                  <DetailRow label="分辨率" value={`${file.width} × ${file.height}`} />
                )}
                {!isImage && <DetailRow label="时长" value={formatDuration(file.duration)} />}
                {!isImage && <DetailRow label="视频编码" value={file.video_codec} />}
                {!isImage && <DetailRow label="音频编码" value={file.audio_codec} />}
              </Box>
              {/* 内容分级（FR2-051）：Badge 展示 + 下拉编辑 */}
              <Divider my={4} label="内容分级" labelPosition="left" />
              <Stack gap={6}>
                <Group gap="xs">
                  <Badge
                    color={contentRatingBadgeColor(contentRating)}
                    variant="light"
                    aria-label={`内容分级 ${formatContentRatingLabel(contentRating)}`}
                  >
                    {formatContentRatingLabel(contentRating)}
                  </Badge>
                </Group>
                <Select
                  aria-label="内容分级"
                  data={CONTENT_RATING_OPTIONS}
                  value={contentRating}
                  onChange={(v) => void handleContentRatingChange(v)}
                  disabled={ratingSaving}
                  allowDeselect={false}
                  size="xs"
                />
              </Stack>
              <Divider my={4} />
              <Box component="dl" style={{ margin: 0 }}>
                <DetailRow label="加入时间" value={formatTime(file.added_at)} />
                <DetailRow label="修改时间" value={formatTime(file.modified_at)} />
              </Box>

              {/* EXIF 信息（FR-38/FR-106）：定宽两列 + 图标 + 单位格式化，有数据才展示 */}
              {hasExif(file) && (
                <>
                  <Divider my={4} label="EXIF" labelPosition="left" />
                  <Box component="dl" role="group" aria-label="EXIF 信息" style={{ margin: 0 }}>
                    <DetailRow
                      label="拍摄时间"
                      value={
                        file.media_time
                          ? `${formatTime(file.media_time)}${file.media_time_source ? `（${MEDIA_TIME_SOURCE_LABEL[file.media_time_source] ?? file.media_time_source}）` : ''}`
                          : ''
                      }
                    />
                    <DetailRow label="相机" value={file.camera} icon={<IconCamera size={14} />} />
                    <DetailRow label="镜头" value={file.lens} icon={<IconPhoto size={14} />} />
                    <DetailRow
                      label="光圈"
                      value={formatAperture(file.aperture)}
                      icon={<IconAperture size={14} />}
                    />
                    <DetailRow
                      label="快门"
                      value={formatShutter(file.shutter)}
                      icon={<IconClock size={14} />}
                    />
                    <DetailRow
                      label="ISO"
                      value={formatIso(file.iso)}
                      icon={<IconAdjustments size={14} />}
                    />
                  </Box>
                  {((file.gps_lat ?? 0) !== 0 || (file.gps_lon ?? 0) !== 0) && (
                    <Stack gap={4}>
                      <Group gap={4} wrap="nowrap" align="center">
                        <IconMapPin
                          size={14}
                          style={{ flexShrink: 0, color: 'var(--mantine-color-dimmed)' }}
                        />
                        <Text size="sm" style={{ minWidth: 0, wordBreak: 'break-word' }}>
                          {`${file.gps_lat?.toFixed(6)}, ${file.gps_lon?.toFixed(6)}`}
                        </Text>
                        <CopyIconButton
                          value={`${file.gps_lat}, ${file.gps_lon}`}
                          label="复制坐标"
                        />
                      </Group>
                      {/* 站内地图为主、外部 OSM 为次（FR-106） */}
                      <Button
                        size="xs"
                        variant="light"
                        leftSection={<IconMapPin size={14} />}
                        onClick={() => openInSiteMap(file.gps_lat ?? 0, file.gps_lon ?? 0)}
                      >
                        在站内地图打开
                      </Button>
                      <Anchor
                        href={`https://www.openstreetmap.org/?mlat=${file.gps_lat}&mlon=${file.gps_lon}#map=15/${file.gps_lat}/${file.gps_lon}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        size="xs"
                        c="dimmed"
                      >
                        在外部地图打开
                      </Anchor>
                    </Stack>
                  )}
                </>
              )}

              <Divider my={4} label="封面" labelPosition="left" />
              <Group justify="space-between" align="center">
                <Badge color={covers.cover?.manual ? 'teal' : 'gray'} variant="light">
                  {covers.cover?.manual ? '人工选择' : covers.cover ? '自动选择' : '尚未生成'}
                </Badge>
                <Button
                  size="xs"
                  variant="light"
                  loading={coverGenerating}
                  onClick={() => void handleGenerateCovers()}
                >
                  {covers.candidates.length > 0 ? '重新生成封面候选' : '生成封面候选'}
                </Button>
              </Group>
              {covers.candidates.length > 0 && (
                <SimpleGrid cols={2} spacing="xs">
                  {covers.candidates.map((candidate) => {
                    const selected = covers.cover?.selected_fingerprint === candidate.fingerprint;
                    const label =
                      candidate.source === 'image'
                        ? '选择图片封面'
                        : `选择 ${candidate.timestamp_seconds.toFixed(1)} 秒封面`;
                    return (
                      <UnstyledButton
                        key={candidate.id}
                        aria-label={label}
                        onClick={() => void handleSelectCover(candidate.id)}
                        style={{
                          border: selected
                            ? '2px solid var(--mantine-color-teal-6)'
                            : '1px solid var(--mantine-color-default-border)',
                          borderRadius: 6,
                          overflow: 'hidden',
                          background: 'var(--mantine-color-default)',
                        }}
                      >
                        <Box
                          component="img"
                          src={candidate.image_url}
                          alt={label}
                          style={{
                            width: '100%',
                            aspectRatio: '16/9',
                            objectFit: 'cover',
                            display: 'block',
                          }}
                        />
                        <Text size="xs" ta="center" py={4} c={selected ? 'teal' : undefined}>
                          {candidate.source === 'image'
                            ? '原图'
                            : `${candidate.timestamp_seconds.toFixed(1)} 秒`}
                        </Text>
                      </UnstyledButton>
                    );
                  })}
                </SimpleGrid>
              )}

              <MediaTagsSection
                tags={mediaTags}
                loading={tagsLoading}
                error={tagsError}
                onFilterByTag={
                  onFilterByTag
                    ? (tag) => {
                        onFilterByTag(tag);
                        onClose();
                      }
                    : undefined
                }
              />
              <InferenceSection
                inference={inference}
                loading={inferenceLoading}
                error={inferenceError}
              />
              <AIResultsSection
                results={aiResults}
                loading={aiLoading}
                error={aiError}
                busyID={aiBusyID}
                onConfirm={handleConfirmAI}
                onReject={handleRejectAI}
              />
              <EmbeddedMetadataInfo
                items={embeddedMetadata}
                error={metadataError}
                loading={metadataLoading}
              />

              <Divider my="sm" />
              {/* 危险写回原文件（FR2-033）：仅图片有写回能力；视频提示仅库内 */}
              {isImage ? (
                <Stack gap={6}>
                  <Button
                    variant="light"
                    color="orange"
                    leftSection={<IconFileUpload size={16} />}
                    fullWidth
                    onClick={() => setWritebackConfirmOpened(true)}
                    loading={writebackSubmitting}
                  >
                    写回原文件元数据
                  </Button>
                  {writebackTaskId && (
                    <Text size="xs" c="dimmed">
                      写回任务 #{writebackTaskId}
                      {writebackStatus ? ` · ${writebackStatus}` : ''}
                    </Text>
                  )}
                </Stack>
              ) : (
                <Text size="xs" c="dimmed">
                  视频仅支持库内元数据，暂不支持写回原文件。
                </Text>
              )}
              <Button
                component="a"
                href={`/api/library/media/${file.id}/download`}
                download
                variant="light"
                leftSection={<IconDownload size={16} />}
                fullWidth
              >
                下载原文件
              </Button>
            </Stack>
          </ScrollArea>
        )}
      </Group>

      {/* 分享弹窗（FR-106）：复用既有 ShareDialog，媒体资源 */}
      <ShareDialog
        opened={shareOpened}
        onClose={() => setShareOpened(false)}
        resourceType="media"
        resourceID={file.id}
        title="分享媒体"
      />
      {/* 打标签弹窗（FR-106）：复用 FR-91 批量编排 */}
      <BatchActionsModals state={batch.modalState} />
      {/* 图片编辑导出（FR2-038） */}
      {isImage && (
        <ImageEditorPanel
          opened={imageEditorOpened}
          mediaId={file.id}
          onClose={() => setImageEditorOpened(false)}
        />
      )}
      {/* 视频粗剪导出（FR2-039） */}
      {!isImage && (
        <ClipExportPanel
          opened={clipExportOpened}
          mediaId={file.id}
          duration={file.duration || 0}
          onClose={() => setClipExportOpened(false)}
        />
      )}
      {/* 危险写回二次确认（FR2-033） */}
      <Modal
        opened={writebackConfirmOpened}
        onClose={() => !writebackSubmitting && setWritebackConfirmOpened(false)}
        title="确认写回原文件"
        size="md"
        centered
        zIndex={400}
      >
        <Stack gap="sm">
          <Alert color="orange" icon={<IconAlertCircle size={16} />}>
            将把库内元数据（相机/镜头/光圈/快门/ISO/GPS/备注/显示名等有限字段）写入磁盘原文件。
            操作前会自动生成快照，但写回仍可能不可逆；请确认已备份重要文件。
          </Alert>
          <Text size="sm">
            媒体：{mediaDisplayName(file)}（#{file.id}）
          </Text>
          <Group justify="flex-end" gap="xs">
            <Button
              variant="default"
              disabled={writebackSubmitting}
              onClick={() => setWritebackConfirmOpened(false)}
            >
              取消
            </Button>
            <Button color="orange" loading={writebackSubmitting} onClick={handleConfirmWriteback}>
              确认写回
            </Button>
          </Group>
        </Stack>
      </Modal>
    </Modal>
  );
}
