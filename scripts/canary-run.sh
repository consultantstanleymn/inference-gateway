#!/usr/bin/env bash
# Run a canary pass or a forced-fail rollback and print the controller output
# and the k6 summary. Usage: scripts/canary-run.sh pass|fail
set -euo pipefail

usage() {
  echo "usage: $0 pass|fail" >&2
  exit 2
}

if [ "$#" -ne 1 ]; then
  usage
fi

MODE="$1"
case "$MODE" in
  pass|fail) ;;
  *) usage ;;
esac

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

STEP_DURATION="${STEP_DURATION:-20s}"
MIN_REQUESTS="${MIN_REQUESTS:-100}"
RATE="${RATE:-300}"

STABLE_ADDR="127.0.0.1:25480"
CANARY_ADDR="127.0.0.1:25481"
V2_ADDR="127.0.0.1:25482"
STUB_ADDR="127.0.0.1:25483"
STABLE_GRPC="127.0.0.1:25490"
V2_GRPC="127.0.0.1:25492"

for port in 25480 25481 25482 25483; do
  if ss -ltn "sport = :$port" | grep -q LISTEN; then
    echo "port $port is in use" >&2
    exit 1
  fi
done

TMPDIR_RUN="$(mktemp -d)"
PIDS=()

cleanup() {
  for pid in "${PIDS[@]:-}"; do
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
    fi
  done
  for pid in "${PIDS[@]:-}"; do
    if [ -n "$pid" ]; then
      wait "$pid" 2>/dev/null || true
    fi
  done
  rm -rf "$TMPDIR_RUN"
}
trap cleanup EXIT

echo "building binaries into $TMPDIR_RUN"
go build -o "$TMPDIR_RUN/gateway" ./cmd/gateway
go build -o "$TMPDIR_RUN/canary" ./cmd/canary
go build -o "$TMPDIR_RUN/stubfail" ./cmd/stubfail

wait_ready() {
  local url="$1"
  local name="$2"
  for _ in $(seq 1 100); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.2
  done
  echo "$name did not become ready at $url" >&2
  return 1
}

echo "starting gateway v1 on $STABLE_ADDR"
GATEWAY_MODEL_DIR=models/v1 GATEWAY_HTTP_ADDR="$STABLE_ADDR" GATEWAY_GRPC_ADDR="$STABLE_GRPC" \
  "$TMPDIR_RUN/gateway" >"$TMPDIR_RUN/gateway-v1.log" 2>&1 &
PIDS+=("$!")
wait_ready "http://$STABLE_ADDR/readyz" "gateway v1"

echo "starting gateway v2 on $V2_ADDR"
GATEWAY_MODEL_DIR=models/v2 GATEWAY_HTTP_ADDR="$V2_ADDR" GATEWAY_GRPC_ADDR="$V2_GRPC" \
  "$TMPDIR_RUN/gateway" >"$TMPDIR_RUN/gateway-v2.log" 2>&1 &
PIDS+=("$!")
wait_ready "http://$V2_ADDR/readyz" "gateway v2"

CANARY_UPSTREAM="http://$V2_ADDR"
if [ "$MODE" = "fail" ]; then
  echo "starting stubfail on $STUB_ADDR in front of $V2_ADDR"
  "$TMPDIR_RUN/stubfail" -listen "$STUB_ADDR" -upstream "http://$V2_ADDR" -fail-rate 0.3 \
    >"$TMPDIR_RUN/stubfail.log" 2>&1 &
  PIDS+=("$!")
  wait_ready "http://$STUB_ADDR/readyz" "stubfail"
  CANARY_UPSTREAM="http://$STUB_ADDR"
fi

STEPS="10,25,50,100"
STEP_COUNT=4
DURATION_SECONDS=$(( STEP_COUNT * ${STEP_DURATION%s} + 15 ))

echo "starting canary proxy on $CANARY_ADDR (mode $MODE)"
"$TMPDIR_RUN/canary" \
  -listen "$CANARY_ADDR" \
  -stable "http://$STABLE_ADDR" \
  -canary "$CANARY_UPSTREAM" \
  -steps "$STEPS" \
  -step-duration "$STEP_DURATION" \
  -min-requests "$MIN_REQUESTS" \
  >"$TMPDIR_RUN/canary.log" 2>&1 &
CANARY_PID="$!"
PIDS+=("$CANARY_PID")

wait_ready "http://$CANARY_ADDR/readyz" "canary proxy"

echo "running k6 for ${DURATION_SECONDS}s at rate $RATE"
K6_OUT="$TMPDIR_RUN/k6.log"
set +e
URL="http://$CANARY_ADDR/v1/classify" RATE="$RATE" DURATION="${DURATION_SECONDS}s" \
  k6 run loadtest/http.js >"$K6_OUT" 2>&1
K6_STATUS=$?
set -e

# Let the controller finish its last step, then stop it with a signal.
sleep 2
kill -TERM "$CANARY_PID" 2>/dev/null || true
wait "$CANARY_PID" 2>/dev/null || true

echo "===== canary controller output ====="
cat "$TMPDIR_RUN/canary.log"
echo "===== k6 summary ====="
grep -E '^K6SUMMARY' "$K6_OUT" || true
echo "===== k6 exit status: $K6_STATUS ====="

if [ "$K6_STATUS" -ne 0 ]; then
  echo "k6 exited with status $K6_STATUS" >&2
fi
