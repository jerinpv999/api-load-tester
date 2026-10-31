// Minimal cURL command parser: covers what browsers' "Copy as cURL" and Postman export produce.

function tokenize(cmd) {
  const s = cmd.replace(/\\\r?\n/g, ' ').replace(/\^\r?\n/g, ' ') // line continuations (bash / cmd)
  const out = []
  let cur = '', q = null, has = false
  for (let i = 0; i < s.length; i++) {
    const c = s[i]
    if (q) {
      if (c === q) { q = null; continue }
      if (c === '\\' && q === '"' && i + 1 < s.length) { cur += s[++i]; continue }
      cur += c
      continue
    }
    if (c === "'" || c === '"') { q = c; has = true; continue }
    if (c === '$' && s[i + 1] === "'") { q = "'"; has = true; i++; continue } // $'...'
    if (c === '\\' && i + 1 < s.length) { cur += s[++i]; has = true; continue }
    if (/\s/.test(c)) {
      if (cur || has) out.push(cur)
      cur = ''; has = false
      continue
    }
    cur += c
  }
  if (cur || has) out.push(cur)
  return out
}

export function parseCurl(cmd) {
  const t = tokenize(cmd.trim())
  if (!t.length || !/^curl(\.exe)?$/i.test(t[0])) throw new Error('Paste a command that starts with "curl"')
  const r = { method: '', url: '', headers: {}, body: '', insecure: false, user: '' }
  const data = []
  for (let i = 1; i < t.length; i++) {
    let a = t[i]
    let v = null
    const eq = a.startsWith('--') ? a.indexOf('=') : -1
    if (eq > 0) { v = a.slice(eq + 1); a = a.slice(0, eq) }
    const next = () => (v !== null ? v : t[++i] ?? '')
    switch (a) {
      case '-X': case '--request': r.method = next().toUpperCase(); break
      case '-H': case '--header': {
        const h = next(), p = h.indexOf(':')
        if (p > 0) r.headers[h.slice(0, p).trim()] = h.slice(p + 1).trim()
        break
      }
      case '-d': case '--data': case '--data-raw': case '--data-binary': case '--data-ascii': case '--data-urlencode':
        data.push(next()); break
      case '--json':
        data.push(next())
        r.headers['Content-Type'] ??= 'application/json'
        r.headers.Accept ??= 'application/json'
        break
      case '-u': case '--user': r.user = next(); break
      case '-b': case '--cookie': r.headers.Cookie = next(); break
      case '-A': case '--user-agent': r.headers['User-Agent'] = next(); break
      case '-e': case '--referer': r.headers.Referer = next(); break
      case '--url': r.url = next(); break
      case '-k': case '--insecure': r.insecure = true; break
      case '-o': case '--output': case '-m': case '--max-time': case '--connect-timeout': case '-w': case '--write-out':
        next(); break
      default:
        if (!a.startsWith('-') && !r.url) r.url = a
    }
  }
  if (!r.url) throw new Error('No URL found in the cURL command')
  if (!/^https?:\/\//i.test(r.url)) r.url = 'http://' + r.url
  r.body = data.join('&')
  if (!r.method) r.method = r.body ? 'POST' : 'GET'
  if (r.body && !Object.keys(r.headers).some((k) => k.toLowerCase() === 'content-type')) {
    r.headers['Content-Type'] = /^\s*[[{]/.test(r.body) ? 'application/json' : 'application/x-www-form-urlencoded'
  }
  return r
}
