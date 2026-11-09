#!/usr/bin/env bash
# Run the local diagnostics stack, send a low steady load, save one trace,
# fetch the metrics endpoint, then stop the stack.
set -euo pipefail

COMPOSE_FILE="deploy/compose/compose.yaml"
IMAGE="localhost/inference-gateway:dev"
BASE="http://127.0.0.1:25480"
JAEGER="http://127.0.0.1:25486"
EVIDENCE_DIR="docs/engagement/evidence"
TRACE_FILE="${EVIDENCE_DIR}/jaeger-request-trace.json"
METRICS_FILE="${TMPDIR:-/tmp}/diag-metrics.txt"
PORTS=(25480 25490 25486 25409 25430)

step() {
  printf '==> %s\n' "$1"
}

cleanup() {
  step "stop the stack"
  podman compose -f "${COMPOSE_FILE}" down || true
}
trap cleanup EXIT

step "check that the ports are free"
for port in "${PORTS[@]}"; do
  if ss -ltn "sport = :${port}" | grep -q LISTEN; then
    printf 'port %s is in use. Stop the process that holds it and run again.\n' "${port}" >&2
    exit 1
  fi
done

if podman image exists "${IMAGE}"; then
  step "image ${IMAGE} exists"
else
  step "build the image"
  make image
fi

step "start the stack"
podman compose -f "${COMPOSE_FILE}" up -d

step "wait for ${BASE}/readyz"
ready=0
for _ in $(seq 1 90); do
  if curl -fs -o /dev/null "${BASE}/readyz"; then
    ready=1
    break
  fi
  sleep 1
done
if [ "${ready}" -ne 1 ]; then
  printf 'the gateway did not become ready in 90 s\n' >&2
  exit 1
fi

step "run the load test"
k6 run -e URL="${BASE}/v1/classify" -e RATE=200 -e DURATION=120s loadtest/http.js

step "save one trace with a tokenize span"
mkdir -p "${EVIDENCE_DIR}"
curl -fs "${JAEGER}/api/traces?service=inference-gateway&limit=20" \
  | jq '{data: [.data[] | select(any(.spans[]; .operationName == "tokenize"))][0:1]}' \
  > "${TRACE_FILE}"
if [ "$(jq '.data | length' "${TRACE_FILE}")" -lt 1 ]; then
  printf 'no trace with a tokenize span was found\n' >&2
  exit 1
fi

step "fetch the metrics endpoint"
mkdir -p "$(dirname "${METRICS_FILE}")"
curl -fs "${BASE}/metrics" > "${METRICS_FILE}"
series=$(grep -vc '^#' "${METRICS_FILE}" || true)
printf 'metrics series lines: %s\n' "${series}"

step "done"
