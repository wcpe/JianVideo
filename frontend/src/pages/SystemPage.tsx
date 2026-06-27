import { useState, useEffect, useCallback, useRef } from 'react'
import {
  Stack, Title, Card, Text, Group, Badge, Button, Alert, Skeleton, Table, SimpleGrid, Code, Box, SegmentedControl,
  TypographyStylesProvider, Modal, Progress,
} from '@mantine/core'
import { useClipboard, useDisclosure } from '@mantine/hooks'
import {
  IconAlertCircle, IconCopy, IconCheck, IconRefresh, IconDownload, IconArrowBackUp,
} from '@tabler/icons-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import * as systemApi from '@/api/system'
import { getSettings, updateSettings, SETTING_KEY_UPDATE_CHANNEL } from '@/api/settings'
import { extractErrorMessage } from '@/utils/error'
import { loadCachedUpdate, saveCachedUpdate, clearCachedUpdate } from '@/utils/update-cache'
import AnchorNav from '@/components/AnchorNav'
import type { SystemInfo, CodecTestResult, UpdateCheckResult, UpdateProgress } from '@/types'

// 检查更新失败的友好兜底文案：超时/网络异常等无后端消息时用它，避免回显裸 axios 串（如 "timeout of 15000ms exceeded"）。
const UPDATE_CHECK_FALLBACK = '检查更新失败：网络异常或 GitHub 暂时不可用，请稍后重试'

// 系统区块取值（FR-113 一级 tab）：运行环境 / 硬件加速 / 编解码 / 应用更新；
// 由 ConsolePage 的一级 tab 驱动选中哪个区块，本组件仅渲染该区块（不再有内层 tab）。
export type SystemSection = 'env' | 'hwaccel' | 'codec' | 'update'

// 区块 → 一级 tab 名称映射（FR-113 后续修复）：内容区 order-2 标题按当前 section 显示对应 tab 名，
// 与 ConsolePage 的 Tabs.Tab 文案保持一致（避免「应用更新」tab 却显示「系统诊断」）。集中一处定义，不散落硬编码。
export const SECTION_TITLES: Record<SystemSection, string> = {
  env: '运行环境',
  hwaccel: '硬件加速',
  codec: '编解码',
  update: '应用更新',
}

// 运行环境区块内左侧锚点（FR-113）：运行环境 / FFmpeg 两区块
const ENV_ANCHORS = [
  { id: 'sys-env', label: '运行环境' },
  { id: 'sys-ffmpeg', label: 'FFmpeg' },
]

/**
 * 单行信息项（FR-101 重排）：定宽 label 列 + 紧贴其后的 value 列的紧凑两列。
 * 此前用 Group justify="space-between" 把 label/value 左右拉满，中间留巨大空隙、长路径换行难读；
 * 改为 label 定宽（flex-shrink:0）、value 紧随，键值成对易读。
 * mono 为真时 value 用等宽字体（路径/版本号/PID 等），并截断 + title 挂全文（保留 FR-97 悬停提示）。
 */
function InfoRow({ label, value, mono }: { label: string; value: React.ReactNode; mono?: boolean }) {
  // 长路径等文本值补 title 全文提示（FR-97 可读性）：值为字符串时把原文挂到 title，非字符串（如徽标节点）不挂。
  const titleText = typeof value === 'string' ? value : undefined
  return (
    <Group gap="sm" wrap="nowrap" align="flex-start">
      <Text size="sm" c="dimmed" style={{ width: 120, flexShrink: 0 }}>{label}</Text>
      <Text
        size="sm"
        title={titleText}
        style={{
          minWidth: 0,
          flex: 1,
          // 等宽值（路径/版本/PID）截断、悬停看全文；普通值允许换行不撑破。
          fontFamily: mono ? 'var(--mantine-font-family-monospace)' : undefined,
          overflow: mono ? 'hidden' : undefined,
          textOverflow: mono ? 'ellipsis' : undefined,
          whiteSpace: mono ? 'nowrap' : undefined,
          wordBreak: mono ? undefined : 'break-all',
        }}
      >
        {value}
      </Text>
    </Group>
  )
}

