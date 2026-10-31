import React, { useEffect, useMemo, useState } from 'react'
import { tryRequest } from './api'
import { parseCurl } from './curl'
import { KVEditor, Field } from './components'
import { fmtMs } from './presets'

const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS']
const BODY_TYPES = {
  none: { label: 'None', ct: null },
  json: { label: 'JSON', ct: 'application/json' },
  form: { label: 'Form URL-encoded', ct: 'application/x-www-form-urlencoded' },
  text: { label: 'Text', ct: 'text/plain' },
  xml: { label: 'XML', ct: 'application/xml' },
}

const blank = {
  method: 'GET',
  url: '',
  auth: { type: 'bearer', token: '', username: '', password: '', keyName: 'X-API-Key', keyValue: '' },
  headers: { Accept: 'application/json' },
  bodyType: 'none',
  body: '',
  expect: '',
  insecureTls: false,
}

function load() {
  try {
    const s = JSON.parse(localStorage.getItem('lf.single'))
    return s ? { ...blank, ...s, auth: { ...blank.auth, ...s.auth } } : blank
  } catch {
    return blank
  }
}

const b64 = (s) => btoa(unescape(encodeURIComponent(s)))
const hasHeader = (h, name) => Object.keys(h).some((k) => k.toLowerCase() === name.toLowerCase())

/** Final header set: user headers + auth + content type. */
export function buildHeaders(r) {
  const h = {}
  for (const [k, v] of Object.entries(r.headers)) if (k.trim()) h[k.trim()] = v
  const a = r.auth
  if (a.type === 'bearer' && a.token.trim()) h.Authorization = `Bearer ${a.token.trim().replace(/^Bearer\s+/i, '')}`
  if (a.type === 'basic' && (a.username || a.password)) h.Authorization = `Basic ${b64(`${a.username}:${a.password}`)}`
  if (a.type === 'apikey' && a.keyName.trim() && a.keyValue) h[a.keyName.trim()] = a.keyValue
  const ct = BODY_TYPES[r.bodyType]?.ct
  if (ct && !hasHeader(h, 'Content-Type')) h['Content-Type'] = ct
  return h
}

