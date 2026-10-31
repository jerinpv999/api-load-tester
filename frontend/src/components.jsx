import React from 'react'

export function Method({ m }) {
  return <span className={`method m-${(m || 'get').toLowerCase()}`}>{m}</span>
}

export function Kpi({ label, value, sub, tone }) {
  return (
    <div className={`kpi ${tone || ''}`}>
      <div className="kpi-l">{label}</div>
      <div className="kpi-v">{value}</div>
      {sub && <div className="kpi-s">{sub}</div>}
    </div>
  )
}

export function Card({ title, subtitle, actions, children, className = '', tour }) {
  return (
    <section className={`card ${className}`} data-tour={tour}>
      {(title || actions) && (
        <div className="card-h">
          <div>
            {title && <h3>{title}</h3>}
            {subtitle && <p className="muted">{subtitle}</p>}
          </div>
          {actions && <div className="row gap">{actions}</div>}
        </div>
      )}
      {children}
    </section>
  )
}

/** Editable key/value list backed by a plain object. */
export function KVEditor({ value = {}, onChange, keyPlaceholder = 'Name', valuePlaceholder = 'Value', secretHint }) {
  const entries = Object.entries(value)
  const set = (i, k, v) => {
    const next = [...entries]
    next[i] = [k, v]
    onChange(Object.fromEntries(next))
  }
  const remove = (i) => onChange(Object.fromEntries(entries.filter((_, j) => j !== i)))
  const add = () => {
    let k = 'new', n = 1
    while (value[k] !== undefined) k = `new${n++}`
    onChange({ ...value, [k]: '' })
  }
  return (
    <div className="kv">
      {entries.map(([k, v], i) => (
        <div className="kv-row" key={i}>
          <input value={k} placeholder={keyPlaceholder} onChange={(e) => set(i, e.target.value, v)} />
          <input
            value={v}
            placeholder={secretHint && /token|key|auth|secret|password/i.test(k) ? 'required — paste value' : valuePlaceholder}
            className={secretHint && v === '' ? 'warn' : ''}
            onChange={(e) => set(i, k, e.target.value)}
          />
          <button className="icon" title="Remove" onClick={() => remove(i)}>×</button>
        </div>
      ))}
      <button className="link" onClick={add}>+ Add</button>
    </div>
  )
}

export function Field({ label, hint, children }) {
  return (
    <label className="field">
      <span className="field-l">{label}</span>
      {children}
      {hint && <span className="field-h">{hint}</span>}
    </label>
  )
}

export function Empty({ title, children }) {
  return (
    <div className="empty">
      <h3>{title}</h3>
      <div className="muted">{children}</div>
    </div>
  )
}
