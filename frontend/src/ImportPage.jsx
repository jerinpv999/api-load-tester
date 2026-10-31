import React, { useEffect, useMemo, useRef, useState } from 'react'
import { importSpec, fetchSpec, demoSpecUrl } from './api'
import { Card, Method, Empty } from './components'
import SingleRequest from './SingleRequest'

export default function ImportPage({ collection, setCollection, onNext, onSingle }) {
  const [mode, setMode] = useState('single')
  const [text, setText] = useState('')
  const [url, setUrl] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [drag, setDrag] = useState(false)
  const [filter, setFilter] = useState('')
  const [open, setOpen] = useState(null)
  const fileRef = useRef()

  // The guided tour asks for the single-request builder to be visible.
  useEffect(() => {
    const f = (e) => e.detail?.importMode && setMode(e.detail.importMode)
    window.addEventListener('lf:tour', f)
    return () => window.removeEventListener('lf:tour', f)
  }, [])

  const run = async (fn) => {
    setBusy(true)
    setError('')
    try {
      const col = await fn()
      setCollection(col)
      setOpen(null)
    } catch (e) {
      setError(e.message)
    } finally {
      setBusy(false)
    }
  }
  const onFile = (f) => f && run(async () => importSpec(await f.text()))
  const loadDemo = () =>
    run(async () => {
      const res = await fetch(demoSpecUrl())
      return importSpec(await res.text())
    })

  const eps = collection?.endpoints || []
  const shown = useMemo(() => {
    const q = filter.toLowerCase()
    return eps.filter((e) => !q || `${e.method} ${e.name} ${e.path} ${e.group}`.toLowerCase().includes(q))
  }, [eps, filter])
  const enabled = eps.filter((e) => e.enabled).length

  const update = (id, patch) =>
    setCollection({ ...collection, endpoints: eps.map((e) => (e.id === id ? { ...e, ...patch } : e)) })
  const setAll = (v) => {
    const ids = new Set(shown.map((e) => e.id))
    setCollection({ ...collection, endpoints: eps.map((e) => (ids.has(e.id) ? { ...e, enabled: v } : e)) })
  }

  return (
    <div className="page">
      <div className="page-h">
        <div>
          <h1>What do you want to test?</h1>
          <p className="muted">A single API with its token &amp; payload, an OpenAPI / Swagger spec (JSON or YAML), or a Postman v2 / v2.1 collection.</p>
        </div>
        <button className="ghost" data-tour="demo" onClick={loadDemo} disabled={busy}>Try the built-in demo API</button>
      </div>

      <Card>
        <div className="tabs-sm" data-tour="import-tabs">
          {[['single', 'Single API request'], ['file', 'Upload spec / collection'], ['paste', 'Paste spec'], ['url', 'Spec from URL']].map(([k, l]) => (
            <button key={k} className={mode === k ? 'on' : ''} onClick={() => setMode(k)}>{l}</button>
          ))}
        </div>
        {mode === 'single' && <SingleRequest collection={collection} onUse={onSingle} />}
        {mode === 'file' && (
          <div
            className={`drop ${drag ? 'drag' : ''}`}
            onDragOver={(e) => { e.preventDefault(); setDrag(true) }}
            onDragLeave={() => setDrag(false)}
            onDrop={(e) => { e.preventDefault(); setDrag(false); onFile(e.dataTransfer.files[0]) }}
            onClick={() => fileRef.current.click()}
          >
            <input ref={fileRef} type="file" accept=".json,.yaml,.yml" hidden onChange={(e) => onFile(e.target.files[0])} />
            <div className="drop-i">⇪</div>
            <b>Drop a spec or collection here</b>
            <span className="muted">or click to browse · .json · .yaml · .yml</span>
          </div>
        )}
        {mode === 'paste' && (
          <div className="stack">
            <textarea className="code" rows={12} value={text} onChange={(e) => setText(e.target.value)}
              placeholder={'openapi: 3.0.0\ninfo:\n  title: My API\n...'} />
            <div><button className="primary" disabled={busy || !text.trim()} onClick={() => run(() => importSpec(text))}>Import</button></div>
          </div>
        )}
        {mode === 'url' && (
          <div className="row gap">
            <input className="grow" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://api.example.com/openapi.json" />
            <button className="primary" disabled={busy || !url} onClick={() => run(async () => importSpec(await fetchSpec(url)))}>Fetch &amp; import</button>
          </div>
        )}
        {busy && <p className="muted">Parsing…</p>}
        {error && <div className="alert err">{error}</div>}
      </Card>

      {mode === 'single' ? null : collection ? (
        <Card
          title={collection.name}
          subtitle={`${collection.source} · ${eps.length} endpoints · ${enabled} selected · base ${collection.baseUrl || '(not set)'}`}
          actions={<button className="primary" disabled={!enabled} onClick={onNext}>Configure test →</button>}
        >
          {collection.warnings?.map((w, i) => <div key={i} className="alert warn">{w}</div>)}
          <div className="row gap toolbar">
            <input className="grow" placeholder="Filter endpoints…" value={filter} onChange={(e) => setFilter(e.target.value)} />
            <button className="ghost sm" onClick={() => setAll(true)}>Select all</button>
            <button className="ghost sm" onClick={() => setAll(false)}>None</button>
          </div>
          <div className="ep-list">
            <div className="ep-row ep-head">
              <span /><span>Endpoint</span><span>Weight</span><span />
            </div>
            {shown.map((e) => (
              <React.Fragment key={e.id}>
                <div className={`ep-row ${e.enabled ? '' : 'off'}`}>
                  <input type="checkbox" checked={e.enabled} onChange={(ev) => update(e.id, { enabled: ev.target.checked })} />
                  <div className="ep-main" onClick={() => setOpen(open === e.id ? null : e.id)}>
                    <Method m={e.method} /> <b>{e.name}</b>
                    {e.group && <span className="chip">{e.group}</span>}
                    <div className="mono muted small">{e.path}</div>
                  </div>
                  <input type="number" min="1" className="w-num" value={e.weight}
                    title="Relative share of traffic" onChange={(ev) => update(e.id, { weight: Math.max(1, +ev.target.value || 1) })} />
                  <button className="icon" onClick={() => setOpen(open === e.id ? null : e.id)}>{open === e.id ? '▴' : '▾'}</button>
                </div>
                {open === e.id && <EndpointEditor ep={e} onChange={(p) => update(e.id, p)} />}
              </React.Fragment>
            ))}
          </div>
        </Card>
      ) : (
        <Empty title="Nothing imported yet">Import a spec above, or click <b>Try the built-in demo API</b> to explore with a sample target.</Empty>
      )}
    </div>
  )
}

