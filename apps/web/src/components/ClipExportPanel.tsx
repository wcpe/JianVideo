import { useState } from 'react';
import { Button, Group, Modal, NumberInput, Select, Stack, Text } from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { enqueueClipExport, exportDownloadUrl } from '@/api/library';
import { getTask } from '@/api/tasks';

export interface ClipExportPanelProps {
  opened: boolean;
  mediaId: number;
  duration: number;
  onClose: () => void;
}

type ClipFormat = 'mp4' | 'mkv' | 'mov';

/**
 * 视频片段粗剪导出（FR2-039）：选择起止秒与格式，入队任务中心，不改原文件。
 */
export default function ClipExportPanel({
  opened,
  mediaId,
  duration,
  onClose,
}: ClipExportPanelProps) {
  const maxEnd = duration > 0 ? duration : 7200;
  const [startSec, setStartSec] = useState(0);
  const [endSec, setEndSec] = useState(Math.min(30, maxEnd));
  const [format, setFormat] = useState<ClipFormat>('mp4');
  const [exporting, setExporting] = useState(false);

  const clipLen = Math.max(0, endSec - startSec);

  const handleExport = async () => {
    if (startSec < 0 || endSec <= startSec) {
      notifications.show({ color: 'red', title: '参数错误', message: '起止时间不合法' });
      return;
    }
    setExporting(true);
    try {
      const accepted = await enqueueClipExport(mediaId, {
        start_sec: startSec,
        end_sec: endSec,
        format,
      });
      notifications.show({
        color: 'blue',
        title: '已入队',
        message: `粗剪导出任务 #${accepted.task_id}`,
      });
      void pollAndNotify(accepted.task_id);
      onClose();
    } catch (err) {
      notifications.show({
        color: 'red',
        title: '导出失败',
        message: err instanceof Error ? err.message : '视频粗剪入队失败',
      });
    } finally {
      setExporting(false);
    }
  };

  return (
    <Modal opened={opened} onClose={onClose} title="片段粗剪导出" size="md" centered>
      <Stack gap="md">
        <Text size="sm" c="dimmed">
          不修改原文件。优先 stream copy，失败时自动重编码。
        </Text>
        <NumberInput
          label="起始（秒）"
          min={0}
          max={maxEnd}
          decimalScale={3}
          value={startSec}
          onChange={(v) => setStartSec(typeof v === 'number' ? v : 0)}
        />
        <NumberInput
          label="结束（秒）"
          min={0}
          max={maxEnd + 1}
          decimalScale={3}
          value={endSec}
          onChange={(v) => setEndSec(typeof v === 'number' ? v : 0)}
        />
        <Text size="sm">
          片段时长：{clipLen.toFixed(2)} 秒
          {duration > 0 ? `（媒体总长 ${duration.toFixed(1)}s）` : ''}
        </Text>
        <Select
          label="导出格式"
          data={[
            { value: 'mp4', label: 'MP4' },
            { value: 'mkv', label: 'MKV' },
            { value: 'mov', label: 'MOV' },
          ]}
          value={format}
          onChange={(v) => setFormat((v as ClipFormat) || 'mp4')}
        />
        <Group justify="flex-end">
          <Button variant="default" onClick={onClose}>
            取消
          </Button>
          <Button loading={exporting} onClick={() => void handleExport()}>
            导出
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}

async function pollAndNotify(taskId: string) {
  for (let i = 0; i < 120; i++) {
    await new Promise((r) => setTimeout(r, 2000));
    try {
      const task = await getTask(String(taskId));
      if (task.status === 'succeeded') {
        notifications.show({
          color: 'green',
          title: '粗剪完成',
          message: '点击下载产物',
          autoClose: 10000,
          onClick: () => {
            window.open(exportDownloadUrl(taskId), '_blank');
          },
        });
        return;
      }
      if (task.status === 'failed' || task.status === 'canceled') {
        notifications.show({
          color: 'red',
          title: '粗剪失败',
          message: task.error || '任务失败',
        });
        return;
      }
    } catch {
      // ignore
    }
  }
}
