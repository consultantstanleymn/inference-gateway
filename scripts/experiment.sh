#!/usr/bin/env bash
# One-factor experiment on the gateway. Change one setting per run, keep the others fixed.
#
# Usage:
#   scripts/experiment.sh --factor workers|max-batch|max-wait|cpus --values "1 2 4" --rate 6000 --duration 60s
#
# For each value the script:
#   1. starts the gateway on the host (HTTP 25480, gRPC 25490, admin 25406),
#   2. waits for /readyz,
#   3. runs k6 loadtest/http.js at a fixed arrival rate,
#   4. takes a 20 second CPU profile from the admin port in the middle of the run,
#   5. writes the k6 summary, the profile and the pprof top list into the evidence folder,
#   6. stops the gateway by its process id.
# Run it under the heavy lock: flock <heavy.lock> nice -n 10 scripts/experiment.sh ...
# The load generator shares the machine with the gateway. Read the numbers as relative.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

FACTOR=""
VALUES=""
RATE=6000
DURATION=60s
OUT="docs/engagement/evidence/experiments"
HTTP_PORT=25480
GRPC_PORT=25490
ADMIN_PORT=25406

while [[ $# -gt 0 ]]; do
  case "$1" in
    --factor) FACTOR="$2"; shift 2 ;;
    --values) VALUES="$2"; shift 2 ;;
    --rate) RATE="$2"; shift 2 ;;
    --duration) DURATION="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

case "$FACTOR" in
  workers|max-batch|max-wait|cpus) ;;
  *) echo "--factor must be workers, max-batch, max-wait or cpus" >&2; exit 2 ;;
esac
[[ -n "$VALUES" ]] || { echo "--values is required" >&2; exit 2; }

# Fixed settings. The factor under test replaces one of them.
WORKERS=1
MAX_BATCH=16
MAX_WAIT=5ms
CPUS=$(nproc)
ORT_THREADS=1
QUEUE_SIZE=1024
REQUEST_TIMEOUT=5s

for port in "$HTTP_PORT" "$GRPC_PORT" "$ADMIN_PORT"; do
  if ss -ltn | grep -q ":${port}\b"; then
    echo "port ${port} is already in use; stop and report" >&2
    exit 1
  fi
done

mkdir -p "$OUT"
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

echo "building the gateway"
go build -o "$TMP/gateway" ./cmd/gateway

for value in $VALUES; do
  case "$FACTOR" in
    workers) WORKERS="$value" ;;
    max-batch) MAX_BATCH="$value" ;;
    max-wait) MAX_WAIT="$value" ;;
    cpus) CPUS="$value" ;;
  esac
  echo "== ${FACTOR}=${value}  fixed: workers=${WORKERS} max-batch=${MAX_BATCH} max-wait=${MAX_WAIT} cpus=${CPUS} ort-threads=${ORT_THREADS} queue=${QUEUE_SIZE} rate=${RATE} duration=${DURATION}"
  name="${FACTOR}-${value}"

  GATEWAY_MODEL_DIR="models/v1" \
  GATEWAY_HTTP_ADDR="127.0.0.1:${HTTP_PORT}" \
  GATEWAY_GRPC_ADDR="127.0.0.1:${GRPC_PORT}" \
  GATEWAY_ADMIN_ADDR="127.0.0.1:${ADMIN_PORT}" \
  GATEWAY_WORKERS="$WORKERS" \
  GATEWAY_MAX_BATCH="$MAX_BATCH" \
  GATEWAY_MAX_WAIT="$MAX_WAIT" \
  GATEWAY_ORT_THREADS="$ORT_THREADS" \
  GATEWAY_QUEUE_SIZE="$QUEUE_SIZE" \
  GATEWAY_REQUEST_TIMEOUT="$REQUEST_TIMEOUT" \
    taskset -c "0-$((CPUS - 1))" "$TMP/gateway" >"$TMP/${name}.log" 2>&1 &
  GW_PID=$!

  ready=""
  for _ in $(seq 1 60); do
    if curl -fs "http://127.0.0.1:${HTTP_PORT}/readyz" >/dev/null 2>&1; then ready=yes; break; fi
    sleep 1
  done
  [[ -n "$ready" ]] || { echo "gateway did not become ready" >&2; tail -20 "$TMP/${name}.log" >&2; exit 1; }

  # Profile in the middle of the run: wait 10 seconds, then record 20 seconds.
  (
    sleep 10
    curl -fs -o "${OUT}/${name}.pb.gz" "http://127.0.0.1:${ADMIN_PORT}/debug/pprof/profile?seconds=20" || true
  ) &
  PROF_PID=$!

  # CPU time of the gateway process during the k6 run, from /proc (user + system ticks).
  ticks_per_sec="$(getconf CLK_TCK)"
  cpu_before="$(awk '{print $14 + $15}' "/proc/${GW_PID}/stat")"
  t_before="$(date +%s.%N)"
  k6 run --quiet \
    -e URL="http://127.0.0.1:${HTTP_PORT}/v1/classify" \
    -e RATE="$RATE" -e DURATION="$DURATION" \
    loadtest/http.js >"$TMP/${name}.k6.txt" 2>&1 || echo "k6 returned a non-zero exit code; see the summary file"
  cpu_after="$(awk '{print $14 + $15}' "/proc/${GW_PID}/stat")"
  t_after="$(date +%s.%N)"
  cores_used="$(awk -v a="$cpu_before" -v b="$cpu_after" -v t0="$t_before" -v t1="$t_after" -v hz="$ticks_per_sec" 'BEGIN { printf "%.2f", ((b - a) / hz) / (t1 - t0) }')"
  # loadtest/http.js prints one line: K6SUMMARY <json>. Keep it with the settings of this run.
  summary="$(sed -n 's/^.*K6SUMMARY //p' "$TMP/${name}.k6.txt" | tail -1)"
  [[ -n "$summary" ]] || { echo "no k6 summary line for ${name}" >&2; tail -20 "$TMP/${name}.k6.txt" >&2; exit 1; }
  jq -n --arg factor "$FACTOR" --arg value "$value" --argjson summary "$summary" --argjson cores_used "$cores_used" \
    --argjson workers "$WORKERS" --argjson max_batch "$MAX_BATCH" --arg max_wait "$MAX_WAIT" \
    --argjson cpus "$CPUS" --argjson ort_threads "$ORT_THREADS" --argjson queue_size "$QUEUE_SIZE" --arg duration "$DURATION" \
    '{factor: $factor, value: $value, settings: {workers: $workers, max_batch: $max_batch, max_wait: $max_wait, cpus: $cpus, ort_threads: $ort_threads, queue_size: $queue_size, duration: $duration}, summary: $summary, measured: {gateway_cpu_cores_used: $cores_used}}' \
    >"${OUT}/${name}.json"
  wait "$PROF_PID" || true

  if [[ -s "${OUT}/${name}.pb.gz" ]]; then
    go tool pprof -top -nodecount=25 "$TMP/gateway" "${OUT}/${name}.pb.gz" >"${OUT}/${name}.top.txt" 2>&1 || true
  else
    echo "no profile was recorded for ${name}" >"${OUT}/${name}.top.txt"
  fi

  kill "$GW_PID" 2>/dev/null || true
  wait "$GW_PID" 2>/dev/null || true
  GW_PID=""
  sleep 2
done
echo "done: files are in ${OUT}"
