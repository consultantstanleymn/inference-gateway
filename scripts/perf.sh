#!/usr/bin/env bash
# Compare max_batch=1 and max_batch=16 at fixed arrival rates (HTTP and gRPC, k6).
# Output: out/perf-<transport>.jsonl and a table on stdout. Run on a quiet machine.
set -euo pipefail
cd "$(dirname "$0")/.."
RATES=${RATES:-"100 200 400 800"}
DURATION=${DURATION:-20s}
MAXWAIT=${MAXWAIT:-5ms}
mkdir -p out; : > out/perf-http.jsonl; : > out/perf-grpc.jsonl
free_port() { python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])'; }
go build -o bin/gateway ./cmd/gateway
{ echo "date: $(date -Is)"; grep -m1 'model name' /proc/cpuinfo; echo "threads: $(nproc)"; echo "load before: $(cut -d' ' -f1-3 /proc/loadavg)"; } | tee out/perf-env.txt
for mb in 1 16; do
  hp=$(free_port); gp=$(free_port)
  nice -n 10 bin/gateway -http-addr 127.0.0.1:$hp -grpc-addr 127.0.0.1:$gp -max-batch $mb -max-wait "$MAXWAIT" -queue-size 1024 -request-timeout 5s > out/gw-$mb.log 2>&1 &
  gpid=$!
  for _ in $(seq 60); do curl -sf "localhost:$hp/readyz" >/dev/null && break; sleep 0.5; done
  for r in $RATES; do
    for t in http grpc; do
      if [ $t = http ]; then
        line=$(nice -n 10 k6 run -q -e URL=http://127.0.0.1:$hp/v1/classify -e RATE=$r -e DURATION=$DURATION loadtest/http.js 2>&1 | { grep K6SUMMARY || echo 'K6SUMMARY {"error":"no summary"}'; } | sed 's/^K6SUMMARY //')
      else
        line=$(cd loadtest && nice -n 10 k6 run -q -e ADDR=127.0.0.1:$gp -e RATE=$r -e DURATION=$DURATION grpc.js 2>&1 | { grep K6SUMMARY || echo 'K6SUMMARY {"error":"no summary"}'; } | sed 's/^K6SUMMARY //')
      fi
      echo "{\"max_batch\":$mb,\"transport\":\"$t\",\"summary\":$line}" >> out/perf-$t.jsonl
      echo "max_batch=$mb $t rate=$r $line"
    done
  done
  echo "-- batch size histogram for max_batch=$mb"
  curl -s "localhost:$hp/metrics" | grep -E '^gateway_(batch_size_(sum|count)|queue_wait_seconds_(sum|count)|batch_run_seconds_(sum|count)) '
  kill -TERM $gpid; wait $gpid || true
done
