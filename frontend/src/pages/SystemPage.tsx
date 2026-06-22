import { useState, useEffect, useCallback } from 'react'
import {
  Stack, Title, Card, Text, Group, Badge, Button, Alert, Skeleton, Table, SimpleGrid, Code, Box, SegmentedControl,
} from '@mantine/core'
import { useClipboard } from '@mantine/hooks'
import { IconAlertCircle, IconCopy, IconCheck, IconRefresh, IconDownload, IconArrowBackUp } from '@tabler/icons-react'
import * as systemApi from '@/api/system'
import { getSettings, updateSettings, SETTING_KEY_UPDATE_CHANNEL } from '@/api/settings'
import { extractErrorMessage } from '@/utils/error'
import type { SystemInfo, CodecTestResult, UpdateCheckResult } from '@/types'

/** 单行信息项：左侧标签 + 右侧值 */
function InfoRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <Group justify="space-between" gap="sm" wrap="nowrap">
      <Text size="sm" c="dimmed">{label}</Text>
      <Text size="sm" ta="right" style={{ wordBreak: 'break-all' }}>{value}</Text>
    </Group>
  )
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
    lines.push(`FFmpeg 可用: ${info.ffmpeg.available ? '是' : '否'}`)
    if (info.ffmpeg.available) {
      lines.push(`FFmpeg 路径: ${info.ffmpeg.path}`)
      lines.push(`FFmpeg 版本: ${info.ffmpeg.version}`)
    }
    lines.push(`首选编码器: ${info.hwaccel.preferred || '无'}`)
    lines.push(`H.264 支持: ${info.hwaccel.h264_supported ? '是' : '否'}`)
    lines.push(`H.265 支持: ${info.hwaccel.h265_supported ? '是' : '否'}`)
    lines.push(`软件回退: ${info.hwaccel.software_fallback ? '是' : '否'}`)
    for (const cap of info.hwaccel.available) {
      lines.push(`硬件加速: ${cap.name} (${cap.device_type}) -> ${cap.h264_encoder} / ${cap.h265_encoder} [${cap.available ? '可用' : '不可用'}]`)
    }
  }
  if (codec) {
    lines.push('')
    lines.push('----- 编解码器测试 -----')
    if (!codec.ffmpeg_available) {
      lines.push('ffmpeg 不可用')
    } else {
      for (const r of codec.results) {
        const status = r.tested_ok ? '成功' : '失败'
        lines.push(`${r.encoder} [${r.family}/${r.codec}] 编入:${r.compiled ? '是' : '否'} 试编码:${status}${r.detail ? ` 详情:${r.detail}` : ''}`)
      }
    }
  }
  return lines.join('\n')
}

