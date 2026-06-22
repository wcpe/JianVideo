import { Group, SegmentedControl, NativeSelect, TextInput, Text } from '@mantine/core'

interface MediaQueryFiltersProps {
  mediaType: '' | 'image' | 'video'
  onMediaTypeChange: (v: '' | 'image' | 'video') => void
  sizeMin: number
  onSizeMinChange: (bytes: number) => void
  timeFrom: string
  onTimeFromChange: (v: string) => void
  timeTo: string
  onTimeToChange: (v: string) => void
}

// 最小大小预设（字节）
const SIZE_PRESETS = [
  { value: '0', label: '不限大小' },
  { value: String(1 << 20), label: '≥ 1MB' },
  { value: String(10 * (1 << 20)), label: '≥ 10MB' },
  { value: String(100 * (1 << 20)), label: '≥ 100MB' },
  { value: String(1 << 30), label: '≥ 1GB' },
]

/**
 * 媒体结构化筛选控件（FR-36，消费 FR-35 引擎）：类型 / 最小大小 / 拍摄时间范围。
 * 受控组件，状态由父页持有并传给 useInfiniteMedia。
 */
export default function MediaQueryFilters({
  mediaType, onMediaTypeChange, sizeMin, onSizeMinChange,
  timeFrom, onTimeFromChange, timeTo, onTimeToChange,
}: MediaQueryFiltersProps) {
  return (
    <Group gap="sm" wrap="wrap" align="center">
      <SegmentedControl
        aria-label="类型筛选"
        size="xs"
        value={mediaType || 'all'}
        onChange={(v) => onMediaTypeChange(v === 'all' ? '' : (v as 'image' | 'video'))}
        data={[
          { value: 'all', label: '全部' },
          { value: 'image', label: '图片' },
          { value: 'video', label: '视频' },
        ]}
      />
      <NativeSelect
        aria-label="最小大小"
        size="xs"
        data={SIZE_PRESETS}
        value={String(sizeMin)}
        onChange={(e) => onSizeMinChange(Number(e.currentTarget.value))}
      />
      <Group gap={4} align="center">
        <TextInput aria-label="起始日期" type="date" size="xs" value={timeFrom} onChange={(e) => onTimeFromChange(e.currentTarget.value)} />
        <Text size="xs" c="dimmed">至</Text>
        <TextInput aria-label="结束日期" type="date" size="xs" value={timeTo} onChange={(e) => onTimeToChange(e.currentTarget.value)} />
      </Group>
    </Group>
  )
}
