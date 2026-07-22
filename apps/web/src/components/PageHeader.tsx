import { Group, Title } from '@mantine/core';

interface PageHeaderProps {
  // 页面标题文案：默认视觉隐藏（仅供屏幕阅读器与文档标题语义），当前位置由导航高亮 + 动态文档标题（FR-129）表明
  title: string;
  // 右侧操作区（如「清理回收站」「新建相册」等按钮）；为空则只渲染隐藏标题占位
  actions?: React.ReactNode;
  // 是否显示大标题（默认隐藏，收敛冗余）：少数确需视觉标题的页面可显式开启
  showTitle?: boolean;
}

/**
 * 统一页面头部（FR-134）：收敛各页冗余的 `<Title order={2}>页名</Title>` 大标题。
 * 默认隐藏大标题（仅保留无障碍可读文本），当前所处页面由左侧导航高亮 + 浏览器标签页标题（FR-129）表明；
 * 右侧操作区原样常驻。showTitle 为少数确需视觉标题的页面留出口。
 */
export default function PageHeader({ title, actions, showTitle = false }: PageHeaderProps) {
  return (
    <Group justify="space-between" align="center">
      {showTitle ? (
        <Title order={2}>{title}</Title>
      ) : (
        // 视觉隐藏但保留 DOM 与无障碍语义：屏幕阅读器仍可读出页名，视觉上不占位
        <Title
          order={2}
          style={{
            position: 'absolute',
            width: 1,
            height: 1,
            padding: 0,
            margin: -1,
            overflow: 'hidden',
            clip: 'rect(0, 0, 0, 0)',
            whiteSpace: 'nowrap',
            border: 0,
          }}
        >
          {title}
        </Title>
      )}
      {actions}
    </Group>
  );
}
