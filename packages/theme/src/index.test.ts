import { describe, expect, it } from "vitest";
import {
  resolveDensity,
  resolveDensityFromCapabilities,
  resolveMuseumTheme,
  themeProfiles,
  themeTokens,
} from "./index";

describe("theme package", () => {
  it("触控设备使用舒适密度", () => {
    expect(resolveDensity({ density: "compact", pointer: "coarse" })).toBe(
      "comfortable",
    );
  });

  it("精确指针设备保留请求密度", () => {
    expect(resolveDensity({ density: "compact", pointer: "fine" })).toBe(
      "compact",
    );
  });

  it("暴露稳定的主题 token", () => {
    expect(themeTokens.radiusSm).toBe("6px");
  });

  it("消费 media-client 端能力结果决定密度", () => {
    expect(
      resolveDensityFromCapabilities({
        network: "fast",
        platform: "tv",
        pointer: "coarse",
        touch: false,
      }),
    ).toBe("comfortable");
    expect(
      resolveDensityFromCapabilities({
        network: "standard",
        platform: "desktop",
        pointer: "fine",
        touch: false,
      }),
    ).toBe("compact");
  });

  it("暴露 wiki 可切换的亮暗主题与密度配置", () => {
    expect(themeProfiles.map((profile) => profile.id)).toEqual(
      expect.arrayContaining([
        "light-comfortable",
        "dark-compact",
        "mobile-comfortable",
      ]),
    );
    expect(resolveMuseumTheme("dark-compact").className).toBe(
      "theme-dark density-compact",
    );
  });

  it("移动主题会收敛为舒适密度", () => {
    expect(resolveMuseumTheme("mobile-comfortable").density).toBe(
      "comfortable",
    );
  });

  it("未知主题抛出中文错误", () => {
    expect(() => resolveMuseumTheme("missing")).toThrow("未知主题配置");
  });
});
