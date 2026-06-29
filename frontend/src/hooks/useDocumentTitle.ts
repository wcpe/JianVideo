import { useEffect } from 'react';

// 应用名（FR-129）：浏览器标签页标题统一后缀
const APP_NAME = 'JianVideo';

/**
 * 动态文档标题（FR-129）：随路由切换设置浏览器标签页标题为「<页面名> - JianVideo」，
 * 无页面名（pageName 为空）时回退为「JianVideo」。
 * pageName 由调用方（AppLayout）按当前路由解析后传入，保持页名真源单一。
 */
export function useDocumentTitle(pageName?: string) {
  useEffect(() => {
    const name = pageName?.trim();
    document.title = name ? `${name} - ${APP_NAME}` : APP_NAME;
  }, [pageName]);
}
