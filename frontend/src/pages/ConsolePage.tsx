import { useCallback } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Tabs } from '@mantine/core';
import {
  IconDeviceDesktop,
  IconCpu,
  IconTestPipe,
  IconCloudDownload,
  IconAdjustments,
} from '@tabler/icons-react';
import SystemPage from './SystemPage';
import { type SystemSection, SECTION_TITLES } from './SystemPage.helpers';
import SettingsPage from './SettingsPage';

// 一级 tab 取值（FR-113）：运行环境 / 硬件加速 / 编解码 / 应用更新 / 设置。
// 前四项渲染 SystemPage 对应区块，settings 渲染 SettingsPage。
const TAB_ENV = 'env';
const TAB_HWACCEL = 'hwaccel';
const TAB_CODEC = 'codec';
const TAB_UPDATE = 'update';
const TAB_SETTINGS = 'settings';
const ALL_TABS = [TAB_ENV, TAB_HWACCEL, TAB_CODEC, TAB_UPDATE, TAB_SETTINGS];
// 系统区块（除设置外四项），用于把一级 tab 值传给 SystemPage
const SYSTEM_SECTIONS: SystemSection[] = [TAB_ENV, TAB_HWACCEL, TAB_CODEC, TAB_UPDATE];

/**
 * resolveTab 把 URL query 归一为一级 tab key（FR-113 拍平两级 tab）。
 * 向后兼容旧深链：旧式 `?tab=system&sys=<x>` 中 `sys` 直接作一级 tab（env/hwaccel/codec/update，缺省 env），
 * `?tab=settings` → settings，`?tab=system` 无 sys → env，新式 `?tab=<key>` 直接采用。非法一律落回 env。
 */
function resolveTab(params: URLSearchParams): string {
  const tab = params.get('tab');
  // 旧式 tab=system：用 sys 子值映射到对应一级 tab
  if (tab === 'system') {
    const sys = params.get('sys');
    return sys && SYSTEM_SECTIONS.includes(sys as SystemSection) ? sys : TAB_ENV;
  }
  if (tab && ALL_TABS.includes(tab)) return tab;
  return TAB_ENV;
}

/**
 * 控制台页（FR-113）：去掉「系统信息 / 设置」两级 tab，拍平为一级 tab——
 * 运行环境 / 硬件加速 / 编解码 / 应用更新 / 设置。tab 状态由 URL query `?tab=` 控制，
 * 并兼容映射旧深链（`?tab=system&sys=update`、`?tab=settings` 等）。每个 tab 内由各页自带左侧锚点导航。
 */
export default function ConsolePage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const activeTab = resolveTab(searchParams);

  // 切 tab 时写回新式 `?tab=<key>` 并清掉遗留的 `sys`；replace 避免历史堆叠切换记录
  const handleTabChange = useCallback(
    (value: string | null) => {
      setSearchParams({ tab: value ?? TAB_ENV }, { replace: true });
    },
    [setSearchParams],
  );

  return (
    <Tabs className="console-tabs" value={activeTab} onChange={handleTabChange} keepMounted={false}>
      <Tabs.List mb="md">
        {/* tab 文案复用 SystemPage 的 SECTION_TITLES（FR-113 后续修复），与各区块内容标题单一真源、不再各写一份 */}
        <Tabs.Tab value={TAB_ENV} leftSection={<IconDeviceDesktop size={16} />}>
          {SECTION_TITLES[TAB_ENV]}
        </Tabs.Tab>
        <Tabs.Tab value={TAB_HWACCEL} leftSection={<IconCpu size={16} />}>
          {SECTION_TITLES[TAB_HWACCEL]}
        </Tabs.Tab>
        <Tabs.Tab value={TAB_CODEC} leftSection={<IconTestPipe size={16} />}>
          {SECTION_TITLES[TAB_CODEC]}
        </Tabs.Tab>
        <Tabs.Tab value={TAB_UPDATE} leftSection={<IconCloudDownload size={16} />}>
          {SECTION_TITLES[TAB_UPDATE]}
        </Tabs.Tab>
        <Tabs.Tab value={TAB_SETTINGS} leftSection={<IconAdjustments size={16} />}>
          设置
        </Tabs.Tab>
      </Tabs.List>

      <Tabs.Panel value={TAB_ENV}>
        <SystemPage section={TAB_ENV} />
      </Tabs.Panel>
      <Tabs.Panel value={TAB_HWACCEL}>
        <SystemPage section={TAB_HWACCEL} />
      </Tabs.Panel>
      <Tabs.Panel value={TAB_CODEC}>
        <SystemPage section={TAB_CODEC} />
      </Tabs.Panel>
      <Tabs.Panel value={TAB_UPDATE}>
        <SystemPage section={TAB_UPDATE} />
      </Tabs.Panel>
      <Tabs.Panel value={TAB_SETTINGS}>
        <SettingsPage />
      </Tabs.Panel>
    </Tabs>
  );
}
