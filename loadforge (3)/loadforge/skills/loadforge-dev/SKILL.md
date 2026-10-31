---
name: loadforge-dev
description: Use when adding features to, fixing, or shipping LoadForge — the Go + React API load/stress testing app (OpenAPI/Postman/single-request import, VU engine, live SSE dashboard, HTML/CSV/JSON reports, guided tour, Docker Compose).
---

# LoadForge development

LoadForge load- and stress-tests HTTP APIs. A Go backend generates the load and writes reports; a React UI drives it; nginx + Docker Compose ship it. Read this before changing anything so changes stay consistent with the existing design.

## 1. Map of the code

```
docker-compose.yml        backend (Go, :8080, volume /data) + frontend (nginx :80 → host ${LOADFORGE_PORT:-3000})
backend/                  Go 1.24, **standard library only** (no go.sum, no third-party modules)
  main.go                 HTTP routes (Go 1.22 ServeMux patterns), run manager (max 3 concurrent), SSE stream, report download
  models.go               All JSON types: Endpoint, Collection, Stage, TestConfig, Point, Summary, Run …
  importer.go             OpenAPI 3.x / Swagger 2.0 / Postman v2.x → Collection (schema sample generator, $ref resolver, auth → {{vars}})
  engine.go               Engine: VU controller (100 ms tick, linear stage ramps), workers, rate limiter, aggregator (1 Point/s), thresholds + breaking point
  metrics.go              Log-bucket Histogram (≈1 % precision, constant memory) → LatencyStats, chart buckets
  report.go               Self-contained HTML report (inline SVG charts, print CSS), CSV, findings text
  try.go                  POST /api/try — one real request for the single-API builder
  store.go                Finished runs as /data/<id>.json + in-memory index
  demo.go                 Built-in demo target API (/demo/*) incl. token-protected POST /demo/secure/orders (Bearer demo-token)
frontend/                 React 18 + Vite 5 + recharts + js-yaml
  src/App.jsx             Tabs (API / Configure / Run & results / History), localStorage persistence, tour wiring
  src/ImportPage.jsx      Import modes (single, file, paste, URL) + endpoint list/editor
  src/SingleRequest.jsx   Postman-style builder: auth/headers/body/options, cURL import, Send, “Load test this API”
  src/curl.js             cURL tokenizer/parser
  src/ConfigPage.jsx      Test-type presets, stage editor, VU preview chart, target/headers/vars, thresholds, Start
  src/RunPage.jsx         EventSource live dashboard, charts, downloads
  src/HistoryPage.jsx     Run list, downloads, delete
  src/Tour.jsx            Guided tour: TOUR_STEPS + spotlight/card positioning
  src/presets.js          TEST_TYPES (load/stress/spike/soak/custom → stages) + formatters
  src/api.js              All fetch calls (relative /api, works behind nginx and Vite proxy)
  nginx.conf              SPA + /api proxy (SSE location has proxy_buffering off) + /demo proxy
scripts/smoke.sh          End-to-end backend check (build, import, try, run, reports)
docs/USER_GUIDE.md        End-user documentation — update it with every user-visible change
```

## 2. Data flow

