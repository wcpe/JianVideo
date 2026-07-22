import { useCallback, useEffect, useState } from 'react';
import {
  Modal,
  Stack,
  Group,
  Text,
  Button,
  FileButton,
  Select,
  Progress,
  ScrollArea,
  ActionIcon,
  Box,
} from '@mantine/core';
import { IconUpload, IconX, IconCheck, IconAlertCircle, IconFileUpload } from '@tabler/icons-react';
import { notifications } from '@mantine/notifications';
import { uploadMedia, getLibraryPaths } from '@/api/library';
import {
  getSettings,
  SETTING_KEY_UPLOAD_TARGET_DIR,
  SETTING_KEY_UPLOAD_NAMING_RULE,
} from '@/api/settings';
import type { LibraryPath, UploadNamingRule } from '@/types';

// 单个待上传文件的状态（FR-149）：覆盖排队/上传中/成功/失败四态
type ItemStatus = 'pending' | 'uploading' | 'done' | 'error';
interface UploadItem {
  file: File;
  status: ItemStatus;
  percent: number;
  error?: string;
}

// 仅接受图片/视频（与后端扩展名策略对齐的浏览器侧粗筛，最终以后端校验为准）
const ACCEPT = 'image/*,video/*';

/**
 * Web 上传入库弹窗（FR-149，见 ADR-0051）。
 * 选文件/拖拽 → 逐个上传（带进度）→ 落盘到目标位置后后端自动触发扫描入库。
 * 目标位置：缺省走设置默认；可在此临时选择已注册本地库目录覆盖。命名规则同理。
 */
