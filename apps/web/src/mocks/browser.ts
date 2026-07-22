import { setupWorker } from 'msw/browser';
import { handlers } from './handlers';

/** 初始化 MSW 浏览器端 mock 服务 */
export async function setupMock() {
  const worker = setupWorker(...handlers);
  await worker.start({
    onUnhandledRequest: 'bypass', // 未匹配的请求直接放行
    quiet: true, // 不在 console 打印未匹配请求
  });
  return worker;
}
