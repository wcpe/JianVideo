import { useState, useEffect, useCallback } from 'react';
import { Stack, Group, Text, Button, Loader, Center, Paper, Badge, Tabs } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import {
  IconTrash,
  IconScan,
  IconCopyOff,
  IconFingerprint,
  IconSparkles,
} from '@tabler/icons-react';
import * as libApi from '@/api/library';
import { listAIDuplicates } from '@/api/ai';
import PageHeader from '@/components/PageHeader';
import MediaRow from '@/components/MediaRow';
import EmptyState from '@/components/EmptyState';
import { extractErrorCode, extractErrorMessage } from '@/utils/error';
import type { AIDuplicateGroup, DuplicateGroup, ExactDuplicateGroup, MediaFile } from '@/types';

type DuplicateMode = 'exact' | 'similar' | 'ai';

type AIResolvedGroup = {
  score: number;
  model_id: string;
  items: MediaFile[];
};

function exactGroupItems(groups: ExactDuplicateGroup[]): MediaFile[][] {
  return groups.map((group) => group.items);
}

function visibleItems(
  mode: DuplicateMode,
  exact: ExactDuplicateGroup[],
  similar: DuplicateGroup[],
  ai: AIResolvedGroup[],
): MediaFile[][] {
  if (mode === 'exact') return exactGroupItems(exact);
  if (mode === 'similar') return similar;
  return ai.map((g) => g.items);
}