export default function UploadModal({ opened, onClose }: { opened: boolean; onClose: () => void }) {
  const [items, setItems] = useState<UploadItem[]>([]);
  const [uploading, setUploading] = useState(false);
  const [dragOver, setDragOver] = useState(false);

  // 目标位置与命名规则：空串表示「沿用设置默认」
  const [libraryPaths, setLibraryPaths] = useState<LibraryPath[]>([]);
  const [targetDir, setTargetDir] = useState('');
  const [namingRule, setNamingRule] = useState('');
  // 设置默认值（仅展示提示，实际缺省由后端兜底）
  const [defaultTargetDir, setDefaultTargetDir] = useState('');

  // 打开时拉取本地库目录与默认设置，供目标位置下拉与缺省提示
  useEffect(() => {
    if (!opened) return;
    getLibraryPaths()
      .then((paths) => setLibraryPaths(paths.filter((p) => p.type === 'local' && p.enabled)))
      .catch(() => {
        /* 拉取失败不阻塞上传，目标位置仍可走设置默认 */
      });
    getSettings()
      .then((s) => {
        setDefaultTargetDir(s[SETTING_KEY_UPLOAD_TARGET_DIR] ?? '');
        setNamingRule(s[SETTING_KEY_UPLOAD_NAMING_RULE] ?? '');
      })
      .catch(() => {
        /* 读默认失败忽略，仍可临时选择 */
      });
  }, [opened]);

  const addFiles = useCallback((files: File[]) => {
    if (files.length === 0) return;
    setItems((prev) => [
      ...prev,
      ...files.map((file) => ({ file, status: 'pending' as ItemStatus, percent: 0 })),
    ]);
  }, []);

  const removeItem = useCallback((index: number) => {
    setItems((prev) => prev.filter((_, i) => i !== index));
  }, []);

  // 拖拽放下：仅取图片/视频 MIME 前缀的文件
  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      setDragOver(false);
      const dropped = Array.from(e.dataTransfer.files).filter(
        (f) => f.type.startsWith('image/') || f.type.startsWith('video/'),
      );
      addFiles(dropped);
    },
    [addFiles],
  );

  // 逐个串行上传，更新各自进度与终态；全部完成后提示
  const handleUpload = useCallback(async () => {
    const targets = items
      .map((it, i) => ({ it, i }))
      .filter(({ it }) => it.status === 'pending' || it.status === 'error');
    if (targets.length === 0) return;

    setUploading(true);
    let success = 0;
    for (const { it, i } of targets) {
      setItems((prev) =>
        prev.map((x, idx) =>
          idx === i ? { ...x, status: 'uploading', percent: 0, error: undefined } : x,
        ),
      );
      try {
        await uploadMedia(it.file, {
          targetDir: targetDir || undefined,
          namingRule: (namingRule || undefined) as UploadNamingRule | undefined,
          onProgress: (percent) =>
            setItems((prev) => prev.map((x, idx) => (idx === i ? { ...x, percent } : x))),
        });
        success += 1;
        setItems((prev) =>
          prev.map((x, idx) => (idx === i ? { ...x, status: 'done', percent: 100 } : x)),
        );
      } catch (err) {
        const message = err instanceof Error ? err.message : '上传失败';
        setItems((prev) =>
          prev.map((x, idx) => (idx === i ? { ...x, status: 'error', error: message } : x)),
        );
      }
    }
    setUploading(false);

    if (success > 0) {
      notifications.show({
        title: '上传完成',
        message: `已上传 ${success} 个文件，正在后台扫描入库`,
        color: 'green',
      });
    }
  }, [items, targetDir, namingRule]);

  const handleClose = useCallback(() => {
    if (uploading) return; // 上传中不允许关闭，避免中断
    setItems([]);
    setTargetDir('');
    onClose();
  }, [uploading, onClose]);

  const pendingCount = items.filter(
    (it) => it.status === 'pending' || it.status === 'error',
  ).length;
  const targetOptions = libraryPaths.map((p) => ({
    value: p.path,
    label: p.label ? `${p.label}（${p.path}）` : p.path,
  }));

  return (
    <Modal opened={opened} onClose={handleClose} title="上传媒体" size="lg" centered>
      <Stack gap="md">
        {/* 拖拽区 + 选文件按钮 */}
        <Box
          onDragOver={(e) => {
            e.preventDefault();
            setDragOver(true);
          }}
          onDragLeave={() => setDragOver(false)}
          onDrop={handleDrop}
          style={{
            border: `2px dashed var(--mantine-color-${dragOver ? 'purple' : 'gray'}-5)`,
            borderRadius: 'var(--mantine-radius-md)',
            padding: 24,
            textAlign: 'center',
            backgroundColor: dragOver ? 'var(--mantine-color-purple-light)' : undefined,
          }}
        >
          <Stack gap="xs" align="center">
            <IconFileUpload size={32} style={{ color: 'var(--mantine-color-dimmed)' }} />
            <Text size="sm" c="dimmed">
              拖拽图片/视频到此处，或
            </Text>
            <FileButton onChange={(files) => addFiles(files ?? [])} accept={ACCEPT} multiple>
              {(props) => (
                <Button
                  {...props}
                  variant="light"
                  color="purple"
                  leftSection={<IconUpload size={16} />}
                >
                  选择文件
                </Button>
              )}
            </FileButton>
          </Stack>
        </Box>

        {/* 目标位置 + 命名规则 */}
        <Group grow align="flex-start">
          <Select
            label="目标位置"
            description={defaultTargetDir ? `默认：${defaultTargetDir}` : '未设默认，请选择'}
            placeholder="沿用设置默认"
            data={targetOptions}
            value={targetDir || null}
            onChange={(v) => setTargetDir(v ?? '')}
            clearable
            searchable
          />
          <Select
            label="命名规则"
            data={[
              { value: 'original', label: '保留原样' },
              { value: 'date', label: '按日期整齐归档（年/月）' },
            ]}
            value={namingRule || null}
            onChange={(v) => setNamingRule(v ?? '')}
            placeholder="沿用设置默认"
            clearable
          />
        </Group>

        {/* 待上传列表 + 各自进度 */}
        {items.length > 0 && (
          <ScrollArea.Autosize mah={220}>
            <Stack gap="xs">
              {items.map((it, i) => (
                <Box key={`${it.file.name}-${i}`}>
                  <Group justify="space-between" gap="xs" wrap="nowrap">
                    <Text size="sm" truncate style={{ flex: 1 }}>
                      {it.file.name}
                    </Text>
                    {it.status === 'done' && (
                      <IconCheck size={16} color="var(--mantine-color-green-6)" />
                    )}
                    {it.status === 'error' && (
                      <IconAlertCircle size={16} color="var(--mantine-color-red-6)" />
                    )}
                    {(it.status === 'pending' || it.status === 'error') && !uploading && (
                      <ActionIcon
                        variant="subtle"
                        color="gray"
                        size="sm"
                        onClick={() => removeItem(i)}
                        aria-label="移除"
                      >
                        <IconX size={14} />
                      </ActionIcon>
                    )}
                  </Group>
                  {it.status === 'uploading' && <Progress value={it.percent} size="sm" mt={4} />}
                  {it.status === 'error' && (
                    <Text size="xs" c="red">
                      {it.error}
                    </Text>
                  )}
                </Box>
              ))}
            </Stack>
          </ScrollArea.Autosize>
        )}

        <Group justify="flex-end">
          <Button variant="default" onClick={handleClose} disabled={uploading}>
            关闭
          </Button>
          <Button
            color="purple"
            onClick={handleUpload}
            loading={uploading}
            disabled={pendingCount === 0}
            leftSection={<IconUpload size={16} />}
          >
            开始上传{pendingCount > 0 ? `（${pendingCount}）` : ''}
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}
