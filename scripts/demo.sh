#!/usr/bin/env bash
# Print a short real session: model gate, one HTTP call, one gRPC call, overload, metrics, shutdown.
set -uo pipefail
cd "$(dirname "$0")/.."
free_port() { python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])'; }
go build -o bin/gateway ./cmd/gateway && go build -o bin/probe ./cmd/probe
echo '$ python train/gate.py models/v1 models/v2'; .venv/bin/python train/gate.py models/v1 models/v2
HP=$(free_port); GP=$(free_port)
echo; echo "\$ bin/gateway -http-addr 127.0.0.1:$HP -grpc-addr 127.0.0.1:$GP"
bin/gateway -http-addr 127.0.0.1:$HP -grpc-addr 127.0.0.1:$GP > out/demo-gw.log 2>&1 & GPID=$!
for _ in $(seq 60); do curl -sf "localhost:$HP/readyz" >/dev/null && break; sleep 0.3; done
echo; echo '$ curl -XPOST /v1/classify'
curl -s -XPOST "localhost:$HP/v1/classify" -d '{"text":"Why was my transfer charged a fee?"}'; echo
echo; echo '$ bin/probe grpc'
bin/probe grpc -addr 127.0.0.1:$GP -text "Why was my transfer charged a fee?"
echo; echo '$ curl -XPOST /v1/classify -d {"text":""}'
curl -s -w ' (HTTP %{http_code})\n' -XPOST "localhost:$HP/v1/classify" -d '{"text":""}'
echo; echo '$ k6 burst of 800 req/s for 8 s against max_batch=16'
k6 run -q -e URL=http://127.0.0.1:$HP/v1/classify -e RATE=800 -e DURATION=8s loadtest/http.js 2>&1 | grep K6SUMMARY
echo; echo '$ curl /metrics | grep gateway_'
curl -s "localhost:$HP/metrics" | grep -E '^gateway_(requests_total|batch_size_(sum|count)|shed_total|model_info|queue_depth)'
echo; echo '$ kill -TERM <gateway>'
kill -TERM $GPID; wait $GPID; echo "exit code $?"; cat out/demo-gw.log
echo; echo "== overload demo: queue-size 4, max-batch 1, 15000 req/s target (more than this laptop can serve)"
HP=$(free_port); GP=$(free_port)
bin/gateway -http-addr 127.0.0.1:$HP -grpc-addr 127.0.0.1:$GP -max-batch 1 -queue-size 4 > out/demo-gw2.log 2>&1 & GPID=$!
for _ in $(seq 60); do curl -sf "localhost:$HP/readyz" >/dev/null && break; sleep 0.3; done
k6 run -q -e URL=http://127.0.0.1:$HP/v1/classify -e RATE=15000 -e DURATION=6s loadtest/http.js 2>&1 | grep K6SUMMARY
curl -s "localhost:$HP/metrics" | grep -E '^gateway_requests_total'
kill -TERM $GPID; wait $GPID
