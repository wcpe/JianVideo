import { renderHook, act } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { useMultiSelect } from './useMultiSelect';

// 有序 id 列表：下标 0..4 对应 id 10..50
const ids = [10, 20, 30, 40, 50];

// 把 selectedIds 取成升序数组，便于断言
function selected(set: Set<number>): number[] {
  return Array.from(set).sort((a, b) => a - b);
}

describe('useMultiSelect（FR-69 多选基建）', () => {
  it('普通单击：仅选中该项，重复单击仍仅它', () => {
    const { result } = renderHook(() => useMultiSelect(ids));
    act(() => result.current.handleItemClick(2, {}));
    expect(selected(result.current.selectedIds)).toEqual([30]);
    act(() => result.current.handleItemClick(0, {}));
    expect(selected(result.current.selectedIds)).toEqual([10]);
  });

  it('Ctrl 单击：在已选基础上切换增减', () => {
    const { result } = renderHook(() => useMultiSelect(ids));
    act(() => result.current.handleItemClick(0, {})); // 选 10
    act(() => result.current.handleItemClick(2, { ctrlKey: true })); // 加 30
    expect(selected(result.current.selectedIds)).toEqual([10, 30]);
    act(() => result.current.handleItemClick(0, { ctrlKey: true })); // 去掉 10
    expect(selected(result.current.selectedIds)).toEqual([30]);
  });

  it('metaKey（Cmd）等价于 Ctrl 切换', () => {
    const { result } = renderHook(() => useMultiSelect(ids));
    act(() => result.current.handleItemClick(1, {}));
    act(() => result.current.handleItemClick(3, { metaKey: true }));
    expect(selected(result.current.selectedIds)).toEqual([20, 40]);
  });

  it('Shift 单击：从锚点到目标的区间全选（含反向）', () => {
    const { result } = renderHook(() => useMultiSelect(ids));
    act(() => result.current.handleItemClick(1, {})); // 锚 20
    act(() => result.current.handleItemClick(3, { shiftKey: true })); // 20..40
    expect(selected(result.current.selectedIds)).toEqual([20, 30, 40]);

    // 反向 Shift：锚仍是 1，往下标 0 连选 → 10..20
    act(() => result.current.handleItemClick(0, { shiftKey: true }));
    expect(selected(result.current.selectedIds)).toEqual([10, 20]);
  });

  it('无锚点时的 Shift 单击退化为单选', () => {
    const { result } = renderHook(() => useMultiSelect(ids));
    act(() => result.current.handleItemClick(2, { shiftKey: true }));
    expect(selected(result.current.selectedIds)).toEqual([30]);
  });

  it('selectAll 全选全部已加载项；clear 清空', () => {
    const { result } = renderHook(() => useMultiSelect(ids));
    act(() => result.current.selectAll());
    expect(selected(result.current.selectedIds)).toEqual([10, 20, 30, 40, 50]);
    act(() => result.current.clear());
    expect(selected(result.current.selectedIds)).toEqual([]);
  });

  it('selectIds 组级全选：并入给定 id（保留原有选中）（FR-146）', () => {
    const { result } = renderHook(() => useMultiSelect(ids));
    act(() => result.current.handleItemClick(0, {})); // 选 10
    act(() => result.current.selectIds([30, 40])); // 并入 30、40
    expect(selected(result.current.selectedIds)).toEqual([10, 30, 40]);
    // 再并入含已选项不重复
    act(() => result.current.selectIds([40, 50]));
    expect(selected(result.current.selectedIds)).toEqual([10, 30, 40, 50]);
  });

  it('selectIds 忽略不在当前列表内的 id（FR-146）', () => {
    const { result } = renderHook(() => useMultiSelect(ids));
    act(() => result.current.selectIds([30, 999])); // 999 不在列表内
    expect(selected(result.current.selectedIds)).toEqual([30]);
  });

  it('invertSelection 反选：已选与未选互换', () => {
    const { result } = renderHook(() => useMultiSelect(ids));
    act(() => result.current.handleItemClick(0, {})); // 选 10
    act(() => result.current.handleItemClick(2, { ctrlKey: true })); // 加 30
    act(() => result.current.invertSelection());
    expect(selected(result.current.selectedIds)).toEqual([20, 40, 50]);
  });

  it('复选框模式下任意单击即切换（不清其它）', () => {
    const { result } = renderHook(() => useMultiSelect(ids));
    act(() => result.current.setCheckboxMode(true));
    act(() => result.current.handleItemClick(0, {})); // 切 10
    act(() => result.current.handleItemClick(2, {})); // 切 30（不清 10）
    expect(selected(result.current.selectedIds)).toEqual([10, 30]);
    act(() => result.current.handleItemClick(0, {})); // 再切 10 → 取消
    expect(selected(result.current.selectedIds)).toEqual([30]);
  });

  it('toggle 与 isSelected 直接按 id 操作', () => {
    const { result } = renderHook(() => useMultiSelect(ids));
    act(() => result.current.toggle(40));
    expect(result.current.isSelected(40)).toBe(true);
    act(() => result.current.toggle(40));
    expect(result.current.isSelected(40)).toBe(false);
  });

  it('id 列表收缩后，selectAll 仅覆盖当前列表；陈旧选中项被清理', () => {
    const { result, rerender } = renderHook(({ list }) => useMultiSelect(list), {
      initialProps: { list: ids },
    });
    act(() => result.current.selectAll());
    expect(result.current.selectedIds.size).toBe(5);
    // 列表收缩为前两项（如翻页/筛选后），陈旧选中项（30/40/50）应被剔除
    rerender({ list: [10, 20] });
    expect(selected(result.current.selectedIds)).toEqual([10, 20]);
  });
});
