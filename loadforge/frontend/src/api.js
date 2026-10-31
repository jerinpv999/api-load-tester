import yaml from 'js-yaml'

const BASE = import.meta.env.VITE_API_BASE || ''

async function req(path, opts = {}) {
  const res = await fetch(BASE + path, opts)
  if (res.status === 204) return null
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`)
  return data
}

/** Accepts JSON or YAML text, normalises to JSON and asks the Go backend to parse it. */
export async function importSpec(text) {
  let doc
  const trimmed = text.trim()
  if (!trimmed) throw new Error('The document is empty')
  try {
    doc = JSON.parse(trimmed)
  } catch {
    try {
      doc = yaml.load(trimmed)
    } catch (e) {
      throw new Error('Not valid JSON or YAML: ' + e.message)
    }
  }
  return req('/api/import', { method: 'POST', body: JSON.stringify(doc) })
}

export async function fetchSpec(url) {
  const { content } = await req('/api/fetch-spec', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url }),
  })
  return content
}

export const startRun = (cfg) =>
  req('/api/runs', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(cfg) })
export const listRuns = () => req('/api/runs')
export const getRun = (id) => req(`/api/runs/${id}`)
export const stopRun = (id) => req(`/api/runs/${id}/stop`, { method: 'POST' })
export const deleteRun = (id) => req(`/api/runs/${id}`, { method: 'DELETE' })
export const streamUrl = (id) => `${BASE}/api/runs/${id}/stream`
export const reportUrl = (id, ext, download = false) => `${BASE}/api/runs/${id}/report/${ext}${download ? '?download=1' : ''}`
export const demoSpecUrl = () => `${BASE}/demo/openapi.json`
