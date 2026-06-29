import { useState, useEffect, useCallback } from 'react';
import { Stack, Title, Group, Text, Button, Loader, Center, Paper, Badge } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { IconTrash, IconScan, IconCopyOff } from '@tabler/icons-react';
import * as libApi from '@/api/library';
import MediaRow from '@/components/MediaRow';
import EmptyState from '@/components/EmptyState';
import type { DuplicateGroup } from '@/types';

/**
 * 重复项页（FR-70）：展示按感知哈希（dHash）聚类的近似重复组。
 * 用户勾选组内多余项后批量软删（复用 FR-69 批量软删端点）进回收站，可在回收站还原。
 * 「扫描重复项」按钮触发后端为缺哈希媒体补算 dHash 再刷新。
 */
export default function DuplicatesPage() {
  const [groups, setGroups] = useState<DuplicateGroup[]>([]);
  const [loading, setLoading] = useState(false);
  const [scanning, setScanning] = useState(false);
  const [deleting, setDeleting] = useState(false);
  // 选中待删除的媒体 ID 集合
  const [selected, setSelected] = useState<Set<number>>(new Set());

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const data = await libApi.getDuplicateGroups();
      setGroups(data);
      // 刷新后清理已不存在的选中项
      setSelected((prev) => {
        const alive = new Set<number>();
        for (const g of data) for (const m of g) if (prev.has(m.id)) alive.add(m.id);
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
    load();
  }, [load]);

  const toggle = useCallback((id: number) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const handleScan = useCallback(async () => {
    setScanning(true);
    try {
      const computed = await libApi.scanDuplicates();
      notifications.show({
        title: '扫描完成',
        message: `本次计算 ${computed} 项媒体哈希`,
        color: 'green',
        autoClose: 3000,
      });
      await load();
    } catch (err) {
      notifications.show({
        title: '扫描失败',
        message: err instanceof Error ? err.message : '去重扫描失败',
        color: 'red',
        autoClose: 3000,
      });
    } finally {
      setScanning(false);
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
      await load();
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
  }, [selected, load]);

  return (
    <Stack gap="md">
      <Group justify="space-between" align="flex-end">
        <Title order={2}>重复项</Title>
        <Group gap="sm">
          <Button
            variant="light"
            color="purple"
            leftSection={<IconScan size={16} />}
            loading={scanning}
            onClick={handleScan}
          >
            扫描重复项
          </Button>
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
      </Group>
      <Text size="sm" c="dimmed">
        基于缩略图感知哈希（dHash）检出的近似重复媒体，按组列出。勾选每组中要删除的多余项后点「删除选中项」软删进回收站（可还原）。
        若新入库媒体尚未参与，请先点「扫描重复项」补算哈希。
      </Text>

      {loading && groups.length === 0 ? (
        <Center py="xl">
          <Loader color="purple" />
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
          description="基于缩略图感知哈希（dHash）未检出近似重复媒体。若新入库媒体尚未参与，点「扫描重复项」补算哈希后再看。"
          action={{
            label: '扫描重复项',
            leftIcon: <IconScan size={16} />,
            loading: scanning,
            onClick: handleScan,
          }}
        />
      ) : (
        <Stack gap="lg">
          {groups.map((group, gi) => (
            <Paper key={group[0].id} withBorder p="md" radius="md">
              <Group mb="xs" gap="xs">
                <Badge variant="light" color="purple">
                  第 {gi + 1} 组
                </Badge>
                <Text size="sm" c="dimmed">
                  {group.length} 项相似
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
