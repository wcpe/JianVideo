interface BrandLogoProps {
  /** 图标边长（像素），默认 64 */
  size?: number;
}

/**
 * 品牌标志 — 原创 SVG（视频/媒体主题）。
 * 由圆角媒体帧轮廓 + 中心播放三角 + 两侧胶片齿孔构成，
 * 配色取自品牌紫（purple-4 与更深一档的 purple-6 CSS 变量），与登录页标题协调。
 * 纯展示组件，无第三方图标库素材。
 */
export default function BrandLogo({ size = 64 }: BrandLogoProps) {
  // 与全站一致，直接引用 Mantine 紫色 CSS 变量
  const purpleMain = 'var(--mantine-color-purple-4)';
  const purpleDeep = 'var(--mantine-color-purple-6)';

  return (
    <svg
      role="img"
      aria-label="JianVideo 标志"
      width={size}
      height={size}
      viewBox="0 0 64 64"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <defs>
        {/* 播放三角的紫色竖向渐变 */}
        <linearGradient id="brandLogoPlay" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={purpleMain} />
          <stop offset="100%" stopColor={purpleDeep} />
        </linearGradient>
      </defs>
      {/* 圆角媒体帧轮廓 */}
      <rect
        x="6"
        y="12"
        width="52"
        height="40"
        rx="9"
        stroke={purpleMain}
        strokeWidth="3.5"
        fill="none"
      />
      {/* 左侧胶片齿孔（上下两枚） */}
      <rect x="12" y="20" width="4" height="4" rx="1.2" fill={purpleMain} />
      <rect x="12" y="40" width="4" height="4" rx="1.2" fill={purpleMain} />
      {/* 右侧胶片齿孔（上下两枚） */}
      <rect x="48" y="20" width="4" height="4" rx="1.2" fill={purpleMain} />
      <rect x="48" y="40" width="4" height="4" rx="1.2" fill={purpleMain} />
      {/* 中心播放三角 */}
      <path
        d="M28 24.5 L40 32 L28 39.5 Z"
        fill="url(#brandLogoPlay)"
        stroke={purpleDeep}
        strokeWidth="2"
        strokeLinejoin="round"
      />
    </svg>
  );
}
