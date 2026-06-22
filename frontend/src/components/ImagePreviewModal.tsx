import { Modal, Box, Group, Button } from '@mantine/core'
import { IconDownload } from '@tabler/icons-react'
import type { MediaFile } from '@/types'

interface ImagePreviewModalProps {
  file: MediaFile | null
  onClose: () => void
}

/** 图片预览弹窗组件 */
export default function ImagePreviewModal({ file, onClose }: ImagePreviewModalProps) {
  return (
    <Modal opened={!!file} onClose={onClose} title={file?.file_name} centered size="xl">
      {file && (
        <>
          {/* 下载原文件（FR-42）：附件下载磁盘原始图片 */}
          <Group justify="flex-end" mb="sm">
            <Button
              component="a"
              href={`/api/library/media/${file.id}/download`}
              download
              variant="light"
              size="xs"
              leftSection={<IconDownload size={14} />}
            >
              下载原文件
            </Button>
          </Group>
          <Box component="img" src={`/api/library/media/${file.id}/raw`} alt={file.file_name} style={{ width: '100%', height: 'auto' }} />
        </>
      )}
    </Modal>
  )
}
