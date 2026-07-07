import { resolveDatasetSummary } from './summary';
import './style.css';

export function App() {
  return (
    <main className="mock-studio-shell">
      <h1>Mock Studio</h1>
      <p>{resolveDatasetSummary()}</p>
    </main>
  );
}
