import { Group, SegmentedControl, NativeSelect, Text } from '@mantine/core';
import { DatePickerInput } from '@mantine/dates';
import { strToDate, dateToStr } from '@/components/MediaQueryFilters.helpers';

interface MediaQueryFiltersProps {
  mediaType: '' | 'image' | 'video';
  onMediaTypeChange: (v: '' | 'image' | 'video') => void;
  sizeMin: number;
  onSizeMinChange: (bytes: number) => void;
  timeFrom: string;
  onTimeFromChange: (v: string) => void;
  timeTo: string;
  onTimeToChange: (v: string) => void;
  /** 最短时长（秒），0 表示不限；FR2-046 */
  durationMin?: number;
  onDurationMinChange?: (seconds: number) => void;
  /** 最小高度（像素），0 表示不限；FR2-046 分辨率下界 */
  heightMin?: number;
  onHeightMinChange?: (pixels: number) => void;
}

// 最小大小预设（字节）
const SIZE_PRESETS = [
  { value: '0', label: '不限大小' },
  { value: String(1 << 20), label: '≥ 1MB' },
  { value: String(10 * (1 << 20)), label: '≥ 10MB' },
  { value: String(100 * (1 << 20)), label: '≥ 100MB' },
  { value: String(1 << 30), label: '≥ 1GB' },
];

// 最短时长预设（秒，FR2-046）
const DURATION_PRESETS = [
  { value: '0', label: '不限时长' },
  { value: '60', label: '≥ 1 分钟' },
  { value: '600', label: '≥ 10 分钟' },
  { value: '3600', label: '≥ 1 小时' },
];

// 最小高度预设（像素，FR2-046）
const HEIGHT_PRESETS = [
  { value: '0', label: '不限分辨率' },
  { value: '720', label: '≥ 720p' },
  { value: '1080', label: '≥ 1080p' },
  { value: '2160', label: '≥ 4K' },
];

/**
 * 媒体结构化筛选控件（FR-36 + FR2-046）：类型 / 大小 / 时长 / 分辨率 / 拍摄时间。
 * 受控组件，状态由父页持有并传给 useInfiniteMedia。
 */
export default function MediaQueryFilters({
  mediaType,
  onMediaTypeChange,
  sizeMin,
  onSizeMinChange,
  timeFrom,
  onTimeFromChange,
  timeTo,
  onTimeToChange,
  durationMin = 0,
  onDurationMinChange,
  heightMin = 0,
  onHeightMinChange,
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
      {onDurationMinChange ? (
        <NativeSelect
          aria-label="最短时长"
          size="xs"
          data={DURATION_PRESETS}
          value={String(durationMin)}
          onChange={(e) => onDurationMinChange(Number(e.currentTarget.value))}
        />
      ) : null}
      {onHeightMinChange ? (
        <NativeSelect
          aria-label="最小分辨率"
          size="xs"
          data={HEIGHT_PRESETS}
          value={String(heightMin)}
          onChange={(e) => onHeightMinChange(Number(e.currentTarget.value))}
        />
      ) : null}
      <Group gap={4} align="center">
        <DatePickerInput
          aria-label="起始日期"
          size="xs"
          placeholder="起始日期"
          valueFormat="YYYY-MM-DD"
          clearable
          w={140}
          value={strToDate(timeFrom)}
          onChange={(v) => onTimeFromChange(dateToStr(v))}
        />
        <Text size="xs" c="dimmed">
          至
        </Text>
        <DatePickerInput
          aria-label="结束日期"
          size="xs"
          placeholder="结束日期"
          valueFormat="YYYY-MM-DD"
          clearable
          w={140}
          value={strToDate(timeTo)}
          onChange={(v) => onTimeToChange(dateToStr(v))}
        />
      </Group>
    </Group>
  );
}
