import { JSDOM } from 'jsdom';

// 创建 jsdom 环境
const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', {
  url: 'http://localhost:3000',
  pretendToBeVisual: true,
});

// 挂载 window 和 document
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;

// 挂载 DOM 类型到 globalThis（Mantine 和 React 需要）
const domTypes = [
  'HTMLElement', 'HTMLInputElement', 'HTMLDivElement', 'HTMLSpanElement',
  'HTMLButtonElement', 'HTMLAnchorElement', 'HTMLImageElement',
  'HTMLFormElement', 'HTMLLabelElement', 'HTMLSelectElement', 'HTMLTextAreaElement',
  'HTMLInputElement', 'HTMLIFrameElement', 'HTMLOptionElement',
  'Element', 'Node', 'DocumentFragment', 'Text', 'Comment',
  'Event', 'MouseEvent', 'KeyboardEvent', 'FocusEvent', 'InputEvent',
  'CustomEvent', 'FormData',
];

for (const type of domTypes) {
  if ((dom.window as any)[type]) {
    (globalThis as any)[type] = (dom.window as any)[type];
  }
}

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

// 挂载 localStorage
Object.defineProperty(globalThis, 'localStorage', {
  value: dom.window.localStorage,
  writable: true,
  configurable: true,
});

// Mantine Transition 需要 cancelAnimationFrame
if (!globalThis.cancelAnimationFrame) {
  globalThis.cancelAnimationFrame = (id: number) => {
    // jsdom 中 requestAnimationFrame 返回的 id 不需要真正清除
  };
}
if (!globalThis.requestAnimationFrame) {
  globalThis.requestAnimationFrame = (cb: FrameRequestCallback) => {
    return setTimeout(() => cb(Date.now()), 0) as unknown as number;
  };
}

// MSW server setup
import '@testing-library/jest-dom';
import { server } from '../mocks/beforeAll';

beforeAll(() => server.listen({ onUnhandledRequest: 'warn' }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
