import type { PixiGridPreviewOptions } from "@jianvideo/render-pixi";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { renderToString } from "react-dom/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";

const renderPixiMock = vi.hoisted(() => ({
  mountPixiGridPreview: vi.fn(),
}));

vi.mock("@jianvideo/render-pixi", () => ({
  mountPixiGridPreview: renderPixiMock.mountPixiGridPreview,
}));

describe("mock studio app", () => {
  let root: Root | undefined;

  beforeEach(() => {
    (
      globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }
    ).IS_REACT_ACT_ENVIRONMENT = true;
    renderPixiMock.mountPixiGridPreview.mockReset();
  });

  afterEach(() => {
    if (root) {
      act(() => {
        root?.unmount();
      });
    }
    root = undefined;
    document.body.replaceChildren();
  });

  it("渲染 mock 数据集摘要", () => {
    const html = renderToString(<App />);

    expect(html).toContain("Mock Studio");
    expect(html).toContain("百万素材压力场景:target-1m");
    expect(html).toContain("FR2-063");
    expect(html).toContain("真实 PixiJS 原型");
    expect(html).toContain("PixiJS 初始化");
    expect(html).toContain("HLS 预览请求");
    expect(html).toContain("HLS 预览按 hover/选中触发");
  });

  it("挂载 Pixi 画布并记录预览请求", async () => {
    const destroy = vi.fn();
    renderPixiMock.mountPixiGridPreview.mockImplementation(
      ({ height, host, width }: PixiGridPreviewOptions) => {
        const canvas = document.createElement("canvas");
        canvas.width = width;
        canvas.height = height;
        host.replaceChildren(canvas);
        return Promise.resolve({
          canvas,
          destroy,
          pixiVersion: "mock-pixi",
          rendererType: "webgl-test",
        });
      },
    );

    await renderApp();
    await flushEffects();

    expect(
      document.querySelector('[data-testid="benchmark-canvas"]'),
    ).toBeInstanceOf(HTMLCanvasElement);
    expect(
      document.querySelector('[data-testid="pixi-status"]')?.textContent,
    ).toContain("真实 PixiJS webgl-test");

    const card = document.querySelector('[data-testid="hls-preview-card"]');
    const button = document.querySelector('[data-testid="hls-select-button"]');
    act(() => {
      card?.dispatchEvent(new MouseEvent("mouseover", { bubbles: true }));
      button?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    expect(
      document.querySelector('[data-testid="hls-count"]')?.textContent,
    ).toBe("2");
    act(() => {
      root?.unmount();
    });
    root = undefined;
    expect(destroy).toHaveBeenCalledTimes(1);
  });

  it("Pixi 初始化失败时回退到 Canvas", async () => {
    // getContext 重载含 webgpu→GPUCanvasContext，mock 用 any 避开联合返回类型冲突。
    const context = {
      fillRect: vi.fn(),
      fillStyle: "",
    };
    const getContext = vi
      .spyOn(HTMLCanvasElement.prototype, "getContext")
      // eslint-disable-next-line @typescript-eslint/no-explicit-any -- 测试 mock 不绑定 DOM 重载
      .mockReturnValue(context as any);
    renderPixiMock.mountPixiGridPreview.mockRejectedValue(
      new Error("webgl unavailable"),
    );

    await renderApp();
    await flushEffects();

    expect(
      document.querySelector('[data-testid="benchmark-canvas"]'),
    ).toBeInstanceOf(HTMLCanvasElement);
    expect(
      document.querySelector('[data-testid="pixi-status"]')?.textContent,
    ).toBe("fallback：Pixi 初始化失败");
    getContext.mockRestore();
  });

  async function renderApp(): Promise<void> {
    const container = document.createElement("div");
    document.body.appendChild(container);
    root = createRoot(container);
    await act(async () => {
      root?.render(<App />);
      await Promise.resolve();
    });
  }

  async function flushEffects(): Promise<void> {
    await act(async () => {
      await Promise.resolve();
    });
  }
});