/** 把字节数格式化为人类可读单位（FR-60 运行时内存展示） */
function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)))
  return `${(bytes / 1024 ** i).toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

/** 把秒数格式化为「Xd Xh Xm Xs」运行时长（FR-60） */
function formatUptime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return '0s'
  const s = Math.floor(seconds)
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60
  const parts: string[] = []
  if (d > 0) parts.push(`${d}d`)
  if (h > 0) parts.push(`${h}h`)
  if (m > 0) parts.push(`${m}m`)
  parts.push(`${sec}s`)
  return parts.join(' ')
}

/** 把运行环境信息整理为可粘贴的纯文本报告（FR-113 后续增强：运行环境区块「复制」） */
function buildEnvReport(info: SystemInfo): string {
  const lines: string[] = []
  lines.push('===== JianVideo 运行环境 =====')
  lines.push(`应用版本: ${info.app_version}`)
  lines.push(`操作系统: ${info.os}/${info.arch}`)
  lines.push(`CPU 核心数: ${info.num_cpu}`)
  lines.push(`主机名: ${info.hostname}`)
  lines.push(`Go 版本: ${info.go_version}`)
  if (info.runtime) {
    const rt = info.runtime
    lines.push(`GOMAXPROCS: ${rt.gomaxprocs}`)
    lines.push(`进程 PID: ${rt.pid}`)
    lines.push(`运行时长: ${formatUptime(rt.uptime_seconds)}`)
    lines.push(`运行内存（已用/申请）: ${formatBytes(rt.mem_alloc)} / ${formatBytes(rt.mem_sys)}`)
    lines.push(`GC 次数: ${rt.num_gc}`)
    lines.push(`数据库路径: ${rt.db_path || '—'}`)
    lines.push(`工作目录: ${rt.work_dir || '—'}`)
    lines.push(`可执行文件: ${rt.executable || '—'}`)
  }
  lines.push(`FFmpeg 可用: ${info.ffmpeg.available ? '是' : '否'}`)
  if (info.ffmpeg.available) {
    lines.push(`FFmpeg 路径: ${info.ffmpeg.path}`)
    lines.push(`FFmpeg 版本: ${info.ffmpeg.version}`)
  }
  return lines.join('\n')
}

/** 把系统信息与编解码器结果整理为可粘贴的纯文本报告 */
function buildReport(info: SystemInfo | null, codec: CodecTestResult | null): string {
  const lines: string[] = []
  lines.push('===== JianVideo 系统诊断 =====')
  if (info) {
    lines.push(`应用版本: ${info.app_version}`)
    lines.push(`操作系统: ${info.os}/${info.arch}`)
    lines.push(`CPU 核心数: ${info.num_cpu}`)
    lines.push(`主机名: ${info.hostname}`)
    lines.push(`Go 版本: ${info.go_version}`)
    if (info.runtime) {
      const rt = info.runtime
      lines.push(`PID: ${rt.pid}`)
      lines.push(`运行时长: ${formatUptime(rt.uptime_seconds)}`)
      lines.push(`工作目录: ${rt.work_dir}`)
      lines.push(`可执行文件: ${rt.executable}`)
      lines.push(`数据库路径: ${rt.db_path}`)
      lines.push(`运行内存: ${formatBytes(rt.mem_alloc)} / ${formatBytes(rt.mem_sys)}（已用/申请）`)
      lines.push(`GC 次数: ${rt.num_gc}`)
      lines.push(`GOMAXPROCS: ${rt.gomaxprocs}`)
    }
    lines.push(`FFmpeg 可用: ${info.ffmpeg.available ? '是' : '否'}`)
    if (info.ffmpeg.available) {
      lines.push(`FFmpeg 路径: ${info.ffmpeg.path}`)
      lines.push(`FFmpeg 版本: ${info.ffmpeg.version}`)
    }
    lines.push(`首选编码器: ${info.hwaccel.preferred || '无'}`)
    lines.push(`可输出编码: ${info.hwaccel.codecs.length ? info.hwaccel.codecs.join(', ') : '无'}`)
    lines.push(`软件回退: ${info.hwaccel.software_fallback ? '是' : '否'}`)
    lines.push(`实测来源: ${info.hwaccel.tested_at ? `${info.hwaccel.from_cache ? '缓存' : '实测'} @ ${info.hwaccel.tested_at}` : '未测'}`)
    for (const cap of info.hwaccel.available) {
      for (const c of cap.codecs) {
        lines.push(`硬件加速: ${cap.family}/${c.codec} ${c.encoder} 试编码:${c.tested_ok ? '成功' : '失败'}`)
      }
    }
  }
  if (codec) {
    lines.push('')
    lines.push('----- 编解码器测试 -----')
    if (!codec.ffmpeg_available) {
      lines.push('ffmpeg 不可用')
    } else {
      if (codec.tested_at) {
        lines.push(`实测来源: ${codec.from_cache ? '缓存' : '实测'} @ ${codec.tested_at}`)
      }
      for (const r of codec.results) {
        const status = r.tested_ok ? '成功' : '失败'
        lines.push(`${r.encoder} [${r.family}/${r.codec}] 编入:${r.compiled ? '是' : '否'} 试编码:${status}${r.detail ? ` 详情:${r.detail}` : ''}`)
      }
    }
  }
  return lines.join('\n')
}

/**
 * 提取检查更新失败的展示文案：
 * 优先用后端返回的友好 message（如「网络不可达或 GitHub 暂时不可用」）；
 * 无响应（超时/网络异常，axios message 形如「timeout of 60000ms exceeded」）时用固定兜底，避免回显裸串。
 */
function updateCheckErrorMessage(err: unknown): string {
  if (err && typeof err === 'object' && 'response' in err) {
    const resp = (err as { response?: { data?: { message?: string } } }).response
    if (resp?.data?.message) return resp.data.message
  }
  return UPDATE_CHECK_FALLBACK
}

// 更新（检查/下载/应用）网络类失败特征：直连 GitHub 常见的超时、连接失败、DNS、连接重置等。
const UPDATE_NETWORK_ERR_RE = /下载失败|超时|timeout|wsarecv|wsasend|dial|connection|connect|refused|reset|EOF|no such host|did not properly respond|网络不可达|网络异常|i\/o timeout/i

/**
 * friendlyUpdateError 把更新的网络类失败映射为友好文案并引导配置代理（FIX-1）；
 * 非网络类错误原样返回。国内直连 GitHub 下载 CDN 常超时，配置出站代理（FR-80）可解。
 */
function friendlyUpdateError(raw: string): string {
  if (UPDATE_NETWORK_ERR_RE.test(raw)) {
    return '更新失败：连接 GitHub 超时或网络不可达。国内直连 GitHub 下载常被限速或阻断，可在「设置 → 网络」配置出站代理后重试。'
  }
  return raw
}

/**
 * 系统诊断页：展示运行环境、FFmpeg/硬件加速信息，并支持编解码器测试与应用更新。
 * FR-113 拍平为一级 tab 后，由 ConsolePage 通过 `section` 选定渲染哪个区块（不再有内层 tab）。
 */
export default function SystemPage({ section }: { section: SystemSection }) {
  const [info, setInfo] = useState<SystemInfo | null>(null)
  const [infoError, setInfoError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [codec, setCodec] = useState<CodecTestResult | null>(null)
  const [codecError, setCodecError] = useState<string | null>(null)
  const [codecLoading, setCodecLoading] = useState(false)
  const clipboard = useClipboard({ timeout: 2000 })

  // 自更新（FR-46）
  const [channel, setChannel] = useState<'stable' | 'prerelease'>('stable')
  const [updateInfo, setUpdateInfo] = useState<UpdateCheckResult | null>(null)
  const [updateError, setUpdateError] = useState<string | null>(null)
  const [updateChecking, setUpdateChecking] = useState(false)
  const [updateBusy, setUpdateBusy] = useState(false) // 应用/回滚中（含重启等待）
  const [restartMsg, setRestartMsg] = useState<string | null>(null)
  // 自更新下载进度（FR-90）：轮询进度端点展示进度条；applyFailed 用于在更新失败时显式提供「重试」入口
  const [updateProgress, setUpdateProgress] = useState<UpdateProgress | null>(null)
  const [applyFailed, setApplyFailed] = useState(false)
  const progressTimer = useRef<ReturnType<typeof setInterval> | null>(null)
  // 更新/回滚二次确认模态框（FR-62 替代原生 window.confirm）
  const [applyModalOpened, applyModal] = useDisclosure(false)
  const [rollbackModalOpened, rollbackModal] = useDisclosure(false)

  // 挂载时加载系统信息
  useEffect(() => {
    let active = true
    setLoading(true)
    setInfoError(null)
    systemApi.getSystemInfo()
      .then((data) => { if (active) setInfo(data) })
      .catch((err) => { if (active) setInfoError(extractErrorMessage(err, '加载系统信息失败')) })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [])

  // 加载持久化的更新频道（正式版/测试版），读失败回退 stable
  useEffect(() => {
    let active = true
    getSettings()
      .then((s) => {
        const ch = s[SETTING_KEY_UPDATE_CHANNEL]
        if (active && (ch === 'prerelease' || ch === 'stable')) setChannel(ch)
      })
      .catch(() => { /* 读失败用默认 stable */ })
    return () => { active = false }
  }, [])

  // 进入 / 切换频道仅展示本地缓存（FR-112）：有缓存直接显示缓存版本与发布说明，无缓存则不显示更新区；
  // 进入不自动联网（去掉原后台 force=false 刷新），需用户显式点「检查更新」才强制直连重查。
  useEffect(() => {
    // 即时展示该频道缓存（含发布说明）；无缓存则清空（updateInfo 为 null → 不渲染更新区）
    setUpdateInfo(loadCachedUpdate(channel))
    setUpdateError(null)
  }, [channel])

  // 默认走缓存（force 为 false），「重新测试」强制重跑（force 为 true）
  const handleCodecTest = useCallback(async (force: boolean) => {
    setCodecLoading(true)
    setCodecError(null)
    try {
      setCodec(await systemApi.runCodecTest(force))
    } catch (err) {
      setCodecError(extractErrorMessage(err, '编解码器测试失败'))
    } finally {
      setCodecLoading(false)
    }
  }, [])

  const handleCopy = useCallback(() => {
    clipboard.copy(buildReport(info, codec))
  }, [clipboard, info, codec])

  // 复制运行环境信息（FR-113 后续增强）：把当前展示的运行环境字段拼成纯文本写入剪贴板
  const handleCopyEnv = useCallback(() => {
    if (info) clipboard.copy(buildEnvReport(info))
  }, [clipboard, info])

  // 「检查更新」强制直连重查（FR-112：单按钮 = force 强制重查，绕后端 TTL 缓存）
  const handleCheckUpdate = useCallback(async (force: boolean) => {
    setUpdateChecking(true)
    setUpdateError(null)
    setUpdateInfo(null)
    try {
      // force 强制绕后端缓存直连 GitHub 重查最新
      const res = await systemApi.checkUpdate(channel, force)
      setUpdateInfo(res)
      // 写入本地缓存（含发布说明），供下次进入即时展示，免再手动点检查
      saveCachedUpdate(channel, res)
    } catch (err) {
      // 仅采用后端返回的友好 message；无响应（超时/网络异常）时用固定兜底，不回显裸 axios 串
      setUpdateError(updateCheckErrorMessage(err))
    } finally {
      setUpdateChecking(false)
    }
  }, [channel])

  // 切换并持久化更新频道（正式版/测试版）；切换后由 [channel] 副作用回填该频道缓存并后台刷新
  const handleChannelChange = useCallback((v: string) => {
    const ch: 'stable' | 'prerelease' = v === 'prerelease' ? 'prerelease' : 'stable'
    setChannel(ch)
    updateSettings({ [SETTING_KEY_UPDATE_CHANNEL]: ch }).catch(() => { /* 持久化失败不阻塞使用 */ })
  }, [])

  // waitForRestart 轮询 /health 等待自更新/回滚后的服务重启，恢复后刷新页面。
  const waitForRestart = useCallback(async (action: string) => {
    setRestartMsg(`${action}已触发，等待服务重启…`)
    for (let i = 0; i < 30; i++) {
      await new Promise((r) => setTimeout(r, 2000))
      if (await systemApi.pingHealth()) {
        setRestartMsg(`${action}完成，服务已重启，即将刷新页面…`)
        setTimeout(() => window.location.reload(), 1500)
        return
      }
    }
    setRestartMsg(`${action}已触发，但等待重启超时，请手动检查服务状态。`)
  }, [])

  // stopProgressPoll 停止下载进度轮询（FR-90）。
  const stopProgressPoll = useCallback(() => {
    if (progressTimer.current) {
      clearInterval(progressTimer.current)
      progressTimer.current = null
    }
  }, [])

  // startProgressPoll 启动下载进度轮询（FR-90）：每 ~800ms 拉一次进度，进入校验/完成或重启等待后由调用方停。
  const startProgressPoll = useCallback(() => {
    stopProgressPoll()
    progressTimer.current = setInterval(() => {
      systemApi.getUpdateProgress()
        .then((p) => setUpdateProgress(p))
        .catch(() => { /* 进度拉取失败不打断更新流程，下次轮询继续 */ })
    }, 800)
  }, [stopProgressPoll])

  // 组件卸载时清理进度轮询定时器，避免泄漏
  useEffect(() => stopProgressPoll, [stopProgressPoll])

  // runApply 执行更新：轮询下载进度、apply 成功后等待重启；失败标记 applyFailed 以展示「重试」入口（FR-90）。
  // 由更新确认模态框「确认」与失败后「重试」按钮共用。
  const runApply = useCallback(async () => {
    setUpdateBusy(true)
    setUpdateError(null)
    setApplyFailed(false)
    setUpdateProgress(null)
    startProgressPoll()
    try {
      await systemApi.applyUpdate(channel)
      stopProgressPoll()
      // 更新成功：清理本地缓存（FR-112），使下次进入不展示过期版本、需用户重新点「检查更新」强制重拉
      clearCachedUpdate(channel)
      setUpdateInfo(null)
      await waitForRestart('更新')
    } catch (err) {
      stopProgressPoll()
      setUpdateError(friendlyUpdateError(extractErrorMessage(err, '更新失败')))
      setApplyFailed(true)
      setUpdateBusy(false)
    }
  }, [channel, startProgressPoll, stopProgressPoll, waitForRestart])

  // 由更新确认模态框「确认」触发：关模态框后执行更新（FR-62 + FR-90 进度/重试）
  const handleApplyUpdate = useCallback(() => {
    applyModal.close()
    void runApply()
  }, [applyModal, runApply])

  // 由失败后「重试」按钮触发：用户已在首次确认过，直接重试更新（FR-90）
  const handleRetryApply = useCallback(() => {
    void runApply()
  }, [runApply])

  // 由回滚确认模态框「确认」触发：关模态框后执行回滚并等待重启（FR-62）
  const handleRollback = useCallback(async () => {
    rollbackModal.close()
    setUpdateBusy(true)
    setUpdateError(null)
    try {
      await systemApi.rollbackUpdate()
      await waitForRestart('回滚')
    } catch (err) {
      setUpdateError(extractErrorMessage(err, '回滚失败'))
      setUpdateBusy(false)
    }
  }, [rollbackModal, waitForRestart])

  return (
    <Stack gap="md">
      {/* 内容区标题按当前 section 显示对应一级 tab 名称（FR-113 后续修复），与上方 tab 文案一致 */}
      <Title order={2}>{SECTION_TITLES[section]}</Title>

      {infoError && (
        <Alert icon={<IconAlertCircle size={16} />} color="red" title="加载失败">
          {infoError}
        </Alert>
      )}

      {/* 运行环境区块（FR-113）：运行环境信息 + FFmpeg，左侧锚点导航定位两区块 */}
      {section === 'env' && (
        <Group align="flex-start" gap="lg" wrap="nowrap">
          {/* 左侧锚点 sticky 常驻（FR-113 修复）：滚动时锚点列吸顶可见、不随内容滚走。
              position 内联（便于 jsdom 单测断言）；top 由 .anchor-nav-sticky 设为「页眉 + tab 条」高度，
              让开固定页眉与 sticky 一级 tab 条（FR-113 第三批修复），不再内联写死 56。 */}
          <Box
            w={160}
            className="anchor-nav-sticky"
            style={{ flexShrink: 0, alignSelf: 'flex-start', position: 'sticky' }}
            visibleFrom="sm"
          >
            <AnchorNav sections={ENV_ANCHORS} />
          </Box>
          <Box style={{ flex: 1, minWidth: 0 }}>
          {loading ? (
            <Skeleton height={320} radius="md" />
          ) : info ? (
            <Stack gap="md">
              {/* 运行环境 */}
              <Card id="sys-env" withBorder padding="md" radius="md">
                <Group justify="space-between" mb="sm">
                  <Title order={4}>运行环境</Title>
                  {/* 复制运行环境报告（FR-113 后续增强）：与编解码「复制结果」同风格，复制后绿勾 + 文案切换 */}
                  <Button
                    variant="light"
                    color={clipboard.copied ? 'green' : 'gray'}
                    size="xs"
                    leftSection={clipboard.copied ? <IconCheck size={16} /> : <IconCopy size={16} />}
                    onClick={handleCopyEnv}
                  >
                    {clipboard.copied ? '已复制' : '复制'}
                  </Button>
                </Group>
                <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="xs">
                  <InfoRow label="应用版本" value={info.app_version} mono />
                  <InfoRow label="操作系统" value={`${info.os}/${info.arch}`} mono />
                  <InfoRow label="CPU 核心数" value={info.num_cpu} mono />
                  <InfoRow label="主机名" value={info.hostname} mono />
                  <InfoRow label="Go 版本" value={info.go_version} mono />
                  {info.runtime && (
                    <>
                      <InfoRow label="GOMAXPROCS" value={info.runtime.gomaxprocs} mono />
                      <InfoRow label="进程 PID" value={info.runtime.pid} mono />
                      <InfoRow label="运行时长" value={formatUptime(info.runtime.uptime_seconds)} />
                      <InfoRow label="运行内存（已用/申请）" value={`${formatBytes(info.runtime.mem_alloc)} / ${formatBytes(info.runtime.mem_sys)}`} />
                      <InfoRow label="GC 次数" value={info.runtime.num_gc} mono />
                      <InfoRow label="数据库路径" value={info.runtime.db_path || '—'} mono />
                      <InfoRow label="工作目录" value={info.runtime.work_dir || '—'} mono />
                      <InfoRow label="可执行文件" value={info.runtime.executable || '—'} mono />
                    </>
                  )}
                </SimpleGrid>
              </Card>

              {/* FFmpeg */}
              <Card id="sys-ffmpeg" withBorder padding="md" radius="md">
                <Group justify="space-between" mb="sm">
                  <Title order={4}>FFmpeg</Title>
                  <Badge color={info.ffmpeg.available ? 'green' : 'red'}>
                    {info.ffmpeg.available ? '可用' : '不可用'}
                  </Badge>
                </Group>
                {info.ffmpeg.available ? (
                  <Stack gap="xs">
                    <InfoRow label="路径" value={info.ffmpeg.path} mono />
                    <InfoRow label="版本" value={info.ffmpeg.version} mono />
                  </Stack>
                ) : (
                  <Text size="sm" c="dimmed">未检测到 FFmpeg，转码相关功能不可用。</Text>
                )}
              </Card>
            </Stack>
          ) : null}
          </Box>
        </Group>
      )}

      {/* 硬件加速区块（FR-113） */}
      {section === 'hwaccel' && (
        loading ? (
          <Skeleton height={320} radius="md" />
        ) : info ? (
            <Card withBorder padding="md" radius="md">
              <Title order={4} mb="sm">硬件加速</Title>
              <Stack gap="xs">
                <InfoRow label="首选编码器" value={info.hwaccel.preferred || '无（使用软件编码）'} />
                {/* 系统可输出编码并集（codecs），按编码逐个展示 */}
                <Group gap="xs" align="center">
                  <Text size="sm" c="dimmed">可输出编码</Text>
                  {info.hwaccel.codecs.length === 0 ? (
                    <Text size="sm" c="dimmed">无</Text>
                  ) : (
                    info.hwaccel.codecs.map((c) => (
                      <Badge key={c} variant="light" color="green">{c}</Badge>
                    ))
                  )}
                </Group>
                <Group gap="xs">
                  {info.hwaccel.software_fallback && (
                    <Badge variant="light" color="yellow">软件回退</Badge>
                  )}
                  {info.hwaccel.intel_gpu && (
                    <Badge variant="light" color="blue">Intel GPU</Badge>
                  )}
                </Group>
                {/* 实测来源（缓存/实测 + 时间） */}
                <Text size="xs" c="dimmed">
                  {info.hwaccel.tested_at
                    ? `${info.hwaccel.from_cache ? '结果来自缓存，实测于' : '实测于'} ${info.hwaccel.tested_at}`
                    : '尚未实测硬件加速能力'}
                </Text>
                {info.hwaccel.available.length === 0 ? (
                  <Text size="sm" c="dimmed">未检测到可用的硬件加速。</Text>
                ) : (
                  /* per-family 卡片一行 2-3 列网格展示（FR-59），提升信息密度 */
                  <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="xs">
                    {info.hwaccel.available.map((cap) => (
                      <Card key={cap.family} withBorder padding="xs" radius="sm" bg="var(--mantine-color-default-hover)">
                        <Group justify="space-between" mb={4}>
                          <Text size="sm" fw={600}>{cap.name}</Text>
                          <Badge size="sm" color={cap.available ? 'green' : 'red'}>
                            {cap.available ? '可用' : '不可用'}
                          </Badge>
                        </Group>
                        {cap.device_type && (
                          <Text size="xs" c="dimmed" mb={4}>设备类型 {cap.device_type}</Text>
                        )}
                        {/* 逐 codec 展示：编码格式 + 编码器名 + 试编码结果 */}
                        <Stack gap={4}>
                          {cap.codecs.map((c) => (
                            <Group key={c.encoder} gap="xs" wrap="nowrap">
                              <Badge size="xs" variant="outline" color="gray">{c.codec}</Badge>
                              <Code>{c.encoder}</Code>
                              <Badge size="xs" color={c.tested_ok ? 'green' : 'red'}>
                                {c.tested_ok ? '✓ 试编码成功' : '✗ 试编码失败'}
                              </Badge>
                            </Group>
                          ))}
                        </Stack>
                      </Card>
                    ))}
                  </SimpleGrid>
                )}
              </Stack>
            </Card>
        ) : null
      )}

      {/* 编解码区块（FR-113） */}
      {section === 'codec' && (
          <CodecTestCard
            info={info}
            codec={codec}
            codecError={codecError}
            codecLoading={codecLoading}
            copied={clipboard.copied}
            onTest={handleCodecTest}
            onCopy={handleCopy}
          />
      )}

      {/* 应用更新区块（FR-46）；id=update 兜底锚点（页眉精确跳转改为选中本一级 tab，FR-113）*/}
      {section === 'update' && (
        <>
          <Card id="update" withBorder padding="md" radius="md">
            <Group justify="space-between" mb="sm">
              <Title order={4}>应用更新</Title>
              <Group gap="xs">
            <SegmentedControl
              size="xs"
              value={channel}
              onChange={handleChannelChange}
              data={[{ label: '正式版', value: 'stable' }, { label: '测试版', value: 'prerelease' }]}
              disabled={updateBusy}
            />
            {/* 单按钮「检查更新」（FR-112）：点击 = force 强制直连重查；进入仅展缓存、不自动联网 */}
            <Button
              variant="light"
              color="purple"
              leftSection={<IconRefresh size={16} />}
              onClick={() => handleCheckUpdate(true)}
              loading={updateChecking}
              disabled={updateBusy}
            >
              检查更新
            </Button>
          </Group>
        </Group>

        {updateError && (
          <Alert icon={<IconAlertCircle size={16} />} color="red" title="更新出错" mb="sm">
            <Stack gap="xs">
              <Text size="sm">{updateError}</Text>
              {/* 更新（下载/校验）失败后显式「重试」入口（FR-90）：用户已确认过，直接重试不再弹模态框 */}
              {applyFailed && (
                <Group>
                  <Button
                    size="xs"
                    color="purple"
                    leftSection={<IconRefresh size={14} />}
                    onClick={handleRetryApply}
                    loading={updateBusy}
                  >
                    重试
                  </Button>
                </Group>
              )}
            </Stack>
          </Alert>
        )}
        {/* 更新进行中展示下载进度条（FR-90）：total 已知按百分比，未知则展示已下载字节 + 不确定态 */}
        {updateBusy && updateProgress && (updateProgress.state === 'downloading' || updateProgress.state === 'verifying') && (
          <Box mb="sm">
            <Group justify="space-between" mb={4}>
              <Text size="sm" c="dimmed">
                {updateProgress.state === 'verifying' ? '校验中…' : '下载中…'}
              </Text>
              <Text size="sm" c="dimmed">
                {updateProgress.total > 0
                  ? `${updateProgress.percent}%（${formatBytes(updateProgress.downloaded)} / ${formatBytes(updateProgress.total)}）`
                  : `已下载 ${formatBytes(updateProgress.downloaded)}`}
              </Text>
            </Group>
            <Progress
              value={updateProgress.total > 0 ? updateProgress.percent : 100}
              animated={updateProgress.total <= 0 || updateProgress.state === 'verifying'}
              color="purple"
            />
          </Box>
        )}
        {restartMsg && <Alert color="blue" mb="sm">{restartMsg}</Alert>}

        {updateInfo ? (
          <Stack gap="xs">
            <InfoRow label="当前版本" value={updateInfo.current} mono />
            <InfoRow label="最新版本" value={updateInfo.latest || '—'} mono />
            <Group gap="xs">
              {updateInfo.has_update
                ? <Badge color="orange">有可用更新</Badge>
                : <Badge color="green">已是最新</Badge>}
              {updateInfo.prerelease && <Badge variant="light" color="grape">预发布</Badge>}
            </Group>
            {updateInfo.notes && (
              <Box style={{ maxHeight: 160, overflowY: 'auto' }}>
                {/* 发布说明为 GitHub Release 的 markdown 原文，经 react-markdown 渲染；外链强制安全新标签页打开 */}
                <TypographyStylesProvider>
                  <ReactMarkdown
                    remarkPlugins={[remarkGfm]}
                    components={{
                      a: ({ node: _node, ...props }) => (
                        <a {...props} target="_blank" rel="noopener noreferrer" />
                      ),
                    }}
                  >
                    {updateInfo.notes}
                  </ReactMarkdown>
                </TypographyStylesProvider>
              </Box>
            )}
            <Group gap="xs">
              <Button
                color="purple"
                leftSection={<IconDownload size={16} />}
                onClick={applyModal.open}
                loading={updateBusy}
                disabled={!updateInfo.has_update}
              >
                立即更新并重启
              </Button>
              {/* 仅当存在可回滚的上一版（.old 备份）时显示回滚按钮（FIX-2） */}
              {updateInfo.rollback_available && (
                <Button
                  variant="default"
                  leftSection={<IconArrowBackUp size={16} />}
                  onClick={rollbackModal.open}
                  disabled={updateBusy}
                >
                  回滚到上一版
                </Button>
              )}
            </Group>
          </Stack>
            ) : (
              // FR-112：无本地缓存时不显示版本/更新区，仅留一行操作引导（不含任何版本或更新提示）
              !updateError && (
                <Text size="sm" c="dimmed">暂无本地缓存的检查结果。点击「检查更新」从 GitHub Releases 强制重查最新版本。</Text>
              )
            )}
          </Card>

          {/* 更新确认模态框（FR-62 替代原生 window.confirm）：点「确认更新」才触发更新 */}
          <Modal opened={applyModalOpened} onClose={applyModal.close} title="确认更新" centered>
            <Stack gap="md">
              <Text size="sm">确定更新到 {updateInfo?.tag}？更新后服务将自动重启。</Text>
              <Group justify="flex-end" gap="xs">
                <Button variant="default" onClick={applyModal.close}>取消</Button>
                <Button color="purple" leftSection={<IconDownload size={16} />} onClick={handleApplyUpdate}>
                  确认更新
                </Button>
              </Group>
            </Stack>
          </Modal>

          {/* 回滚确认模态框（FR-62 替代原生 window.confirm）：点「确认回滚」才触发回滚 */}
          <Modal opened={rollbackModalOpened} onClose={rollbackModal.close} title="确认回滚" centered>
            <Stack gap="md">
              <Text size="sm">确定回滚到上一版本？服务将自动重启。</Text>
              <Group justify="flex-end" gap="xs">
                <Button variant="default" onClick={rollbackModal.close}>取消</Button>
                <Button color="red" leftSection={<IconArrowBackUp size={16} />} onClick={handleRollback}>
                  确认回滚
                </Button>
              </Group>
            </Stack>
          </Modal>
        </>
      )}
    </Stack>
  )
}

/** 编解码测试卡片：抽出供「编解码测试」子 tab 渲染（FR-59 拆 tab 后内容不变） */
function CodecTestCard(props: {
  info: SystemInfo | null
  codec: CodecTestResult | null
  codecError: string | null
  codecLoading: boolean
  copied: boolean
  onTest: (force: boolean) => void
  onCopy: () => void
}) {
  const { info, codec, codecError, codecLoading, copied, onTest, onCopy } = props
  return (
    <Card withBorder padding="md" radius="md">
      <Group justify="space-between" mb="sm">
        <Title order={4}>编解码器测试</Title>
        <Group gap="xs">
          <Button color="purple" onClick={() => onTest(false)} loading={codecLoading}>
            测试编解码器
          </Button>
          <Button
            variant="light"
            color="purple"
            leftSection={<IconRefresh size={16} />}
            onClick={() => onTest(true)}
            loading={codecLoading}
          >
            重新测试
          </Button>
          <Button
            variant="light"
            color={copied ? 'green' : 'gray'}
            leftSection={copied ? <IconCheck size={16} /> : <IconCopy size={16} />}
            onClick={onCopy}
            disabled={!info && !codec}
          >
            {copied ? '已复制' : '复制结果'}
          </Button>
        </Group>
      </Group>

        {codecError && (
          <Alert icon={<IconAlertCircle size={16} />} color="red" title="测试失败" mb="sm">
            {codecError}
          </Alert>
        )}

        {codec && !codec.ffmpeg_available && (
          <Alert icon={<IconAlertCircle size={16} />} color="yellow">
            ffmpeg 不可用，无法测试编解码器。
          </Alert>
        )}

        {codec && codec.ffmpeg_available && codec.tested_at && (
          <Text size="xs" c="dimmed" mb="xs">
            {codec.from_cache ? `结果来自缓存，实测于 ${codec.tested_at}` : `实测于 ${codec.tested_at}`}
          </Text>
        )}

        {codec && codec.ffmpeg_available && (
          <Box style={{ overflowX: 'auto' }}>
            <Table striped highlightOnHover withTableBorder miw={560}>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>编码器</Table.Th>
                  <Table.Th>编码格式</Table.Th>
                  <Table.Th>类别</Table.Th>
                  <Table.Th>已编入</Table.Th>
                  <Table.Th>试编码</Table.Th>
                  <Table.Th>详情</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {codec.results.map((r) => (
                  <Table.Tr key={r.encoder}>
                    <Table.Td><Code>{r.encoder}</Code></Table.Td>
                    <Table.Td>{r.codec}</Table.Td>
                    <Table.Td>{r.family}</Table.Td>
                    <Table.Td>
                      <Badge size="sm" variant="light" color={r.compiled ? 'green' : 'gray'}>
                        {r.compiled ? '是' : '否'}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <Badge size="sm" color={r.tested_ok ? 'green' : 'red'}>
                        {r.tested_ok ? '✓ 成功' : '✗ 失败'}
                      </Badge>
                    </Table.Td>
                    <Table.Td>
                      <Text size="xs" c="dimmed" lineClamp={2} style={{ maxWidth: 320, wordBreak: 'break-all' }}>
                        {r.detail || '—'}
                      </Text>
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Box>
        )}

      {!codec && !codecError && (
        <Text size="sm" c="dimmed">点击「测试编解码器」检测各编码器是否可用。</Text>
      )}
    </Card>
  )
}