function EndpointEditor({ ep, onChange }) {
  const [headers, setHeaders] = useState(JSON.stringify(ep.headers || {}, null, 2))
  const [hErr, setHErr] = useState('')
  const [exp, setExp] = useState((ep.expectStatus || []).join(', '))
  return (
    <div className="ep-edit">
      <div className="grid-3">
        <label className="field"><span className="field-l">Method</span>
          <select value={ep.method} onChange={(e) => onChange({ method: e.target.value })}>
            {['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'].map((m) => <option key={m}>{m}</option>)}
          </select>
        </label>
        <label className="field span-2"><span className="field-l">Path or full URL</span>
          <input className="mono" value={ep.path} onChange={(e) => onChange({ path: e.target.value })} />
        </label>
      </div>
      <div className="grid-2">
        <label className="field"><span className="field-l">Headers (JSON)</span>
          <textarea className="code" rows={6} value={headers}
            onChange={(e) => {
              setHeaders(e.target.value)
              try { onChange({ headers: JSON.parse(e.target.value || '{}') }); setHErr('') } catch { setHErr('Invalid JSON') }
            }} />
          {hErr && <span className="field-h err-t">{hErr}</span>}
        </label>
        <label className="field"><span className="field-l">Body</span>
          <textarea className="code" rows={6} value={ep.body || ''} onChange={(e) => onChange({ body: e.target.value })} />
        </label>
      </div>
      <label className="field"><span className="field-l">Expected status codes</span>
        <input placeholder="Blank = any status below 400 is success · e.g. 200, 201"
          value={exp}
          onChange={(e) => {
            setExp(e.target.value)
            onChange({ expectStatus: e.target.value.split(/[,\s]+/).map(Number).filter((n) => n >= 100 && n < 600) })
          }} />
      </label>
      <p className="muted small">Use <code>{'{{variable}}'}</code> for variables and <code>{'{{$uuid}}'}</code>, <code>{'{{$randomInt}}'}</code>, <code>{'{{$timestamp}}'}</code>, <code>{'{{$randomEmail}}'}</code> for per-request dynamic values.</p>
    </div>
  )
}
