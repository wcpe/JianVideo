export type DensityMode = 'comfortable' | 'compact';

export interface DeviceThemeProfile {
  readonly density: DensityMode;
  readonly pointer: 'fine' | 'coarse';
}

export const themeTokens = {
  colorAccent: '#2563eb',
  colorDanger: '#dc2626',
  colorSurface: '#ffffff',
  radiusSm: '6px',
  spacingSm: '8px',
} as const;

export function resolveDensity(profile: DeviceThemeProfile): DensityMode {
  return profile.pointer === 'coarse' ? 'comfortable' : profile.density;
}
