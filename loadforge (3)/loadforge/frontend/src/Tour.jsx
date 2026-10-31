import React, { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'

/**
 * Lightweight guided tour (no dependencies).
 * Each step may switch tab, may require a collection, and points at a [data-tour="…"] element.
 * If the element is not on screen (e.g. no run yet) the card is shown centred instead.
 */
export const TOUR_STEPS = [
  { tab: 'import', target: null, title: 'Welcome to LoadForge 👋',
    body: 'This 1-minute tour shows how to load test and stress test your APIs and download a graphical report. Use ← → keys or the buttons; Esc to exit.' },
  { tab: 'import', target: 'nav', title: 'Four simple steps',
    body: 'Work left to right: pick the API → configure the test → watch it run → find every past run in History.' },
  { tab: 'import', target: 'import-tabs', title: 'Choose what to test',
    body: 'Test a single API by hand, or import an OpenAPI / Swagger spec or a Postman collection (upload, paste, or from a URL).' },
  { tab: 'import', target: 'req-line', title: 'Single API: method & URL',
    body: 'Pick the method and enter the full URL. For an API running on your own machine use http://host.docker.internal:PORT — inside Docker, localhost means the LoadForge container.' },
  { tab: 'import', target: 'req-tabs', title: 'Token, headers & payload',
    body: 'Auth: paste a Bearer token, Basic credentials or an API key. Headers: anything extra. Body: JSON (validated), form, text or XML. Use {{$uuid}} or {{$randomInt}} for values that change on every request.' },
  { tab: 'import', target: 'curl', title: 'Already have a cURL command?',
    body: 'Paste it here (e.g. from your browser’s “Copy as cURL”) and the method, URL, token, headers and body are filled in for you.' },
  { tab: 'import', target: 'use-bar', title: 'Send once, then load test',
    body: 'Press Send to make one real request and check the response — a 401/403 means the token is wrong. When it works, click “Load test this API →”.' },
  { tab: 'import', target: 'demo', title: 'No API handy?',
    body: 'The built-in demo API lets you try everything immediately. It slows down and starts failing past ~150 users, so stress tests show a real breaking point.' },
  { tab: 'config', needsCollection: true, target: 'types', title: 'Pick a test type',
    body: 'Load = normal traffic · Stress = step up until it breaks · Spike = sudden burst · Soak = long run to find leaks · Custom = your own stages.' },
  { tab: 'config', needsCollection: true, target: 'profile', title: 'Shape the load',
    body: 'Set virtual users (VUs = simultaneous clients) and durations. The chart previews exactly how many VUs will run over time.' },
  { tab: 'config', needsCollection: true, target: 'target', title: 'Target, headers & variables',
    body: 'Change the Base URL (e.g. staging vs production) and add headers or variables shared by every request. Empty token variables are highlighted.' },
  { tab: 'config', needsCollection: true, target: 'exec', title: 'Limits & pass/fail rules',
    body: 'Optionally cap requests/sec and set thresholds such as “p95 ≤ 500 ms” and “errors ≤ 1%”. The report marks the test PASSED or FAILED against them.' },
  { tab: 'config', needsCollection: true, target: 'start', title: 'Start the test',
    body: 'Only test systems you own or are authorised to test. The run continues on the server even if you close the browser.' },
  { tab: 'run', target: 'run-head', title: 'Watch it live',
    body: 'Throughput, response times (p50/p95/p99), errors and per-endpoint stats update every second. Stop the test early at any time.' },
  { tab: 'run', target: 'run-downloads', title: 'Download the report',
    body: 'When it finishes, download the graphical HTML report (works offline; print it to PDF), or CSV / JSON for spreadsheets and CI.' },
  { tab: 'history', target: 'history', title: 'History',
    body: 'Every run is saved on the server. Open any run again, download its report, or delete it.' },
  { tab: 'import', target: 'help', title: 'That’s it!',
    body: 'Replay this tour any time from the Tour button. The user guide (docs/USER_GUIDE.md) covers everything in detail.' },
]

const PAD = 8

export default function Tour({ open, onClose, setTab, ensureCollection }) {
  const [i, setI] = useState(0)
  const [rect, setRect] = useState(null)
  const [ready, setReady] = useState(false)
  const cardRef = useRef()
  const [cardPos, setCardPos] = useState({ top: 0, left: 0, center: true })
  const step = TOUR_STEPS[i]

  useEffect(() => { if (open) setI(0) }, [open])

  // Navigate + locate target for the current step.
  useEffect(() => {
    if (!open) return
    let cancelled = false
    setReady(false)
    ;(async () => {
      if (step.needsCollection) await ensureCollection()
      setTab(step.tab)
      if (step.tab === 'import') window.dispatchEvent(new CustomEvent('lf:tour', { detail: { importMode: 'single' } }))
      if (!step.target) { if (!cancelled) { setRect(null); setReady(true) } return }
      let el = null
      for (let t = 0; t < 25 && !cancelled; t++) {
        el = document.querySelector(`[data-tour="${step.target}"]`)
        if (el) break
        await new Promise((r) => setTimeout(r, 60))
      }
      if (cancelled) return
      if (el) {
        el.scrollIntoView({ block: 'center', behavior: 'instant' })
        await new Promise((r) => requestAnimationFrame(() => r()))
        setRect(el.getBoundingClientRect())
      } else setRect(null)
      setReady(true)
    })()
    return () => { cancelled = true }
  }, [open, i]) // eslint-disable-line react-hooks/exhaustive-deps

  // Keep spotlight aligned on scroll / resize.
  const remeasure = useCallback(() => {
    if (!step?.target) return
    const el = document.querySelector(`[data-tour="${step.target}"]`)
    setRect(el ? el.getBoundingClientRect() : null)
  }, [step])
  useEffect(() => {
    if (!open) return
    window.addEventListener('resize', remeasure)
    window.addEventListener('scroll', remeasure, true)
    return () => { window.removeEventListener('resize', remeasure); window.removeEventListener('scroll', remeasure, true) }
  }, [open, remeasure])

  // Place the card next to the spotlight.
  useLayoutEffect(() => {
    if (!open || !ready) return
    const card = cardRef.current
    if (!card) return
    const cw = card.offsetWidth, ch = card.offsetHeight
    const vw = window.innerWidth, vh = window.innerHeight
    if (!rect) { setCardPos({ center: true }); return }
    const clampTop = (t) => Math.min(vh - ch - 12, Math.max(12, t))
    const gap = PAD + 12
    let top, left
    if (rect.bottom + gap + ch <= vh - 12) { // below
      top = rect.bottom + gap
      left = rect.left + rect.width / 2 - cw / 2
    } else if (rect.top - gap - ch >= 12) { // above
      top = rect.top - gap - ch
      left = rect.left + rect.width / 2 - cw / 2
    } else if (rect.left - gap - cw >= 12) { // left side
      top = clampTop(rect.top + Math.min(rect.height, vh) / 2 - ch / 2)
      left = rect.left - gap - cw
    } else if (vw - rect.right - gap - cw >= 12) { // right side
      top = clampTop(rect.top + Math.min(rect.height, vh) / 2 - ch / 2)
      left = rect.right + gap
    } else { // no free space: overlap the bottom of the viewport
      top = vh - ch - 12
      left = rect.left + rect.width / 2 - cw / 2
    }
    left = Math.max(12, Math.min(vw - cw - 12, left))
    setCardPos({ top, left, center: false })
  }, [open, ready, rect, i])

  const next = useCallback(() => (i < TOUR_STEPS.length - 1 ? setI(i + 1) : onClose(true)), [i, onClose])
  const prev = useCallback(() => i > 0 && setI(i - 1), [i])
  useEffect(() => {
    if (!open) return
    const k = (e) => {
      if (e.key === 'Escape') onClose(false)
      else if (e.key === 'ArrowRight') next()
      else if (e.key === 'ArrowLeft') prev()
    }
    window.addEventListener('keydown', k)
    return () => window.removeEventListener('keydown', k)
  }, [open, next, prev, onClose])

  if (!open) return null
  const last = i === TOUR_STEPS.length - 1
  return (
    <div className="tour" role="dialog" aria-modal="true" aria-label="Product tour">
      {rect && ready ? (
        <div className="tour-spot" style={{ top: rect.top - PAD, left: rect.left - PAD, width: rect.width + PAD * 2, height: rect.height + PAD * 2 }} />
      ) : (
        <div className="tour-dim" />
      )}
      <div ref={cardRef} className={`tour-card ${cardPos.center ? 'center' : ''} ${ready ? 'in' : ''}`}
        style={cardPos.center ? undefined : { top: cardPos.top, left: cardPos.left }}>
        <div className="tour-step">Step {i + 1} of {TOUR_STEPS.length}</div>
        <h3>{step.title}</h3>
        <p>{step.body}</p>
        <div className="tour-dots">{TOUR_STEPS.map((_, j) => <i key={j} className={j === i ? 'on' : j < i ? 'done' : ''} />)}</div>
        <div className="tour-actions">
          <button className="link muted-l" onClick={() => onClose(false)}>{last ? '' : 'Skip tour'}</button>
          <div className="row gap">
            {i > 0 && <button className="ghost sm" onClick={prev}>Back</button>}
            <button className="primary sm" onClick={next} autoFocus>{last ? 'Finish' : i === 0 ? 'Start tour' : 'Next'}</button>
          </div>
        </div>
      </div>
    </div>
  )
}
