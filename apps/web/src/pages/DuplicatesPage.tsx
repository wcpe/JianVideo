import { useState, useEffect, useCallback } from 'react';
import { Stack, Group, Text, Button, Loader, Center, Paper, Badge, Tabs } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { IconTrash, IconScan, IconCopyOff, IconFingerprint } from '@tabler/icons-react';
import * as libApi from '@/api/library';
import PageHeader from '@/components/PageHeader';
import MediaRow from '@/components/MediaRow';
import EmptyState from '@/components/EmptyState';
import type { DuplicateGroup, ExactDuplicateGroup, MediaFile } from '@/types';

type DuplicateMode = 'exact' | 'similar';

function exactGroupItems(groups: ExactDuplicateGroup[]): MediaFile[][] {
  return groups.map((group) => group.items);
}

function visibleItems(
  mode: DuplicateMode,
  exact: ExactDuplicateGroup[],
  similar: DuplicateGroup[],
) {
  return mode === 'exact' ? exactGroupItems(exact) : similar;
}

export default function DuplicatesPage() {
  const [mode, setMode] = useState<DuplicateMode>('exact');
  const [exactGroups, setExactGroups] = useState<ExactDuplicateGroup[]>([]);
  const [similarGroups, setSimilarGroups] = useState<DuplicateGroup[]>([]);
  const [loading, setLoading] = useState(false);
  const [exactBackfilling, setExactBackfilling] = useState(false);
  const [similarScanning, setSimilarScanning] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [selected, setSelected] = useState<Set<number>>(new Set());

  const load = useCallback(async (targetMode: DuplicateMode) => {
    setLoading(true);
    try {
      let nextGroups: MediaFile[][];
      if (targetMode === 'exact') {
        const exact = await libApi.getExactDuplicateGroups();
        setExactGroups(exact);
        nextGroups = exactGroupItems(exact);
      } else {
        const similar = await libApi.getDuplicateGroups();
        setSimilarGroups(similar);
        nextGroups = similar;
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
        message: err instanceof Error ? err.message : '无法加载重复项',
        color: 'red',
        autoClose: 3000,
      });
    } finally {
      setLoading(false);
    }
  }, []);

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
        message: err instanceof Error ? err.message : '内容哈希回填失败',
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
        message: err instanceof Error ? err.message : '相似重复扫描失败',
        color: 'red',
        autoClose: 3000,
      });
    } finally {
      setSimilarScanning(false);
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
        message: err instanceof Error ? err.message : '批量删除失败',
        color: 'red',
        autoClose: 3000,
      });
    } finally {
      setDeleting(false);
    }
  }, [selected, load, mode]);

  const groups = visibleItems(mode, exactGroups, similarGroups);
  const actionLabel = mode === 'exact' ? '回填精确哈希' : '扫描相似重复';
  const actionLoading = mode === 'exact' ? exactBackfilling : similarScanning;
  const actionIcon = mode === 'exact' ? <IconFingerprint size={16} /> : <IconScan size={16} />;
  const actionHandler = mode === 'exact' ? handleExactBackfill : handleSimilarScan;
  const showHeaderAction = groups.length > 0;

  return (
    <Stack gap="md">
      <PageHeader
        title="重复项"
        actions={
          <Group gap="sm">
            {showHeaderAction ? (
              <Button
                variant="light"
                color={mode === 'exact' ? 'blue' : 'purple'}
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
        </Tabs.List>
      </Tabs>

      {loading && groups.length === 0 ? (
        <Center py="xl">
          <Loader color={mode === 'exact' ? 'blue' : 'purple'} />
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
          description={mode === 'exact' ? '当前没有精确重复媒体。' : '当前没有相似重复媒体。'}
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
                <Badge variant="light" color={mode === 'exact' ? 'blue' : 'purple'}>
                  第 {gi + 1} 组
                </Badge>
                <Text size="sm" c="dimmed">
                  {group.length} 项{mode === 'exact' ? '精确重复' : '相似'}
                </Text>
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
