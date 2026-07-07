export type TaskState = 'pending' | 'running' | 'succeeded' | 'failed' | 'canceled';

export interface SpaceContext {
  readonly spaceId: string;
}

export interface QueryKeyFactory {
  readonly mediaList: (space: SpaceContext, page: number) => readonly ['media', 'list', string, number];
  readonly taskList: (space: SpaceContext) => readonly ['tasks', 'list', string];
}

export function createQueryKeys(): QueryKeyFactory {
  return {
    mediaList: (space, page) => ['media', 'list', space.spaceId, page] as const,
    taskList: (space) => ['tasks', 'list', space.spaceId] as const,
  };
}

export function normalizeLegacyTaskState(state: string): TaskState {
  if (state === 'completed') {
    return 'succeeded';
  }
  if (state === 'error') {
    return 'failed';
  }
  if (state === 'pending' || state === 'running' || state === 'succeeded' || state === 'failed' || state === 'canceled') {
    return state;
  }
  throw new Error(`未知任务状态：${state}`);
}
