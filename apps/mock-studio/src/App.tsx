import { mountPixiGridPreview, type PixiGridPreviewHandle } from '@jianvideo/render-pixi';
import { useEffect, useRef, useState } from 'react';
import { resolveBenchmarkDashboard, resolveBenchmarkEntranceSummary, resolveDatasetSummary } from './summary';
import './style.css';

export function App() {
  const dashboard = resolveBenchmarkDashboard();
  const pixiHostRef = useRef<HTMLDivElement>(null);
  const [hlsPreviewRequests, setHlsPreviewRequests] = useState(0);
  const [pixiStatus, setPixiStatus] = useState('初始化中');
  const [selected, setSelected] = useState(false);

  useEffect(() => {
    const host = pixiHostRef.current;
    if (!host) {
      return;
    }

    let disposed = false;
    let preview: PixiGridPreviewHandle | undefined;
    void mountPixiGridPreview({ height: 128, host, width: 336 })
      .then((nextPreview) => {
        if (disposed) {
          nextPreview.destroy();
          return;
        }
        preview = nextPreview;
        nextPreview.canvas.classList.add('benchmark-canvas');
        nextPreview.canvas.setAttribute('data-testid', 'benchmark-canvas');
        setPixiStatus(`真实 PixiJS ${nextPreview.rendererType} ${nextPreview.pixiVersion}`);
      })
      .catch(() => {
        if (!disposed) {
          drawFallbackCanvas(host);
          setPixiStatus('fallback：Pixi 初始化失败');
        }
      });

    return () => {
      disposed = true;
      preview?.destroy();
    };
  }, []);

  const recordPreviewRequest = () => {
    setHlsPreviewRequests((current) => current + 1);
  };

  return (
    <main className="mock-studio-shell" data-testid="fr2-063-dashboard">
      <h1>Mock Studio</h1>
      <p>{resolveDatasetSummary()}</p>
      <p>{resolveBenchmarkEntranceSummary()}</p>
      <section className="benchmark-panel" aria-label="FR2-063 Benchmark">
        <div>
          <h2>{dashboard.title}</h2>
          <p>{dashboard.modeLabel}</p>
          <dl>
            <div>
              <dt>前端帧耗时</dt>
              <dd data-testid="frontend-p95">{dashboard.frontendP95}</dd>
              <dd data-testid="frontend-p99">{dashboard.frontendP99}</dd>
            </div>
            <div>
              <dt>PixiJS 初始化</dt>
              <dd data-testid="pixi-status">{pixiStatus}</dd>
            </div>
            <div>
              <dt>后端数据集</dt>
              <dd>{dashboard.backendDatasets.join(' / ')}</dd>
            </div>
            <div>
              <dt>HLS 预览请求</dt>
              <dd data-testid="hls-count">{hlsPreviewRequests}</dd>
            </div>
          </dl>
        </div>
        <div
          aria-label="PixiJS 原型核心模型画布"
          className="benchmark-canvas-host"
          data-testid="benchmark-canvas-host"
          ref={pixiHostRef}
        />
        <article
          className={selected ? 'preview-card preview-card-selected' : 'preview-card'}
          data-testid="hls-preview-card"
          onMouseEnter={recordPreviewRequest}
          tabIndex={0}
        >
          <strong>{dashboard.hlsPreviewPolicy}</strong>
          <button
            data-testid="hls-select-button"
            onClick={() => {
              setSelected(true);
              recordPreviewRequest();
            }}
            type="button"
          >
            选中预览
          </button>
        </article>
      </section>
    </main>
  );
}

function drawFallbackCanvas(host: HTMLDivElement): void {
  const canvas = document.createElement('canvas');
  canvas.className = 'benchmark-canvas';
  canvas.dataset.testid = 'benchmark-canvas';
  canvas.width = 336;
  canvas.height = 128;
  const context = canvas.getContext('2d');
  if (context) {
    context.fillStyle = '#0f172a';
    context.fillRect(0, 0, canvas.width, canvas.height);
    for (let row = 0; row < 3; row += 1) {
      for (let column = 0; column < 8; column += 1) {
        context.fillStyle = (row + column) % 3 === 0 ? '#38bdf8' : '#22c55e';
        context.fillRect(12 + column * 38, 12 + row * 32, 26, 20);
      }
    }
  }
  host.replaceChildren(canvas);
}
