import { useState, useEffect, useCallback, useRef } from 'react';
import type { PreparedPreviewTrack } from '@jianvideo/player-core';
import { useParams, useNavigate } from 'react-router-dom';
import {
  Button,
  Text,
  Group,
  Badge,
  Skeleton,
  Alert,
  Box,
  Title,
  Menu,
  Drawer,
  Textarea,
  TextInput,
  NumberInput,
  Stack,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import {
  IconArrowLeft,
  IconAlertCircle,
  IconPencil,
  IconFileText,
  IconDownload,
  IconShare,
  IconExternalLink,
  IconMovie,
  IconDots,
  IconMaximize,
  IconMinimize,
  IconInfoCircle,
} from '@tabler/icons-react';
import VideoPlayer from '@/components/VideoPlayer';
import NameEditModal from '@/components/NameEditModal';
import ShareDialog from '@/components/ShareDialog';
import ExternalPlayerDialog from '@/components/ExternalPlayerDialog';
import PregenDialog from '@/components/PregenDialog';
import { parseTimelinePreviewVtt } from '@/utils/timeline-preview';
import { mediaDisplayName } from '@/utils/media';
import { mediaStreamUrl } from '@/utils/media-url';
import { probeClientCapabilities } from '@/utils/codec-capability';
import { useCinemaMode } from '@/hooks/cinema-context';
import * as libApi from '@/api/library';
import * as playApi from '@/api/play';
import { getTask } from '@/api/tasks';
import * as subtitleApi from '@/api/subtitle';
import type { TrackResponse } from '@/api/subtitle';
import type { MediaFile, MediaInference, PlaybackDescriptor } from '@/types';

// 双模式改名弹窗类型：显示名（仅库内）/ 真实文件名（磁盘改名）
type NameEditKind = 'display' | 'real';

type TimelinePreviewData = {
  track: PreparedPreviewTrack;
  spriteUrls: Readonly<Record<string, string>>;
};

type MediaRequest = {
  controller: AbortController;
  generation: number;
  mediaId: number;
};

const TIMELINE_TASK_POLL_INTERVAL = 1000;
const TIMELINE_TASK_POLL_MAX_INTERVAL = 8000;

function abortError(): DOMException {
  return new DOMException('请求已取消', 'AbortError');
}

function waitForTimelinePoll(signal: AbortSignal, interval: number): Promise<void> {
  if (signal.aborted) return Promise.reject(abortError());
  return new Promise((resolve, reject) => {
    const onAbort = () => {
      window.clearTimeout(timer);
      reject(abortError());
    };
    const timer = window.setTimeout(() => {
      signal.removeEventListener('abort', onAbort);
      resolve();
    }, interval);
    signal.addEventListener('abort', onAbort, { once: true });
  });
}

async function waitForTimelineTask(taskID: number, signal: AbortSignal): Promise<boolean> {
  let interval = TIMELINE_TASK_POLL_INTERVAL;
  while (!signal.aborted) {
    await waitForTimelinePoll(signal, interval);
    try {
      const task = await getTask(String(taskID), signal);
      if (task.status === 'succeeded') return true;
      if (task.status === 'failed' || task.status === 'canceled') return false;
      interval = TIMELINE_TASK_POLL_INTERVAL;
    } catch {
      if (signal.aborted) throw abortError();
      interval = Math.min(interval * 2, TIMELINE_TASK_POLL_MAX_INTERVAL);
    }
  }
  throw abortError();
}

async function loadTimelinePreview(
  mediaID: number,
  signal: AbortSignal,
): Promise<TimelinePreviewData | null> {
  let status = await playApi.getTimelinePreviewStatus(mediaID, undefined, signal);
  if (status.status === 'pending' && status.task_id) {
    if (!(await waitForTimelineTask(status.task_id, signal))) return null;
    status = await playApi.getTimelinePreviewStatus(mediaID, status.profile_id, signal);
  }
  if (
    status.status !== 'available' ||
    !status.vtt_url ||
    !status.generation_id ||
    !status.source_fingerprint ||
    !status.sprite_urls
  )
    return null;
  const response = await fetch(status.vtt_url, { signal });
  if (!response.ok) return null;
  const vtt = await response.text();
  return {
    track: parseTimelinePreviewVtt(vtt, {
      generationId: status.generation_id,
      mediaId: String(mediaID),
      profileId: status.profile_id,
      sourceFingerprint: status.source_fingerprint,
      spriteUrls: status.sprite_urls,
    }),
    spriteUrls: status.sprite_urls,
  };
}

/** 视频播放页 — Mantine + VideoPlayer */
export default function PlayPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  // 影院模式（FR-85）：临时收起左导航扩大视频区，播放页本地态（不污染全站持久态）
  const { cinema, setCinema } = useCinemaMode();
  const [media, setMedia] = useState<MediaFile | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [trackManifest, setTrackManifest] = useState<TrackResponse | null | undefined>();

  // 播放直连优先；仅在原文件直连失败时查询并切换已生成的 HLS preview。
  const [playerUrl, setPlayerUrl] = useState<string | null>(null);
  const [playerIsABR, setPlayerIsABR] = useState<boolean | null>(null);
  const [descriptor, setDescriptor] = useState<PlaybackDescriptor | null>(null);
  const [previewTrack, setPreviewTrack] = useState<PreparedPreviewTrack | undefined>();
  const [previewSpriteUrls, setPreviewSpriteUrls] = useState<
    Readonly<Record<string, string>> | undefined
  >();
  const previewGenerationRef = useRef(0);
  const mediaGenerationRef = useRef(0);
  const mediaRequestRef = useRef<MediaRequest | null>(null);

  // 双模式改名（FR-30）：null 表示弹窗关闭
  const [nameEditKind, setNameEditKind] = useState<NameEditKind | null>(null);

  // 分享弹窗开关（FR-43）
  const [shareOpened, setShareOpened] = useState(false);
  // 外部播放器深链弹窗开关（FR-79）
  const [extPlayerOpened, setExtPlayerOpened] = useState(false);
  // 加入预生成队列弹窗开关（FR-77）
  const [pregenOpened, setPregenOpened] = useState(false);
  const [abrEnqueueing, setABREnqueueing] = useState(false);
  // 媒体信息抽屉开关（FR-103）：信息移出文档流、收进右侧抽屉，经「更多」菜单「详情」打开
  const [infoOpened, setInfoOpened] = useState(false);
  const [nameEditSaving, setNameEditSaving] = useState(false);

  // 库内备注编辑（FR-137）：抽屉内编辑草稿与保存中标记，纳入基础搜索
  const [notesDraft, setNotesDraft] = useState('');
  const [notesSaving, setNotesSaving] = useState(false);
  const [inference, setInference] = useState<MediaInference | null>(null);
  const [inferenceTitle, setInferenceTitle] = useState('');
  const [inferenceYear, setInferenceYear] = useState<number | string>('');
  const [inferenceSeason, setInferenceSeason] = useState<number | string>('');
  const [inferenceEpisode, setInferenceEpisode] = useState<number | string>('');
  const [inferenceEpisodeTitle, setInferenceEpisodeTitle] = useState('');
  const [inferenceSaving, setInferenceSaving] = useState(false);

  const setInferenceDraft = (data: MediaInference | null) => {
    setInferenceTitle(data?.title || '');
    setInferenceYear(data?.year || '');
    setInferenceSeason(data?.season || '');
    setInferenceEpisode(data?.episode || '');
    setInferenceEpisodeTitle(data?.episode_title || '');
  };

  const isCurrentMediaRequest = useCallback((request: MediaRequest): boolean => {
    const current = mediaRequestRef.current;
    return (
      !request.controller.signal.aborted &&
      current?.generation === request.generation &&
      current.mediaId === request.mediaId
    );
  }, []);

  useEffect(() => {
    if (!id) return;
    const mediaId = parseInt(id, 10);
    const request: MediaRequest = {
      controller: new AbortController(),
      generation: ++mediaGenerationRef.current,
      mediaId,
    };
    mediaRequestRef.current?.controller.abort();
    mediaRequestRef.current = request;
    setLoading(true);
    setError(null);
    setMedia(null);
    setInference(null);
    setTrackManifest(undefined);
    setDescriptor(null);
    setPlayerUrl(null);
    setPlayerIsABR(null);

    if (isNaN(mediaId) || mediaId <= 0) {
      setError('无效的媒体 ID');
      setLoading(false);
      return () => request.controller.abort();
    }

    const signal = request.controller.signal;
    void libApi
      .getMediaFile(mediaId, signal)
      .then((data) => {
        if (isCurrentMediaRequest(request)) setMedia(data);
      })
      .catch(() => {
        if (isCurrentMediaRequest(request)) setError('媒体文件不存在');
      })
      .finally(() => {
        if (isCurrentMediaRequest(request)) setLoading(false);
      });
    void libApi
      .getMediaInference(mediaId, signal)
      .then((data) => {
        if (!isCurrentMediaRequest(request)) return;
        setInference(data);
        setInferenceDraft(data);
      })
      .catch(() => {
        if (isCurrentMediaRequest(request)) setInference(null);
      });

    // 记录最近查看（FR-120）：进入播放页即标记 last_viewed_at，失败静默不阻塞播放
    void libApi.setMediaViewed(mediaId, signal).catch(() => undefined);

    void subtitleApi
      .getTracks(mediaId, signal)
      .then((response) => {
        if (isCurrentMediaRequest(request)) setTrackManifest(response);
      })
      .catch(() => {
        if (isCurrentMediaRequest(request)) setTrackManifest(null);
      });

    // H.264 默认直连优先；保留 FR-53 高级编码协商，避免破坏既有 fMP4 播放契约。
    setPlayerUrl(mediaStreamUrl(mediaId));
    setPlayerIsABR(false);
    void playApi
      .negotiate(mediaId, probeClientCapabilities(), signal)
      .then((nextDescriptor) => {
        if (!isCurrentMediaRequest(request)) return;
        if (nextDescriptor.path === 'fmp4') {
          setDescriptor(nextDescriptor);
          setPlayerUrl(nextDescriptor.url);
          setPlayerIsABR(true);
          return;
        }
        if (nextDescriptor.path === 'mp4' && nextDescriptor.framePresentation) {
          setDescriptor(nextDescriptor);
          setPlayerUrl(nextDescriptor.url);
        }
      })
      .catch(() => undefined);

    return () => {
      request.controller.abort();
      if (mediaRequestRef.current === request) mediaRequestRef.current = null;
    };
  }, [id, isCurrentMediaRequest]);

  useEffect(() => {
    const generation = ++previewGenerationRef.current;
    const mediaID = Number(id);
    setPreviewTrack(undefined);
    setPreviewSpriteUrls(undefined);
    if (!Number.isInteger(mediaID) || mediaID <= 0) return undefined;
    const controller = new AbortController();
    void loadTimelinePreview(mediaID, controller.signal)
      .then((preview) => {
        if (!preview || controller.signal.aborted || generation !== previewGenerationRef.current)
          return;
        setPreviewTrack(preview.track);
        setPreviewSpriteUrls(preview.spriteUrls);
      })
      .catch(() => {});
    return () => {
      previewGenerationRef.current += 1;
      controller.abort();
    };
  }, [id]);

  // 影院模式（FR-85）：离开播放页（卸载）时自动恢复全站导航，避免影院态泄漏到其它页面
  useEffect(() => {
    return () => setCinema(false);
  }, [setCinema]);

  // 全屏沉浸布局（FR-103）：播放路由挂载时给 body 加 play-immersive 类，
  // index.css 据此去掉 AppShell.Main 的 padding 并锁高、禁外层滚动；卸载时移除，仅作用播放路由。
  useEffect(() => {
    document.body.classList.add('play-immersive');
    return () => document.body.classList.remove('play-immersive');
  }, []);

  const refreshTrackManifest = useCallback(async (): Promise<TrackResponse> => {
    const request = mediaRequestRef.current;
    if (!media || !request || request.mediaId !== media.id) throw abortError();
    const response = await subtitleApi.getTracks(media.id, request.controller.signal);
    if (!isCurrentMediaRequest(request)) throw abortError();
    setTrackManifest(response);
    return response;
  }, [isCurrentMediaRequest, media]);

  // 续播与观看状态（FR-44）：上报播放位置、看完标记。失败仅静默忽略，不打断播放。
  const handlePositionReport = useCallback(
    (position: number) => {
      if (!media) return;
      void libApi.updateWatchPosition(media.id, position).catch(() => {});
    },
    [media],
  );

  const handleEnded = useCallback(() => {
    if (!media) return;
    void libApi.markWatched(media.id).catch(() => {});
  }, [media]);

  const handlePlaybackError = useCallback(() => {
    const request = mediaRequestRef.current;
    if (!media || playerIsABR || !request || request.mediaId !== media.id) return;
    void (async () => {
      for (const profileID of ['abr-h264', 'h264']) {
        const status = await playApi.getHLSStatus(
          media.id,
          profileID,
          request.controller.signal,
        );
        if (!isCurrentMediaRequest(request)) throw abortError();
        if (!status.available) continue;
        setDescriptor(null);
        setPlayerUrl(status.url);
        setPlayerIsABR(true);
        return;
      }
    })().catch(() => undefined);
  }, [isCurrentMediaRequest, media, playerIsABR]);

  const enqueueABR = async () => {
    if (!media || abrEnqueueing) return;
    setABREnqueueing(true);
    try {
      await playApi.createHLSABR(media.id);
      notifications.show({
        title: '已加入自适应版本生成队列',
        message: '生成完成后，直连失败可自动回退到多码率播放',
        color: 'green',
        autoClose: 3000,
      });
    } catch (err) {
      notifications.show({
        title: '创建自适应版本失败',
        message: err instanceof Error ? err.message : '请稍后重试',
        color: 'red',
        autoClose: 3000,
      });
    } finally {
      setABREnqueueing(false);
    }
  };

  // 确认改名（FR-30）：display 仅改库内显示名，real 走磁盘改名
  const confirmNameEdit = async (value: string) => {
    if (!media || !nameEditKind) return;
    setNameEditSaving(true);
    try {
      const updated =
        nameEditKind === 'display'
          ? await libApi.updateDisplayName(media.id, value)
          : await libApi.renameMediaFile(media.id, value);
      setMedia(updated);
      setNameEditKind(null);
      notifications.show({
        title: '修改成功',
        message: nameEditKind === 'display' ? '已更新显示名' : '已修改真实文件名',
        color: 'green',
        autoClose: 2500,
      });
    } catch (err) {
      notifications.show({
        title: '修改失败',
        message: err instanceof Error ? err.message : '请稍后重试',
        color: 'red',
        autoClose: 3000,
      });
    } finally {
      setNameEditSaving(false);
    }
  };

  // 保存库内备注（FR-137）：去首尾空白后持久化，空串表示清除备注
  const confirmNotesEdit = async () => {
    if (!media) return;
    setNotesSaving(true);
    try {
      const updated = await libApi.updateMediaNotes(media.id, notesDraft);
      setMedia(updated);
      notifications.show({
        title: '保存成功',
        message: '已更新备注',
        color: 'green',
        autoClose: 2500,
      });
    } catch (err) {
      notifications.show({
        title: '保存失败',
        message: err instanceof Error ? err.message : '请稍后重试',
        color: 'red',
        autoClose: 3000,
      });
    } finally {
      setNotesSaving(false);
    }
  };

  const numberValue = (value: number | string): number => {
    if (typeof value === 'number') return value;
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : 0;
  };

  const confirmInferenceEdit = async () => {
    if (!media) return;
    setInferenceSaving(true);
    try {
      const updated = await libApi.updateMediaInference(media.id, {
        title: inferenceTitle,
        year: numberValue(inferenceYear),
        season: numberValue(inferenceSeason),
        episode: numberValue(inferenceEpisode),
        episode_title: inferenceEpisodeTitle,
      });
      setInference(updated);
      setInferenceDraft(updated);
      notifications.show({
        title: '保存成功',
        message: '已更新影视信息',
        color: 'green',
        autoClose: 2500,
      });
    } catch (err) {
      notifications.show({
        title: '保存失败',
        message: err instanceof Error ? err.message : '请稍后重试',
        color: 'red',
        autoClose: 3000,
      });
    } finally {
      setInferenceSaving(false);
    }
  };

  if (loading) {
    return <Skeleton height={400} radius="md" />;
  }

  if (error || !media) {
    return (
      <Alert
        icon={<IconAlertCircle size={16} />}
        color="red"
        withCloseButton
        onClose={() => navigate(-1)}
      >
        {error || '媒体文件不存在'}
      </Alert>
    );
  }

  return (
    // 全屏沉浸容器（FR-103）：100dvh 铺满视口、列向 flex、overflow hidden 锁纵向滚动；
    // 用 dvh 避开移动端地址栏高度抖动。头部 flex-shrink，视频区 flex:1 吃满剩余高度。
    <Box
      data-testid="play-immersive-root"
      style={{
        height: '100dvh',
        display: 'flex',
        flexDirection: 'column',
        overflow: 'hidden',
        gap: 'var(--mantine-spacing-sm)',
      }}
    >
      {/* 头部（FR-85 操作收纳）：外露返回 + 标题 + 影院 + 更多，次要操作收进「更多 ⋯」菜单；
          wrap="nowrap" + 标题截断，窄屏不换行铺满、不挤出按钮。右侧保留字幕菜单。 */}
      <Group gap="sm" justify="space-between" wrap="nowrap">
        <Group gap="sm" wrap="nowrap" style={{ minWidth: 0 }}>
          <Button
            variant="subtle"
            color="gray"
            size="sm"
            leftSection={<IconArrowLeft size={14} />}
            onClick={() => navigate(-1)}
          >
            返回
          </Button>
          <Title order={3} lineClamp={1} style={{ wordBreak: 'break-all' }}>
            {mediaDisplayName(media, inference)}
          </Title>
          {/* 影院模式（FR-85）：临时收起左导航扩大视频区，切出/离开播放页自动恢复 */}
          <Button
            variant={cinema ? 'light' : 'subtle'}
            color={cinema ? 'purple' : 'gray'}
            size="sm"
            leftSection={cinema ? <IconMinimize size={14} /> : <IconMaximize size={14} />}
            onClick={() => setCinema(!cinema)}
          >
            {cinema ? '退出影院' : '影院模式'}
          </Button>
          {/* 「更多」菜单（FR-85 操作收纳）：收纳改名/下载/分享/外部播放器/加入预生成等次要操作 */}
          <Menu position="bottom-start">
            <Menu.Target>
              <Button
                variant="subtle"
                color="gray"
                size="sm"
                leftSection={<IconDots size={14} />}
                aria-label="更多操作"
              >
                更多
              </Button>
            </Menu.Target>
            <Menu.Dropdown>
              {/* 媒体信息详情（FR-103）：信息移出文档流后由此入口打开右侧抽屉查看 */}
              <Menu.Item
                leftSection={<IconInfoCircle size={14} />}
                onClick={() => {
                  // 打开抽屉时同步备注草稿，确保编辑前展示最新已存值（FR-137）
                  setNotesDraft(media.notes || '');
                  setInferenceDraft(inference);
                  setInfoOpened(true);
                }}
              >
                详情
              </Menu.Item>
              {/* 双模式改名入口（FR-30）：均需二次确认 */}
              <Menu.Item
                leftSection={<IconPencil size={14} />}
                onClick={() => setNameEditKind('display')}
              >
                改显示名
              </Menu.Item>
              <Menu.Item
                leftSection={<IconFileText size={14} />}
                onClick={() => setNameEditKind('real')}
              >
                改文件名
              </Menu.Item>
              {/* 下载原文件（FR-42）：附件下载磁盘原始文件 */}
              <Menu.Item
                component="a"
                href={`/api/library/media/${media.id}/download`}
                download
                leftSection={<IconDownload size={14} />}
              >
                下载
              </Menu.Item>
              {/* 分享链接（FR-43）：生成 token 化免登访问链接 */}
              <Menu.Item leftSection={<IconShare size={14} />} onClick={() => setShareOpened(true)}>
                分享
              </Menu.Item>
              {/* 外部播放器深链（FR-79）：生成 VLC/IINA 可打开的免登网络串流地址 */}
              <Menu.Item
                leftSection={<IconExternalLink size={14} />}
                onClick={() => setExtPlayerOpened(true)}
              >
                外部播放器
              </Menu.Item>
              <Menu.Item
                leftSection={<IconMovie size={14} />}
                disabled={abrEnqueueing}
                onClick={() => void enqueueABR()}
              >
                生成自适应版本
              </Menu.Item>
              {/* 加入预生成（FR-77）：选预设把本媒体加入预生成队列预热首播 */}
              <Menu.Item
                leftSection={<IconMovie size={14} />}
                onClick={() => setPregenOpened(true)}
              >
                加入预生成
              </Menu.Item>
            </Menu.Dropdown>
          </Menu>
        </Group>
      </Group>

      {/* 视频区（FR-103）：flex:1 + minHeight:0 吃满头部/控件之外的全部高度，
          VideoPlayer 传 fill 让视频以 object-fit:contain 填满（letterbox 黑边）。 */}
      <Box style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
        {/* H.264 先直连原文件，失败时查询 HLS preview；高级编码保留协商描述符播放。 */}
        {playerUrl && playerIsABR !== null && trackManifest !== undefined && (
          <VideoPlayer
            url={playerUrl}
            descriptor={descriptor ?? undefined}
            frameMarker={descriptor?.framePresentation?.marker}
            frameTimeline={descriptor?.framePresentation?.timeline}
            nominalFrameRate={descriptor?.framePresentation?.nominalFrameRate}
            mediaId={media.id}
            mediaTitle={mediaDisplayName(media)}
            poster={`/api/library/thumbnail/${media.id}`}
            trackResponse={trackManifest ?? undefined}
            onTrackManifestRefresh={refreshTrackManifest}
            autoPlay
            fill
            previewTrack={previewTrack}
            previewSpriteUrls={previewSpriteUrls}
            isABR={playerIsABR}
            streamType={playerIsABR ? 'mpegts' : 'mp4'}
            initialPosition={media.last_position}
            onPositionReport={handlePositionReport}
            onEnded={handleEnded}
            onPlaybackError={handlePlaybackError}
          />
        )}
      </Box>

      {/* 媒体信息抽屉（FR-103）：信息移出视频下方文档流、不再撑高页面，
          经「更多」菜单「详情」打开右侧抽屉查看，内容不变。 */}
      <Drawer
        opened={infoOpened}
        onClose={() => setInfoOpened(false)}
        position="right"
        title="媒体信息"
        size="md"
      >
        <Group gap="md" wrap="wrap">
          <div>
            <Text size="xs" c="dimmed">
              真实文件名
            </Text>
            <Text size="sm" truncate maw={400}>
              {media.file_name}
            </Text>
          </div>
          <div>
            <Text size="xs" c="dimmed">
              路径
            </Text>
            <Text size="sm" truncate maw={400}>
              {media.file_path}
            </Text>
          </div>
          <div>
            <Text size="xs" c="dimmed">
              格式
            </Text>
            <Badge size="sm" variant="light" color="blue">
              {media.format.toUpperCase()}
            </Badge>
          </div>
          <div>
            <Text size="xs" c="dimmed">
              编码
            </Text>
            <Text size="sm">
              {media.video_codec || '-'} / {media.audio_codec || '-'}
            </Text>
          </div>
          <div>
            <Text size="xs" c="dimmed">
              分辨率
            </Text>
            <Text size="sm">{media.width > 0 ? `${media.width}x${media.height}` : '-'}</Text>
          </div>
        </Group>

        {/* 库内备注编辑（FR-137）：自由文本备注，保存后持久化并纳入基础搜索；留空即清除 */}
        <Stack gap="xs" mt="md">
          <Text size="xs" c="dimmed">
            备注
          </Text>
          <Textarea
            placeholder="为这个媒体添加备注，可被搜索命中（留空则清除备注）"
            autosize
            minRows={2}
            maxRows={6}
            value={notesDraft}
            onChange={(e) => setNotesDraft(e.currentTarget.value)}
          />
          <Group justify="flex-end">
            <Button
              size="xs"
              loading={notesSaving}
              disabled={notesDraft.trim() === (media.notes || '').trim()}
              onClick={confirmNotesEdit}
            >
              保存备注
            </Button>
          </Group>
        </Stack>

        <Stack gap="xs" mt="md">
          <Text size="xs" c="dimmed">
            影视信息
          </Text>
          <TextInput
            label="标题"
            value={inferenceTitle}
            onChange={(e) => setInferenceTitle(e.currentTarget.value)}
          />
          <Group grow>
            <NumberInput label="年份" value={inferenceYear} onChange={setInferenceYear} min={0} />
            <NumberInput label="季" value={inferenceSeason} onChange={setInferenceSeason} min={0} />
            <NumberInput
              label="集"
              value={inferenceEpisode}
              onChange={setInferenceEpisode}
              min={0}
            />
          </Group>
          <TextInput
            label="集标题"
            value={inferenceEpisodeTitle}
            onChange={(e) => setInferenceEpisodeTitle(e.currentTarget.value)}
          />
          <Text size="xs" c="dimmed">
            {inference
              ? `${inference.manual ? '人工纠正' : '自动推断'} · 置信度 ${Math.round(inference.confidence * 100)}%`
              : '暂无推断，可手动填写'}
          </Text>
          <Group justify="flex-end">
            <Button size="xs" loading={inferenceSaving} onClick={confirmInferenceEdit}>
              保存影视信息
            </Button>
          </Group>
        </Stack>
      </Drawer>

      {/* 改显示名（仅库内，不动磁盘）：二次确认弹窗 */}
      <NameEditModal
        opened={nameEditKind === 'display'}
        title="修改显示名"
        label="显示名"
        message="仅修改系统内的展示名称，不会改动磁盘上的真实文件。留空则清除显示名、回退为真实文件名。"
        initialValue={media.display_name || ''}
        allowEmpty
        loading={nameEditSaving}
        onConfirm={confirmNameEdit}
        onCancel={() => setNameEditKind(null)}
      />

      {/* 改真实文件名（磁盘改名）：二次确认弹窗 */}
      <NameEditModal
        opened={nameEditKind === 'real'}
        title="修改真实文件名"
        label="真实文件名"
        message="将直接重命名磁盘上的文件，此操作会改动真实文件名。请确认无误后再继续。"
        initialValue={media.file_name}
        loading={nameEditSaving}
        onConfirm={confirmNameEdit}
        onCancel={() => setNameEditKind(null)}
      />

      {/* 分享链接弹窗（FR-43） */}
      <ShareDialog
        opened={shareOpened}
        onClose={() => setShareOpened(false)}
        resourceType="media"
        resourceID={media.id}
        title="分享此媒体"
      />

      {/* 外部播放器深链弹窗（FR-79）：续播点取自 media.last_position */}
      <ExternalPlayerDialog
        opened={extPlayerOpened}
        onClose={() => setExtPlayerOpened(false)}
        mediaID={media.id}
        lastPosition={media.last_position}
      />

      {/* 加入预生成队列弹窗（FR-77）：选预设把本媒体加入转码预生成队列 */}
      <PregenDialog
        opened={pregenOpened}
        onClose={() => setPregenOpened(false)}
        mediaID={media.id}
      />
    </Box>
  );
}
