import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MantineProvider } from '@mantine/core'
import { MemoryRouter } from 'react-router-dom'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import type { MediaFile, MediaListResponse } from '@/types'

// 桩掉 react-leaflet（jsdom 无法渲染真实地图）：组件降级为可断言的 div，
// 把初始 center/zoom 透出到 data 属性，便于断言站内地图定位（FR-106）。
vi.mock('react-leaflet', () => ({
  MapContainer: ({ children, center, zoom }: { children?: React.ReactNode; center?: [number, number]; zoom?: number }) => (
    <div data-testid="map" data-center={center ? center.join(',') : ''} data-zoom={zoom}>{children}</div>
  ),
  TileLayer: () => <div data-testid="tile" />,
  Marker: ({ children }: { children?: React.ReactNode }) => <div data-testid="marker">{children}</div>,
  Popup: ({ children }: { children?: React.ReactNode }) => <div>{children}</div>,
  // FR-76 轨迹折线桩：把点数渲染进 data 属性便于断言
  Polyline: ({ positions }: { positions?: [number, number][] }) => (
    <div data-testid="polyline" data-points={positions?.length ?? 0} />
  ),
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

function geo(id: number, lat: number, lon: number, mediaTime = ''): MediaFile {
  return {
    id, library_id: 1, file_path: `D:/p/${id}.jpg`, file_name: `${id}.jpg`, file_size: 100,
    format: 'jpg', video_codec: '', audio_codec: '', duration: 0, width: 0, height: 0,
    bitrate: 0, subtitle_tracks: '', added_at: mediaTime, modified_at: mediaTime,
    media_time: mediaTime, gps_lat: lat, gps_lon: lon,
  } as MediaFile
}

function renderPage(initialEntry = '/map') {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={[initialEntry]}>
        <MapPage />
      </MemoryRouter>
    </MantineProvider>,
  )
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
    const { container } = renderPage()
    expect(await screen.findByText(/暂无带 GPS 定位的照片/)).toBeInTheDocument()
    expect(screen.queryByTestId('map')).not.toBeInTheDocument()
    // 接入统一 EmptyState（FR-98）：空态容器存在
    expect(container.querySelector('[data-testid="empty-state"]')).toBeInTheDocument()
  })

  it('首屏加载时渲染骨架屏（FR-98）', () => {
    // 返回永不结算的 Promise，保持 loading 态
    mockGetMediaFiles.mockReturnValue(new Promise<MediaListResponse>(() => {}))
    const { container } = renderPage()
    // Mantine Skeleton 渲染为 mantine-Skeleton-root
    expect(container.querySelector('.mantine-Skeleton-root')).toBeInTheDocument()
    expect(screen.queryByTestId('map')).not.toBeInTheDocument()
  })

  it('加载失败时显示插画+重试空态（FR-98）', async () => {
    mockGetMediaFiles.mockRejectedValue(new Error('boom'))
    renderPage()
    expect(await screen.findByText('加载照片地图失败')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument()
  })

  it('带 ?lat=&lon= 时把地图初始视图定位到该坐标并放大（FR-106）', async () => {
    mockGetMediaFiles.mockResolvedValue({
      items: [geo(1, 31.2, 121.4), geo(2, 39.9, 116.4)], total: 2, page: 1, page_size: 100,
    })
    renderPage('/map?lat=39.9&lon=116.4')
    const map = await screen.findByTestId('map')
    expect(map).toHaveAttribute('data-center', '39.9,116.4')
    expect(map).toHaveAttribute('data-zoom', '14')
  })

  it('无 lat/lon 参数时用默认全局视图（FR-106）', async () => {
    mockGetMediaFiles.mockResolvedValue({
      items: [geo(1, 31.2, 121.4)], total: 1, page: 1, page_size: 100,
    })
    renderPage('/map')
    const map = await screen.findByTestId('map')
    expect(map).toHaveAttribute('data-center', '31.2,121.4')
    expect(map).toHaveAttribute('data-zoom', '4')
  })
})

describe('MapPage 旅程轨迹（FR-76）', () => {
  beforeEach(() => vi.clearAllMocks())

  it('同一天≥2 个 GPS 点时默认渲染一条轨迹折线，点数与当天点数一致', async () => {
    mockGetMediaFiles.mockResolvedValue({
      items: [
        geo(1, 31.0, 121.0, '2025-01-01T08:00:00Z'),
        geo(2, 31.1, 121.1, '2025-01-01T12:00:00Z'),
      ],
      total: 2, page: 1, page_size: 100,
    })
    renderPage()
    await waitFor(() => expect(screen.getByTestId('map')).toBeInTheDocument())
    const lines = screen.getAllByTestId('polyline')
    expect(lines).toHaveLength(1)
    expect(lines[0]).toHaveAttribute('data-points', '2')
  })

  it('多天各≥2 点时每天一条折线', async () => {
    mockGetMediaFiles.mockResolvedValue({
      items: [
        geo(1, 31.0, 121.0, '2025-01-01T08:00:00Z'),
        geo(2, 31.1, 121.1, '2025-01-01T12:00:00Z'),
        geo(3, 39.9, 116.4, '2025-01-02T09:00:00Z'),
        geo(4, 39.8, 116.3, '2025-01-02T18:00:00Z'),
      ],
      total: 4, page: 1, page_size: 100,
    })
    renderPage()
    await waitFor(() => expect(screen.getByTestId('map')).toBeInTheDocument())
    expect(screen.getAllByTestId('polyline')).toHaveLength(2)
  })

  it('当天仅 1 个 GPS 点时不渲染折线（仅散点）', async () => {
    mockGetMediaFiles.mockResolvedValue({
      items: [
        geo(1, 31.0, 121.0, '2025-01-01T08:00:00Z'),
        geo(2, 39.9, 116.4, '2025-01-02T09:00:00Z'),
      ],
      total: 2, page: 1, page_size: 100,
    })
    renderPage()
    await waitFor(() => expect(screen.getByTestId('map')).toBeInTheDocument())
    expect(screen.getAllByTestId('marker')).toHaveLength(2)
    expect(screen.queryByTestId('polyline')).not.toBeInTheDocument()
  })

  it('关闭「轨迹模式」开关后仅散点、不再渲染折线', async () => {
    mockGetMediaFiles.mockResolvedValue({
      items: [
        geo(1, 31.0, 121.0, '2025-01-01T08:00:00Z'),
        geo(2, 31.1, 121.1, '2025-01-01T12:00:00Z'),
      ],
      total: 2, page: 1, page_size: 100,
    })
    renderPage()
    await waitFor(() => expect(screen.getByTestId('map')).toBeInTheDocument())
    expect(screen.getByTestId('polyline')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('switch', { name: /轨迹模式/ }))
    expect(screen.queryByTestId('polyline')).not.toBeInTheDocument()
    // 散点 Marker 仍在
    expect(screen.getAllByTestId('marker')).toHaveLength(2)
  })
})
