import { useState, useEffect, useCallback } from 'react'
import {
  Stack, Title, Card, TextInput, Textarea, Button, Alert, Skeleton, Text, Table, Badge, Group, Code,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconAlertCircle } from '@tabler/icons-react'
import {
  getSettings,
  updateSettings,
  SETTING_KEY_RECYCLE_BIN_PATHS,
  SETTING_KEY_SCAN_INTERVAL,
  SETTING_KEY_FFMPEG_PATH,
  SETTING_KEY_FFPROBE_PATH,
} from '@/api/settings'
import { getEnvVars, detectFFmpeg } from '@/api/system'
import { extractErrorMessage } from '@/utils/error'
import type { EnvVar } from '@/types'

/** 设置页（FR-24/FR-56）：读写运行期键值设置（扫描周期、回收站路径、ffmpeg 路径），并只读查看环境变量 */
export default function SettingsPage() {
  const [scanInterval, setScanInterval] = useState('')
  const [recycleBinPaths, setRecycleBinPaths] = useState('')
  const [ffmpegPath, setFfmpegPath] = useState('')
  const [ffprobePath, setFfprobePath] = useState('')
  const [loadError, setLoadError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  // 环境变量（FR-56，只读）
  const [envVars, setEnvVars] = useState<EnvVar[]>([])
  const [envError, setEnvError] = useState<string | null>(null)
  const [envLoading, setEnvLoading] = useState(true)

  // FFmpeg 路径检测（FR-56）
  const [detecting, setDetecting] = useState(false)
  const [detectResult, setDetectResult] = useState<{ available: boolean; version: string } | null>(null)

  // 挂载时加载现有设置
  useEffect(() => {
    let active = true
    setLoading(true)
    setLoadError(null)
    getSettings()
      .then((data) => {
        if (!active) return
        setScanInterval(data[SETTING_KEY_SCAN_INTERVAL] ?? '')
        setRecycleBinPaths(data[SETTING_KEY_RECYCLE_BIN_PATHS] ?? '')
        setFfmpegPath(data[SETTING_KEY_FFMPEG_PATH] ?? '')
        setFfprobePath(data[SETTING_KEY_FFPROBE_PATH] ?? '')
      })
      .catch((err) => { if (active) setLoadError(extractErrorMessage(err, '加载设置失败')) })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [])

  // 挂载时加载环境变量（只读）
  useEffect(() => {
    let active = true
    setEnvLoading(true)
    setEnvError(null)
    getEnvVars()
      .then((data) => { if (active) setEnvVars(data) })
      .catch((err) => { if (active) setEnvError(extractErrorMessage(err, '加载环境变量失败')) })
      .finally(() => { if (active) setEnvLoading(false) })
    return () => { active = false }
  }, [])

  const handleSave = useCallback(async () => {
    setSaving(true)
    try {
      const updated = await updateSettings({
        [SETTING_KEY_SCAN_INTERVAL]: scanInterval,
        [SETTING_KEY_RECYCLE_BIN_PATHS]: recycleBinPaths,
        [SETTING_KEY_FFMPEG_PATH]: ffmpegPath,
        [SETTING_KEY_FFPROBE_PATH]: ffprobePath,
      })
      // 以回读结果刷新输入框，确保展示与持久化一致
      setScanInterval(updated[SETTING_KEY_SCAN_INTERVAL] ?? '')
      setRecycleBinPaths(updated[SETTING_KEY_RECYCLE_BIN_PATHS] ?? '')
      setFfmpegPath(updated[SETTING_KEY_FFMPEG_PATH] ?? '')
      setFfprobePath(updated[SETTING_KEY_FFPROBE_PATH] ?? '')
      notifications.show({ title: '保存成功', message: '设置已保存', color: 'green', autoClose: 3000 })
    } catch (err) {
      notifications.show({
        title: '保存失败',
        message: extractErrorMessage(err, '保存设置失败'),
        color: 'red',
        autoClose: 4000,
      })
    } finally {
      setSaving(false)
    }
  }, [scanInterval, recycleBinPaths, ffmpegPath, ffprobePath])

  // 检测当前输入的 ffmpeg 路径是否可用（保存前先验）
  const handleDetect = useCallback(async () => {
    setDetecting(true)
    setDetectResult(null)
    try {
      const res = await detectFFmpeg(ffmpegPath)
      setDetectResult({ available: res.ffmpeg_available, version: res.ffmpeg_version })
    } catch (err) {
      notifications.show({
        title: '检测失败',
        message: extractErrorMessage(err, '检测 FFmpeg 路径失败'),
        color: 'red',
        autoClose: 4000,
      })
    } finally {
      setDetecting(false)
    }
  }, [ffmpegPath])

  return (
    <Stack gap="md">
      <Title order={2}>设置</Title>

      {loadError && (
        <Alert icon={<IconAlertCircle size={16} />} color="red" title="加载失败">
          {loadError}
        </Alert>
      )}

      {loading ? (
        <Skeleton height={240} radius="md" />
      ) : (
        <Card withBorder padding="md" radius="md">
          <Stack gap="md">
            <TextInput
              label="扫描周期（秒）"
              description="定时扫描的间隔周期，供后续定时扫描能力消费"
              value={scanInterval}
              onChange={(e) => setScanInterval(e.currentTarget.value)}
            />
            <Textarea
              label="每盘符回收站路径（JSON）"
              description='以 JSON 映射配置各盘符回收站目录，如 {"D":"D:/.recycle"}'
              value={recycleBinPaths}
              onChange={(e) => setRecycleBinPaths(e.currentTarget.value)}
              autosize
              minRows={2}
            />
            <Text size="xs" c="dimmed">
              设置保存到数据库，运行期可改、重启后保留。
            </Text>
            <Button color="purple" onClick={handleSave} loading={saving} style={{ alignSelf: 'flex-start' }}>
              保存设置
            </Button>
          </Stack>
        </Card>
      )}

      {/* FFmpeg 路径（FR-56）：可配置 + 检测，随上方「保存设置」一并保存 */}
      <Title order={3}>FFmpeg 路径</Title>
      {loading ? (
        <Skeleton height={180} radius="md" />
      ) : (
        <Card withBorder padding="md" radius="md">
          <Stack gap="md">
            <TextInput
              label="FFmpeg 路径"
              description="ffmpeg 可执行文件路径；留空则按环境变量→同目录捆绑版→PATH 自动发现"
              placeholder="如 D:/tools/ffmpeg.exe"
              value={ffmpegPath}
              onChange={(e) => { setFfmpegPath(e.currentTarget.value); setDetectResult(null) }}
            />
            <TextInput
              label="FFprobe 路径"
              description="ffprobe 可执行文件路径；留空则自动发现"
              placeholder="如 D:/tools/ffprobe.exe"
              value={ffprobePath}
              onChange={(e) => setFfprobePath(e.currentTarget.value)}
            />
            <Group gap="sm">
              <Button variant="default" onClick={handleDetect} loading={detecting}>
                检测
              </Button>
              {detectResult && (
                detectResult.available ? (
                  <Badge color="green">可用：{detectResult.version}</Badge>
                ) : (
                  <Badge color="red">不可用</Badge>
                )
              )}
            </Group>
            <Text size="xs" c="dimmed">
              先「检测」验证路径可用，再用上方「保存设置」持久化；保存后即时生效，无需重启。
            </Text>
          </Stack>
        </Card>
      )}

      {/* 环境变量（FR-56）：只读查看，敏感项脱敏 */}
      <Title order={3}>环境变量</Title>
      {envError && (
        <Alert icon={<IconAlertCircle size={16} />} color="red" title="加载失败">
          {envError}
        </Alert>
      )}
      {envLoading ? (
        <Skeleton height={200} radius="md" />
      ) : (
        <Card withBorder padding="md" radius="md">
          <Stack gap="sm">
            <Text size="xs" c="dimmed">
              环境变量为进程级，仅供查看；如需修改请调整启动配置后重启。敏感项不显示明文。
            </Text>
            <Table striped withTableBorder>
              <Table.Thead>
                <Table.Tr>
                  <Table.Th>变量名</Table.Th>
                  <Table.Th>用途</Table.Th>
                  <Table.Th>值</Table.Th>
                </Table.Tr>
              </Table.Thead>
              <Table.Tbody>
                {envVars.map((ev) => (
                  <Table.Tr key={ev.key}>
                    <Table.Td>
                      <Code>{ev.key}</Code>
                    </Table.Td>
                    <Table.Td>
                      <Text size="sm">{ev.description}</Text>
                    </Table.Td>
                    <Table.Td>
                      {ev.sensitive ? (
                        <Badge color={ev.set ? 'gray' : 'dark'} variant="light">{ev.value}</Badge>
                      ) : ev.value ? (
                        <Code>{ev.value}</Code>
                      ) : (
                        <Text size="sm" c="dimmed">（未设置）</Text>
                      )}
                    </Table.Td>
                  </Table.Tr>
                ))}
              </Table.Tbody>
            </Table>
          </Stack>
        </Card>
      )}
    </Stack>
  )
}
