# LoadForge — API load & stress testing (Go + React)

Test a **single API** (URL + bearer/basic/API-key token + headers + JSON payload, or paste a cURL), or import an **OpenAPI 3.x / Swagger 2.0** spec (JSON or YAML) or a **Postman v2/v2.1 collection**, pick a test profile, watch results live, and download a **self-contained graphical HTML report** (plus CSV and JSON).

```
┌──────────── Browser ────────────┐        ┌──────────── backend (Go) ─────────────┐
│ React UI (nginx :3000)          │ /api → │ importers: OpenAPI / Swagger / Postman │
│  Import → Configure → Run → Hist│  SSE ← │ load engine: goroutine VUs, stages,   │──► your APIs
└─────────────────────────────────┘        │ rate limit, HDR-style histograms      │
                                           │ reports: HTML (SVG charts) / CSV / JSON│
                                           │ volume /data: run history              │
                                           └────────────────────────────────────────┘
```

📘 **[User guide](docs/USER_GUIDE.md)** · 🛠 **[Developer skill](skills/loadforge-dev/SKILL.md)**

## Quick start

```bash
docker compose up -d --build
open http://localhost:3000        # change port with LOADFORGE_PORT=8088
```

To try the single-API flow, use `POST http://localhost:8080/demo/secure/orders` with Bearer token `demo-token` and body `{"productId": 7}` (a wrong token returns 401).

Or click **Try the built-in demo API** on the Import screen to run a full test immediately — the backend ships with a small demo target that slows down and starts returning 503s past ~150 concurrent users, so stress tests show a real breaking point.

## Features

| Area | What you get |
|---|---|
| Guided tour | 17-step spotlight tour on first visit (replay via **? Tour**) covering every screen. |
| Single API | Postman-style builder: method + URL, Auth tab (Bearer / Basic / API key, masked), headers, body (JSON with validation & format, form, text, XML), expected status codes. **Import from cURL** (browser “Copy as cURL” works). **Send** a real test request from the load-generator container and inspect status, time, response body & headers before load testing — 401/403 are flagged. Then *Load test this API* or add it to an imported collection. |
| Import | OpenAPI 3.0/3.1, Swagger 2.0, Postman v2.0/v2.1 (folders, `{{vars}}`, `:pathVars`, bearer/basic/apikey auth, raw/urlencoded/form/GraphQL bodies). Upload, paste or fetch from URL. Request bodies are generated from schema examples / `$ref`s. |
| Edit | Enable/disable endpoints, traffic weights, method/path/headers/body, expected status codes. |
| Test types | **Load** (ramp + hold), **Stress** (stepped ramp to find the breaking point), **Spike** (burst & recovery), **Soak** (long duration), **Custom** stages. Live VU-profile preview. |
| Execution | Up to 20k VUs per runner, global max RPS, think time, timeouts, keep-alive, skip-TLS-verify, global headers & variables. Dynamic values: `{{$uuid}}`, `{{$randomInt}}`, `{{$timestamp}}`, `{{$isoTimestamp}}`, `{{$randomEmail}}`. |
| Live view | Streaming KPIs, throughput vs VUs, p50/p95/p99 over time, errors, status codes, latency histogram, per-endpoint table, Stop button. |
| Results | Pass/fail thresholds (p95, p99, error %, min RPS), automatic **breaking-point** detection, key findings, error breakdown. |
| Reports | **HTML** (single file, inline SVG charts, works offline, print-to-PDF ready), **CSV** (per-second series + per-endpoint), **JSON** (everything). History of all runs on a Docker volume. |

## Testing APIs

* **Public / remote API** — use its normal URL as Base URL.
* **API running on your machine** — use `http://host.docker.internal:<port>` (not `localhost`, which means the LoadForge container).
* **API in another compose project** — attach the backend to that network, or use the host port.
* Secrets: auth headers from the spec become `{{token}}`-style variables; fill them on the Configure screen (empty ones are highlighted).

> Only load test systems you own or are authorised to test.

## Use from CI (REST API)

Uncomment `ports: 8080:8080` on the backend service, then:

```bash
# import a spec
curl -s -X POST --data-binary @openapi.json http://localhost:8080/api/import > collection.json   # YAML → convert to JSON first (e.g. yq -o=json)

# start a run (TestConfig: name,type,baseUrl,headers,variables,endpoints,stages,maxRps,timeoutMs,thresholds…)
curl -s -X POST -H 'Content-Type: application/json' -d @config.json http://localhost:8080/api/runs   # → {"id":"…"}

curl -s http://localhost:8080/api/runs/<id>                      # status + summary (summary.passed)
curl -o report.html "http://localhost:8080/api/runs/<id>/report/html?download=1"
```

| Endpoint | Purpose |
|---|---|
| `POST /api/import` | Parse spec/collection (JSON body) → endpoints |
| `POST /api/fetch-spec` | `{url}` → raw spec text |
| `POST /api/try` | Send one request `{method,url,headers,body}` → status, time, headers, body |
| `POST /api/runs` · `GET /api/runs` | Start / list runs |
| `GET /api/runs/{id}` · `DELETE /api/runs/{id}` | Run detail / delete |
| `POST /api/runs/{id}/stop` | Stop early |
| `GET /api/runs/{id}/stream` | Server-Sent Events live metrics |
| `GET /api/runs/{id}/report/{html\|csv\|json}` | Reports (`?download=1` to save) |

## Local development

```bash
cd backend && go run .                    # :8080 (stdlib only, no deps)
cd frontend && npm install && npm run dev # :5173, proxies /api to :8080
```
End-to-end check: `scripts/smoke.sh` (build, import, single request, 6 s load test, all report formats).

Single-binary mode: `npm run build` then `STATIC_DIR=../frontend/dist go run .` serves UI + API on :8080.

## Tuning for big tests

* Latency is measured client-side, so the load generator must not be the bottleneck — give the backend container enough CPU (`deploy.resources` / `cpus:`), and watch its CPU while testing.
* The compose file raises `nofile` to 65535 and widens the ephemeral port range; keep **keep-alive on** for high RPS.
* Latency percentiles use a log-bucketed histogram (~1% precision) so memory stays constant even for multi-hour soak tests; charts are down-sampled to ~600 points.
* Up to 3 runs can execute concurrently per backend.

## Project layout

```
docker-compose.yml
backend/   main.go (HTTP API) · importer.go · engine.go · metrics.go · report.go · store.go · demo.go · Dockerfile
frontend/  src/{App,ImportPage,SingleRequest,ConfigPage,RunPage,HistoryPage,Tour}.jsx · curl.js · presets.js · nginx.conf · Dockerfile
docs/      USER_GUIDE.md + screenshots
skills/    loadforge-dev/SKILL.md (how to extend, test and ship)
scripts/   smoke.sh
```
