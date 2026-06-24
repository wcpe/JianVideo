import { useState, useEffect, useCallback } from 'react'
import {
  Stack, Title, Card, TextInput, Button, Alert, Skeleton, Text, Table, Badge, Group, Code, ActionIcon,
} from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconAlertCircle, IconTrash, IconPlus } from '@tabler/icons-react'
import {
  getSettings,
  updateSettings,
  SETTING_KEY_RECYCLE_BIN_PATHS,
  SETTING_KEY_SCAN_INTERVAL,
  SETTING_KEY_FFMPEG_PATH,
  SETTING_KEY_FFPROBE_PATH,
  SETTING_KEY_MAGICK_PATH,
  SETTING_KEY_NETWORK_PROXY,
} from '@/api/settings'
import { getEnvVars, detectFFmpeg } from '@/api/system'
import { extractErrorMessage } from '@/utils/error'
import {
  parseRecycleBinRows,
  validateRecycleBinRows,
  serializeRecycleBinRows,
  type RecycleBinRow,
} from '@/utils/recycle-bin'
import type { EnvVar } from '@/types'

/** 设置页（FR-24/FR-56/FR-63/FR-87）：按扫描/网络/工具路径/回收站分区读写运行期键值设置，并只读查看环境变量 */
export default function SettingsPage() {
  const [scanInterval, setScanInterval] = useState('')
  // 回收站路径以结构化「盘符→路径」行列表编辑（FR-87），保存时序列化为 JSON 串
  const [recycleBinRows, setRecycleBinRows] = useState<RecycleBinRow[]>([])
  const [ffmpegPath, setFfmpegPath] = useState('')
  const [ffprobePath, setFfprobePath] = useState('')
  const [magickPath, setMagickPath] = useState('')
  const [networkProxy, setNetworkProxy] = useState('')
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
        setRecycleBinRows(parseRecycleBinRows(data[SETTING_KEY_RECYCLE_BIN_PATHS] ?? ''))
        setFfmpegPath(data[SETTING_KEY_FFMPEG_PATH] ?? '')
        setFfprobePath(data[SETTING_KEY_FFPROBE_PATH] ?? '')
        setMagickPath(data[SETTING_KEY_MAGICK_PATH] ?? '')
        setNetworkProxy(data[SETTING_KEY_NETWORK_PROXY] ?? '')
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

  // 回收站行编辑器操作
  const addRecycleRow = useCallback(() => {
    setRecycleBinRows((rows) => [...rows, { drive: '', path: '' }])
  }, [])
  const removeRecycleRow = useCallback((index: number) => {
    setRecycleBinRows((rows) => rows.filter((_, i) => i !== index))
  }, [])
  const updateRecycleRow = useCallback((index: number, patch: Partial<RecycleBinRow>) => {
    setRecycleBinRows((rows) => rows.map((row, i) => (i === index ? { ...row, ...patch } : row)))
  }, [])

  // 回收站行校验结果，驱动行内错误展示与提交拦截
  const recycleValidation = validateRecycleBinRows(recycleBinRows)

  const handleSave = useCallback(async () => {
    // 回收站行非法（空盘符/重复盘符）时行内提示并阻止提交
    const validation = validateRecycleBinRows(recycleBinRows)
    if (!validation.valid) {
      notifications.show({
        title: '回收站路径有误',
        message: '请修正盘符为空或重复的行后再保存',
        color: 'red',
        autoClose: 4000,
      })
      return
    }
    setSaving(true)
    try {
      const updated = await updateSettings({
        [SETTING_KEY_SCAN_INTERVAL]: scanInterval,
        [SETTING_KEY_RECYCLE_BIN_PATHS]: serializeRecycleBinRows(recycleBinRows),
        [SETTING_KEY_FFMPEG_PATH]: ffmpegPath,
        [SETTING_KEY_FFPROBE_PATH]: ffprobePath,
        [SETTING_KEY_MAGICK_PATH]: magickPath,
        [SETTING_KEY_NETWORK_PROXY]: networkProxy,
      })
      // 以回读结果刷新输入框，确保展示与持久化一致
      setScanInterval(updated[SETTING_KEY_SCAN_INTERVAL] ?? '')
      setRecycleBinRows(parseRecycleBinRows(updated[SETTING_KEY_RECYCLE_BIN_PATHS] ?? ''))
      setFfmpegPath(updated[SETTING_KEY_FFMPEG_PATH] ?? '')
      setFfprobePath(updated[SETTING_KEY_FFPROBE_PATH] ?? '')
      setMagickPath(updated[SETTING_KEY_MAGICK_PATH] ?? '')
      setNetworkProxy(updated[SETTING_KEY_NETWORK_PROXY] ?? '')
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
  }, [scanInterval, recycleBinRows, ffmpegPath, ffprobePath, magickPath, networkProxy])

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
        <Skeleton height={400} radius="md" />
      ) : (
        <>
          {/* 扫描分区（FR-24）：定时扫描周期 */}
          <Title order={3}>扫描</Title>
          <Card withBorder padding="md" radius="md">
            <TextInput
              label="扫描周期（秒）"
              description="定时扫描的间隔周期，供后续定时扫描能力消费"
              value={scanInterval}
              onChange={(e) => setScanInterval(e.currentTarget.value)}
            />
          </Card>

          {/* 网络分区（FR-80）：后端出站网络代理，空=直连；随「保存设置」一并保存、保存即生效 */}
          <Title order={3}>网络</Title>
          <Card withBorder padding="md" radius="md">
            <TextInput
              label="网络代理"
              description="用于自更新等后端外部网络访问；留空则直连。支持 http/https/socks5"
              placeholder="如 http://host:port 或 socks5://host:port"
              value={networkProxy}
              onChange={(e) => setNetworkProxy(e.currentTarget.value)}
            />
          </Card>

          {/* 工具路径分区（FR-56/FR-63）：ffmpeg/ffprobe/magick 可配置，随「保存设置」一并保存、保存即生效 */}
          <Title order={3}>工具路径</Title>
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
              {/* Magick 路径（FR-63）：HEIC/RAW 转 JPEG 用 */}
              <TextInput
                label="Magick 路径"
                description="ImageMagick magick 可执行文件路径，用于 HEIC/RAW 转 JPEG；留空则按环境变量→同目录捆绑版→PATH 自动发现"
                placeholder="如 D:/tools/magick.exe"
                value={magickPath}
                onChange={(e) => setMagickPath(e.currentTarget.value)}
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
                先「检测」验证路径可用，再用下方「保存设置」持久化；保存后即时生效，无需重启。
              </Text>
            </Stack>
          </Card>

          {/* 回收站分区（FR-87）：结构化「盘符→路径」行列表编辑，序列化为 recycle_bin_paths JSON 串 */}
          <Title order={3}>回收站</Title>
          <Card withBorder padding="md" radius="md">
            <Stack gap="sm">
              <Text size="xs" c="dimmed">
                为各盘符配置回收站目录；删除媒体时按所在盘符移入对应目录。盘符不可为空、不可重复。
              </Text>
              {recycleBinRows.map((row, i) => (
                <Group key={i} gap="sm" align="flex-start" wrap="nowrap">
                  <TextInput
                    aria-label={`盘符 ${i + 1}`}
                    placeholder="盘符，如 D"
                    style={{ width: 120 }}
                    value={row.drive}
                    error={recycleValidation.rowErrors[i] ?? undefined}
                    onChange={(e) => updateRecycleRow(i, { drive: e.currentTarget.value })}
                  />
                  <TextInput
                    aria-label={`回收站路径 ${i + 1}`}
                    placeholder="回收站目录，如 D:/.recycle"
                    style={{ flex: 1 }}
                    value={row.path}
                    onChange={(e) => updateRecycleRow(i, { path: e.currentTarget.value })}
                  />
                  <ActionIcon
                    variant="subtle"
                    color="red"
                    aria-label={`删除盘符 ${i + 1}`}
                    onClick={() => removeRecycleRow(i)}
                    mt={4}
                  >
                    <IconTrash size={16} />
                  </ActionIcon>
                </Group>
              ))}
              <Button
                variant="default"
                leftSection={<IconPlus size={16} />}
                onClick={addRecycleRow}
                style={{ alignSelf: 'flex-start' }}
              >
                添加盘符
              </Button>
            </Stack>
          </Card>

          <Text size="xs" c="dimmed">
            设置保存到数据库，运行期可改、重启后保留。
          </Text>
          <Button color="purple" onClick={handleSave} loading={saving} style={{ alignSelf: 'flex-start' }}>
            保存设置
          </Button>
        </>
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
            {/* JWT_SECRET 未设警示（FR-87）：未设置时启动随机生成、每次重启需重新登录 */}
            {envVars.some((ev) => ev.key === 'JWT_SECRET' && !ev.set) && (
              <Alert icon={<IconAlertCircle size={16} />} color="yellow" title="会话密钥未持久化">
                未设置 JWT_SECRET，启动时会随机生成会话签名密钥，导致每次重启后所有用户需重新登录。建议在启动环境中设置 JWT_SECRET 环境变量以持久化会话。
              </Alert>
            )}
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