export default function SingleRequest({ collection, onUse }) {
  const [r, setR] = useState(load)
  const [section, setSection] = useState('auth')
  const [busy, setBusy] = useState(false)
  const [resp, setResp] = useState(null)
  const [err, setErr] = useState('')
  const [curlOpen, setCurlOpen] = useState(false)
  const [curl, setCurl] = useState('')
  const [respTab, setRespTab] = useState('body')
  const [showToken, setShowToken] = useState(false)

  useEffect(() => {
    try { localStorage.setItem('lf.single', JSON.stringify(r)) } catch { /* ignore */ }
  }, [r])

  const set = (p) => setR((x) => ({ ...x, ...p }))
  const setAuth = (p) => setR((x) => ({ ...x, auth: { ...x.auth, ...p } }))
  const sendsBody = r.bodyType !== 'none' && !['GET', 'HEAD'].includes(r.method)
  const body = sendsBody ? r.body : ''
  const headers = useMemo(() => buildHeaders({ ...r, bodyType: sendsBody ? r.bodyType : 'none' }), [r, sendsBody])

  const jsonError = useMemo(() => {
    if (r.bodyType !== 'json' || !r.body.trim()) return ''
    try {
      JSON.parse(r.body.replace(/\{\{[^}]+\}\}/g, '0')) // allow {{vars}}
      return ''
    } catch (e) {
      return e.message
    }
  }, [r.body, r.bodyType])

  const urlOk = /^https?:\/\/[^/\s]+/i.test(r.url.trim())
  const authMissing = (r.auth.type === 'bearer' && !r.auth.token.trim()) || (r.auth.type === 'apikey' && !r.auth.keyValue)

  const send = async () => {
    setBusy(true); setErr(''); setResp(null)
    try {
      const res = await tryRequest({ method: r.method, url: r.url.trim(), headers, body, insecureTls: r.insecureTls, timeoutMs: 30000 })
      setResp(res)
      setRespTab('body')
    } catch (e) {
      setErr(e.message)
    } finally {
      setBusy(false)
    }
  }

  const importCurl = () => {
    try {
      const c = parseCurl(curl)
      const h = { ...c.headers }
      const auth = { ...blank.auth, type: 'none' }
      const authKey = Object.keys(h).find((k) => k.toLowerCase() === 'authorization')
      if (authKey && /^bearer\s+/i.test(h[authKey])) {
        auth.type = 'bearer'
        auth.token = h[authKey].replace(/^bearer\s+/i, '')
        delete h[authKey]
      } else if (c.user) {
        const p = c.user.indexOf(':')
        Object.assign(auth, { type: 'basic', username: p < 0 ? c.user : c.user.slice(0, p), password: p < 0 ? '' : c.user.slice(p + 1) })
      }
      const ctKey = Object.keys(h).find((k) => k.toLowerCase() === 'content-type')
      const ct = (ctKey && h[ctKey]) || ''
      let bodyType = 'none', b = c.body
      if (b) {
        bodyType = ct.includes('json') ? 'json' : ct.includes('x-www-form-urlencoded') ? 'form' : ct.includes('xml') ? 'xml' : 'text'
        if (bodyType === 'json') try { b = JSON.stringify(JSON.parse(b), null, 2) } catch { /* keep raw */ }
      }
      setR({ ...blank, method: c.method, url: c.url, headers: h, auth, bodyType, body: b, insecureTls: c.insecure })
      setSection(auth.type !== 'none' ? 'auth' : b ? 'body' : 'headers')
      setCurlOpen(false); setCurl(''); setErr(''); setResp(null)
    } catch (e) {
      setErr(e.message)
    }
  }

  const toEndpoint = (absolute) => {
    const u = new URL(r.url.trim())
    const name = `${r.method} ${u.pathname === '/' ? u.host : u.pathname}`
    return {
      id: `s${Date.now().toString(36)}`, name, group: 'Single request', method: r.method,
      path: absolute ? r.url.trim() : u.pathname + u.search,
      headers: headers, body, weight: 1, enabled: true,
      expectStatus: r.expect.split(/[,\s]+/).map(Number).filter((n) => n >= 100 && n < 600),
      _origin: u.origin,
    }
  }

  const useAlone = () => {
    const ep = toEndpoint(false)
    const origin = ep._origin
    delete ep._origin
    onUse({ name: ep.name, source: 'single', baseUrl: origin, headers: {}, variables: {}, endpoints: [ep] }, r.insecureTls)
  }
  const addToCollection = () => {
    const ep = toEndpoint(true)
    delete ep._origin
    onUse({ ...collection, endpoints: [...collection.endpoints, ep] }, r.insecureTls)
  }

  const prettyBody = useMemo(() => {
    if (!resp?.body) return ''
    const ct = Object.entries(resp.headers || {}).find(([k]) => k.toLowerCase() === 'content-type')?.[1] || ''
    if (ct.includes('json') || /^\s*[[{]/.test(resp.body)) {
      try { return JSON.stringify(JSON.parse(resp.body), null, 2) } catch { /* not json */ }
    }
    return resp.body
  }, [resp])

  const statusTone = !resp ? '' : resp.error ? 'bad' : resp.status < 300 ? 'good' : resp.status < 400 ? 'info' : 'warnc'

  return (
    <div className="single">
      <div className="req-line" data-tour="req-line">
        <select className={`meth m-${r.method.toLowerCase()}`} value={r.method} onChange={(e) => set({ method: e.target.value })}>
          {METHODS.map((m) => <option key={m}>{m}</option>)}
        </select>
        <input className="mono grow" value={r.url} placeholder="https://api.example.com/v1/orders?status=open"
          onChange={(e) => set({ url: e.target.value })} onKeyDown={(e) => e.key === 'Enter' && urlOk && send()} />
        <button className="primary" disabled={busy || !urlOk} onClick={send}>{busy ? 'Sending…' : 'Send'}</button>
      </div>
      <div className="row between small">
        <span className="muted">Tip: use <code>host.docker.internal</code> instead of <code>localhost</code> for APIs on your machine.</span>
        <button className="link" data-tour="curl" onClick={() => setCurlOpen(!curlOpen)}>{curlOpen ? 'Cancel' : 'Import from cURL'}</button>
      </div>

      {curlOpen && (
        <div className="stack curl-box">
          <textarea className="code" rows={6} value={curl} autoFocus onChange={(e) => setCurl(e.target.value)}
            placeholder={`curl -X POST 'https://api.example.com/orders' \\\n  -H 'Authorization: Bearer eyJhbGci...' \\\n  -H 'Content-Type: application/json' \\\n  -d '{"productId": 7, "qty": 2}'`} />
          <div><button className="primary sm" disabled={!curl.trim()} onClick={importCurl}>Fill request from cURL</button></div>
        </div>
      )}

      <div className="tabs-line" data-tour="req-tabs">
        {[
          ['auth', 'Auth', r.auth.type !== 'none' ? (authMissing ? '!' : '✓') : ''],
          ['headers', 'Headers', Object.keys(r.headers).length || ''],
          ['body', 'Body', r.bodyType !== 'none' ? BODY_TYPES[r.bodyType].label : ''],
          ['options', 'Options', ''],
        ].map(([k, l, badge]) => (
          <button key={k} className={section === k ? 'on' : ''} onClick={() => setSection(k)}>
            {l}{badge !== '' && <em className={badge === '!' ? 'warnb' : ''}>{badge}</em>}
          </button>
        ))}
      </div>

      {section === 'auth' && (
        <div className="sect">
          <div className="seg">
            {[['none', 'No auth'], ['bearer', 'Bearer token'], ['basic', 'Basic'], ['apikey', 'API key header']].map(([k, l]) => (
              <button key={k} className={r.auth.type === k ? 'on' : ''} onClick={() => setAuth({ type: k })}>{l}</button>
            ))}
          </div>
          {r.auth.type === 'bearer' && (
            <Field label="Token" hint="Sent as  Authorization: Bearer <token>  — paste the token only (JWT, OAuth access token…).">
              <div className="row gap">
                <input className={`mono grow ${!r.auth.token ? 'warn' : ''}`} type={showToken ? 'text' : 'password'} value={r.auth.token}
                  placeholder="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9…" onChange={(e) => setAuth({ token: e.target.value })} />
                <button className="ghost sm" onClick={() => setShowToken(!showToken)}>{showToken ? 'Hide' : 'Show'}</button>
              </div>
            </Field>
          )}
          {r.auth.type === 'basic' && (
            <div className="grid-2 tight">
              <Field label="Username"><input value={r.auth.username} onChange={(e) => setAuth({ username: e.target.value })} /></Field>
              <Field label="Password"><input type="password" value={r.auth.password} onChange={(e) => setAuth({ password: e.target.value })} /></Field>
            </div>
          )}
          {r.auth.type === 'apikey' && (
            <div className="grid-2 tight">
              <Field label="Header name"><input value={r.auth.keyName} onChange={(e) => setAuth({ keyName: e.target.value })} /></Field>
              <Field label="Key value"><input className={!r.auth.keyValue ? 'warn' : ''} type={showToken ? 'text' : 'password'} value={r.auth.keyValue} onChange={(e) => setAuth({ keyValue: e.target.value })} /></Field>
            </div>
          )}
          {r.auth.type === 'none' && <p className="muted small">No authentication header will be added. You can still add any header manually.</p>}
        </div>
      )}

      {section === 'headers' && (
        <div className="sect">
          <KVEditor value={r.headers} onChange={(headers) => set({ headers })} keyPlaceholder="Header" />
          <div className="final-h">
            <span className="field-l">Headers that will be sent</span>
            {Object.entries(headers).map(([k, v]) => (
              <div key={k} className="mono small"><b>{k}:</b> {/authorization|api-key|token/i.test(k) ? v.slice(0, 14) + (v.length > 14 ? '…' : '') : v}</div>
            ))}
          </div>
        </div>
      )}

      {section === 'body' && (
        <div className="sect">
          <div className="seg">
            {Object.entries(BODY_TYPES).map(([k, t]) => (
              <button key={k} className={r.bodyType === k ? 'on' : ''} onClick={() => set({ bodyType: k })}>{t.label}</button>
            ))}
            {r.bodyType === 'json' && (
              <button className="link fmt" disabled={!!jsonError || !r.body.trim()}
                onClick={() => { try { set({ body: JSON.stringify(JSON.parse(r.body), null, 2) }) } catch { /* invalid */ } }}>Format JSON</button>
            )}
          </div>
          {['GET', 'HEAD'].includes(r.method) && r.bodyType !== 'none' && <div className="alert warn">{r.method} requests are sent without a body. Switch the method to POST/PUT/PATCH to send this payload.</div>}
          {r.bodyType !== 'none' && (
            <>
              <textarea className={`code ${jsonError ? 'bad-b' : ''}`} rows={12} value={r.body} onChange={(e) => set({ body: e.target.value })}
                placeholder={r.bodyType === 'json' ? '{\n  "productId": 7,\n  "quantity": 2,\n  "requestId": "{{$uuid}}"\n}' : r.bodyType === 'form' ? 'name=alice&qty=2' : ''} />
              {jsonError ? <span className="field-h err-t">Invalid JSON: {jsonError}</span>
                : <span className="field-h">Per-request dynamic values: <code>{'{{$uuid}}'}</code> <code>{'{{$randomInt}}'}</code> <code>{'{{$timestamp}}'}</code> <code>{'{{$randomEmail}}'}</code></span>}
            </>
          )}
        </div>
      )}

      {section === 'options' && (
        <div className="sect grid-2 tight">
          <Field label="Expected status codes" hint="Blank = any status below 400 counts as success · e.g. 200, 201">
            <input value={r.expect} placeholder="200, 201" onChange={(e) => set({ expect: e.target.value })} />
          </Field>
          <div className="field checks">
            <label><input type="checkbox" checked={r.insecureTls} onChange={(e) => set({ insecureTls: e.target.checked })} /> Skip TLS verification (self-signed certs)</label>
          </div>
        </div>
      )}

      {err && <div className="alert err">{err}</div>}

      {resp && (
        <div className="resp">
          <div className="resp-h">
            <span className={`pill ${statusTone}`}>{resp.error ? 'Failed' : `${resp.status} ${resp.statusText}`}</span>
            <span className="muted small">{fmtMs(resp.durationMs)}</span>
            {!resp.error && <span className="muted small">{resp.size >= 1024 ? (resp.size / 1024).toFixed(1) + ' KB' : resp.size + ' B'}</span>}
            {!resp.error && (
              <div className="tabs-sm mini-tabs">
                <button className={respTab === 'body' ? 'on' : ''} onClick={() => setRespTab('body')}>Body</button>
                <button className={respTab === 'headers' ? 'on' : ''} onClick={() => setRespTab('headers')}>Headers</button>
              </div>
            )}
          </div>
          {resp.error ? <div className="alert err">{resp.error}</div> : (
            <>
              {(resp.status === 401 || resp.status === 403) && <div className="alert warn">The API rejected the credentials ({resp.status}). Check the token in the Auth tab — a load test would fail the same way.</div>}
              {respTab === 'body'
                ? <pre className="resp-body">{prettyBody || <span className="muted">(empty body)</span>}{resp.truncated && '\n… (truncated)'}</pre>
                : <pre className="resp-body">{Object.entries(resp.headers).sort().map(([k, v]) => `${k}: ${v}`).join('\n')}</pre>}
            </>
          )}
        </div>
      )}

      <div className="use-bar" data-tour="use-bar">
        <span className="muted small">
          {resp && !resp.error && resp.ok ? '✓ Request works — ready to load test.' : 'Send once to confirm the token and payload work, then load test it.'}
        </span>
        <div className="row gap wrap">
          {collection && collection.source !== 'single' && (
            <button className="ghost" disabled={!urlOk} onClick={addToCollection}>Add to “{collection.name}”</button>
          )}
          <button className="primary" disabled={!urlOk || !!jsonError} onClick={useAlone}>Load test this API →</button>
        </div>
      </div>
    </div>
  )
}
