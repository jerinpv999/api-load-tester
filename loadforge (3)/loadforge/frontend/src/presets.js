// Test-type presets turn a few friendly parameters into VU stages for the Go engine.

export const TEST_TYPES = {
  load: {
    label: 'Load',
    blurb: 'Steady expected traffic. Verifies the API meets its SLOs under normal load.',
    params: { vus: 50, rampSec: 30, holdSec: 120 },
    fields: [
      ['vus', 'Virtual users'],
      ['rampSec', 'Ramp-up (s)'],
      ['holdSec', 'Hold (s)'],
    ],
    stages: (p) => [
      { durationSec: Math.max(1, p.rampSec), targetVus: p.vus },
      { durationSec: p.holdSec, targetVus: p.vus },
      { durationSec: 10, targetVus: 0 },
    ],
  },
  stress: {
    label: 'Stress',
    blurb: 'Steps load up past capacity to find the breaking point.',
    params: { startVus: 20, maxVus: 400, steps: 5, stepSec: 30 },
    fields: [
      ['startVus', 'Start VUs'],
      ['maxVus', 'Max VUs'],
      ['steps', 'Steps'],
      ['stepSec', 'Seconds / step'],
    ],
    stages: (p) => {
      const out = []
      const steps = Math.max(1, p.steps)
      for (let i = 0; i < steps; i++) {
        const vus = Math.round(p.startVus + ((p.maxVus - p.startVus) * i) / Math.max(1, steps - 1))
        out.push({ durationSec: 5, targetVus: vus }, { durationSec: Math.max(1, p.stepSec - 5), targetVus: vus })
      }
      out.push({ durationSec: 15, targetVus: 0 })
      return out
    },
  },
  spike: {
    label: 'Spike',
    blurb: 'Sudden burst of traffic, then back to baseline. Tests elasticity & recovery.',
    params: { baseVus: 20, spikeVus: 500, baseSec: 30, spikeSec: 20 },
    fields: [
      ['baseVus', 'Baseline VUs'],
      ['spikeVus', 'Spike VUs'],
      ['baseSec', 'Baseline (s)'],
      ['spikeSec', 'Spike (s)'],
    ],
    stages: (p) => [
      { durationSec: 5, targetVus: p.baseVus },
      { durationSec: p.baseSec, targetVus: p.baseVus },
      { durationSec: 3, targetVus: p.spikeVus },
      { durationSec: p.spikeSec, targetVus: p.spikeVus },
      { durationSec: 3, targetVus: p.baseVus },
      { durationSec: p.baseSec, targetVus: p.baseVus },
      { durationSec: 5, targetVus: 0 },
    ],
  },
  soak: {
    label: 'Soak',
    blurb: 'Moderate load held for a long time to expose leaks and degradation.',
    params: { vus: 40, minutes: 30 },
    fields: [
      ['vus', 'Virtual users'],
      ['minutes', 'Duration (min)'],
    ],
    stages: (p) => [
      { durationSec: 60, targetVus: p.vus },
      { durationSec: Math.max(1, p.minutes * 60 - 60), targetVus: p.vus },
      { durationSec: 30, targetVus: 0 },
    ],
  },
  custom: {
    label: 'Custom',
    blurb: 'Define your own stages. Each stage ramps linearly to its target VUs.',
    params: {},
    fields: [],
    stages: null,
  },
}

export function profilePoints(stages) {
  const pts = [{ t: 0, vus: 0 }]
  let t = 0
  for (const s of stages) {
    t += Number(s.durationSec) || 0
    pts.push({ t, vus: Number(s.targetVus) || 0 })
  }
  return pts
}

export const totalDuration = (stages) => stages.reduce((a, s) => a + (Number(s.durationSec) || 0), 0)

export function fmtDur(sec) {
  sec = Math.round(sec)
  const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60), s = sec % 60
  if (h) return `${h}h ${String(m).padStart(2, '0')}m`
  if (m) return `${m}m ${String(s).padStart(2, '0')}s`
  return `${s}s`
}

export function fmtNum(v) {
  if (v == null || isNaN(v)) return '—'
  if (v >= 1e6) return (v / 1e6).toFixed(1) + 'M'
  if (v >= 1e4) return (v / 1e3).toFixed(1) + 'k'
  if (Number.isInteger(v)) return String(v)
  return v >= 100 ? v.toFixed(0) : v.toFixed(1)
}

export function fmtMs(v) {
  if (v == null || isNaN(v)) return '—'
  if (v >= 1000) return (v / 1000).toFixed(2) + ' s'
  if (v >= 100) return v.toFixed(0) + ' ms'
  return v.toFixed(1) + ' ms'
}
