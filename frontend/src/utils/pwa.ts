// Service Worker 注册封装。
// 把 vite-plugin-pwa 的 registerSW 作为参数注入，便于单元测试 mock，
// 且生产环境注册失败时静默吞掉，不阻断应用启动。

// registerSW 选项（仅声明本项目用到的字段）。
export interface RegisterSWOptions {
  immediate?: boolean;
}

// registerSW 函数签名：vite-plugin-pwa 的 virtual:pwa-register 默认导出与之兼容。
export type RegisterSW = (options?: RegisterSWOptions) => unknown;

// 执行 Service Worker 注册。register 缺省（如不支持的环境）或抛错时安全返回。
export function registerPwa(register?: RegisterSW): void {
  if (typeof register !== 'function') {
    return;
  }
  try {
    // autoUpdate 模式：immediate 让新 SW 就绪后立即接管
    register({ immediate: true });
  } catch (err) {
    // 注册失败不应中断应用启动，仅告警
    console.warn('Service Worker 注册失败，离线能力不可用', err);
  }
}
