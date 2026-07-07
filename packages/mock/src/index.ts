export type MockScenarioId =
  | 'empty-library'
  | 'normal-library'
  | 'million-assets'
  | 'missing-thumbnail'
  | 'hls-pending'
  | 'transcode-failed'
  | 'permission-denied'
  | 'ai-review-pending';

export interface MockScenario {
  readonly id: MockScenarioId;
  readonly title: string;
  readonly dataset: 'smoke' | 'target-1m' | 'index-5m' | 'index-10m';
}

export const mockScenarios: readonly MockScenario[] = [
  { id: 'empty-library', title: '空媒体库', dataset: 'smoke' },
  { id: 'normal-library', title: '正常媒体库', dataset: 'smoke' },
  { id: 'million-assets', title: '百万素材压力场景', dataset: 'target-1m' },
] as const;

export function findScenario(id: MockScenarioId): MockScenario {
  const scenario = mockScenarios.find((item) => item.id === id);
  if (!scenario) {
    throw new Error(`未知 mock 场景：${id}`);
  }
  return scenario;
}