1. UI turns any input into a **Collection** `{name, source, baseUrl, headers, variables, endpoints[]}`. YAML is converted to JSON **in the browser** (js-yaml) so the backend stays dependency-free. Single requests build the Collection client-side (`source: "single"`, auth header inside the endpoint's headers).
2. ConfigPage sends a **TestConfig** to `POST /api/runs`; `validateConfig` keeps only enabled endpoints and checks URLs.
3. `Engine` spawns one goroutine per VU; each picks an endpoint by weight, substitutes `{{vars}}` / `{{$dynamic}}`, sends, and pushes a `result` onto a buffered channel (dropped, never blocking, if the aggregator is saturated).
4. The aggregator flushes one **Point** per second and rebuilds Summary/EndpointSummary; `broadcast()` pushes a `tick` SSE message. On finish: `evaluate()` (thresholds + breaking point), store save, `done` event.
5. Reports are rendered on demand from the stored Run: `/api/runs/{id}/report/{html|csv|json}` (`?download=1` adds Content-Disposition).

## 3. Rules to keep

- **Backend stays stdlib-only.** Do not add Go modules; the build must work with just `go build`. Need YAML or similar? Do it in the frontend.
- **Every JSON slice must serialize as `[]`, not `null`** (`make([]T, 0)`), or the UI crashes — this already bit `Snapshot().Series`. The UI also normalises with `norm()` in RunPage.
- **ServeMux wildcards must be whole path segments** (`/report/{ext}`, not `/report.{ext}` — that panics at startup).
- **Never block the load path**: no locks held during HTTP calls; metrics go through the channel; per-request allocations kept minimal (static URL/headers are pre-resolved in `NewEngine`; only `dynamic` endpoints re-substitute).
- **Keep latency memory constant**: use `Histogram`, never store per-request samples.
- **Reports must stay self-contained** (no CDN, no JS required): inline SVG + CSS only, so they open offline and print to PDF.
- **Mask secrets in UI summaries** (Authorization, api-key, token). Tokens may live in localStorage — never send them anywhere except the target API.
- New UI targets for the tour get a `data-tour="…"` attribute (or `tour="…"` on `<Card>`).
- Styling: plain CSS in `styles.css` using the existing tokens (`--accent`, `--ink`, `--muted`, `--line`, `--soft`, `--red/--green/--amber`); chart colours blue #2563eb, amber #d97706, red #dc2626, green #059669, violet #7c3aed, slate #64748b — same in the Go report palette.
- Inside Docker, `localhost` is the backend container. Error messages and docs should steer users to `host.docker.internal` (compose already maps it for Linux via `host-gateway`).

## 4. Recipes

**Add an import format** (e.g. HAR, Insomnia)
1. Detect it in `ImportDocument` (importer.go) and write `importX(doc) (*Collection, error)` producing `Endpoint{ID, Name, Group, Method, Path, Headers, Body, Weight:1, Enabled:true}`; put shared auth in `col.Headers` with `{{var}}` placeholders and default values in `col.Variables`.
2. If the format isn't JSON, convert it in `frontend/src/api.js:importSpec` before POSTing.
3. Add a sample to `scripts/smoke.sh` or test with `curl -X POST --data-binary @file /api/import`.
4. Document it in USER_GUIDE.md (“Importing”) and README feature table.

**Add a metric** (e.g. TTFB, bytes out)
1. Capture it in `Engine.do` → `result`; accumulate in `record`; emit in `flushLocked` (`Point`) and/or `buildSummaryLocked` (`Summary`).
2. Add fields to models.go (JSON camelCase).
3. Show it: RunPage KPI/chart, report.go (`kpis`, a `timeChart` series, CSV columns — append columns at the end to keep CSV consumers working).

**Add a test type preset** — add an entry to `TEST_TYPES` in presets.js (`label, blurb, params, fields, stages(p)`); the engine only sees stages. Update the tour step “Pick a test type” and the guide.

**Add an API endpoint** — register in main.go (`METHOD /api/...`), use `writeJSON` / `apiErr`, add a helper in api.js. Everything under `/api/` is already proxied by nginx; a streaming endpoint needs its own nginx `location` with `proxy_buffering off`.

**Add / change a tour step** — edit `TOUR_STEPS` in Tour.jsx: `{tab, target, title, body, needsCollection?}`. Missing targets fall back to a centred card, so steps for pages that may be empty (Run) are safe. Steps on Configure need `needsCollection: true` (the tour loads the demo API if nothing is imported). Keep the step count in USER_GUIDE.md in sync. To reset the auto-start: `localStorage.removeItem('lf.tourDone')`.

**Change the report** — edit `RenderHTML` in report.go. Use `timeChart` (time series, optional right axis, breaking-point marker), `hbars`, `histogramChart`; escape all user text with `html.EscapeString`. Check visually (see §5).

## 5. Verify before shipping

```bash
cd backend && gofmt -l . && go vet ./... && go build ./...     # must print nothing from gofmt
cd frontend && npm install && npx vite build                    # must succeed
scripts/smoke.sh                                                # ✓ smoke test passed
```

UI changes: run single-binary mode and look at it in a headless browser.
```bash
cd frontend && npx vite build
cd ../backend && STATIC_DIR=../frontend/dist DATA_DIR=/tmp/lfdata go run .   # UI + API on :8080
```
With Playwright + Chrome, launch with `args=["--no-sandbox", "--no-proxy-server"]` (without `--no-proxy-server` page loads can hang behind an HTTP proxy). Clear `localStorage` first so the tour auto-starts; walk all tour steps and screenshot key pages at 1280 px and 390 px widths; assert there are no `pageerror` events.

Report: `curl localhost:8080/api/runs/<id>/report/html -o r.html` and screenshot it (`google-chrome --headless=new --screenshot=r.png --window-size=1200,3600 file://$PWD/r.html`).

## 6. Ship

1. Update `docs/USER_GUIDE.md` and README for user-visible changes.
2. `docker compose config -q` (syntax) then `docker compose up -d --build`; open `http://localhost:${LOADFORGE_PORT:-3000}`, run the demo stress test end to end, download the HTML report.
3. Base images: `golang:1.24-alpine`, `alpine:3.20`, `node:22-alpine`, `nginx:1.27-alpine`. Keep `package-lock.json` committed so `npm ci` is reproducible.
4. Data lives in the `loadforge-data` volume; the JSON run format is the compatibility contract — only add fields, never rename.
