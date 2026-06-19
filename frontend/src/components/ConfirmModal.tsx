import { Modal, Text, Button, Group, Stack } from '@mantine/core'
import { IconAlertTriangle } from '@tabler/icons-react'

interface ConfirmModalProps {
  opened: boolean
  title: string
  message: string
  confirmLabel?: string
  cancelLabel?: string
  confirmColor?: string
  onConfirm: () => void
  onCancel: () => void
  loading?: boolean
}

/** 确认弹窗组件 */
export default function ConfirmModal({
  opened,
  title,
  message,
  confirmLabel = '确认',
  cancelLabel = '取消',
  confirmColor = 'red',
  onConfirm,
  onCancel,
  loading = false,
}: ConfirmModalProps) {
  return (
    <Modal opened={opened} onClose={onCancel} title={title} centered size="sm">
      <Stack gap="md">
        <Group gap="xs">
          <IconAlertTriangle size={20} style={{ color: 'var(--mantine-color-yellow-6)' }} />
          <Text size="sm">{message}</Text>
        </Group>
        <Group justify="flex-end">
          <Button variant="subtle" color="gray" onClick={onCancel} disabled={loading}>
            {cancelLabel}
          </Button>
          <Button color={confirmColor} onClick={onConfirm} loading={loading}>
            {confirmLabel}
          </Button>
        </Group>
      </Stack>
    </Modal>
  )
}
