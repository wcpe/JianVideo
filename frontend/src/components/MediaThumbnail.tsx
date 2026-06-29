import { useState, useEffect, useRef, useCallback } from 'react';
import { Box, Skeleton, Center } from '@mantine/core';
import { IconPhotoOff } from '@tabler/icons-react';

interface MediaThumbnailProps {
  mediaID: number;
  fileName: string;
  // 容器纵横比（CSS aspect-ratio 值），默认 16/9 与历史一致；详情页等可传 '1/1'。
  aspectRatio?: string;
  // 图片填充方式（FR-99）：默认 contain 不裁切；网格卡可传 cover 让缩略图铺满更饱满。
  objectFit?: 'contain' | 'cover';
  // 叠层内容（FR-99）：渲染在缩略图之上（角标 / 播放叠层 / 信息层等），加载/降级态亦保留。
  overlay?: React.ReactNode;
}

// 受支持的多尺寸缩略图宽度（与后端 thumbnailSizes 白名单一致，FR-81 P12）。
const THUMB_SIZES = [160, 320, 640];
// 命中 202「生成中」时的轮询重试上限与间隔（毫秒）。
const MAX_RETRIES = 8;
const RETRY_INTERVAL_MS = 1500;

// thumbnailURL 按尺寸拼接缩略图请求地址。
function thumbnailURL(mediaID: number, size: number): string {
  return `/api/library/thumbnail/${mediaID}?size=${size}`;
}

export default function MediaThumbnail({
  mediaID,
  fileName,
  aspectRatio = '16/9',
  objectFit = 'contain',
  overlay,
}: MediaThumbnailProps) {
  // 状态机：loading 显骨架、loaded 显图、error 显降级占位。
  const [status, setStatus] = useState<'loading' | 'loaded' | 'error'>('loading');
  // reloadKey 自增以强制 <img> 重新发起请求（202 轮询时用）。
  const [reloadKey, setReloadKey] = useState(0);
  const retriesRef = useRef(0);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // mediaID 变化时重置状态，避免沿用上一张图的加载结果。
  useEffect(() => {
    setStatus('loading');
    setReloadKey(0);
    retriesRef.current = 0;
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, [mediaID]);

  const handleLoad = useCallback(() => setStatus('loaded'), []);

  // 加载失败时探测服务端：202 表示生成中，短间隔重试；其余错误显降级占位。
  const handleError = useCallback(async () => {
    if (retriesRef.current >= MAX_RETRIES) {
      setStatus('error');
      return;
    }
    try {
      const resp = await fetch(thumbnailURL(mediaID, 320), { method: 'GET' });
      if (resp.status === 202) {
        retriesRef.current += 1;
        timerRef.current = setTimeout(() => setReloadKey((k) => k + 1), RETRY_INTERVAL_MS);
        return;
      }
    } catch {
      // 网络异常按失败兜底
    }
    setStatus('error');
  }, [mediaID]);

  return (
    <Box
      style={{
        aspectRatio,
        background: 'var(--mantine-color-default-hover)',
        borderRadius: 4,
        overflow: 'hidden',
        position: 'relative',
      }}
    >
      {status === 'error' && (
        <Center h="100%" c="dimmed">
          <IconPhotoOff size={28} aria-label={`${fileName} 缩略图不可用`} />
        </Center>
      )}
      {status !== 'error' && (
        <img
          key={reloadKey}
          src={thumbnailURL(mediaID, 320)}
          srcSet={THUMB_SIZES.map((s) => `${thumbnailURL(mediaID, s)} ${s}w`).join(', ')}
          sizes="(max-width: 600px) 50vw, (max-width: 1200px) 25vw, 320px"
          alt={fileName}
          loading="lazy"
          onLoad={handleLoad}
          onError={handleError}
          style={{
            width: '100%',
            height: '100%',
            objectFit,
          }}
        />
      )}
      {/* 加载中以骨架覆盖在图片之上，图片始终在 DOM 中（可被角色查询命中且可无障碍访问） */}
      {status === 'loading' && (
        <Skeleton
          height="100%"
          width="100%"
          radius={4}
          style={{ position: 'absolute', inset: 0 }}
        />
      )}
      {/* 叠层内容（FR-99）：角标 / 播放叠层 / 底部信息层等，叠在缩略图之上 */}
      {overlay}
    </Box>
  );
}
