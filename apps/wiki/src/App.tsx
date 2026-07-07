import { useMemo, useState } from 'react';
import {
  filterWikiCatalog,
  getWikiPixiMetricSummary,
  getWikiScenarioSummary,
  listThemeProfileTitles,
  listWikiGroups,
  listWikiScenarioTitles,
  type WikiGroupId,
} from './catalog';
import { mockScenarios, type MockScenarioId } from '@jianvideo/mock';
import type { ComponentState } from '@jianvideo/ui';
import './style.css';

export function App() {
  const [query, setQuery] = useState('');
  const [group, setGroup] = useState<WikiGroupId | undefined>();
  const [scenarioId, setScenarioId] = useState<MockScenarioId>('normal-library');
  const [activeState, setActiveState] = useState<ComponentState>('default');
  const previews = useMemo(() => filterWikiCatalog(group ? { group, query } : { query }), [group, query]);
  const firstPreview = previews[0];

  return (
    <main className="wiki-shell">
      <header className="wiki-header">
        <h1>JianVideo Wiki</h1>
        <p>UI 博物馆与 mockup 工作台</p>
      </header>

      <section className="wiki-toolbar" aria-label="筛选">
        <input
          aria-label="搜索组件"
          data-testid="wiki-search"
          onChange={(event) => {
            setQuery(event.currentTarget.value);
          }}
          placeholder="搜索控件、状态或场景"
          type="search"
          value={query}
        />
        <div className="wiki-tabs" role="tablist">
          {listWikiGroups().map((item) => (
            <button
              aria-pressed={group === item.id}
              data-testid={`wiki-group-${item.id}`}
              key={item.id}
              onClick={() => {
                setGroup(item.id);
              }}
              type="button"
            >
              {item.title}
            </button>
          ))}
        </div>
      </section>

      <section className="wiki-controls" aria-label="Mock 场景与状态">
        <label>
          Mock 场景
          <select
            data-testid="wiki-scenario-select"
            onChange={(event) => {
              setScenarioId(event.currentTarget.value as MockScenarioId);
            }}
            value={scenarioId}
          >
            {mockScenarios.map((scenario) => (
              <option key={scenario.id} value={scenario.id}>
                {scenario.title}
              </option>
            ))}
          </select>
        </label>
        <div className="wiki-state-list">
          {(['default', 'loading', 'disabled', 'empty', 'error', 'selected', 'dense', 'mobile'] as const).map((state) => (
            <button
              data-testid={`wiki-state-${state}`}
              key={state}
              onClick={() => {
                setActiveState(state);
              }}
              type="button"
            >
              {state}
            </button>
          ))}
        </div>
      </section>

      <section className="wiki-grid" aria-label="组件预览">
        {previews.map((item) => (
          <article className="wiki-card" data-testid={`wiki-preview-${item.id}`} key={item.id}>
            <h2>{item.title}</h2>
            <p className="wiki-card-meta">{item.group} · {item.states.join(' / ')}</p>
            <pre data-testid={`wiki-snippet-${item.id}`}>{item.snippet.code}</pre>
          </article>
        ))}
      </section>

      <aside className="wiki-inspector" aria-label="当前预览">
        <strong data-testid="wiki-active-state">状态：{activeState}</strong>
        <span data-testid="wiki-selected-scenario">{getWikiScenarioSummary(scenarioId)}</span>
        <span>{getWikiPixiMetricSummary()}</span>
        <span>{listThemeProfileTitles().join(' / ')}</span>
        {firstPreview ? <code>{firstPreview.snippet.importPath}</code> : <code>无匹配预览</code>}
      </aside>

      <section className="wiki-scenarios" aria-label="Mock 场景">
        {listWikiScenarioTitles().map((title) => (
          <span key={title}>{title}</span>
        ))}
      </section>
    </main>
  );
}
