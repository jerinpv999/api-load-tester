import React, { useEffect, useState } from 'react'
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { TEST_TYPES, profilePoints, totalDuration, fmtDur } from './presets'
import { Card, Field, KVEditor, Empty } from './components'
import { startRun } from './api'

export default function ConfigPage({ collection, setCollection, config, setConfig, onStarted, goImport }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  // Regenerate stages when a preset's params change.
  const preset = TEST_TYPES[config.type]
  useEffect(() => {
    if (preset.stages) setConfig((c) => ({ ...c, stages: preset.stages(c.params[c.type] || preset.params) }))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [config.type, JSON.stringify(config.params[config.type])])

  if (!collection) return <div className="page"><Empty title="Import an API first"><button className="primary" onClick={goImport}>Go to import</button></Empty></div>

  const enabled = collection.endpoints.filter((e) => e.enabled)
  const set = (patch) => setConfig({ ...config, ...patch })
  const params = config.params[config.type] || preset.params
  const setParam = (k, v) => set({ params: { ...config.params, [config.type]: { ...params, [k]: Math.max(0, Number(v) || 0) } } })
  const setThr = (k, v) => set({ thresholds: { ...config.thresholds, [k]: v === '' ? 0 : Number(v) } })
  const peak = Math.max(0, ...config.stages.map((s) => +s.targetVus || 0))
  const missing = Object.entries({ ...collection.variables }).filter(([, v]) => v === '').map(([k]) => k)

  const start = async () => {
    setBusy(true)
    setError('')
    try {
      const { id } = await startRun({
        name: config.name || collection.name,
        type: config.type,
        baseUrl: collection.baseUrl,
        headers: collection.headers,
        variables: collection.variables,
        endpoints: collection.endpoints,
        stages: config.stages.map((s) => ({ durationSec: +s.durationSec, targetVus: +s.targetVus })),
        maxRps: +config.maxRps || 0,
        timeoutMs: +config.timeoutMs || 30000,
        thinkTimeMs: +config.thinkTimeMs || 0,
        keepAlive: config.keepAlive,
        insecureTls: config.insecureTls,
        thresholds: config.thresholds,
      })
      onStarted(id)
    } catch (e) {
      setError(e.message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="page">
      <div className="page-h">
        <div>
          <h1>Configure test</h1>
          <p className="muted">{collection.name} · {enabled.length} endpoint{enabled.length === 1 ? '' : 's'} selected</p>
        </div>
      </div>

      {collection.source === 'single' && enabled[0] && <SingleSummary ep={enabled[0]} base={collection.baseUrl} onEdit={goImport} />}

      <Card title="Test profile">
        <div className="types" data-tour="types">
          {Object.entries(TEST_TYPES).map(([k, t]) => (
            <button key={k} className={`type ${config.type === k ? 'on' : ''}`} onClick={() => set({ type: k })}>
              <b>{t.label}</b>
              <span>{t.blurb}</span>
            </button>
          ))}
        </div>

        <div className="profile" data-tour="profile">
          <div className="profile-params">
            {preset.fields.map(([k, label]) => (
              <Field key={k} label={label}>
                <input type="number" min="0" value={params[k]} onChange={(e) => setParam(k, e.target.value)} />
              </Field>
            ))}
            {config.type === 'custom' && <StageEditor stages={config.stages} onChange={(stages) => set({ stages })} />}
            {config.type !== 'custom' && (
              <button className="link" onClick={() => set({ type: 'custom' })}>Fine-tune stages manually →</button>
            )}
          </div>
          <div className="profile-chart">
            <div className="row between small muted">
              <span>Virtual users over time</span>
              <span>peak <b>{peak}</b> VUs · total <b>{fmtDur(totalDuration(config.stages))}</b></span>
            </div>
            <ResponsiveContainer width="100%" height={200}>
              <AreaChart data={profilePoints(config.stages)} margin={{ top: 10, right: 8, left: -12, bottom: 0 }}>
                <defs>
                  <linearGradient id="pg" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="var(--accent)" stopOpacity={0.35} />
                    <stop offset="100%" stopColor="var(--accent)" stopOpacity={0.02} />
                  </linearGradient>
                </defs>
                <CartesianGrid stroke="var(--grid)" vertical={false} />
                <XAxis dataKey="t" type="number" domain={[0, 'dataMax']} tickFormatter={fmtDur} tick={{ fontSize: 11 }} stroke="var(--muted)" />
                <YAxis tick={{ fontSize: 11 }} stroke="var(--muted)" allowDecimals={false} />
                <Tooltip formatter={(v) => [v, 'VUs']} labelFormatter={fmtDur} />
                <Area type="linear" dataKey="vus" stroke="var(--accent)" strokeWidth={2} fill="url(#pg)" isAnimationActive={false} />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </div>
      </Card>

      <div className="grid-2">
        <Card title="Target" tour="target">
          <Field label="Test name"><input value={config.name} placeholder={collection.name} onChange={(e) => set({ name: e.target.value })} /></Field>
          <Field label="Base URL" hint="Inside Docker, use host.docker.internal to reach services on your machine.">
            <input className="mono" value={collection.baseUrl} placeholder="https://api.example.com"
              onChange={(e) => setCollection({ ...collection, baseUrl: e.target.value })} />
          </Field>
          <div className="field"><span className="field-l">Global headers</span>
            <KVEditor value={collection.headers || {}} secretHint onChange={(headers) => setCollection({ ...collection, headers })} keyPlaceholder="Header" />
          </div>
          <div className="field"><span className="field-l">Variables <span className="muted">— referenced as {'{{name}}'}</span></span>
            <KVEditor value={collection.variables || {}} secretHint onChange={(variables) => setCollection({ ...collection, variables })} />
          </div>
          {missing.length > 0 && <div className="alert warn">Empty variables: {missing.join(', ')}. Requests may fail authentication.</div>}
        </Card>

        <Card title="Execution & thresholds" tour="exec">
          <div className="grid-2 tight">
            <Field label="Max requests/sec" hint="0 = unlimited"><input type="number" min="0" value={config.maxRps} onChange={(e) => set({ maxRps: e.target.value })} /></Field>
            <Field label="Request timeout (ms)"><input type="number" min="100" value={config.timeoutMs} onChange={(e) => set({ timeoutMs: e.target.value })} /></Field>
            <Field label="Think time per VU (ms)"><input type="number" min="0" value={config.thinkTimeMs} onChange={(e) => set({ thinkTimeMs: e.target.value })} /></Field>
            <div className="field checks">
              <label><input type="checkbox" checked={config.keepAlive} onChange={(e) => set({ keepAlive: e.target.checked })} /> HTTP keep-alive</label>
              <label><input type="checkbox" checked={config.insecureTls} onChange={(e) => set({ insecureTls: e.target.checked })} /> Skip TLS verification</label>
            </div>
          </div>
          <h4 className="sub">Pass / fail thresholds <span className="muted">(blank = off)</span></h4>
          <div className="grid-2 tight">
            <Field label="p95 latency ≤ (ms)"><input type="number" min="0" value={config.thresholds.p95Ms || ''} onChange={(e) => setThr('p95Ms', e.target.value)} /></Field>
            <Field label="p99 latency ≤ (ms)"><input type="number" min="0" value={config.thresholds.p99Ms || ''} onChange={(e) => setThr('p99Ms', e.target.value)} /></Field>
            <Field label="Error rate ≤ (%)"><input type="number" min="0" step="0.1" value={config.thresholds.errorRatePct || ''} onChange={(e) => setThr('errorRatePct', e.target.value)} /></Field>
            <Field label="Throughput ≥ (req/s)"><input type="number" min="0" value={config.thresholds.minRps || ''} onChange={(e) => setThr('minRps', e.target.value)} /></Field>
          </div>
        </Card>
      </div>

      {error && <div className="alert err">{error}</div>}
      <div className="start-bar" data-tour="start">
        <span className="muted">Only test systems you own or are authorised to test.</span>
        <button className="primary lg" disabled={busy || !enabled.length} onClick={start}>{busy ? 'Starting…' : `▶ Start ${TEST_TYPES[config.type].label.toLowerCase()} test`}</button>
      </div>
    </div>
  )
}

function StageEditor({ stages, onChange }) {
  const upd = (i, k, v) => onChange(stages.map((s, j) => (j === i ? { ...s, [k]: Math.max(0, Number(v) || 0) } : s)))
  return (
    <div className="stages">
      <div className="stage-row head"><span>#</span><span>Duration (s)</span><span>Target VUs</span><span /></div>
      {stages.map((s, i) => (
        <div className="stage-row" key={i}>
          <span className="muted">{i + 1}</span>
          <input type="number" min="1" value={s.durationSec} onChange={(e) => upd(i, 'durationSec', e.target.value)} />
          <input type="number" min="0" value={s.targetVus} onChange={(e) => upd(i, 'targetVus', e.target.value)} />
          <button className="icon" disabled={stages.length < 2} onClick={() => onChange(stages.filter((_, j) => j !== i))}>×</button>
        </div>
      ))}
      <button className="link" onClick={() => onChange([...stages, { durationSec: 30, targetVus: stages.at(-1)?.targetVus || 10 }])}>+ Add stage</button>
    </div>
  )
}

function SingleSummary({ ep, base, onEdit }) {
  const mask = (k, v) => (/authorization|api-key|apikey|token|secret/i.test(k) ? v.slice(0, 13) + (v.length > 13 ? '•••' : '') : v)
  const h = Object.entries(ep.headers || {})
  return (
    <Card title="API under test" actions={<button className="ghost sm" onClick={onEdit}>Edit request</button>}>
      <div className="mono req-sum"><span className={`method m-${ep.method.toLowerCase()}`}>{ep.method}</span> {base}{ep.path}</div>
      <div className="grid-2 tight sum-grid">
        <div>
          <div className="field-l">Headers ({h.length})</div>
          {h.length ? h.map(([k, v]) => <div key={k} className="mono small"><b>{k}:</b> {mask(k, v)}</div>) : <div className="muted small">none</div>}
        </div>
        <div>
          <div className="field-l">Payload</div>
          {ep.body ? <pre className="resp-body small-pre">{ep.body.length > 600 ? ep.body.slice(0, 600) + '\n…' : ep.body}</pre> : <div className="muted small">no body</div>}
        </div>
      </div>
    </Card>
  )
}
