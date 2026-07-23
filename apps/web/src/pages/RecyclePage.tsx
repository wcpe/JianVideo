import { useState, useEffect, useCallback, useMemo } from 'react';
import { Stack, Text, Button, Loader, Center } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { IconArrowBackUp, IconTrash } from '@tabler/icons-react';
import type { AxiosError } from 'axios';
import * as libApi from '@/api/library';
import {
  getSettings,
  parseBooleanSetting,
  SETTING_KEY_RECYCLE_AUTO_CLEANUP_ENABLED,
  SETTING_KEY_RECYCLE_RETENTION_DAYS,
} from '@/api/settings';
import PageHeader from '@/components/PageHeader';
import ConfirmModal from '@/components/ConfirmModal';
import MediaRow from '@/components/MediaRow';
import EmptyState from '@/components/EmptyState';
import { formatRecycleExpiryHint } from '@/utils/recycle-bin';
import type { MediaFile } from '@/types';

/** 回收站页（FR-25/FR-26/FR2-054）：软删列表、还原、手动清理，并展示到期自动清理提示 */
export default function RecyclePage() {
  const [items, setItems] = useState<MediaFile[]>([]);
  const [loading, setLoading] = useState(false);
  const [restoringID, setRestoringID] = useState<number | null>(null);
  const [cleanupOpen, setCleanupOpen] = useState(false);
  const [cleaning, setCleaning] = useState(false);
  // 保留策略（FR2-054）：用于列表行到期提示；加载失败时回退默认 30 天 + 开启
  const [retentionDays, setRetentionDays] = useState(30);
  const [autoCleanupEnabled, setAutoCleanupEnabled] = useState(true);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [media, settings] = await Promise.all([
        libApi.getRecycleMediaFiles(),
        getSettings().catch(() => null),
      ]);
      setItems(media);
      if (settings) {
        const days = Number.parseInt(settings[SETTING_KEY_RECYCLE_RETENTION_DAYS] ?? '30', 10);
        setRetentionDays(Number.isFinite(days) && days >= 0 ? days : 30);
        setAutoCleanupEnabled(
          parseBooleanSetting(settings[SETTING_KEY_RECYCLE_AUTO_CLEANUP_ENABLED], true),
        );
      }
    } catch (err) {
      notifications.show({
        title: '加载失败',
        message: err instanceof Error ? err.message : '无法加载回收站',
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

  const policyHint = useMemo(() => {
    if (!autoCleanupEnabled || retentionDays <= 0) {
      return '当前未启用到期自动清理，仅可手动清理；可在设置页调整保留天数与开关。';
    }
    return `当前保留 ${retentionDays} 天，到期后将自动移入盘符回收站目录；可在设置页调整。`;
  }, [autoCleanupEnabled, retentionDays]);

  const handleRestore = useCallback(async (media: MediaFile) => {
    setRestoringID(media.id);
    try {
      await libApi.restoreMediaFile(media.id);
      // 还原成功后从回收站列表移除
      setItems((prev) => prev.filter((m) => m.id !== media.id));
    } catch (err) {
      notifications.show({
        title: '还原失败',
        message: err instanceof Error ? err.message : '还原媒体失败',
        color: 'red',
        autoClose: 3000,
      });
    } finally {
      setRestoringID(null);
    }
  }, []);

  const handleCleanup = useCallback(async () => {
    setCleaning(true);
    try {
      const result = await libApi.cleanupRecycle();
      setCleanupOpen(false);
      notifications.show({
        title: '清理完成',
        message: `已移动 ${result.moved} 个文件到回收站目录${result.failed > 0 ? `，${result.failed} 个失败` : ''}`,
        color: 'green',
        autoClose: 3000,
      });
      await load();
    } catch (err) {
      // 未配置盘符回收站路径（409）：引导用户去设置页配置
      const status =
        (err as AxiosError | undefined)?.response?.status ??
        (err as { cause?: AxiosError })?.cause?.response?.status;
      if (status === 409) {
        notifications.show({
          title: '尚未配置回收站路径',
          message: '请先到设置页配置对应盘符的回收站路径，再执行清理。',
          color: 'orange',
          autoClose: 5000,
        });
      } else {
        notifications.show({
          title: '清理失败',
          message: err instanceof Error ? err.message : '清理回收站失败',
          color: 'red',
          autoClose: 4000,
        });
      }
    } finally {
      setCleaning(false);
    }
  }, [load]);

  return (
    <Stack gap="md">
      <PageHeader
        title="回收站"
        actions={
          items.length > 0 && (
            <Button
              color="red"
              variant="light"
              leftSection={<IconTrash size={16} />}
              onClick={() => setCleanupOpen(true)}
            >
              清理回收站
            </Button>
          )
        }
      />
      <Text size="sm" c="dimmed">
        这里的媒体已从媒体库移除但源文件仍在磁盘，可随时还原；清理会把源文件移动到所在盘符的回收站目录（按删除日期分类）并移除记录。
      </Text>
      <Text size="sm" c="dimmed">
        {policyHint}
      </Text>

      {loading && items.length === 0 ? (
        <Center py="xl">
          <Loader color="purple" />
        </Center>
      ) : items.length === 0 ? (
        <EmptyState
          icon={
            <IconTrash
              size={72}
              stroke={1.2}
              style={{ color: 'var(--mantine-color-dimmed)', opacity: 0.7 }}
            />
          }
          title="回收站是空的"
          description="被软删的媒体会出现在这里，源文件仍在磁盘可随时还原。当前没有待还原的媒体。"
        />
      ) : (
        <Stack gap="xs">
          {items.map((media) => {
            const expiry = formatRecycleExpiryHint(
              media.deleted_at,
              retentionDays,
              autoCleanupEnabled,
            );
            return (
              <MediaRow
                key={media.id}
                mediaID={media.id}
                thumbFileName={media.file_name}
                title={media.file_name}
                subtitle={expiry?.text}
                actions={
                  <Button
                    size="compact-xs"
                    variant="light"
                    color="purple"
                    leftSection={<IconArrowBackUp size={14} />}
                    loading={restoringID === media.id}
                    onClick={() => handleRestore(media)}
                  >
                    还原
                  </Button>
                }
              />
            );
          })}
        </Stack>
      )}

      <ConfirmModal
        opened={cleanupOpen}
        title="清理回收站"
        message="将把回收站内所有媒体的源文件移动到其所在盘符的回收站目录（按删除日期分类），并从媒体库移除对应记录。此操作不可撤销，确定继续？"
        confirmLabel="清理"
        cancelLabel="取消"
        confirmColor="red"
        loading={cleaning}
        onConfirm={handleCleanup}
        onCancel={() => setCleanupOpen(false)}
      />
    </Stack>
  );
}
