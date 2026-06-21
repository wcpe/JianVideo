import { useState, useEffect } from 'react'
import { Box, Loader } from '@mantine/core'

interface MediaThumbnailProps {
  mediaID: number
  fileName: string
}

export default function MediaThumbnail({ mediaID, fileName }: MediaThumbnailProps) {
  const [src, setSrc] = useState('')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    setSrc(`/api/library/thumbnail/${mediaID}`)
    setLoading(false)
  }, [mediaID])

  return (
    <Box style={{ aspectRatio: '16/9', background: 'var(--mantine-color-default-hover)', borderRadius: 4, overflow: 'hidden', position: 'relative' }}>
      {loading && <Loader size="xs" />}
      {src && (
        <img src={src} alt={fileName} loading="lazy" style={{ width: '100%', height: '100%', objectFit: 'cover' }} />
      )}
    </Box>
  )
}
