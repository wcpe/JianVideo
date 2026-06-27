import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button, Indicator, Tooltip } from '@mantine/core'
import { IconArrowUpCircle } from '@tabler/icons-react'

import { checkUpdate } from '@/api/system'
import { getSettings, SETTING_KEY_UPDATE_CHANNEL } from '@/api/settings'

// 点击指示器跳转目标：控制台「应用更新」一级 tab（FR-113 拍平为一级 tab）。
// ConsolePage 读 query `?tab=update` 直接选中应用更新 tab，精确定位；旧式 `?tab=system&sys=update` 仍兼容。
const UPDATE_ROUTE = '/system?tab=update'

/**
 * 页眉「更新可用」提示（FR-58）。
 * 挂载时按持久化频道调一次 FR-46 的更新检查端点（非 force，命中后端 TTL 缓存即廉价返回），
 * 仅当结果为「有更新」时常驻展示提示；点击精确跳转到系统信息 tab 的「应用更新」子 tab。
 * 检查失败 / 无更新一律不展示，失败静默（复用 FR-46 优雅降级语义），不打扰、不阻塞页眉其余渲染。
 */
export default function UpdateIndicator() {
  const navigate = useNavigate()
  // 目标版本号；为空表示无更新或尚未检测出更新，此时不展示指示器
  const [latestVersion, setLatestVersion] = useState('')

  useEffect(() => {
    let active = true
    const check = async () => {
      // 频道沿用持久化设置（与 SystemPage 一致），读失败回退 stable
      let channel = 'stable'
      try {
        const settings = await getSettings()
        const ch = settings[SETTING_KEY_UPDATE_CHANNEL]
        if (ch === 'prerelease' || ch === 'stable') channel = ch
      } catch {
        // 读设置失败用默认 stable，不影响后续检查
      }
      try {
        // 非 force：命中后端 10 分钟 TTL 缓存即廉价返回，不重复请求 GitHub
        const res = await checkUpdate(channel, false)
        if (active && res.has_update) setLatestVersion(res.latest || res.tag)
      } catch {
        // 检查失败静默：页眉不显提示，不报错、不影响其余布局（FR-46 优雅降级语义）
      }
    }
    check()
    return () => { active = false }
  }, [])

  // 无可用更新时不渲染，保持页眉简洁
  if (!latestVersion) return null

  return (
    <Tooltip label="有新版本可更新" position="bottom" withArrow>
      <Indicator size={8} color="orange" offset={4} processing>
        <Button
          variant="subtle"
          color="orange"
          size="compact-sm"
          leftSection={<IconArrowUpCircle size={16} />}
          onClick={() => navigate(UPDATE_ROUTE)}
          aria-label={`更新可用：${latestVersion}`}
        >
          {latestVersion} 可更新
        </Button>
      </Indicator>
    </Tooltip>
  )
}
