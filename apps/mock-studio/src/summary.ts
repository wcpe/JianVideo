import { mockScenarios } from '@jianvideo/mock';

export function resolveDatasetSummary(): string {
  return mockScenarios.map((scenario) => `${scenario.title}:${scenario.dataset}`).join('；');
}