export default function DuplicatesPage() {
  const [mode, setMode] = useState<DuplicateMode>('exact');
  const [exactGroups, setExactGroups] = useState<ExactDuplicateGroup[]>([]);
  const [similarGroups, setSimilarGroups] = useState<DuplicateGroup[]>([]);
  const [aiGroups, setAIGroups] = useState<AIResolvedGroup[]>([]);
  const [loading, setLoading] = useState(false);
  const [exactBackfilling, setExactBackfilling] = useState(false);
  const [similarScanning, setSimilarScanning] = useState(false);
  const [aiRefreshing, setAIRefreshing] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [aiDisabledHint, setAIDisabledHint] = useState<string | null>(null);

  const resolveAIGroups = useCallback(
    async (raw: AIDuplicateGroup[]): Promise<AIResolvedGroup[]> => {
      const out: AIResolvedGroup[] = [];
      for (const group of raw) {
        const items: MediaFile[] = [];
        for (const id of group.media_ids) {
          try {
            items.push(await libApi.getMediaFile(id));
          } catch {
            /* 个别媒体不可见时跳过，保留其余成员 */
          }
        }
        if (items.length >= 2) {
          out.push({ score: group.score, model_id: group.model_id, items });
        }
      }
      return out;
    },
    [],
  );

  const load = useCallback(
    async (targetMode: DuplicateMode) => {
      setLoading(true);
      try {
        let nextGroups: MediaFile[][];
        if (targetMode === 'exact') {
          const exact = await libApi.getExactDuplicateGroups();
          setExactGroups(exact);
          nextGroups = exactGroupItems(exact);
        } else if (targetMode === 'similar') {
          const similar = await libApi.getDuplicateGroups();
          setSimilarGroups(similar);
          nextGroups = similar;
        } else {
          setAIDisabledHint(null);
          try {
            const raw = await listAIDuplicates(0.92);
            const resolved = await resolveAIGroups(raw);
            setAIGroups(resolved);
            nextGroups = resolved.map((g) => g.items);
          } catch (err) {
            const code = extractErrorCode(err);
            if (code === 'AI_DISABLED' || code === 'AI_UNAVAILABLE') {
              setAIGroups([]);
              setAIDisabledHint('AI 未启用或未配置模型，无法加载 AI 相似候选。可在设置页开启 AI。');
              nextGroups = [];
            } else {
              throw err;
            }
          }
        }
        setSelected((prev) => {
          const alive = new Set<number>();
          for (const group of nextGroups)
            for (const media of group) if (prev.has(media.id)) alive.add(media.id);
          return alive;
        });
      } catch (err) {
        notifications.show({
          title: '加载失败',
          message: extractErrorMessage(err, '无法加载重复项'),
          color: 'red',
          autoClose: 3000,
        });
      } finally {
        setLoading(false);
      }
    },
    [resolveAIGroups],
  );

  useEffect(() => {
    void load(mode);
  }, [load, mode]);

  const toggle = useCallback((id: number) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const handleExactBackfill = useCallback(async () => {
    setExactBackfilling(true);
    try {
      const task = await libApi.backfillFileHashes();
      notifications.show({
        title: '已入队',
        message: `内容哈希回填任务 ${task.task_id} 已创建`,
        color: 'green',
        autoClose: 3000,
      });
      await load('exact');
    } catch (err) {
      notifications.show({
        title: '入队失败',
        message: extractErrorMessage(err, '内容哈希回填失败'),
        color: 'red',
        autoClose: 3000,
      });
    } finally {
      setExactBackfilling(false);
    }
  }, [load]);

  const handleSimilarScan = useCallback(async () => {
    setSimilarScanning(true);
    try {
      const computed = await libApi.scanDuplicates();
      notifications.show({
        title: '扫描完成',
        message: `本次计算 ${computed} 项媒体感知哈希`,
        color: 'green',
        autoClose: 3000,
      });
      await load('similar');
    } catch (err) {
      notifications.show({
        title: '扫描失败',
        message: extractErrorMessage(err, '相似重复扫描失败'),
        color: 'red',
        autoClose: 3000,
      });
    } finally {
      setSimilarScanning(false);
    }
  }, [load]);

  const handleAIRefresh = useCallback(async () => {
    setAIRefreshing(true);
    try {
      await load('ai');
    } finally {
      setAIRefreshing(false);
    }
  }, [load]);

  const handleDelete = useCallback(async () => {
    const ids = Array.from(selected);
    if (ids.length === 0) return;
    setDeleting(true);
    try {
      const deleted = await libApi.batchDeleteMediaFiles(ids);
      notifications.show({
        title: '已移入回收站',
        message: `已删除 ${deleted} 项（可在回收站还原）`,
        color: 'green',
        autoClose: 3000,
      });
      setSelected(new Set());
      await load(mode);
    } catch (err) {
      notifications.show({
        title: '删除失败',
        message: extractErrorMessage(err, '批量删除失败'),
        color: 'red',
        autoClose: 3000,
      });
    } finally {
      setDeleting(false);
    }
  }, [selected, load, mode]);

  const groups = visibleItems(mode, exactGroups, similarGroups, aiGroups);
  const actionLabel =
    mode === 'exact' ? '回填精确哈希' : mode === 'similar' ? '扫描相似重复' : '刷新 AI 候选';
  const actionLoading =
    mode === 'exact' ? exactBackfilling : mode === 'similar' ? similarScanning : aiRefreshing;
  const actionIcon =
    mode === 'exact' ? (
      <IconFingerprint size={16} />
    ) : mode === 'similar' ? (
      <IconScan size={16} />
    ) : (
      <IconSparkles size={16} />
    );
  const actionHandler =
    mode === 'exact'
      ? handleExactBackfill
      : mode === 'similar'
        ? handleSimilarScan
        : handleAIRefresh;
  const showHeaderAction = groups.length > 0 || mode === 'ai';
  const tabColor = mode === 'exact' ? 'blue' : mode === 'similar' ? 'purple' : 'grape';

  return (
    <Stack gap="md">
      <PageHeader
        title="重复项"
        actions={
          <Group gap="sm">
            {showHeaderAction ? (
              <Button
                variant="light"
                color={tabColor}
                leftSection={actionIcon}
                loading={actionLoading}
                onClick={actionHandler}
              >
                {actionLabel}
              </Button>
            ) : null}
            <Button
              color="red"
              leftSection={<IconTrash size={16} />}
              loading={deleting}
              disabled={selected.size === 0}
              onClick={handleDelete}
            >
              删除选中项{selected.size > 0 ? `（${selected.size}）` : ''}
            </Button>
          </Group>
        }
      />

      <Tabs value={mode} onChange={(value) => setMode((value as DuplicateMode) || 'exact')}>
        <Tabs.List>
          <Tabs.Tab value="exact" leftSection={<IconFingerprint size={16} />}>
            精确重复
          </Tabs.Tab>
          <Tabs.Tab value="similar" leftSection={<IconScan size={16} />}>
            相似重复
          </Tabs.Tab>
          <Tabs.Tab value="ai" leftSection={<IconSparkles size={16} />}>
            AI 相似
          </Tabs.Tab>
        </Tabs.List>
      </Tabs>

      {mode === 'ai' && (
        <Text size="sm" c="dimmed">
          基于 embedding 余弦相似度的候选组，与哈希去重并列展示；不会自动删除媒体。
        </Text>
      )}

      {loading && groups.length === 0 ? (
        <Center py="xl">
          <Loader color={tabColor} />
        </Center>
      ) : groups.length === 0 ? (
        <EmptyState
          icon={
            <IconCopyOff
              size={72}
              stroke={1.2}
              style={{ color: 'var(--mantine-color-dimmed)', opacity: 0.7 }}
            />
          }
          title="没有发现重复项"
          description={
            mode === 'exact'
              ? '当前没有精确重复媒体。'
              : mode === 'similar'
                ? '当前没有相似重复媒体。'
                : (aiDisabledHint ?? '当前没有 AI 相似候选。')
          }
          action={{
            label: actionLabel,
            leftIcon: actionIcon,
            loading: actionLoading,
            onClick: actionHandler,
          }}
        />
      ) : (
        <Stack gap="lg">
          {groups.map((group, gi) => (
            <Paper key={group[0].id} withBorder p="md" radius="md">
              <Group mb="xs" gap="xs">
                <Badge variant="light" color={tabColor}>
                  第 {gi + 1} 组
                </Badge>
                <Text size="sm" c="dimmed">
                  {group.length} 项
                  {mode === 'exact' ? '精确重复' : mode === 'similar' ? '相似' : 'AI 相似'}
                </Text>
                {mode === 'ai' && aiGroups[gi] && (
                  <Badge size="sm" variant="outline" color="grape">
                    相似度 {(aiGroups[gi].score * 100).toFixed(1)}%
                  </Badge>
                )}
              </Group>
              <Stack gap="xs">
                {group.map((media) => (
                  <MediaRow
                    key={media.id}
                    mediaID={media.id}
                    thumbFileName={media.file_name}
                    title={media.file_name}
                    selectable
                    selected={selected.has(media.id)}
                    onToggle={() => toggle(media.id)}
                    checkboxLabel={`选择 ${media.file_name}`}
                  />
                ))}
              </Stack>
            </Paper>
          ))}
        </Stack>
      )}
    </Stack>
  );
}