/** 系统诊断页：展示运行环境、FFmpeg/硬件加速信息，并支持编解码器测试 */
export default function SystemPage() {
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

  const handleCodecTest = useCallback(async () => {
    setCodecLoading(true)
    setCodecError(null)
    try {
      setCodec(await systemApi.runCodecTest())
    } catch (err) {
      setCodecError(extractErrorMessage(err, '编解码器测试失败'))
    } finally {
      setCodecLoading(false)
    }
  }, [])

  const handleCopy = useCallback(() => {
    clipboard.copy(buildReport(info, codec))
  }, [clipboard, info, codec])

  const handleCheckUpdate = useCallback(async () => {
    setUpdateChecking(true)
    setUpdateError(null)
    setUpdateInfo(null)
    try {
      setUpdateInfo(await systemApi.checkUpdate(channel))
    } catch (err) {
      setUpdateError(extractErrorMessage(err, '检查更新失败'))
    } finally {
      setUpdateChecking(false)
    }
  }, [channel])

  // 切换并持久化更新频道（正式版/测试版），清空旧检查结果
  const handleChannelChange = useCallback((v: string) => {
    const ch: 'stable' | 'prerelease' = v === 'prerelease' ? 'prerelease' : 'stable'
    setChannel(ch)
    setUpdateInfo(null)
    setUpdateError(null)
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

  const handleApplyUpdate = useCallback(async () => {
    if (!window.confirm(`确定更新到 ${updateInfo?.tag}？更新后服务将自动重启。`)) return
    setUpdateBusy(true)
    setUpdateError(null)
    try {
      await systemApi.applyUpdate(channel)
      await waitForRestart('更新')
    } catch (err) {
      setUpdateError(extractErrorMessage(err, '更新失败'))
      setUpdateBusy(false)
    }
  }, [channel, updateInfo, waitForRestart])

  const handleRollback = useCallback(async () => {
    if (!window.confirm('确定回滚到上一版本？服务将自动重启。')) return
    setUpdateBusy(true)
    setUpdateError(null)
    try {
      await systemApi.rollbackUpdate()
      await waitForRestart('回滚')
    } catch (err) {
      setUpdateError(extractErrorMessage(err, '回滚失败'))
      setUpdateBusy(false)
    }
  }, [waitForRestart])

  return (
    <Stack gap="md">
      <Title order={2}>系统诊断</Title>

      {infoError && (
        <Alert icon={<IconAlertCircle size={16} />} color="red" title="加载失败">
          {infoError}
        </Alert>
      )}

      {loading ? (
        <Skeleton height={320} radius="md" />
      ) : info ? (
        <>
          {/* 运行环境 */}
          <Card withBorder padding="md" radius="md">
            <Title order={4} mb="sm">运行环境</Title>
            <SimpleGrid cols={{ base: 1, sm: 2 }} spacing="xs">
              <InfoRow label="应用版本" value={info.app_version} />
              <InfoRow label="操作系统" value={`${info.os}/${info.arch}`} />
              <InfoRow label="CPU 核心数" value={info.num_cpu} />
              <InfoRow label="主机名" value={info.hostname} />
              <InfoRow label="Go 版本" value={info.go_version} />
            </SimpleGrid>
          </Card>

          {/* FFmpeg */}
          <Card withBorder padding="md" radius="md">
            <Group justify="space-between" mb="sm">
              <Title order={4}>FFmpeg</Title>
              <Badge color={info.ffmpeg.available ? 'green' : 'red'}>
                {info.ffmpeg.available ? '可用' : '不可用'}
              </Badge>
            </Group>
            {info.ffmpeg.available ? (
              <Stack gap="xs">
                <InfoRow label="路径" value={info.ffmpeg.path} />
                <InfoRow label="版本" value={info.ffmpeg.version} />
              </Stack>
            ) : (
              <Text size="sm" c="dimmed">未检测到 FFmpeg，转码相关功能不可用。</Text>
            )}
          </Card>

          {/* 硬件加速 */}
          <Card withBorder padding="md" radius="md">
            <Title order={4} mb="sm">硬件加速</Title>
            <Stack gap="xs">
              <InfoRow label="首选编码器" value={info.hwaccel.preferred || '无（使用软件编码）'} />
              <Group gap="xs">
                <Badge variant="light" color={info.hwaccel.h264_supported ? 'green' : 'gray'}>
                  H.264 {info.hwaccel.h264_supported ? '支持' : '不支持'}
                </Badge>
                <Badge variant="light" color={info.hwaccel.h265_supported ? 'green' : 'gray'}>
                  H.265 {info.hwaccel.h265_supported ? '支持' : '不支持'}
                </Badge>
                {info.hwaccel.software_fallback && (
                  <Badge variant="light" color="yellow">软件回退</Badge>
                )}
                {info.hwaccel.intel_gpu && (
                  <Badge variant="light" color="blue">Intel GPU</Badge>
                )}
              </Group>
              {info.hwaccel.available.length === 0 ? (
                <Text size="sm" c="dimmed">未检测到可用的硬件加速。</Text>
              ) : (
                info.hwaccel.available.map((cap) => (
                  <Card key={cap.name} withBorder padding="xs" radius="sm" bg="var(--mantine-color-default-hover)">
                    <Group justify="space-between" mb={4}>
                      <Text size="sm" fw={600}>{cap.name}</Text>
                      <Badge size="sm" color={cap.available ? 'green' : 'red'}>
                        {cap.available ? '可用' : '不可用'}
                      </Badge>
                    </Group>
                    <Text size="xs" c="dimmed">
                      设备类型 {cap.device_type}；H.264 编码器 {cap.h264_encoder || '无'}；H.265 编码器 {cap.h265_encoder || '无'}
                    </Text>
                  </Card>
                ))
              )}
            </Stack>
          </Card>
        </>
      ) : null}

      {/* 应用更新（FR-46）*/}
      <Card withBorder padding="md" radius="md">
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
            <Button
              variant="light"
              color="purple"
              leftSection={<IconRefresh size={16} />}
              onClick={handleCheckUpdate}
              loading={updateChecking}
              disabled={updateBusy}
            >
              检查更新
            </Button>
          </Group>
        </Group>

        {updateError && (
          <Alert icon={<IconAlertCircle size={16} />} color="red" title="更新出错" mb="sm">
            {updateError}
          </Alert>
        )}
        {restartMsg && <Alert color="blue" mb="sm">{restartMsg}</Alert>}

        {updateInfo ? (
          <Stack gap="xs">
            <InfoRow label="当前版本" value={updateInfo.current} />
            <InfoRow label="最新版本" value={updateInfo.latest || '—'} />
            <Group gap="xs">
              {updateInfo.has_update
                ? <Badge color="orange">有可用更新</Badge>
                : <Badge color="green">已是最新</Badge>}
              {updateInfo.prerelease && <Badge variant="light" color="grape">预发布</Badge>}
            </Group>
            {updateInfo.notes && (
              <Box style={{ maxHeight: 160, overflowY: 'auto' }}>
                <Text size="xs" c="dimmed" style={{ whiteSpace: 'pre-wrap' }}>{updateInfo.notes}</Text>
              </Box>
            )}
            <Group gap="xs">
              <Button
                color="purple"
                leftSection={<IconDownload size={16} />}
                onClick={handleApplyUpdate}
                loading={updateBusy}
                disabled={!updateInfo.has_update}
              >
                立即更新并重启
              </Button>
              <Button
                variant="default"
                leftSection={<IconArrowBackUp size={16} />}
                onClick={handleRollback}
                disabled={updateBusy}
              >
                回滚到上一版
              </Button>
            </Group>
          </Stack>
        ) : (
          !updateError && (
            <Text size="sm" c="dimmed">选择频道后点击「检查更新」，从 GitHub Releases 检测新版本。</Text>
          )
        )}
      </Card>

      {/* 编解码器测试 */}
      <Card withBorder padding="md" radius="md">
        <Group justify="space-between" mb="sm">
          <Title order={4}>编解码器测试</Title>
          <Group gap="xs">
            <Button color="purple" onClick={handleCodecTest} loading={codecLoading}>
              测试编解码器
            </Button>
            <Button
              variant="light"
              color={clipboard.copied ? 'green' : 'gray'}
              leftSection={clipboard.copied ? <IconCheck size={16} /> : <IconCopy size={16} />}
              onClick={handleCopy}
              disabled={!info && !codec}
            >
              {clipboard.copied ? '已复制' : '复制结果'}
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
    </Stack>
  )
}
