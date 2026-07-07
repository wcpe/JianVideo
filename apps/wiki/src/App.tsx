import { listWikiScenarioTitles, wikiPreviewCatalog } from './catalog';
import './style.css';

export function App() {
  return (
    <main className="wiki-shell">
      <header className="wiki-header">
        <h1>JianVideo Wiki</h1>
        <p>UI 博物馆与 mockup 工作台</p>
      </header>
      <section className="wiki-grid" aria-label="组件预览">
        {wikiPreviewCatalog.map((item) => (
          <article className="wiki-card" key={item.id}>
            <h2>{item.title}</h2>
            <p>{item.snippet.code}</p>
          </article>
        ))}
      </section>
      <section className="wiki-scenarios" aria-label="Mock 场景">
        {listWikiScenarioTitles().map((title) => (
          <span key={title}>{title}</span>
        ))}
      </section>
    </main>
  );
}
