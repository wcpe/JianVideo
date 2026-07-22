import type { MediaFile } from '@/types';
import { groupMediaByDate } from './timeline';

/** 一条按天聚合的旅程轨迹（FR-76）：date 为当天日期键，positions 为按时间序的坐标点，color 为折线颜色 */
export interface DayTrack {
  date: string;
  positions: [number, number][];
  color: string;
}

/**
 * 轨迹调色板：多天多条轨迹按下标循环取色以便区分。
 * 取一组高对比、亮暗主题下均可辨识的颜色。
 */
export const TRACK_COLORS = [
  '#e64980', // 粉
  '#7950f2', // 紫
  '#15aabf', // 青
  '#fab005', // 黄
  '#40c057', // 绿
  '#fd7e14', // 橙
] as const;

/** 判断媒体是否含有效 GPS 坐标：经纬度均存在且非 0,0 空岛（与后端 has_gps 语义一致） */
function hasValidGps(file: MediaFile): boolean {
  const { gps_lat, gps_lon } = file;
  if (gps_lat == null || gps_lon == null) return false;
  return gps_lat !== 0 || gps_lon !== 0;
}

/**
 * 把带 GPS 的媒体按「天」聚合成旅程轨迹（FR-76，扩 FR-39）。
 * - 仅纳入含有效 GPS 坐标的媒体（无坐标 / 0,0 空岛剔除）。
 * - 复用 `groupMediaByDate(files, 'day')` 按天分组；组内保持输入顺序，故调用方应传入按时间升序的媒体，
 *   分组后每条轨迹的点序即为当天时间升序。
 * - 丢弃当天点数 < 2 的天（单点无法连线）。
 * - 输出按日期升序排列，颜色按下标循环取自 TRACK_COLORS。
 * 纯函数，无副作用，便于穷举单测。
 */
export function buildDayTracks(files: MediaFile[]): DayTrack[] {
  const geotagged = files.filter(hasValidGps);
  const groups = groupMediaByDate(geotagged, 'day');
  return (
    groups
      .filter((g) => g.files.length >= 2)
      // groupMediaByDate 按日期键倒序返回，轨迹按日期升序展示并据此取色
      .sort((a, b) => (a.date < b.date ? -1 : a.date > b.date ? 1 : 0))
      .map((g, index) => ({
        date: g.date,
        positions: g.files.map(
          (f) => [f.gps_lat as number, f.gps_lon as number] as [number, number],
        ),
        color: TRACK_COLORS[index % TRACK_COLORS.length],
      }))
  );
}
