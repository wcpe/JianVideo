import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { MantineProvider } from '@mantine/core'
import { http, HttpResponse, delay } from 'msw'
import SystemPage, { type SystemSection } from './SystemPage'
import { server } from '@/mocks/beforeAll'

// 复制功能依赖 navigator.clipboard，jsdom 默认缺失，这里注入可断言的 mock
const writeText = vi.fn().mockResolvedValue(undefined)

// 渲染指定区块（FR-113 一级 tab 后，由 ConsolePage 选定 section，本组件只渲染该区块）
function renderSection(section: SystemSection) {
  return render(
    <MantineProvider>
      <MemoryRouter>
        <SystemPage section={section} />
      </MemoryRouter>
    </MantineProvider>,
  )
}

describe('SystemPage（FR-113 区块化渲染）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
      writable: true,
    })
    // jsdom 无 IntersectionObserver，注入空 stub 供运行环境区块的锚点导航挂载
    vi.stubGlobal('IntersectionObserver', class {
      observe() {}
      unobserve() {}
      disconnect() {}
      takeRecords() { return [] }
    })
  })

  it('运行环境区块渲染系统信息（含应用版本/Go 版本）', async () => {
    renderSection('env')
    await waitFor(() => {
      expect(screen.getByText('0.3.0')).toBeVisible()
    })
    expect(screen.getByText('系统诊断')).toBeVisible()
    expect(screen.getByText('go1.22.5')).toBeVisible()
  })

  it('运行环境区块展示运行时信息（PID/运行时长/数据库路径等，FR-60）', async () => {
    renderSection('env')
    await screen.findByText('0.3.0')

    expect(screen.getByText('GOMAXPROCS')).toBeVisible()
    expect(screen.getByText('进程 PID')).toBeVisible()
    expect(screen.getByText('12345')).toBeVisible()
    expect(screen.getByText('运行时长')).toBeVisible()
    // 3661 秒 = 1h 1m 1s
    expect(screen.getByText('1h 1m 1s')).toBeVisible()
    expect(screen.getByText('数据库路径')).toBeVisible()
    expect(screen.getByText('/opt/jianvideo/data/jianvideo.db')).toBeVisible()
    expect(screen.getByText('运行内存（已用/申请）')).toBeVisible()
  })

  it('运行环境区块提供左侧锚点导航（运行环境/FFmpeg，FR-113）', async () => {
    renderSection('env')
    await screen.findByText('0.3.0')
    const nav = screen.getByRole('navigation', { name: '区块导航' })
    expect(within(nav).getByRole('button', { name: '运行环境' })).toBeInTheDocument()
    expect(within(nav).getByRole('button', { name: 'FFmpeg' })).toBeInTheDocument()
  })

  it('键值为定宽两列：label 定宽、路径/版本值用 monospace（FR-101）', async () => {
    renderSection('env')
    await screen.findByText('0.3.0')

    const dbPath = screen.getByText('/opt/jianvideo/data/jianvideo.db')
    expect(dbPath.style.fontFamily).toContain('monospace')
    const label = screen.getByText('数据库路径')
    expect(label.style.width).toBe('120px')
    expect(label.style.flexShrink).toBe('0')
    expect(screen.getByText('go1.22.5').style.fontFamily).toContain('monospace')
  })

  it('硬件加速区块渲染卡片的 per-codec 能力与可输出编码', async () => {
    renderSection('hwaccel')

    expect(await screen.findByText('AMD AMF')).toBeVisible()
    // per-codec：逐编码列出编码器名（h264_amf 同时出现在「首选编码器」，故用 getAllByText）
    expect(screen.getAllByText('h264_amf').length).toBeGreaterThan(0)
    expect(screen.getByText('hevc_amf')).toBeVisible()
    expect(screen.getByText('av1_amf')).toBeVisible()
    expect(screen.getByText('可输出编码')).toBeVisible()
    expect(screen.getAllByText('✗ 试编码失败').length).toBeGreaterThan(0)
  })

  it('编解码区块点「测试编解码器」后渲染 per-codec 结果行并显示缓存来源', async () => {
    const user = userEvent.setup()
    renderSection('codec')

    await user.click(screen.getByRole('button', { name: '测试编解码器' }))

    await waitFor(() => {
      expect(screen.getAllByText('libx264').length).toBeGreaterThan(0)
    })
    expect(screen.getAllByText('av1_amf').length).toBeGreaterThan(0)
    expect(screen.getByText(/结果来自缓存，实测于/)).toBeVisible()
  })

  it('编解码区块「重新测试」按钮强制重跑（force）', async () => {
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

    renderSection('codec')
    const rerunBtn = screen.getByRole('button', { name: '重新测试' })
    expect(rerunBtn).toBeVisible()
    await user.click(rerunBtn)

    await waitFor(() => {
      expect(screen.getByText(/实测于 2026-06-23T11:00:00Z/)).toBeVisible()
    })
    expect(forceParam).toBe('true')
  })

  it('编解码区块 ffmpeg 不可用时提示无法测试', async () => {
    server.use(
      http.post('*/api/system/codec-test', () =>
        HttpResponse.json({ ffmpeg_available: false, results: [], from_cache: false, ffmpeg_version: '', tested_at: '' }),
      ),
    )

    const user = userEvent.setup()
    renderSection('codec')
    await user.click(screen.getByRole('button', { name: '测试编解码器' }))

    await waitFor(() => {
      expect(screen.getByText('ffmpeg 不可用，无法测试编解码器。')).toBeVisible()
    })
  })

  it('应用更新区块检查更新后展示最新版本与可更新状态（FR-46）', async () => {
    const user = userEvent.setup()
    renderSection('update')

    await user.click(screen.getByRole('button', { name: '检查更新' }))

    await waitFor(() => {
      expect(screen.getByText('v0.6.3')).toBeVisible()
    })
    expect(screen.getByText('有可用更新')).toBeVisible()
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
    renderSection('update')

    await user.click(screen.getByRole('button', { name: '检查更新' }))

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
    renderSection('update')

    await user.click(screen.getByRole('button', { name: '检查更新' }))

    await waitFor(() => {
      expect(screen.getByText('更新出错')).toBeVisible()
    })
    expect(screen.getByText(/网络不可达或 GitHub 暂时不可用/)).toBeVisible()
    expect(screen.queryByText(/timeout of/i)).toBeNull()
  })

  it('切换到测试版后检查更新走预发布频道（FR-46）', async () => {
    const user = userEvent.setup()
    renderSection('update')

    // 切到「测试版」频道（Mantine SegmentedControl 点标签文本）
    await user.click(screen.getByText('测试版'))
    await user.click(screen.getByRole('button', { name: '检查更新' }))

    await waitFor(() => {
      expect(screen.getByText('v0.6.3-dev.abc1234')).toBeVisible()
    })
    expect(screen.getByText('预发布')).toBeVisible()
  })

  it('「检查更新」默认走缓存（不带 force query，FR-62）', async () => {
    const user = userEvent.setup()
    // 进入即有一次自动检查（force=false），故收集全部 force 取值再断言「检查更新」未带 force
    const seen: (string | null)[] = []
    server.use(
      http.get('*/api/system/update/check', ({ request }) => {
        seen.push(new URL(request.url).searchParams.get('force'))
        return HttpResponse.json({
          current: '0.3.0', latest: 'v0.6.3', has_update: true, tag: 'v0.6.3',
          prerelease: false, channel: 'stable', notes: '', asset_name: 'jianvideo-linux-amd64',
        })
      }),
    )

    renderSection('update')
    await user.click(screen.getByRole('button', { name: '检查更新' }))

    await waitFor(() => {
      expect(screen.getByText('v0.6.3')).toBeVisible()
    })
    // 「检查更新」与自动检查均走缓存：从未带 force=true
    expect(seen).not.toContain('true')
  })

  it('「获取更新」强制刷新走 force=true（FR-62）', async () => {
    const user = userEvent.setup()
    // 进入有一次自动检查（force=false），点击「获取更新」应另发一次 force=true，故收集全部取值断言含 true
    const seen: (string | null)[] = []
    server.use(
      http.get('*/api/system/update/check', ({ request }) => {
        seen.push(new URL(request.url).searchParams.get('force'))
        return HttpResponse.json({
          current: '0.3.0', latest: 'v0.6.3', has_update: true, tag: 'v0.6.3',
          prerelease: false, channel: 'stable', notes: '', asset_name: 'jianvideo-linux-amd64',
        })
      }),
    )

    renderSection('update')
    const forceBtn = screen.getByRole('button', { name: '获取更新' })
    expect(forceBtn).toBeVisible()
    await user.click(forceBtn)

    await waitFor(() => {
      expect(seen).toContain('true')
    })
  })

  it('命中本地缓存时进入即时展示缓存的发布说明（不依赖网络）', async () => {
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

    renderSection('update')

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

    renderSection('update')
    await user.click(screen.getByRole('button', { name: '检查更新' }))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /立即更新并重启/ })).toBeEnabled()
    })

    await user.click(screen.getByRole('button', { name: /立即更新并重启/ }))

    const dialog = await screen.findByRole('dialog')
    expect(dialog).toBeVisible()
    expect(applyCalled).toBe(false)

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

    renderSection('update')
    await user.click(screen.getByRole('button', { name: '检查更新' }))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /立即更新并重启/ })).toBeEnabled()
    })

    await user.click(screen.getByRole('button', { name: /立即更新并重启/ }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '取消' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull()
    })
    expect(applyCalled).toBe(false)
  })

  it('点「回滚到上一版」弹模态框，确认触发回滚、取消不触发（FR-62）', async () => {
    const user = userEvent.setup()
    let rollbackCalled = false
    server.use(
      http.get('*/api/system/update/check', () =>
        HttpResponse.json({
          current: '0.3.0', latest: 'v0.6.3', has_update: true, tag: 'v0.6.3',
          prerelease: false, channel: 'stable', notes: '', asset_name: 'jianvideo-linux-amd64',
          rollback_available: true,
        }),
      ),
      http.post('*/api/system/update/rollback', () => {
        rollbackCalled = true
        return HttpResponse.json({ status: 'rolling_back', message: '' })
      }),
    )

    renderSection('update')
    await user.click(screen.getByRole('button', { name: '检查更新' }))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /回滚到上一版/ })).toBeEnabled()
    })
    await user.click(screen.getByRole('button', { name: /回滚到上一版/ }))

    let dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '取消' }))
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull()
    })
    expect(rollbackCalled).toBe(false)

    await user.click(screen.getByRole('button', { name: /回滚到上一版/ }))
    dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '确认回滚' }))
    await waitFor(() => {
      expect(rollbackCalled).toBe(true)
    })
  })

  it('更新下载网络失败时展示友好提示并引导配代理（FIX-1）', async () => {
    const user = userEvent.setup()
    server.use(
      http.post('*/api/system/update/apply', () =>
        HttpResponse.json(
          { code: 'UPDATE_FAILED', message: '更新失败：下载失败: Get "https://github.com/wcpe/JianVideo/releases/download/v0.17.0/jianvideo-windows-amd64.exe": read tcp 192.168.31.123:2946->140.82.112.4:443: wsarecv: A connection attempt failed because the connected party did not properly respond' },
          { status: 500 },
        ),
      ),
    )
    renderSection('update')

    await user.click(screen.getByRole('button', { name: '检查更新' }))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: '立即更新并重启' })).toBeEnabled()
    })
    await user.click(screen.getByRole('button', { name: '立即更新并重启' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '确认更新' }))

    await waitFor(() => {
      expect(screen.getByText(/设置 → 网络/)).toBeVisible()
    })
    expect(screen.queryByText(/wsarecv/)).toBeNull()
  })

  it('无可回滚版本（rollback_available=false）时隐藏回滚按钮（FIX-2）', async () => {
    const user = userEvent.setup()
    server.use(
      http.get('*/api/system/update/check', () =>
        HttpResponse.json({
          current: '0.3.0', latest: 'v0.6.3', has_update: true, tag: 'v0.6.3',
          prerelease: false, channel: 'stable', notes: '', asset_name: 'jianvideo-linux-amd64',
          rollback_available: false,
        }),
      ),
    )
    renderSection('update')

    await user.click(screen.getByRole('button', { name: '检查更新' }))
    await waitFor(() => {
      expect(screen.getByText('v0.6.3')).toBeVisible()
    })
    expect(screen.queryByRole('button', { name: /回滚到上一版/ })).toBeNull()
  })

  it('更新进行中轮询进度端点并展示下载进度条与百分比（FR-90）', async () => {
    const user = userEvent.setup()
    server.use(
      http.post('*/api/system/update/apply', async () => {
        await delay(1200)
        return HttpResponse.json({ status: 'updating', message: '更新已应用，服务即将重启' })
      }),
      http.get('*/api/system/update/progress', () =>
        HttpResponse.json({ state: 'downloading', downloaded: 6 * 1024 * 1024, total: 12 * 1024 * 1024, percent: 50 }),
      ),
    )

    renderSection('update')
    await user.click(screen.getByRole('button', { name: '检查更新' }))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /立即更新并重启/ })).toBeEnabled()
    })
    await user.click(screen.getByRole('button', { name: /立即更新并重启/ }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '确认更新' }))

    await waitFor(() => {
      expect(screen.getByText('下载中…')).toBeVisible()
    })
    expect(screen.getByText(/50%/)).toBeVisible()
    const bar = screen.getByRole('progressbar')
    expect(bar).toHaveAttribute('aria-valuenow', '50')
  })

  it('更新失败后展示「重试」按钮，点击重新触发更新（FR-90）', async () => {
    const user = userEvent.setup()
    let applyCalls = 0
    server.use(
      http.post('*/api/system/update/apply', () => {
        applyCalls++
        if (applyCalls === 1) {
          return HttpResponse.json({ code: 'UPDATE_FAILED', message: '更新失败：下载中断' }, { status: 500 })
        }
        return HttpResponse.json({ status: 'updating', message: '更新已应用，服务即将重启' })
      }),
    )

    renderSection('update')
    await user.click(screen.getByRole('button', { name: '检查更新' }))
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /立即更新并重启/ })).toBeEnabled()
    })
    await user.click(screen.getByRole('button', { name: /立即更新并重启/ }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '确认更新' }))

    await waitFor(() => {
      expect(screen.getByText('更新出错')).toBeVisible()
    })
    const retryBtn = await screen.findByRole('button', { name: '重试' })
    expect(retryBtn).toBeVisible()

    await user.click(retryBtn)
    await waitFor(() => {
      expect(applyCalls).toBe(2)
    })
  })

  it('编解码区块点击「复制结果」后写入剪贴板并显示已复制反馈', async () => {
    const user = userEvent.setup()
    renderSection('codec')

    // 复制按钮在系统信息加载完成（info 就绪）后才可用
    await waitFor(() => {
      expect(screen.getByRole('button', { name: '复制结果' })).toBeEnabled()
    })
    await user.click(screen.getByRole('button', { name: '复制结果' }))

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledTimes(1)
    })
    expect(writeText.mock.calls[0][0]).toContain('0.3.0')
    await waitFor(() => {
      expect(screen.getByRole('button', { name: '已复制' })).toBeVisible()
    })
  })
})
