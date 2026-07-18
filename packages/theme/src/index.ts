import type { DeviceCapabilities } from "@jianvideo/media-client";

export type DensityMode = "comfortable" | "compact";
export type ColorScheme = "light" | "dark";

export interface DeviceThemeProfile {
  readonly density: DensityMode;
  readonly pointer: "fine" | "coarse";
}

export interface MuseumThemeProfile extends DeviceThemeProfile {
  readonly id: string;
  readonly title: string;
  readonly scheme: ColorScheme;
  readonly className: string;
}

export const themeTokens = {
  colorAccent: "#2563eb",
  colorDanger: "#dc2626",
  colorSurface: "#ffffff",
  colorSurfaceDark: "#111827",
  colorText: "#0f172a",
  colorTextDark: "#f8fafc",
  radiusSm: "6px",
  spacingSm: "8px",
} as const;

export const themeProfiles: readonly MuseumThemeProfile[] = [
  {
    id: "light-comfortable",
    title: "亮色舒适",
    scheme: "light",
    density: "comfortable",
    pointer: "fine",
    className: "theme-light density-comfortable",
  },
  {
    id: "dark-compact",
    title: "暗色紧凑",
    scheme: "dark",
    density: "compact",
    pointer: "fine",
    className: "theme-dark density-compact",
  },
  {
    id: "mobile-comfortable",
    title: "移动舒适",
    scheme: "light",
    density: "compact",
    pointer: "coarse",
    className: "theme-light density-comfortable",
  },
] as const;

export function resolveDensity(profile: DeviceThemeProfile): DensityMode {
  return profile.pointer === "coarse" ? "comfortable" : profile.density;
}

export function resolveDensityFromCapabilities(
  capabilities: Pick<
    DeviceCapabilities,
    "network" | "platform" | "pointer" | "touch"
  >,
  density: DensityMode = "compact",
): DensityMode {
  if (
    capabilities.pointer === "coarse" ||
    capabilities.touch ||
    capabilities.platform === "tv" ||
    capabilities.platform === "automotive"
  ) {
    return "comfortable";
  }
  return capabilities.network === "constrained" ? "compact" : density;
}

export function resolveMuseumTheme(id: string): MuseumThemeProfile {
  const profile = themeProfiles.find((item) => item.id === id);
  if (!profile) {
    throw new Error(`未知主题配置：${id}`);
  }
  return profile.pointer === "coarse" && profile.density === "compact"
    ? {
        ...profile,
        density: "comfortable",
        className: "theme-light density-comfortable",
      }
    : profile;
}
