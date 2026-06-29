import { useState } from 'react';
import {
  Modal,
  Button,
  Select,
  Group,
  TextInput,
  NumberInput,
  CopyButton,
  Stack,
  Text,
} from '@mantine/core';
import { IconShare, IconCopy, IconCheck } from '@tabler/icons-react';
import { notifications } from '@mantine/notifications';
import { createShare } from '@/api/share';
import type { ShareResourceType } from '@/types';

interface ShareDialogProps {
  opened: boolean;
  onClose: () => void;
  resourceType: ShareResourceType;
  resourceID: number;
  title?: string;
}

// 有效期选项（小时）：0 表示永不过期
const EXPIRY_OPTIONS = [
  { value: '0', label: '永不过期' },
  { value: '24', label: '1 天' },
  { value: '168', label: '7 天' },
  { value: '720', label: '30 天' },
];

/** 创建分享链接弹窗（FR-43；密码/限次见 FR-78）：选有效期/密码/限次 → 生成 → 复制免登链接 */
export default function ShareDialog({
  opened,
  onClose,
  resourceType,
  resourceID,
  title,
}: ShareDialogProps) {
  const [expiry, setExpiry] = useState('0');
  const [password, setPassword] = useState('');
  const [maxUses, setMaxUses] = useState<number>(0);
  const [link, setLink] = useState('');
  const [loading, setLoading] = useState(false);

  async function handleCreate() {
    setLoading(true);
    try {
      const share = await createShare(resourceType, resourceID, Number(expiry), password, maxUses);
      setLink(`${window.location.origin}/s/${share.token}`);
    } catch (err) {
      notifications.show({
        color: 'red',
        message: err instanceof Error ? err.message : '创建分享失败',
      });
    } finally {
      setLoading(false);
    }
  }

  function handleClose() {
    setLink('');
    setExpiry('0');
    setPassword('');
    setMaxUses(0);
    onClose();
  }

  return (
    <Modal opened={opened} onClose={handleClose} title={title ?? '创建分享链接'} centered>
      <Stack>
        <Select
          label="有效期"
          data={EXPIRY_OPTIONS}
          value={expiry}
          onChange={(v) => setExpiry(v ?? '0')}
          allowDeselect={false}
        />
        <TextInput
          label="访问密码"
          placeholder="不设密码"
          value={password}
          onChange={(e) => setPassword(e.currentTarget.value)}
          description="设置后访客需输入密码才能访问"
        />
        <NumberInput
          label="访问次数上限"
          placeholder="0 表示不限次"
          min={0}
          value={maxUses}
          onChange={(v) => setMaxUses(typeof v === 'number' ? v : 0)}
          description="0 表示无限次；达到次数后链接失效"
        />
        {!link ? (
          <Button onClick={handleCreate} loading={loading} leftSection={<IconShare size={16} />}>
            生成链接
          </Button>
        ) : (
          <>
            <Text size="sm" c="dimmed">
              免登访问链接（任何持有者可只读访问被分享内容）：
            </Text>
            <Group gap="xs" wrap="nowrap">
              <TextInput value={link} readOnly style={{ flex: 1 }} aria-label="分享链接" />
              <CopyButton value={link}>
                {({ copied, copy }) => (
                  <Button
                    color={copied ? 'teal' : 'blue'}
                    onClick={copy}
                    leftSection={copied ? <IconCheck size={16} /> : <IconCopy size={16} />}
                  >
                    {copied ? '已复制' : '复制'}
                  </Button>
                )}
              </CopyButton>
            </Group>
          </>
        )}
      </Stack>
    </Modal>
  );
}
