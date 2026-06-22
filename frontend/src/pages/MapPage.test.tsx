import { render, screen, waitFor } from '@testing-library/react'
import { MantineProvider } from '@mantine/core'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import type { MediaFile, MediaListResponse } from '@/types'

// 桩掉 react-leaflet（jsdom 无法渲染真实地图）：组件降级为可断言的 div
vi.mock('react-leaflet', () => ({
  MapContainer: ({ children }: { children?: React.ReactNode }) => <div data-testid="map">{children}</div>,
  TileLayer: () => <div data-testid="tile" />,
  Marker: ({ children }: { children?: React.ReactNode }) => <div data-testid="marker">{children}</div>,
  Popup: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
}))
// 桩 leaflet：仅需 L.Icon.Default.mergeOptions 存在（模块加载期调用）
vi.mock('leaflet', () => ({ default: { Icon: { Default: { mergeOptions: vi.fn() } } } }))
// 桩 leaflet 资源导入
vi.mock('leaflet/dist/leaflet.css', () => ({}))
vi.mock('leaflet/dist/images/marker-icon.png', () => ({ default: 'icon.png' }))
vi.mock('leaflet/dist/images/marker-icon-2x.png', () => ({ default: 'icon-2x.png' }))
vi.mock('leaflet/dist/images/marker-shadow.png', () => ({ default: 'shadow.png' }))

const mockGetMediaFiles = vi.fn<() => Promise<MediaListResponse>>()
vi.mock('@/api/library', () => ({ getMediaFiles: () => mockGetMediaFiles() }))

import MapPage from './MapPage'

function geo(id: number, lat: number, lon: number): MediaFile {
  return {
    id, library_id: 1, file_path: `D:/p/${id}.jpg`, file_name: `${id}.jpg`, file_size: 100,
    format: 'jpg', video_codec: '', audio_codec: '', duration: 0, width: 0, height: 0,
    bitrate: 0, subtitle_tracks: '', added_at: '', modified_at: '', gps_lat: lat, gps_lon: lon,
  } as MediaFile
}

function renderPage() {
  return render(<MantineProvider><MapPage /></MantineProvider>)
}

describe('MapPage 照片地图（FR-39）', () => {
  beforeEach(() => vi.clearAllMocks())

  it('每个带 GPS 的照片渲染一个标记', async () => {
    mockGetMediaFiles.mockResolvedValue({
      items: [geo(1, 31.2, 121.4), geo(2, 39.9, 116.4)], total: 2, page: 1, page_size: 100,
    })
    renderPage()
    await waitFor(() => expect(screen.getByTestId('map')).toBeInTheDocument())
    expect(screen.getAllByTestId('marker')).toHaveLength(2)
  })

  it('无 GPS 照片时显示空状态而非地图', async () => {
    mockGetMediaFiles.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 100 })
    renderPage()
    expect(await screen.findByText(/暂无带 GPS 定位的照片/)).toBeInTheDocument()
    expect(screen.queryByTestId('map')).not.toBeInTheDocument()
  })
})
