import { useEffect, useMemo, useState } from 'react'
import { Stack, Title, Text, Loader, Center, Box, Group, Switch } from '@mantine/core'
import { MapContainer, TileLayer, Marker, Popup, Polyline } from 'react-leaflet'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import markerIcon from 'leaflet/dist/images/marker-icon.png'
import markerIcon2x from 'leaflet/dist/images/marker-icon-2x.png'
import markerShadow from 'leaflet/dist/images/marker-shadow.png'
import * as libApi from '@/api/library'
import { mediaDisplayName } from '@/utils/media'
import { buildDayTracks } from '@/utils/gpsTrack'
import type { MediaFile } from '@/types'

// 修复打包后 leaflet 默认 marker 图标路径丢失（经 Vite 资源 URL 重新指向）
L.Icon.Default.mergeOptions({ iconUrl: markerIcon, iconRetinaUrl: markerIcon2x, shadowUrl: markerShadow })

// 拉取全部带 GPS 的媒体（分页累积，仅地理标记子集，通常有界）。
// 按媒体时间升序（FR-76），便于后续按天连成时间序轨迹。
async function fetchAllGeotagged(): Promise<MediaFile[]> {
  const all: MediaFile[] = []
  let page = 1
  for (;;) {
    const res = await libApi.getMediaFiles({ has_gps: true, sort: 'media_time_asc', page, page_size: 100 })
    all.push(...res.items)
    if (res.items.length === 0 || all.length >= res.total) break
    page++
  }
  return all
}

/** 照片地图视图（FR-39）：基于 EXIF GPS 在 OSM 在线瓦片上展示照片地理分布；叠加旅程轨迹（FR-76） */
export default function MapPage() {
  const [media, setMedia] = useState<MediaFile[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  // 轨迹模式（FR-76）：开=散点 + 按天折线轨迹（默认开），关=仅散点
  const [showTracks, setShowTracks] = useState(true)

  useEffect(() => {
    let active = true
    fetchAllGeotagged()
      .then((m) => { if (active) setMedia(m) })
      .catch(() => { if (active) setError('加载照片地图失败') })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [])

  // 按天聚合的旅程轨迹（FR-76）：仅在媒体变化时重算
  const tracks = useMemo(() => buildDayTracks(media), [media])

  if (loading) return <Center h="60vh"><Loader color="purple" /></Center>

  return (
    <Stack gap="md">
      <Group justify="space-between" align="center">
        <Title order={2}>照片地图</Title>
        {media.length > 0 && (
          <Switch
            label="轨迹模式"
            checked={showTracks}
            onChange={(e) => setShowTracks(e.currentTarget.checked)}
          />
        )}
      </Group>
      {error && <Text c="red">{error}</Text>}
      {media.length === 0 ? (
        <Text c="dimmed">暂无带 GPS 定位的照片。扫描含 GPS EXIF 的照片后将在此显示。</Text>
      ) : (
        <Box style={{ height: '70vh', borderRadius: 8, overflow: 'hidden' }}>
          <MapContainer center={[media[0].gps_lat ?? 0, media[0].gps_lon ?? 0]} zoom={4} style={{ height: '100%', width: '100%' }} scrollWheelZoom>
            <TileLayer
              attribution='&copy; OpenStreetMap contributors'
              url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png"
            />
            {showTracks && tracks.map((t) => (
              <Polyline key={t.date} positions={t.positions} pathOptions={{ color: t.color, weight: 3, opacity: 0.8 }} />
            ))}
            {media.map((m) => (
              <Marker key={m.id} position={[m.gps_lat ?? 0, m.gps_lon ?? 0]}>
                <Popup>
                  <div style={{ textAlign: 'center' }}>
                    <img src={`/api/library/thumbnail/${m.id}`} alt={m.file_name} style={{ width: 120, height: 'auto', borderRadius: 4 }} />
                    <div style={{ marginTop: 4, fontSize: 12 }}>{mediaDisplayName(m)}</div>
                  </div>
                </Popup>
              </Marker>
            ))}
          </MapContainer>
        </Box>
      )}
    </Stack>
  )
}
