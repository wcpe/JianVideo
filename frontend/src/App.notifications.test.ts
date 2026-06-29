import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

// 通知上限守护（FR-115 后续修复·通知白屏）：
// 全屏渲染 App 依赖完整路由与认证态，成本高且脆。这里以源码级断言守住关键不变量——
// 全局 <Notifications> 必须设置 limit，限制同时渲染数量，防轮询/后台请求反复失败时
// toast 无上限堆积撑爆 DOM 导致白屏；同时保持 position 为右上。
const here = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(resolve(here, 'App.tsx'), 'utf-8');

describe('全局通知配置（FR-115 后续修复·通知白屏）', () => {
  it('<Notifications> 设置了 limit（限制同时渲染数量）', () => {
    expect(appSource).toMatch(/<Notifications[^>]*\blimit=\{\d+\}/);
  });

  it('<Notifications> 位置保持 top-right', () => {
    expect(appSource).toMatch(/<Notifications[^>]*position="top-right"/);
  });
});
