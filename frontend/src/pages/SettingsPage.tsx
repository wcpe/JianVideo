import { useState, useEffect, useCallback } from 'react'
import { Stack, Title, Card, TextInput, Textarea, Button, Alert, Skeleton, Text } from '@mantine/core'
import { notifications } from '@mantine/notifications'
import { IconAlertCircle } from '@tabler/icons-react'
import {
  getSettings,
  updateSettings,
  SETTING_KEY_RECYCLE_BIN_PATHS,
  SETTING_KEY_SCAN_INTERVAL,
} from '@/api/settings'
import { extractErrorMessage } from '@/utils/error'

/** 设置页（FR-24）：读写运行期键值设置，如扫描周期、每盘符回收站路径 */
export default function SettingsPage() {
  const [scanInterval, setScanInterval] = useState('')
  const [recycleBinPaths, setRecycleBinPaths] = useState('')
  const [loadError, setLoadError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

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
      })
      .catch((err) => { if (active) setLoadError(extractErrorMessage(err, '加载设置失败')) })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [])

  const handleSave = useCallback(async () => {
    setSaving(true)
    try {
      const updated = await updateSettings({
        [SETTING_KEY_SCAN_INTERVAL]: scanInterval,
        [SETTING_KEY_RECYCLE_BIN_PATHS]: recycleBinPaths,
      })
      // 以回读结果刷新输入框，确保展示与持久化一致
      setScanInterval(updated[SETTING_KEY_SCAN_INTERVAL] ?? '')
      setRecycleBinPaths(updated[SETTING_KEY_RECYCLE_BIN_PATHS] ?? '')
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
  }, [scanInterval, recycleBinPaths])

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
    </Stack>
  )
}
