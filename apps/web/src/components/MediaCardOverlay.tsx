import { Box, Group, Text, Badge, Center } from '@mantine/core';
import { IconPlayerPlayFilled, IconCheck } from '@tabler/icons-react';
import { formatSize, formatDuration } from '@/utils/format';
import { mediaDisplayName } from '@/utils/media';
import type { MediaFile } from '@/types';

interface MediaCardOverlayProps {
  file: MediaFile;
  // 是否图片（由调用方按 isImageFile 判定，避免本组件重复持有后缀白名单）
  isImage: boolean;
  // 是否多选选中
  selected: boolean;
  // 复选框模式（仅影响是否给左上角让位，本组件不渲染复选框本体）
  checkboxMode: boolean;
}

/**
 * 媒体卡叠层（FR-99）：叠在缩略图之上的纯展示层。
 * - 底部渐变信息层：单行文件名 + 类型 + 大小（视频再加时长），提升信息密度。
 * - 视频卡：右下角时长角标 + 中心淡播放三角，强化「视频感」；图片卡均不渲染。
 * - 选中态：品牌紫勾选叠层（强反馈）。
 * 不持有交互逻辑，悬停浮层与复选框由卡片容器渲染。
 */
export default function MediaCardOverlay({ file, isImage, selected }: MediaCardOverlayProps) {
  const isVideo = !isImage;
  const duration = formatDuration(file.duration);
  const hasDuration = isVideo && duration !== '-';
  const name = mediaDisplayName(file);

  return (
    <>
      {/* 视频中心播放三角：淡白圆底 + 紫填充三角，弱化但可辨 */}
      {isVideo && (
        <Center
          aria-label="视频"
          style={{
            position: 'absolute',
            inset: 0,
            pointerEvents: 'none',
            zIndex: 1,
          }}
        >
          <Box
            style={{
              width: 44,
              height: 44,
              borderRadius: '50%',
              background: 'rgba(0, 0, 0, 0.45)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              backdropFilter: 'blur(2px)',
            }}
          >
            <IconPlayerPlayFilled size={20} color="#fff" style={{ marginLeft: 2 }} />
          </Box>
        </Center>
      )}

      {/* 视频时长角标：右下角半透明黑底 */}
      {hasDuration && (
        <Box
          style={{
            position: 'absolute',
            right: 6,
            bottom: 6,
            zIndex: 3,
            padding: '1px 6px',
            borderRadius: 4,
            background: 'rgba(0, 0, 0, 0.7)',
            fontSize: 11,
            lineHeight: 1.4,
            color: '#fff',
            fontVariantNumeric: 'tabular-nums',
            pointerEvents: 'none',
          }}
        >
          {duration}
        </Box>
      )}

      {/* 底部渐变信息层：单行文件名 + 类型 + 大小，提升信息密度 */}
      <Box
        style={{
          position: 'absolute',
          left: 0,
          right: 0,
          bottom: 0,
          zIndex: 2,
          padding: '14px 8px 6px',
          background:
            'linear-gradient(to top, rgba(0,0,0,0.78) 0%, rgba(0,0,0,0.45) 45%, rgba(0,0,0,0) 100%)',
          pointerEvents: 'none',
        }}
      >
        <Text
          size="xs"
          fw={600}
          truncate
          c="#fff"
          title={name}
          style={{ textShadow: '0 1px 2px rgba(0,0,0,0.6)' }}
        >
          {name}
        </Text>
        <Group gap={6} wrap="nowrap" mt={2}>
          <Badge size="xs" variant="filled" color="purple" radius="sm">
            {file.format.toUpperCase()}
          </Badge>
          <Text
            size="10px"
            c="rgba(255,255,255,0.85)"
            style={{ textShadow: '0 1px 2px rgba(0,0,0,0.6)' }}
          >
            {formatSize(file.file_size)}
          </Text>
        </Group>
      </Box>

      {/* 选中态强反馈：品牌紫半透明罩 + 右上勾选徽标 */}
      {selected && (
        <Box
          aria-label="已选中"
          style={{
            position: 'absolute',
            inset: 0,
            zIndex: 4,
            pointerEvents: 'none',
            background: 'var(--mantine-color-purple-light)',
            boxShadow: 'inset 0 0 0 3px var(--mantine-color-purple-6)',
            borderRadius: 4,
          }}
        >
          <Box
            style={{
              position: 'absolute',
              top: 6,
              right: 6,
              width: 22,
              height: 22,
              borderRadius: '50%',
              background: 'var(--mantine-color-purple-6)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
            }}
          >
            <IconCheck size={14} color="#fff" stroke={3} />
          </Box>
        </Box>
      )}
    </>
  );
}
