#!/usr/bin/env bash
# Run k6 and Locust against the same gateway at the same target rates and print one table.
# Run it under the heavy lock: flock <heavy.lock> nice -n 10 scripts/locust-vs-k6.sh
# Needs: k6, the Locust virtualenv (TV=<path to tools venv>), go, jq.
# The load generators share the machine with the gateway. Read the numbers as relative.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

TV="${TV:?set TV to the tools virtualenv that holds locust}"
RATES="${RATES:-2000 4000}"
DURATION="${DURATION:-60}"
HTTP_PORT=25480
GRPC_PORT=25490
OUT="docs/engagement/evidence"
RPS_PER_USER=10

for port in "$HTTP_PORT" "$GRPC_PORT"; do
  if ss -ltn | grep -q ":${port}\b"; then
    echo "port ${port} is already in use; stop and report" >&2
    exit 1
  fi
done

TMP="$(mktemp -d)"
GW_PID=""
cleanup() {
  if [[ -n "$GW_PID" ]] && kill -0 "$GW_PID" 2>/dev/null; then
    kill "$GW_PID" 2>/dev/null || true
    wait "$GW_PID" 2>/dev/null || true
  fi
  rm -rf "$TMP"
}
trap cleanup EXIT

echo "load average before: $(cut -d' ' -f1-3 /proc/loadavg)"
go build -o "$TMP/gateway" ./cmd/gateway
GATEWAY_MODEL_DIR=models/v1 \
GATEWAY_HTTP_ADDR="127.0.0.1:${HTTP_PORT}" \
GATEWAY_GRPC_ADDR="127.0.0.1:${GRPC_PORT}" \
GATEWAY_WORKERS=1 GATEWAY_MAX_BATCH=16 GATEWAY_MAX_WAIT=5ms GATEWAY_ORT_THREADS=1 \
GATEWAY_QUEUE_SIZE=1024 GATEWAY_REQUEST_TIMEOUT=5s \
  "$TMP/gateway" >"$TMP/gateway.log" 2>&1 &
GW_PID=$!
for _ in $(seq 1 60); do
  if curl -fs "http://127.0.0.1:${HTTP_PORT}/readyz" >/dev/null 2>&1; then break; fi
  sleep 1
done
curl -fs "http://127.0.0.1:${HTTP_PORT}/readyz" >/dev/null || { echo "gateway not ready" >&2; exit 1; }
echo "gateway settings: workers=1 max-batch=16 max-wait=5ms ort-threads=1 queue=1024 timeout=5s, model v1"

printf '\n%-8s | %-7s | %12s | %8s | %8s | %9s\n' "target" "tool" "achieved rps" "p50 ms" "p95 ms" "failures"
for rate in $RATES; do
  # k6 first.
  k6 run --quiet -e URL="http://127.0.0.1:${HTTP_PORT}/v1/classify" -e RATE="$rate" -e DURATION="${DURATION}s" \
    loadtest/http.js >"$TMP/k6-${rate}.txt" 2>&1 || true
  summary="$(sed -n 's/^.*K6SUMMARY //p' "$TMP/k6-${rate}.txt" | tail -1)"
  [[ -n "$summary" ]] || { echo "no k6 summary for ${rate}" >&2; exit 1; }
  printf '%-8s | %-7s | %12s | %8s | %8s | %9s\n' "$rate" "k6" \
    "$(jq -r .throughput_rps <<<"$summary")" "$(jq -r .p50_ms <<<"$summary")" "$(jq -r .p95_ms <<<"$summary")" \
    "$(jq -r '(.failed_rate * .requests | floor)' <<<"$summary")"
  sleep 5

  # Locust: users x rate per user = target.
  users=$((rate / RPS_PER_USER))
  LOCUST_RPS_PER_USER="$RPS_PER_USER" "$TV/bin/locust" -f loadtest/locustfile.py --headless \
    -u "$users" -r 50 -t "${DURATION}s" --host "http://127.0.0.1:${HTTP_PORT}" \
    --csv "$TMP/locust-${rate}" --html "${OUT}/locust-${rate}.html" >"$TMP/locust-${rate}.txt" 2>&1 || true
  python3 - "$TMP/locust-${rate}_stats.csv" "$rate" "$DURATION" <<'PY'
import csv, sys
rows = list(csv.DictReader(open(sys.argv[1], encoding="utf-8")))
agg = next(r for r in rows if r["Name"] == "Aggregated")
reqs = int(agg["Request Count"]); fails = int(agg["Failure Count"])
# Average over the full run, including the short ramp. This matches how k6 reports requests / time.
achieved = round(reqs / float(sys.argv[3]), 1)
print(f'{sys.argv[2]:<8} | {"locust":<7} | {achieved:>12} | {agg["50%"]:>8} | {agg["95%"]:>8} | {fails:>9}')
PY
  sleep 5
done
echo "load average after: $(cut -d' ' -f1-3 /proc/loadavg)"
