// @ts-nocheck
import { JSDOM } from 'jsdom';

// 测试只访问本机 MSW mock，清理宿主机代理变量，避免 Axios/Node 适配器把 localhost 拼成非法代理 URL。
for (const key of ['HTTP_PROXY', 'HTTPS_PROXY', 'ALL_PROXY', 'http_proxy', 'https_proxy', 'all_proxy']) {
  delete process.env[key];
}
process.env.NO_PROXY = 'localhost,127.0.0.1,::1';
process.env.no_proxy = process.env.NO_PROXY;

// 创建 jsdom 环境
const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', {
  url: 'http://localhost:3000',
  pretendToBeVisual: true,
});

// 挂载 window 和 document
globalThis.window = dom.window;
globalThis.document = dom.window.document;

// 挂载 navigator：react-dom 在模块求值期读取 navigator，Node 21+ 才有此全局，
// Node 20（CI）缺失会抛 "navigator is not defined"。用可扩展的纯对象（不含 clipboard，
// 与 Node 全局 navigator 一致）跨 Node 版本统一行为，使依赖注入 navigator.clipboard 的
// 用例可照常 defineProperty 覆盖。
Object.defineProperty(globalThis, 'navigator', {
  value: {
    userAgent: dom.window.navigator.userAgent,
    language: dom.window.navigator.language,
    languages: dom.window.navigator.languages,
  },
  writable: true,
  configurable: true,
});

// 挂载 DOM 类型到 globalThis
const domTypes = [
  'HTMLElement',
  'HTMLInputElement',
  'HTMLDivElement',
  'HTMLSpanElement',
  'HTMLButtonElement',
  'HTMLAnchorElement',
  'HTMLImageElement',
  'HTMLFormElement',
  'HTMLLabelElement',
  'HTMLSelectElement',
  'HTMLTextAreaElement',
  'Element',
  'Node',
  'DocumentFragment',
  'Text',
  'Comment',
  'Event',
  'MouseEvent',
  'KeyboardEvent',
  'FocusEvent',
  'InputEvent',
  'CustomEvent',
  'FormData',
];

for (const type of domTypes) {
  if (dom.window[type]) {
    globalThis[type] = dom.window[type];
  }
}

// Mantine ScrollArea（Select 下拉等）需要 getComputedStyle 作为全局函数
Object.defineProperty(globalThis, 'getComputedStyle', {
  value: (elt: Element, pseudoElt?: string) => dom.window.getComputedStyle(elt, pseudoElt),
  writable: true,
  configurable: true,
});

// Mantine Select 高亮选项时会调用 scrollIntoView，jsdom 不实现该方法。
Object.defineProperty(globalThis.HTMLElement.prototype, 'scrollIntoView', {
  value: () => {},
  writable: true,
  configurable: true,
});

// Mantine ScrollArea 需要 ResizeObserver（jsdom 不提供，给个空实现）
Object.defineProperty(globalThis, 'ResizeObserver', {
  value: class {
    observe() {}
    unobserve() {}
    disconnect() {}
  },
  writable: true,
  configurable: true,
});

// Mantine SegmentedControl 等需要 MutationObserver（jsdom 不提供，给个空实现）
Object.defineProperty(globalThis, 'MutationObserver', {
  value: class {
    observe() {}
    disconnect() {}
    takeRecords() {
      return [];
    }
  },
  writable: true,
  configurable: true,
});

// Mantine 需要 matchMedia
Object.defineProperty(globalThis.window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
});

// Mantine Transition 需要 cancelAnimationFrame
Object.defineProperty(globalThis, 'cancelAnimationFrame', {
  value: (_id: number) => {},
  writable: true,
});
Object.defineProperty(globalThis, 'requestAnimationFrame', {
  value: (cb: FrameRequestCallback) => setTimeout(() => cb(Date.now()), 0) as unknown as number,
  writable: true,
});
Object.defineProperty(globalThis, 'queueMicrotask', {
  value: (fn: () => void) => {
    Promise.resolve().then(fn);
  },
  writable: true,
});

// 挂载 localStorage
Object.defineProperty(globalThis, 'localStorage', {
  value: dom.window.localStorage,
  writable: true,
  configurable: true,
});

// MSW server setup
import '@testing-library/jest-dom';
import { server } from '../mocks/beforeAll';

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
