// @ts-nocheck
import { JSDOM } from 'jsdom';

// 创建 jsdom 环境
const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', {
  url: 'http://localhost:3000',
  pretendToBeVisual: true,
});

// 挂载 window 和 document
globalThis.window = dom.window;
globalThis.document = dom.window.document;

// 挂载 DOM 类型到 globalThis
const domTypes = [
  'HTMLElement', 'HTMLInputElement', 'HTMLDivElement', 'HTMLSpanElement',
  'HTMLButtonElement', 'HTMLAnchorElement', 'HTMLImageElement',
  'HTMLFormElement', 'HTMLLabelElement', 'HTMLSelectElement', 'HTMLTextAreaElement',
  'Element', 'Node', 'DocumentFragment', 'Text', 'Comment',
  'Event', 'MouseEvent', 'KeyboardEvent', 'FocusEvent', 'InputEvent',
  'CustomEvent', 'FormData',
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
  value: (fn: () => void) => { Promise.resolve().then(fn); },
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

beforeAll(() => server.listen({ onUnhandledRequest: 'warn' }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
