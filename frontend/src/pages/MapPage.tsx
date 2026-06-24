import { useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { Stack, Title, Box, Group, Switch, Skeleton } from '@mantine/core'
import { IconMapPin, IconAlertTriangle } from '@tabler/icons-react'
import { MapContainer, TileLayer, Marker, Popup, Polyline } from 'react-leaflet'
import L from 'leaflet'
import 'leaflet/dist/leaflet.css'
import markerIcon from 'leaflet/dist/images/marker-icon.png'
import markerIcon2x from 'leaflet/dist/images/marker-icon-2x.png'
import markerShadow from 'leaflet/dist/images/marker-shadow.png'
import * as libApi from '@/api/library'
import { mediaDisplayName } from '@/utils/media'
import { buildDayTracks } from '@/utils/gpsTrack'
import EmptyState from '@/components/EmptyState'
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
  // 重载触发器（FR-98）：加载失败时点「重试」自增以重新拉取
  const [reloadKey, setReloadKey] = useState(0)
  // 站内地图定位入口（FR-106）：灯箱 GPS「在站内地图打开」跳本页带 ?lat=&lon=，
  // 合法时把初始视图定位到该坐标并放大；非法或缺省走默认全局视图。
  const [searchParams] = useSearchParams()
  const focus = useMemo(() => {
    const lat = Number(searchParams.get('lat'))
    const lon = Number(searchParams.get('lon'))
    const valid = searchParams.has('lat') && searchParams.has('lon')
      && Number.isFinite(lat) && Number.isFinite(lon)
      && Math.abs(lat) <= 90 && Math.abs(lon) <= 180
    return valid ? { lat, lon } : null
  }, [searchParams])

  useEffect(() => {
    let active = true
    setLoading(true)
    setError('')
    fetchAllGeotagged()
      .then((m) => { if (active) setMedia(m) })
      .catch(() => { if (active) setError('加载照片地图失败') })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [reloadKey])

  // 按天聚合的旅程轨迹（FR-76）：仅在媒体变化时重算
  const tracks = useMemo(() => buildDayTracks(media), [media])

  return (
    <Stack gap="md">
      <Group justify="space-between" align="center">
        <Title order={2}>照片地图</Title>
        {!loading && !error && media.length > 0 && (
          <Switch
            label="轨迹模式"
            checked={showTracks}
            onChange={(e) => setShowTracks(e.currentTarget.checked)}
          />
        )}
      </Group>
      {loading ? (
        // 首屏加载骨架屏（FR-98）：替代地图区空白闪现
        <Skeleton height="70vh" radius="md" />
      ) : error ? (
        // 加载失败态（FR-98）：插画 + 重试
        <EmptyState
          icon={<IconAlertTriangle size={72} stroke={1.2} style={{ color: 'var(--mantine-color-red-5)', opacity: 0.8 }} />}
          title="加载照片地图失败"
          description="可能是网络或服务暂时不可用，请稍后重试。"
          action={{ label: '重试', onClick: () => setReloadKey((k) => k + 1) }}
        />
      ) : media.length === 0 ? (
        // 无带 GPS 照片空态（FR-98）
        <EmptyState
          icon={<IconMapPin size={72} stroke={1.2} style={{ color: 'var(--mantine-color-dimmed)', opacity: 0.7 }} />}
          title="暂无带 GPS 定位的照片"
          description="扫描含 GPS EXIF 的照片后，它们的拍摄位置将在此地图上显示。"
        />
      ) : (
        <Box style={{ height: '70vh', borderRadius: 8, overflow: 'hidden' }}>
          <MapContainer
            center={focus ? [focus.lat, focus.lon] : [media[0].gps_lat ?? 0, media[0].gps_lon ?? 0]}
            zoom={focus ? 14 : 4}
            style={{ height: '100%', width: '100%' }}
            scrollWheelZoom
          >
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
