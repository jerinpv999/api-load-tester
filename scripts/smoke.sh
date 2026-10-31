#!/usr/bin/env bash
# End-to-end smoke test: builds the backend, imports the demo spec, runs a short
# load test, a single-request check and verifies all three report formats.
# Usage: scripts/smoke.sh            (needs go, curl, python3)
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PORT="${PORT:-18080}"
TMP="$(mktemp -d)"
BIN="$TMP/loadforge"
API="http://127.0.0.1:$PORT"

cleanup() { [[ -n "${PID:-}" ]] && kill "$PID" 2>/dev/null || true; rm -rf "$TMP"; }
trap cleanup EXIT

echo "▸ build"
(cd "$ROOT/backend" && go vet ./... && go build -o "$BIN" .)

echo "▸ start on :$PORT"
DATA_DIR="$TMP/data" PORT="$PORT" "$BIN" >"$TMP/server.log" 2>&1 &
PID=$!
for _ in $(seq 1 50); do curl -sf "$API/api/health" >/dev/null && break; sleep 0.1; done

echo "▸ import demo OpenAPI spec"
curl -sf "$API/demo/openapi.json" | curl -sf -X POST --data-binary @- "$API/api/import" >"$TMP/col.json"

echo "▸ single request (token-protected endpoint)"
curl -sf -X POST "$API/api/try" -H 'Content-Type: application/json' \
  -d "{\"method\":\"POST\",\"url\":\"$API/demo/secure/orders\",\"headers\":{\"Authorization\":\"Bearer demo-token\",\"Content-Type\":\"application/json\"},\"body\":\"{\\\"productId\\\":1}\"}" \
  | python3 -c 'import sys,json; r=json.load(sys.stdin); assert r["status"]==201, r; print("  status", r["status"])'

echo "▸ run 6s load test"
ID=$(python3 - "$TMP/col.json" "$API" <<'EOF'
import json, sys, urllib.request
col = json.load(open(sys.argv[1]))
cfg = {"name": "smoke", "type": "load", "baseUrl": col["baseUrl"], "variables": col["variables"],
       "endpoints": col["endpoints"], "stages": [{"durationSec": 1, "targetVus": 10}, {"durationSec": 5, "targetVus": 10}],
       "maxRps": 200, "keepAlive": True, "thresholds": {"errorRatePct": 5}}
req = urllib.request.Request(sys.argv[2] + "/api/runs", json.dumps(cfg).encode(), {"Content-Type": "application/json"})
print(json.load(urllib.request.urlopen(req))["id"])
EOF
)
echo "  run $ID"
for _ in $(seq 1 60); do
  STATUS=$(curl -sf "$API/api/runs/$ID" | python3 -c 'import sys,json;print(json.load(sys.stdin)["status"])')
  [[ "$STATUS" != "running" ]] && break; sleep 0.5
done
curl -sf "$API/api/runs/$ID" | python3 -c '
import sys, json
r = json.load(sys.stdin); s = r["summary"]
assert r["status"] == "completed", r["status"]
assert s["requests"] > 500, s["requests"]
assert 150 <= s["avgRps"] <= 230, s["avgRps"]
print("  %d req, %.1f req/s, p95 %.1f ms, errors %.2f%%" % (s["requests"], s["avgRps"], s["latency"]["p95"], s["errorRate"]))'

echo "▸ reports"
curl -sf "$API/api/runs/$ID/report/html" | grep -q "<svg" && echo "  html ok"
curl -sf "$API/api/runs/$ID/report/csv" | head -1 | grep -q "^second," && echo "  csv ok"
curl -sf "$API/api/runs/$ID/report/json" | python3 -c 'import sys,json;json.load(sys.stdin)' && echo "  json ok"

echo "✓ smoke test passed"
