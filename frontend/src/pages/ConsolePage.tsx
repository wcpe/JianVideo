import { useSearchParams } from 'react-router-dom'
import { Tabs } from '@mantine/core'
import { IconDeviceDesktopAnalytics, IconAdjustments } from '@tabler/icons-react'
import SystemPage from './SystemPage'
import SettingsPage from './SettingsPage'

// tab 取值与缺省：系统信息 / 设置，URL query 缺省或非法均落回系统信息
const TAB_SYSTEM = 'system'
const TAB_SETTINGS = 'settings'

/**
 * 控制台页（FR-55）：把「系统信息」与「设置」合并为单页两 tab（参考宝塔面板）。
 * 两 tab 分别原样承载 SystemPage / SettingsPage，行为不变；tab 状态由 URL query
 * `?tab=system|settings` 控制，便于深链与后续页眉跳转（FR-58）。
 */
export default function ConsolePage() {
  const [searchParams, setSearchParams] = useSearchParams()
  // 仅识别 settings，其余（缺省/非法）一律视作系统信息
  const activeTab = searchParams.get('tab') === TAB_SETTINGS ? TAB_SETTINGS : TAB_SYSTEM

  // 切 tab 时同步更新 query，replace 避免在历史里堆叠切换记录
  const handleTabChange = (value: string | null) => {
    setSearchParams({ tab: value ?? TAB_SYSTEM }, { replace: true })
  }

  return (
    <Tabs value={activeTab} onChange={handleTabChange} keepMounted={false}>
      <Tabs.List mb="md">
        <Tabs.Tab value={TAB_SYSTEM} leftSection={<IconDeviceDesktopAnalytics size={16} />}>
          系统信息
        </Tabs.Tab>
        <Tabs.Tab value={TAB_SETTINGS} leftSection={<IconAdjustments size={16} />}>
          设置
        </Tabs.Tab>
      </Tabs.List>

      <Tabs.Panel value={TAB_SYSTEM}>
        <SystemPage />
      </Tabs.Panel>
      <Tabs.Panel value={TAB_SETTINGS}>
        <SettingsPage />
      </Tabs.Panel>
    </Tabs>
  )
}
