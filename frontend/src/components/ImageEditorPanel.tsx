import { useMemo, useState } from 'react';
import {
  Button,
  Group,
  Modal,
  Select,
  Slider,
  Stack,
  Text,
  Box,
  Image,
} from '@mantine/core';
import { notifications } from '@mantine/notifications';
import { enqueueImageExport, exportDownloadUrl } from '@/api/library';
import { getTask } from '@/api/tasks';
import { mediaRawUrl } from '@/utils/media-url';

export interface ImageEditorPanelProps {
  opened: boolean;
  mediaId: number;
  onClose: () => void;
}

type ImageFormat = 'jpeg' | 'png' | 'webp';

const DEFAULTS = {
  exposure: 0,
  contrast: 0,
  saturation: 0,
  temperature: 0,
  format: 'jpeg' as ImageFormat,
};

/**
 * 图片高级编辑器（FR2-038）：预览用 CSS filter 近似，导出入队任务中心。
 * 预览与导出可能有色差，以服务端产物为准。
 */
export default function ImageEditorPanel({ opened, mediaId, onClose }: ImageEditorPanelProps) {
  const [exposure, setExposure] = useState(DEFAULTS.exposure);
  const [contrast, setContrast] = useState(DEFAULTS.contrast);
  const [saturation, setSaturation] = useState(DEFAULTS.saturation);
  const [temperature, setTemperature] = useState(DEFAULTS.temperature);
  const [format, setFormat] = useState<ImageFormat>(DEFAULTS.format);
  const [exporting, setExporting] = useState(false);

  const filterStyle = useMemo(() => {
    // CSS filter 近似：曝光→brightness，对比→contrast，饱和→saturate，色温→sepia 微偏
    const brightness = 1 + exposure / 100;
    const contrastVal = 1 + contrast / 100;
    const saturateVal = 1 + saturation / 100;
    const sepia = Math.max(0, temperature / 200);
    return {
      filter: `brightness(${brightness}) contrast(${contrastVal}) saturate(${saturateVal}) sepia(${sepia})`,
    };
  }, [exposure, contrast, saturation, temperature]);

  const reset = () => {
    setExposure(DEFAULTS.exposure);
    setContrast(DEFAULTS.contrast);
    setSaturation(DEFAULTS.saturation);
    setTemperature(DEFAULTS.temperature);
    setFormat(DEFAULTS.format);
  };

  const handleExport = async () => {
    setExporting(true);
    try {
      const accepted = await enqueueImageExport(mediaId, {
        exposure,
        contrast,
        saturation,
        temperature,
        format,
      });
      notifications.show({
        color: 'blue',
        title: '已入队',
        message: `图片导出任务 #${accepted.task_id}，完成后可在任务中心下载`,
      });
      // 轻量轮询：成功后弹出下载提示
      void pollAndNotify(accepted.task_id);
      onClose();
    } catch (err) {
      notifications.show({
        color: 'red',
        title: '导出失败',
        message: err instanceof Error ? err.message : '图片导出入队失败',
      });
    } finally {
      setExporting(false);
    }
  };

  return (
    <Modal opened={opened} onClose={onClose} title="图片编辑导出" size="lg" centered>
      <Stack gap="md">
        <Box style={{ maxHeight: 320, overflow: 'hidden', textAlign: 'center' }}>
          <Image
            src={mediaRawUrl(mediaId)}
            alt="预览"
            fit="contain"
            h={300}
            style={filterStyle}
          />
        </Box>
        <Text size="xs" c="dimmed">
          预览为浏览器近似效果，最终以服务端导出为准
        </Text>
        <SliderLabel label="曝光" value={exposure} onChange={setExposure} />
        <SliderLabel label="对比度" value={contrast} onChange={setContrast} />
        <SliderLabel label="饱和度" value={saturation} onChange={setSaturation} />
        <SliderLabel label="色温" value={temperature} onChange={setTemperature} />
        <Select
          label="导出格式"
          data={[
            { value: 'jpeg', label: 'JPEG' },
            { value: 'png', label: 'PNG' },
            { value: 'webp', label: 'WebP' },
          ]}
          value={format}
          onChange={(v) => setFormat((v as ImageFormat) || 'jpeg')}
        />
        <Group justify="space-between">
          <Button variant="default" onClick={reset}>
            重置
          </Button>
          <Group>
            <Button variant="default" onClick={onClose}>
              取消
            </Button>
            <Button loading={exporting} onClick={() => void handleExport()}>
              导出
            </Button>
          </Group>
        </Group>
      </Stack>
    </Modal>
  );
}

function SliderLabel({
  label,
  value,
  onChange,
}: {
  label: string;
  value: number;
  onChange: (v: number) => void;
}) {
  return (
    <div>
      <Group justify="space-between" mb={4}>
        <Text size="sm">{label}</Text>
        <Text size="sm" c="dimmed">
          {value}
        </Text>
      </Group>
      <Slider min={-100} max={100} step={1} value={value} onChange={onChange} />
    </div>
  );
}

async function pollAndNotify(taskId: string) {
  for (let i = 0; i < 60; i++) {
    await new Promise((r) => setTimeout(r, 1500));
    try {
      const task = await getTask(String(taskId));
      if (task.status === 'succeeded') {
        notifications.show({
          color: 'green',
          title: '导出完成',
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
          title: '导出失败',
          message: task.error || '任务失败',
        });
        return;
      }
    } catch {
      // 轮询失败忽略
    }
  }
}
