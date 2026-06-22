import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse } from 'msw'
import SystemPage from './SystemPage'
import { server } from '@/mocks/beforeAll'

// 复制功能依赖 navigator.clipboard，jsdom 默认缺失，这里注入可断言的 mock
const writeText = vi.fn().mockResolvedValue(undefined)

function renderPage() {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={['/system']}>
        <SystemPage />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('SystemPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
      writable: true,
    })
  })

  it('渲染系统信息（含应用版本）', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('0.3.0')).toBeVisible()
    })
    expect(screen.getByText('系统诊断')).toBeVisible()
    expect(screen.getByText('go1.22.5')).toBeVisible()
  })

  it('渲染硬件加速卡片的 per-codec 能力与可输出编码', async () => {
    renderPage()
    await screen.findByText('0.3.0')

    // 家族显示名
    expect(await screen.findByText('AMD AMF')).toBeVisible()
    // per-codec：逐编码列出编码器名（h264_amf 同时出现在「首选编码器」，故用 getAllByText）
    expect(screen.getAllByText('h264_amf').length).toBeGreaterThan(0)
    expect(screen.getByText('hevc_amf')).toBeVisible()
    expect(screen.getByText('av1_amf')).toBeVisible()
    // 系统可输出编码并集以 Badge 展示（h264/h265）
    expect(screen.getByText('可输出编码')).toBeVisible()
    // av1 试编码失败 → 失败标记存在
    expect(screen.getAllByText('✗ 试编码失败').length).toBeGreaterThan(0)
  })

  it('点击「测试编解码器」后渲染 per-codec 结果行并显示缓存来源', async () => {
    const user = userEvent.setup()
    renderPage()

    // 等系统信息加载完成
    await screen.findByText('0.3.0')

    await user.click(screen.getByRole('button', { name: '测试编解码器' }))

    // libx264 成功行出现（结果表内 Code 渲染）
    await waitFor(() => {
      expect(screen.getAllByText('libx264').length).toBeGreaterThan(0)
    })
    // AMF 编码器结果行也应渲染
    expect(screen.getAllByText('av1_amf').length).toBeGreaterThan(0)
    // from_cache 提示：结果来自缓存
    expect(screen.getByText(/结果来自缓存，实测于/)).toBeVisible()
  })

  it('提供「重新测试」按钮强制重跑（force）', async () => {
    const user = userEvent.setup()
    let forceParam: string | null = null
    server.use(
      http.post('*/api/system/codec-test', ({ request }) => {
        forceParam = new URL(request.url).searchParams.get('force')
        return HttpResponse.json({
          ffmpeg_available: true,
          results: [
            { encoder: 'libx264', family: 'software', codec: 'h264', compiled: true, tested_ok: true, detail: '' },
          ],
          from_cache: false,
          ffmpeg_version: 'ffmpeg version 6.1.1',
          tested_at: '2026-06-23T11:00:00Z',
        })
      }),
    )

    renderPage()
    await screen.findByText('0.3.0')

    const rerunBtn = screen.getByRole('button', { name: '重新测试' })
    expect(rerunBtn).toBeVisible()
    await user.click(rerunBtn)

    // 强制重跑应带 force=true，且结果来源标为实测（非缓存）
    await waitFor(() => {
      expect(screen.getByText(/实测于 2026-06-23T11:00:00Z/)).toBeVisible()
    })
    expect(forceParam).toBe('true')
  })

  it('ffmpeg 不可用时提示无法测试', async () => {
    server.use(
      http.post('*/api/system/codec-test', () =>
        HttpResponse.json({ ffmpeg_available: false, results: [], from_cache: false, ffmpeg_version: '', tested_at: '' }),
      ),
    )

    const user = userEvent.setup()
    renderPage()
    await screen.findByText('0.3.0')

    await user.click(screen.getByRole('button', { name: '测试编解码器' }))

    await waitFor(() => {
      expect(screen.getByText('ffmpeg 不可用，无法测试编解码器。')).toBeVisible()
    })
  })

  it('检查更新后展示最新版本与可更新状态（FR-46）', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('0.3.0') // 系统信息加载完成

    await user.click(screen.getByRole('button', { name: '检查更新' }))

    await waitFor(() => {
      expect(screen.getByText('v0.6.3')).toBeVisible()
    })
    expect(screen.getByText('有可用更新')).toBeVisible()
    // 有更新时「立即更新并重启」按钮可用
    expect(screen.getByRole('button', { name: /立即更新并重启/ })).toBeEnabled()
  })

  it('切换到测试版后检查更新走预发布频道（FR-46）', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('0.3.0')

    // 切到「测试版」频道（Mantine SegmentedControl 点标签文本）
    await user.click(screen.getByText('测试版'))
    await user.click(screen.getByRole('button', { name: '检查更新' }))

    // 预发布频道应返回 dev 内嵌版本 + 预发布标记
    await waitFor(() => {
      expect(screen.getByText('v0.6.3-dev.abc1234')).toBeVisible()
    })
    expect(screen.getByText('预发布')).toBeVisible()
  })

  it('点击「复制结果」后写入剪贴板并显示已复制反馈', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('0.3.0')

    await user.click(screen.getByRole('button', { name: '复制结果' }))

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledTimes(1)
    })
    // 写入内容应包含应用版本
    expect(writeText.mock.calls[0][0]).toContain('0.3.0')
    // 反馈文案切换为「已复制」
    await waitFor(() => {
      expect(screen.getByRole('button', { name: '已复制' })).toBeVisible()
    })
  })
})
