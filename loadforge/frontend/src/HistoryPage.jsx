import React, { useEffect, useState } from 'react'
import { listRuns, deleteRun, reportUrl } from './api'
import { Card, Empty } from './components'
import { fmtMs, fmtNum } from './presets'

export default function HistoryPage({ openRun }) {
  const [runs, setRuns] = useState(null)
  const [error, setError] = useState('')
  const load = () => listRuns().then(setRuns).catch((e) => setError(e.message))
  useEffect(() => {
    load()
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [])

  const remove = async (id) => {
    if (!confirm('Delete this run and its report? This cannot be undone.')) return
    try { await deleteRun(id); load() } catch (e) { setError(e.message) }
  }

  return (
    <div className="page">
      <div className="page-h"><div><h1>History</h1><p className="muted">Every run is saved on the server volume with downloadable reports.</p></div></div>
      {error && <div className="alert err">{error}</div>}
      {runs === null ? <p className="muted">Loading…</p> : runs.length === 0 ? (
        <Empty title="No runs yet">Your completed tests will appear here.</Empty>
      ) : (
        <Card>
          <div className="scroll">
            <table className="tbl hover">
              <thead><tr><th>Test</th><th>Type</th><th>Status</th><th>Started</th><th className="r">Requests</th><th className="r">req/s</th><th className="r">p95</th><th className="r">Errors</th><th className="r">Report</th><th /></tr></thead>
              <tbody>
                {runs.map((r) => (
                  <tr key={r.id} onClick={() => openRun(r.id)}>
                    <td><b>{r.name}</b><div className="mono muted small">{r.id}</div></td>
                    <td><span className="chip">{r.type}</span></td>
                    <td>
                      <span className={`status st-${r.status}`}>{r.status === 'running' && <i className="pulse" />}{r.status}</span>
                      {r.status !== 'running' && r.passed && <span className="tag pass">PASS</span>}
                    </td>
                    <td className="small">{new Date(r.startedAt).toLocaleString()}</td>
                    <td className="r">{fmtNum(r.requests)}</td>
                    <td className="r">{fmtNum(r.avgRps)}</td>
                    <td className="r">{fmtMs(r.p95)}</td>
                    <td className={`r ${r.errorRate > 1 ? 'bad' : ''}`}>{r.errorRate.toFixed(2)}%</td>
                    <td className="r nowrap" onClick={(e) => e.stopPropagation()}>
                      {r.status !== 'running' && <>
                        <a className="btn ghost sm" href={reportUrl(r.id, 'html', true)}>⤓ HTML</a>{' '}
                        <a className="btn ghost sm" href={reportUrl(r.id, 'csv', true)}>CSV</a>
                      </>}
                    </td>
                    <td onClick={(e) => e.stopPropagation()}>
                      {r.status !== 'running' && <button className="icon" title="Delete" onClick={() => remove(r.id)}>🗑</button>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}
    </div>
  )
}
