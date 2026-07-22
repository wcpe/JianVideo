import { useState } from 'react';
import { ActionIcon, Box, Paper, Stack, Text, Tooltip } from '@mantine/core';
import { IconHelp } from '@tabler/icons-react';

/**
 * 搜索语法帮助（FR-136）：点击问号按钮展开浮层，说明可搜字段（文件名/显示名/相机/镜头/备注）
 * 与支持的表达式 token（ext: / type: / size: / camera: / lens:）。
 * 用条件渲染的 Paper 而非 Mantine Popover，规避 jsdom 下 floating-ui 依赖 getComputedStyle 的问题
 * （同 ScanTaskIndicator 的做法）。
 */
export default function SearchSyntaxHelp() {
  const [open, setOpen] = useState(false);
  return (
    <Box style={{ position: 'relative', flexShrink: 0 }}>
      <Tooltip label="搜索语法帮助">
        <ActionIcon
          variant="default"
          size="lg"
          aria-label="搜索语法帮助"
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
        >
          <IconHelp size={18} />
        </ActionIcon>
      </Tooltip>
      {open && (
        <Paper
          withBorder
          shadow="md"
          radius="sm"
          p="sm"
          role="dialog"
          aria-label="搜索语法说明"
          style={{
            position: 'absolute',
            top: '100%',
            right: 0,
            marginTop: 4,
            width: 300,
            zIndex: 200,
          }}
        >
          <Stack gap={6}>
            <Text size="xs" fw={600}>
              可搜字段
            </Text>
            <Text size="xs" c="dimmed">
              裸词同时匹配 文件名、显示名、相机、镜头、备注。
            </Text>
            <Text size="xs" fw={600} mt={4}>
              表达式 token
            </Text>
            <Text size="xs" c="dimmed">
              ext:jpg,png 按扩展名
            </Text>
            <Text size="xs" c="dimmed">
              type:image / type:video 按类型
            </Text>
            <Text size="xs" c="dimmed">
              size:&gt;10mb / size:&lt;=1gb 按大小
            </Text>
            <Text size="xs" c="dimmed">
              camera:Canon 按相机
            </Text>
            <Text size="xs" c="dimmed">
              lens:50mm 按镜头
            </Text>
          </Stack>
        </Paper>
      )}
    </Box>
  );
}
