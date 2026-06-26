import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse, delay } from 'msw'
import SystemPage from './SystemPage'
import { server } from '@/mocks/beforeAll'

// 复制功能依赖 navigator.clipboard，jsdom 默认缺失，这里注入可断言的 mock
const writeText = vi.fn().mockResolvedValue(undefined)

// 把 MemoryRouter 的内存路由 search 暴露到 DOM，供断言子 tab 的 URL query 同步（FR-59）
function LocationProbe() {
  const location = useLocation()
  return <div data-testid="location-search">{location.search}</div>
}

// 在指定初始 URL 下渲染系统页（默认进运行环境子 tab，FR-59）
function renderPage(initialPath = '/system') {
  return render(
    <MantineProvider>
      <MemoryRouter initialEntries={[initialPath]}>
        <SystemPage />
        <LocationProbe />
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

  it('渲染系统信息（含应用版本，缺省进运行环境子 tab）', async () => {
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('0.3.0')).toBeVisible()
    })
    expect(screen.getByText('系统诊断')).toBeVisible()
    expect(screen.getByText('go1.22.5')).toBeVisible()
    // 缺省选中「运行环境」子 tab
    expect(screen.getByRole('tab', { name: '运行环境' })).toHaveAttribute('aria-selected', 'true')
  })

  it('运行环境子 tab 展示运行时信息（PID/运行时长/数据库路径等，FR-60）', async () => {
    renderPage()
    await screen.findByText('0.3.0')

    // 缺省即运行环境子 tab，运行时字段可见
    expect(screen.getByText('GOMAXPROCS')).toBeVisible()
    expect(screen.getByText('进程 PID')).toBeVisible()
    expect(screen.getByText('12345')).toBeVisible()
    expect(screen.getByText('运行时长')).toBeVisible()
    // 3661 秒 = 1h 1m 1s
    expect(screen.getByText('1h 1m 1s')).toBeVisible()
    // 数据库路径与运行内存
    expect(screen.getByText('数据库路径')).toBeVisible()
    expect(screen.getByText('/opt/jianvideo/data/jianvideo.db')).toBeVisible()
    expect(screen.getByText('运行内存（已用/申请）')).toBeVisible()
  })

  it('键值为定宽两列：label 定宽、路径/版本值用 monospace（FR-101）', async () => {
    renderPage()
    await screen.findByText('0.3.0')

    // 数据库路径值用等宽字体（monospace token）
    const dbPath = screen.getByText('/opt/jianvideo/data/jianvideo.db')
    expect(dbPath.style.fontFamily).toContain('monospace')
    // 其 label 列定宽（紧凑两列，不再 space-between 拉满）
    const label = screen.getByText('数据库路径')
    expect(label.style.width).toBe('120px')
    expect(label.style.flexShrink).toBe('0')

    // Go 版本（版本号）也用 monospace
    expect(screen.getByText('go1.22.5').style.fontFamily).toContain('monospace')
  })

  it('渲染四个子 tab（运行环境/硬件加速/编解码测试/应用更新，FR-59）', async () => {
    renderPage()
    await screen.findByText('0.3.0')
    for (const name of ['运行环境', '硬件加速', '编解码测试', '应用更新']) {
      expect(screen.getByRole('tab', { name })).toBeInTheDocument()
    }
  })

  it('切换子 tab 后 URL query sys 同步更新（保留外层 tab=system，FR-59）', async () => {
    const user = userEvent.setup()
    renderPage('/system?tab=system')
    await screen.findByText('0.3.0')

    await user.click(screen.getByRole('tab', { name: '应用更新' }))

    await waitFor(() => {
      expect(screen.getByRole('tab', { name: '应用更新' })).toHaveAttribute('aria-selected', 'true')
    })
    // URL query 已同步，且保留外层 tab=system（函数式更新不丢失既有 query）
    expect(screen.getByTestId('location-search')).toHaveTextContent('sys=update')
    expect(screen.getByTestId('location-search')).toHaveTextContent('tab=system')
  })

  it('以 ?sys=update 进入时直接选中应用更新子 tab（FR-59 页眉精确跳转）', async () => {
    renderPage('/system?tab=system&sys=update')

    // 直接选中「应用更新」子 tab，无需手动点击；应用更新区内容可见（检查更新按钮）
    expect(screen.getByRole('tab', { name: '应用更新' })).toHaveAttribute('aria-selected', 'true')
    expect(await screen.findByRole('button', { name: '检查更新' })).toBeVisible()
  })

  it('非法 sys query 落回运行环境子 tab（FR-59）', async () => {
    renderPage('/system?sys=bogus')
    await screen.findByText('0.3.0')
    expect(screen.getByRole('tab', { name: '运行环境' })).toHaveAttribute('aria-selected', 'true')
  })

  it('渲染硬件加速卡片的 per-codec 能力与可输出编码', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('0.3.0')

    // 切到「硬件加速」子 tab
    await user.click(screen.getByRole('tab', { name: '硬件加速' }))

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

    // 切到「编解码测试」子 tab
    await user.click(screen.getByRole('tab', { name: '编解码测试' }))
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

    // 切到「编解码测试」子 tab
    await user.click(screen.getByRole('tab', { name: '编解码测试' }))
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

    // 切到「编解码测试」子 tab
    await user.click(screen.getByRole('tab', { name: '编解码测试' }))
    await user.click(screen.getByRole('button', { name: '测试编解码器' }))

    await waitFor(() => {
      expect(screen.getByText('ffmpeg 不可用，无法测试编解码器。')).toBeVisible()
    })
  })

  it('检查更新后展示最新版本与可更新状态（FR-46）', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('0.3.0') // 系统信息加载完成

    // 切到「应用更新」子 tab
    await user.click(screen.getByRole('tab', { name: '应用更新' }))
    await user.click(screen.getByRole('button', { name: '检查更新' }))

    await waitFor(() => {
      expect(screen.getByText('v0.6.3')).toBeVisible()
    })
    expect(screen.getByText('有可用更新')).toBeVisible()
    // 有更新时「立即更新并重启」按钮可用
    expect(screen.getByRole('button', { name: /立即更新并重启/ })).toBeEnabled()
  })

  it('发布说明经 markdown 渲染出标题/列表/加粗/安全外链（FR-46）', async () => {
    server.use(
      http.get('*/api/system/update/check', () =>
        HttpResponse.json({
          current: '0.3.0',
          latest: 'v0.6.3',
          has_update: true,
          tag: 'v0.6.3',
          prerelease: false,
          channel: 'stable',
          notes: '## 更新要点\n\n- 修复 **超时** 问题\n- 详见 [发布页](https://example.com/release)',
          asset_name: 'jianvideo-linux-amd64',
        }),
      ),
    )

    const user = userEvent.setup()
    renderPage()
    await screen.findByText('0.3.0')

    // 切到「应用更新」子 tab
    await user.click(screen.getByRole('tab', { name: '应用更新' }))
    await user.click(screen.getByRole('button', { name: '检查更新' }))

    // markdown 渲染出标题 / 列表 / 加粗 / 外链（外链安全打开新标签页）
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: '更新要点' })).toBeVisible()
    })
    expect(screen.getByText('超时').tagName).toBe('STRONG')
    expect(screen.getAllByRole('listitem').length).toBeGreaterThan(0)
    const link = screen.getByRole('link', { name: '发布页' })
    expect(link).toHaveAttribute('href', 'https://example.com/release')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'))
  })

  it('检查更新失败时展示友好提示而非裸 timeout 串（FR-46）', async () => {
    server.use(
      http.get('*/api/system/update/check', () =>
        HttpResponse.json(
          { code: 'UPDATE_CHECK_FAILED', message: '检查更新失败：网络不可达或 GitHub 暂时不可用，请稍后重试' },
          { status: 502 },
        ),
      ),
    )

    const user = userEvent.setup()
    renderPage()
    await screen.findByText('0.3.0')

    // 切到「应用更新」子 tab
    await user.click(screen.getByRole('tab', { name: '应用更新' }))
    await user.click(screen.getByRole('button', { name: '检查更新' }))

    await waitFor(() => {
      expect(screen.getByText('更新出错')).toBeVisible()
    })
    expect(screen.getByText(/网络不可达或 GitHub 暂时不可用/)).toBeVisible()
    // 不应出现裸 axios 超时串
    expect(screen.queryByText(/timeout of/i)).toBeNull()
  })

  it('切换到测试版后检查更新走预发布频道（FR-46）', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('0.3.0')

    // 切到「应用更新」子 tab
    await user.click(screen.getByRole('tab', { name: '应用更新' }))
    // 切到「测试版」频道（Mantine SegmentedControl 点标签文本）
    await user.click(screen.getByText('测试版'))
    await user.click(screen.getByRole('button', { name: '检查更新' }))

    // 预发布频道应返回 dev 内嵌版本 + 预发布标记
    await waitFor(() => {
      expect(screen.getByText('v0.6.3-dev.abc1234')).toBeVisible()
    })
    expect(screen.getByText('预发布')).toBeVisible()
  })

  it('「检查更新」默认走缓存（不带 force query，FR-62）', async () => {
    const user = userEvent.setup()
    let forceParam: string | null = 'unset'
    server.use(
      http.get('*/api/system/update/check', ({ request }) => {
        forceParam = new URL(request.url).searchParams.get('force')
        return HttpResponse.json({
          current: '0.3.0', latest: 'v0.6.3', has_update: true, tag: 'v0.6.3',
          prerelease: false, channel: 'stable', notes: '', asset_name: 'jianvideo-linux-amd64',
        })
      }),
    )

    renderPage()
    await screen.findByText('0.3.0')

    await user.click(screen.getByRole('tab', { name: '应用更新' }))
    await user.click(screen.getByRole('button', { name: '检查更新' }))

    await waitFor(() => {
      expect(screen.getByText('v0.6.3')).toBeVisible()
    })
    // 默认走缓存：请求不带 force（缺省 null）
    expect(forceParam).toBeNull()
  })

  it('「获取更新」强制刷新走 force=true（FR-62）', async () => {
    const user = userEvent.setup()
    let forceParam: string | null = null
    server.use(
      http.get('*/api/system/update/check', ({ request }) => {
        forceParam = new URL(request.url).searchParams.get('force')
        return HttpResponse.json({
          current: '0.3.0', latest: 'v0.6.3', has_update: true, tag: 'v0.6.3',
          prerelease: false, channel: 'stable', notes: '', asset_name: 'jianvideo-linux-amd64',
        })
      }),
    )

    renderPage()
    await screen.findByText('0.3.0')

    await user.click(screen.getByRole('tab', { name: '应用更新' }))
    const forceBtn = screen.getByRole('button', { name: '获取更新' })
    expect(forceBtn).toBeVisible()
    await user.click(forceBtn)

    await waitFor(() => {
      expect(screen.getByText('v0.6.3')).toBeVisible()
    })
    // 强制刷新绕缓存：请求带 force=true
    expect(forceParam).toBe('true')
  })

  it('进入应用更新子 tab 即自动展示更新信息（免手动点检查更新）', async () => {
    localStorage.clear()
    // 确定性 handler（不依赖跨测试泄漏的频道设置）：任意频道都回固定版本 + 发布说明
    server.use(
      http.get('*/api/system/update/check', () =>
        HttpResponse.json({
          current: '0.16.0', latest: 'v0.16.9', has_update: true, tag: 'v0.16.9',
          prerelease: false, channel: 'stable', notes: '自动展示的发布说明', asset_name: 'jianvideo-windows-amd64',
        }),
      ),
    )
    // 直接以 ?sys=update 进入应用更新子 tab，不点击任何按钮
    renderPage('/system?tab=system&sys=update')

    // 自动检查（非 force，走后端缓存）后应自动展示最新版本与发布说明，无需手动点
    await waitFor(() => {
      expect(screen.getByText('v0.16.9')).toBeVisible()
    })
    expect(screen.getByText('自动展示的发布说明')).toBeVisible()
    // 不应再停留在「点击检查更新」的引导占位文案
    expect(screen.queryByText(/点击「检查更新」/)).toBeNull()
  })

  it('命中本地缓存时进入即时展示缓存的发布说明（不依赖网络）', async () => {
    localStorage.clear()
    // 预置 stable 频道的本地缓存（含独特发布说明），并让网络长时间不返回
    localStorage.setItem('jianvideo.update.check.stable', JSON.stringify({
      current: '0.16.0', latest: 'v0.16.1', has_update: true, tag: 'v0.16.1',
      prerelease: false, channel: 'stable', notes: '缓存的更新日志要点', asset_name: 'jianvideo-windows-amd64',
    }))
    server.use(
      http.get('*/api/system/update/check', async () => {
        await delay(10000) // 网络迟迟不返回，验证缓存能先行展示
        return HttpResponse.json({ current: '0.16.0', latest: 'v0.16.1', has_update: true, tag: 'v0.16.1', prerelease: false, channel: 'stable', notes: '', asset_name: '' })
      }),
    )

    renderPage('/system?tab=system&sys=update')

    // 缓存在挂载时同步注入，发布说明无需等待网络即可见
    expect(await screen.findByText('缓存的更新日志要点')).toBeVisible()
    expect(screen.getByText('v0.16.1')).toBeVisible()
  })

  it('点「立即更新并重启」弹 Mantine 模态框，确认才触发更新（FR-62）', async () => {
    const user = userEvent.setup()
    let applyCalled = false
    server.use(
      http.post('*/api/system/update/apply', () => {
        applyCalled = true
        return HttpResponse.json({ status: 'updating', message: '更新已应用，服务即将重启' })
      }),
    )

    renderPage()
    await screen.findByText('0.3.0')

    await user.click(screen.getByRole('tab', { name: '应用更新' }))
    await user.click(screen.getByRole('button', { name: '检查更新' }))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /立即更新并重启/ })).toBeEnabled()
    })

    await user.click(screen.getByRole('button', { name: /立即更新并重启/ }))

    // 弹出 Mantine 确认模态框（含确认版本文案），此刻尚未触发更新请求
    const dialog = await screen.findByRole('dialog')
    expect(dialog).toBeVisible()
    expect(applyCalled).toBe(false)

    // 模态框内点「确认」才触发更新
    await user.click(within(dialog).getByRole('button', { name: '确认更新' }))
    await waitFor(() => {
      expect(applyCalled).toBe(true)
    })
  })

  it('更新确认模态框点「取消」不触发更新（FR-62）', async () => {
    const user = userEvent.setup()
    let applyCalled = false
    server.use(
      http.post('*/api/system/update/apply', () => {
        applyCalled = true
        return HttpResponse.json({ status: 'updating', message: '' })
      }),
    )

    renderPage()
    await screen.findByText('0.3.0')

    await user.click(screen.getByRole('tab', { name: '应用更新' }))
    await user.click(screen.getByRole('button', { name: '检查更新' }))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /立即更新并重启/ })).toBeEnabled()
    })

    await user.click(screen.getByRole('button', { name: /立即更新并重启/ }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '取消' }))

    // 模态框关闭、未触发更新请求
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull()
    })
    expect(applyCalled).toBe(false)
  })

  it('点「回滚到上一版」弹模态框，确认触发回滚、取消不触发（FR-62）', async () => {
    const user = userEvent.setup()
    let rollbackCalled = false
    server.use(
      http.post('*/api/system/update/rollback', () => {
        rollbackCalled = true
        return HttpResponse.json({ status: 'rolling_back', message: '' })
      }),
    )

    renderPage()
    await screen.findByText('0.3.0')

    await user.click(screen.getByRole('tab', { name: '应用更新' }))
    // 回滚按钮位于检查结果区，先检查更新使其渲染
    await user.click(screen.getByRole('button', { name: '检查更新' }))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /回滚到上一版/ })).toBeEnabled()
    })
    await user.click(screen.getByRole('button', { name: /回滚到上一版/ }))

    // 先取消：不触发回滚
    let dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '取消' }))
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull()
    })
    expect(rollbackCalled).toBe(false)

    // 再确认：触发回滚
    await user.click(screen.getByRole('button', { name: /回滚到上一版/ }))
    dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '确认回滚' }))
    await waitFor(() => {
      expect(rollbackCalled).toBe(true)
    })
  })

  it('更新进行中轮询进度端点并展示下载进度条与百分比（FR-90）', async () => {
    const user = userEvent.setup()
    server.use(
      // apply 故意挂起一会儿，让进度轮询有机会渲染进度条
      http.post('*/api/system/update/apply', async () => {
        await delay(1200)
        return HttpResponse.json({ status: 'updating', message: '更新已应用，服务即将重启' })
      }),
      http.get('*/api/system/update/progress', () =>
        HttpResponse.json({ state: 'downloading', downloaded: 6 * 1024 * 1024, total: 12 * 1024 * 1024, percent: 50 }),
      ),
    )

    renderPage()
    await screen.findByText('0.3.0')

    await user.click(screen.getByRole('tab', { name: '应用更新' }))
    await user.click(screen.getByRole('button', { name: '检查更新' }))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /立即更新并重启/ })).toBeEnabled()
    })
    await user.click(screen.getByRole('button', { name: /立即更新并重启/ }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '确认更新' }))

    // 轮询进度端点后展示进度条与百分比 + 已下载/总字节
    await waitFor(() => {
      expect(screen.getByText('下载中…')).toBeVisible()
    })
    expect(screen.getByText(/50%/)).toBeVisible()
    // 进度条（Mantine Progress 渲染 role=progressbar）存在且值为 50
    const bar = screen.getByRole('progressbar')
    expect(bar).toHaveAttribute('aria-valuenow', '50')
  })

  it('更新失败后展示「重试」按钮，点击重新触发更新（FR-90）', async () => {
    const user = userEvent.setup()
    let applyCalls = 0
    server.use(
      http.post('*/api/system/update/apply', () => {
        applyCalls++
        // 首次失败、重试成功
        if (applyCalls === 1) {
          return HttpResponse.json({ code: 'UPDATE_FAILED', message: '更新失败：下载中断' }, { status: 500 })
        }
        return HttpResponse.json({ status: 'updating', message: '更新已应用，服务即将重启' })
      }),
    )

    renderPage()
    await screen.findByText('0.3.0')

    await user.click(screen.getByRole('tab', { name: '应用更新' }))
    await user.click(screen.getByRole('button', { name: '检查更新' }))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /立即更新并重启/ })).toBeEnabled()
    })
    await user.click(screen.getByRole('button', { name: /立即更新并重启/ }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '确认更新' }))

    // 失败后出现错误提示与「重试」按钮
    await waitFor(() => {
      expect(screen.getByText('更新出错')).toBeVisible()
    })
    const retryBtn = await screen.findByRole('button', { name: '重试' })
    expect(retryBtn).toBeVisible()

    // 点「重试」直接重新触发 apply（无需再弹模态框）
    await user.click(retryBtn)
    await waitFor(() => {
      expect(applyCalls).toBe(2)
    })
  })

  it('点击「复制结果」后写入剪贴板并显示已复制反馈', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByText('0.3.0')

    // 切到「编解码测试」子 tab（复制结果按钮所在）
    await user.click(screen.getByRole('tab', { name: '编解码测试' }))
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
