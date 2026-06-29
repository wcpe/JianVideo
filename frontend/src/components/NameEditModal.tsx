import { useState, useEffect } from 'react';
import { Modal, TextInput, Text, Button, Group, Stack } from '@mantine/core';

interface NameEditModalProps {
  /** 是否打开 */
  opened: boolean;
  /** 弹窗标题 */
  title: string;
  /** 输入框标签（同时作为 aria-label，供测试与无障碍定位） */
  label: string;
  /** 二次确认说明文案 */
  message: string;
  /** 输入框初始值 */
  initialValue: string;
  /** 是否允许空值提交（显示名允许清空，真实文件名不允许） */
  allowEmpty?: boolean;
  /** 提交中状态 */
  loading?: boolean;
  /** 确认回调，回传去首尾空白后的值 */
  onConfirm: (value: string) => void;
  /** 取消回调 */
  onCancel: () => void;
}

/**
 * 名称编辑 + 二次确认弹窗（FR-30）。
 * 弹窗本身即二次确认步骤：用户输入新名后须点「确认修改」才真正提交。
 */
export default function NameEditModal({
  opened,
  title,
  label,
  message,
  initialValue,
  allowEmpty = false,
  loading = false,
  onConfirm,
  onCancel,
}: NameEditModalProps) {
  const [value, setValue] = useState(initialValue);

  // 每次打开时同步初始值，避免上次输入残留
  useEffect(() => {
    if (opened) setValue(initialValue);
  }, [opened, initialValue]);

  const trimmed = value.trim();
  const canSubmit = allowEmpty || trimmed !== '';

  return (
    <Modal opened={opened} onClose={onCancel} title={title} centered size="sm">
      <Stack gap="md">
        <TextInput
          label={label}
          aria-label={label}
          value={value}
          onChange={(e) => setValue(e.currentTarget.value)}
          disabled={loading}
          data-autofocus
        />
        <Text size="xs" c="dimmed">
          {message}
        </Text>
        <Group justify="flex-end">
          <Button variant="subtle" color="gray" onClick={onCancel} disabled={loading}>
            取消
          </Button>
          <Button onClick={() => onConfirm(trimmed)} loading={loading} disabled={!canSubmit}>
            确认修改
          </Button>
        </Group>
      </Stack>
    </Modal>
  );
}
