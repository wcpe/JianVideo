import {
  createApiClient,
  getMedia,
  getTask,
  listMedia,
  taskPollInterval,
  type TaskState,
} from '@jianvideo/media-client';
import { createMockFetch } from '@jianvideo/mock';

export interface ClientDemoPage {
  readonly firstPageTitles: readonly string[];
  readonly secondPageTitles: readonly string[];
}

export interface ClientDemoSnapshot {
  readonly defaultSpace: ClientDemoPage;
  readonly detailTitle: string;
  readonly studioSpace: Pick<ClientDemoPage, 'firstPageTitles'>;
  readonly taskPollInterval: 2000 | false;
  readonly taskStatuses: readonly TaskState[];
}

export async function loadClientDemoSnapshot(): Promise<ClientDemoSnapshot> {
  const mockFetch = createMockFetch();
  const defaultClient = createApiClient({ fetch: mockFetch, space: { spaceId: 'space-default' } });
  const studioClient = defaultClient.withSpace({ spaceId: 'space-studio' });
  const firstPage = await listMedia(defaultClient, { page: 1, pageSize: 1 });
  const secondPage = await listMedia(defaultClient, { page: 2, pageSize: 1 });
  const studioPage = await listMedia(studioClient, { page: 1, pageSize: 1 });
  const detail = await getMedia(defaultClient, firstPage.items[0]?.id ?? '');
  const runningTask = await getTask(defaultClient, 'task-transcode-default');
  const finishedTask = await getTask(defaultClient, 'task-transcode-default');

  return {
    defaultSpace: {
      firstPageTitles: firstPage.items.map((item) => item.title),
      secondPageTitles: secondPage.items.map((item) => item.title),
    },
    detailTitle: detail.title,
    studioSpace: {
      firstPageTitles: studioPage.items.map((item) => item.title),
    },
    taskPollInterval: taskPollInterval(runningTask),
    taskStatuses: [runningTask.status, finishedTask.status],
  };
}
