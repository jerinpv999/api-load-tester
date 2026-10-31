import React, { useEffect, useState } from 'react'
import ImportPage from './ImportPage'
import ConfigPage from './ConfigPage'
import RunPage from './RunPage'
import HistoryPage from './HistoryPage'
import { TEST_TYPES } from './presets'

const defaultConfig = {
  name: '',
  type: 'load',
  params: Object.fromEntries(Object.entries(TEST_TYPES).map(([k, t]) => [k, t.params])),
  stages: TEST_TYPES.load.stages(TEST_TYPES.load.params),
  maxRps: 0,
  timeoutMs: 10000,
  thinkTimeMs: 0,
  keepAlive: true,
  insecureTls: false,
  thresholds: { p95Ms: 500, errorRatePct: 1 },
}

function usePersisted(key, initial) {
  const [v, setV] = useState(() => {
    try {
      const s = localStorage.getItem(key)
      return s ? JSON.parse(s) : initial
    } catch {
      return initial
    }
  })
  useEffect(() => {
    try { localStorage.setItem(key, JSON.stringify(v)) } catch { /* storage unavailable */ }
  }, [key, v])
  return [v, setV]
}

const NAV = [
  ['import', '1', 'Import'],
  ['config', '2', 'Configure'],
  ['run', '3', 'Run & results'],
  ['history', '', 'History'],
]

export default function App() {
  const [tab, setTab] = useState('import')
  const [collection, setCollection] = usePersisted('lf.collection', null)
  const [config, setConfig] = usePersisted('lf.config', defaultConfig)
  const [runId, setRunId] = usePersisted('lf.runId', null)

  return (
    <div className="app">
      <nav className="top">
        <div className="brand">
          <svg viewBox="0 0 32 32" width="26" height="26"><rect width="32" height="32" rx="7" fill="var(--accent)" /><path d="M7 22l6-7 4 4 8-10" stroke="#fff" strokeWidth="3" fill="none" strokeLinecap="round" strokeLinejoin="round" /></svg>
          <span>LoadForge</span>
        </div>
        <div className="nav">
          {NAV.map(([k, n, l]) => (
            <button key={k} className={tab === k ? 'on' : ''} onClick={() => setTab(k)}>
              {n && <i>{n}</i>}{l}
            </button>
          ))}
        </div>
      </nav>
      <main>
        {tab === 'import' && <ImportPage collection={collection} setCollection={setCollection} onNext={() => setTab('config')} />}
        {tab === 'config' && (
          <ConfigPage collection={collection} setCollection={setCollection} config={config} setConfig={setConfig}
            goImport={() => setTab('import')} onStarted={(id) => { setRunId(id); setTab('run') }} />
        )}
        {tab === 'run' && <RunPage runId={runId} goHistory={() => setTab('history')} />}
        {tab === 'history' && <HistoryPage openRun={(id) => { setRunId(id); setTab('run') }} />}
      </main>
    </div>
  )
}
