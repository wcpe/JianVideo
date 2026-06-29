import { Group, Box, Anchor, Text, TextInput, ScrollArea } from '@mantine/core';
import { IconSearch, IconChevronRight, IconDeviceDesktop } from '@tabler/icons-react';
import type { BreadcrumbItem } from '@/types';

interface DirectoryAddressBarProps {
  /** 地址段（消费后端 breadcrumbs），点击跳该层 */
  breadcrumbs: BreadcrumbItem[];
  /** 点击地址段跳转 */
  onNavigate: (path: string) => void;
  /** 当前目录搜索框值（FR-36） */
  searchValue: string;
  onSearchChange: (v: string) => void;
}

/**
 * 资源管理器地址栏（FR-121）：左侧可点路径段（取代页内目录树面包屑），右侧当前目录搜索框（复用 FR-36）。
 * 路径段窄屏可横滚（FR-86 响应式惯例）；点击任一段跳到该层。
 */
export default function DirectoryAddressBar({
  breadcrumbs,
  onNavigate,
  searchValue,
  onSearchChange,
}: DirectoryAddressBarProps) {
  return (
    <Group gap="xs" wrap="nowrap" align="center">
      {/* 地址段：横向可滚，避免深路径挤压搜索框 */}
      <Box
        style={{
          flex: 1,
          minWidth: 0,
          border: '1px solid var(--mantine-color-default-border)',
          borderRadius: 'var(--mantine-radius-sm)',
          background: 'var(--mantine-color-default)',
          padding: '4px 8px',
        }}
      >
        <ScrollArea type="never" scrollbarSize={6} aria-label="地址栏">
          <Group gap={2} wrap="nowrap" align="center" style={{ minWidth: 'max-content' }}>
            <IconDeviceDesktop
              size={14}
              style={{ color: 'var(--mantine-color-dimmed)', flexShrink: 0 }}
            />
            {breadcrumbs.map((seg, index) => {
              const isLast = index === breadcrumbs.length - 1;
              return (
                <Group key={seg.path} gap={2} wrap="nowrap" align="center">
                  <IconChevronRight
                    size={12}
                    style={{ color: 'var(--mantine-color-dimmed)', flexShrink: 0 }}
                  />
                  {isLast ? (
                    <Text size="sm" fw={600} style={{ whiteSpace: 'nowrap' }}>
                      {seg.name}
                    </Text>
                  ) : (
                    <Anchor
                      component="button"
                      type="button"
                      size="sm"
                      underline="hover"
                      onClick={() => onNavigate(seg.path)}
                      style={{ whiteSpace: 'nowrap' }}
                    >
                      {seg.name}
                    </Anchor>
                  )}
                </Group>
              );
            })}
          </Group>
        </ScrollArea>
      </Box>

      {/* 当前目录搜索框（FR-36）：按当前目录递归筛选 */}
      <TextInput
        aria-label="在当前目录下搜索"
        placeholder="在当前目录下搜索：文件名，或 ext:jpg type:image size:>10mb"
        leftSection={<IconSearch size={14} />}
        value={searchValue}
        onChange={(e) => onSearchChange(e.currentTarget.value)}
        size="sm"
        style={{ flexShrink: 0, width: 'min(360px, 42vw)' }}
      />
    </Group>
  );
}
