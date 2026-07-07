import type { ClientDemoSnapshot } from './clientDemo';

export function ClientDemoPanel({ snapshot }: { readonly snapshot: ClientDemoSnapshot }) {
  return (
    <section aria-label="API client 示例">
      <h2>API client 示例</h2>
      <p>{snapshot.defaultSpace.firstPageTitles.join(' / ')}</p>
      <p>{snapshot.defaultSpace.secondPageTitles.join(' / ')}</p>
      <p>{snapshot.detailTitle}</p>
      <p>{snapshot.taskStatuses.join(' → ')}</p>
      <p>{snapshot.studioSpace.firstPageTitles.join(' / ')}</p>
    </section>
  );
}
