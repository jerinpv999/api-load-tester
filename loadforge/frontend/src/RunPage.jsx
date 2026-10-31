import React, { useEffect, useMemo, useRef, useState } from 'react'
import {
  Area, Bar, BarChart, CartesianGrid, ComposedChart, Legend, Line, ResponsiveContainer, Tooltip, XAxis, YAxis, ReferenceLine, Cell,
} from 'recharts'
import { getRun, stopRun, streamUrl, reportUrl } from './api'
import { Card, Kpi, Method, Empty } from './components'
import { fmtDur, fmtMs, fmtNum, totalDuration } from './presets'

const norm = (r) => ({ ...r, series: r.series || [], endpoints: r.endpoints || [], summary: r.summary || {} })

const C = { blue: '#2563eb', amber: '#d97706', red: '#dc2626', green: '#059669', slate: '#64748b', violet: '#7c3aed' }

export default function RunPage({ runId, goHistory }) {
  const [run, setRun] = useState(null)
  const [error, setError] = useState('')
  const [stopping, setStopping] = useState(false)
  const esRef = useRef()

  useEffect(() => {
    if (!runId) return
    setRun(null)
    setError('')
    setStopping(false)
    const es = new EventSource(streamUrl(runId))
    esRef.current = es
    es.addEventListener('init', (e) => setRun(norm(JSON.parse(e.data))))
    es.addEventListener('tick', (e) => {
      const t = JSON.parse(e.data)
      setRun((r) => r && {
        ...r, status: t.status, summary: t.summary, endpoints: t.endpoints,
        series: t.point && (!r.series.length || r.series.at(-1).t < t.point.t) ? [...r.series, t.point] : r.series,
      })
    })
    es.addEventListener('done', (e) => { setRun(norm(JSON.parse(e.data))); es.close() })
    es.onerror = () => {
      es.close()
      getRun(runId).then((r) => setRun(norm(r))).catch((err) => setError(err.message))
    }
    return () => es.close()
  }, [runId])

  if (!runId) return <div className="page"><Empty title="No test selected">Start a test from <b>Configure</b>, or open one from <button className="link" onClick={goHistory}>History</button>.</Empty></div>
  if (error) return <div className="page"><div className="alert err">{error}</div></div>
  if (!run) return <div className="page"><p className="muted">Connecting to test runner…</p></div>

  const s = run.summary || {}
  const lat = s.latency || {}
  const running = run.status === 'running'
  const total = totalDuration(run.config.stages)
  const elapsed = run.series.length
  const bp = s.breakingPoint

  return (
    <div className="page">
      <div className="page-h">
        <div>
          <div className="row gap">
            <span className={`status st-${run.status}`}>{running && <i className="pulse" />}{run.status}</span>
            <span className="chip">{run.config.type}</span>
            {!running && s.thresholds?.length > 0 && <span className={`verdict ${s.passed ? 'pass' : 'fail'}`}>{s.passed ? 'PASSED' : 'FAILED'}</span>}
          </div>
          <h1>{run.config.name}</h1>
          <p className="muted mono small">{run.config.baseUrl} · {run.id}</p>
        </div>
        <div className="row gap wrap">
          {running ? (
            <button className="danger" disabled={stopping} onClick={() => { setStopping(true); stopRun(runId).catch(() => {}) }}>
              {stopping ? 'Stopping…' : '■ Stop test'}
            </button>
          ) : (
            <>
              <a className="btn primary" href={reportUrl(runId, 'html', true)}>⤓ HTML report</a>
              <a className="btn ghost" href={reportUrl(runId, 'html')} target="_blank" rel="noreferrer">Open report</a>
              <a className="btn ghost" href={reportUrl(runId, 'csv', true)}>CSV</a>
              <a className="btn ghost" href={reportUrl(runId, 'json', true)}>JSON</a>
            </>
          )}
        </div>
      </div>

      {running && (
        <div className="progress">
          <div className="progress-bar" style={{ width: `${Math.min(100, (elapsed / Math.max(total, 1)) * 100)}%` }} />
          <span>{fmtDur(elapsed)} / {fmtDur(total)} · {run.series.at(-1)?.vus ?? 0} VUs active</span>
        </div>
      )}

      <div className="kpis">
        <Kpi label="Requests" value={fmtNum(s.requests)} sub={`${fmtNum(s.avgRps)} req/s avg`} />
        <Kpi label="Current throughput" value={`${fmtNum(run.series.at(-1)?.rps ?? 0)}/s`} sub={`peak ${fmtNum(s.peakRps)}/s`} />
        <Kpi label="Error rate" value={`${(s.errorRate ?? 0).toFixed(2)}%`} sub={`${fmtNum(s.errors)} failed`} tone={s.errorRate > 1 ? 'bad' : ''} />
        <Kpi label="p50 / avg" value={fmtMs(lat.p50)} sub={`avg ${fmtMs(lat.avg)}`} />
        <Kpi label="p95" value={fmtMs(lat.p95)} sub={`p90 ${fmtMs(lat.p90)}`} />
        <Kpi label="p99" value={fmtMs(lat.p99)} sub={`max ${fmtMs(lat.max)}`} />
      </div>

      {bp && !running && (
        <div className="alert warn">
          <b>Breaking point</b> at {fmtDur(bp.t)} with <b>{bp.vus} VUs</b> (~{fmtNum(bp.rps)} req/s): p95 {fmtMs(bp.p95)}, {bp.errors}/{bp.requests} errors that second.
        </div>
      )}

      <Charts series={run.series} bp={bp} />

      <div className="grid-2">
        <StatusCard s={s} />
        <Card title="Latency distribution">
          <ResponsiveContainer width="100%" height={220}>
            <BarChart data={(s.histogram || []).map((b, i, a) => ({ label: b.leMs < 0 ? `>${a[i - 1]?.leMs ?? 0}ms` : `≤${b.leMs >= 1000 ? b.leMs / 1000 + 's' : b.leMs + 'ms'}`, count: b.count }))}>
              <CartesianGrid stroke="var(--grid)" vertical={false} />
              <XAxis dataKey="label" tick={{ fontSize: 10 }} stroke="var(--muted)" interval={0} angle={-30} textAnchor="end" height={45} />
              <YAxis tick={{ fontSize: 11 }} stroke="var(--muted)" tickFormatter={fmtNum} />
              <Tooltip formatter={(v) => [fmtNum(v), 'requests']} />
              <Bar dataKey="count" fill={C.violet} radius={[3, 3, 0, 0]} isAnimationActive={false} />
            </BarChart>
          </ResponsiveContainer>
        </Card>
      </div>

      <Card title="Endpoints">
        <div className="scroll">
          <table className="tbl">
            <thead><tr><th>Endpoint</th><th className="r">Requests</th><th className="r">req/s</th><th className="r">Errors</th><th className="r">avg</th><th className="r">p50</th><th className="r">p95</th><th className="r">p99</th><th className="r">max</th></tr></thead>
            <tbody>
              {(run.endpoints || []).map((e) => (
                <tr key={e.id}>
                  <td><Method m={e.method} /> <b>{e.name}</b><div className="mono muted small">{e.path}</div></td>
                  <td className="r">{fmtNum(e.requests)}</td>
                  <td className="r">{fmtNum(e.rps)}</td>
                  <td className={`r ${e.errorRate > 1 ? 'bad' : ''}`}>{e.errorRate.toFixed(2)}%</td>
                  <td className="r">{fmtMs(e.latency.avg)}</td>
                  <td className="r">{fmtMs(e.latency.p50)}</td>
                  <td className="r"><b>{fmtMs(e.latency.p95)}</b></td>
                  <td className="r">{fmtMs(e.latency.p99)}</td>
                  <td className="r">{fmtMs(e.latency.max)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>

      {s.errorsByType && Object.keys(s.errorsByType).length > 0 && (
        <Card title="Errors by type">
          <div className="bars">
            {Object.entries(s.errorsByType).sort((a, b) => b[1] - a[1]).slice(0, 10).map(([k, v], _, arr) => (
              <div className="bar-row" key={k}>
                <span className="bar-l">{k}</span>
                <span className="bar-t"><span style={{ width: `${(v / arr[0][1]) * 100}%`, background: C.red }} /></span>
                <span className="bar-v">{fmtNum(v)}</span>
              </div>
            ))}
          </div>
        </Card>
      )}

      {!running && s.thresholds?.length > 0 && (
        <Card title="Thresholds">
          <table className="tbl">
            <thead><tr><th>Check</th><th className="r">Limit</th><th className="r">Actual</th><th className="r">Result</th></tr></thead>
            <tbody>{s.thresholds.map((t) => (
              <tr key={t.name}><td>{t.name}</td><td className="r">{fmtNum(t.limit)}</td><td className="r">{fmtNum(t.actual)}</td>
                <td className="r"><span className={`tag ${t.passed ? 'pass' : 'fail'}`}>{t.passed ? 'PASS' : 'FAIL'}</span></td></tr>
            ))}</tbody>
          </table>
        </Card>
      )}
    </div>
  )
}

function Charts({ series, bp }) {
  const data = useMemo(() => series.map((p) => ({ ...p, errPct: p.requests ? (p.errors / p.requests) * 100 : 0 })), [series])
  const ref = bp ? <ReferenceLine x={bp.t} stroke={C.red} strokeDasharray="4 4" label={{ value: 'breaking point', fill: C.red, fontSize: 11, position: 'insideTopLeft' }} /> : null
  const x = <XAxis dataKey="t" type="number" domain={['dataMin', 'dataMax']} tickFormatter={fmtDur} tick={{ fontSize: 11 }} stroke="var(--muted)" />
  const tip = <Tooltip labelFormatter={(t) => `t = ${fmtDur(t)}`} formatter={(v, n) => [/^p\d+$/.test(n) ? fmtMs(v) : fmtNum(v), n]} />
  return (
    <>
      <Card title="Throughput & virtual users">
        <ResponsiveContainer width="100%" height={260}>
          <ComposedChart data={data} margin={{ top: 8, right: 8, left: -8, bottom: 0 }}>
            <CartesianGrid stroke="var(--grid)" vertical={false} />
            {x}
            <YAxis yAxisId="l" tick={{ fontSize: 11 }} stroke="var(--muted)" tickFormatter={fmtNum} />
            <YAxis yAxisId="r" orientation="right" tick={{ fontSize: 11 }} stroke="var(--muted)" />
            {tip}<Legend wrapperStyle={{ fontSize: 12 }} />
            {bp && React.cloneElement(ref, { yAxisId: 'l' })}
            <Area yAxisId="l" type="monotone" dataKey="rps" name="Requests/s" stroke={C.blue} fill={C.blue} fillOpacity={0.12} strokeWidth={2} isAnimationActive={false} dot={false} />
            <Line yAxisId="r" type="stepAfter" dataKey="vus" name="Virtual users" stroke={C.slate} strokeDasharray="5 4" strokeWidth={1.5} dot={false} isAnimationActive={false} />
          </ComposedChart>
        </ResponsiveContainer>
      </Card>
      <div className="grid-2">
        <Card title="Response time (ms)">
          <ResponsiveContainer width="100%" height={240}>
            <ComposedChart data={data} margin={{ top: 8, right: 8, left: -8, bottom: 0 }}>
              <CartesianGrid stroke="var(--grid)" vertical={false} />
              {x}<YAxis tick={{ fontSize: 11 }} stroke="var(--muted)" />{tip}<Legend wrapperStyle={{ fontSize: 12 }} />
              {ref}
              <Line type="monotone" dataKey="p50" stroke={C.green} dot={false} strokeWidth={2} isAnimationActive={false} />
              <Line type="monotone" dataKey="p95" stroke={C.amber} dot={false} strokeWidth={2} isAnimationActive={false} />
              <Line type="monotone" dataKey="p99" stroke={C.red} dot={false} strokeWidth={2} isAnimationActive={false} />
            </ComposedChart>
          </ResponsiveContainer>
        </Card>
        <Card title="Errors">
          <ResponsiveContainer width="100%" height={240}>
            <ComposedChart data={data} margin={{ top: 8, right: 8, left: -8, bottom: 0 }}>
              <CartesianGrid stroke="var(--grid)" vertical={false} />
              {x}
              <YAxis yAxisId="l" tick={{ fontSize: 11 }} stroke="var(--muted)" tickFormatter={fmtNum} />
              <YAxis yAxisId="r" orientation="right" domain={[0, 100]} tick={{ fontSize: 11 }} stroke="var(--muted)" unit="%" />
              {tip}<Legend wrapperStyle={{ fontSize: 12 }} />
              <Bar yAxisId="l" dataKey="errors" name="Errors/s" fill={C.red} isAnimationActive={false} />
              <Line yAxisId="r" type="monotone" dataKey="errPct" name="Error %" stroke="var(--ink)" dot={false} strokeWidth={1.5} isAnimationActive={false} />
            </ComposedChart>
          </ResponsiveContainer>
        </Card>
      </div>
    </>
  )
}

function StatusCard({ s }) {
  const data = Object.entries(s.statusCodes || {}).sort().map(([code, count]) => ({ code, count }))
  const color = (c) => (c.startsWith('2') ? C.green : c.startsWith('3') ? C.blue : c.startsWith('4') ? C.amber : C.red)
  return (
    <Card title="Status codes">
      {data.length === 0 ? <p className="muted">Waiting for responses…</p> : (
        <ResponsiveContainer width="100%" height={220}>
          <BarChart data={data} layout="vertical" margin={{ left: 0, right: 24 }}>
            <XAxis type="number" hide />
            <YAxis type="category" dataKey="code" width={48} tick={{ fontSize: 12 }} stroke="var(--muted)" />
            <Tooltip formatter={(v) => [`${fmtNum(v)} (${((v / Math.max(s.requests, 1)) * 100).toFixed(1)}%)`, 'responses']} />
            <Bar dataKey="count" radius={[0, 4, 4, 0]} isAnimationActive={false} label={{ position: 'right', fontSize: 11, formatter: fmtNum }}>
              {data.map((d) => <Cell key={d.code} fill={color(d.code)} />)}
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      )}
    </Card>
  )
}
