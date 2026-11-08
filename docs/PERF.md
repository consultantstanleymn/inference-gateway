# Performance

All numbers are real, measured on 2026-11-07 on the machine below. They are not gated in CI. Read them as relative, not absolute.

## Test machine and method

- MacBook Pro mid 2015, Intel Core i7-4770HQ @ 2.20 GHz (4 cores, 8 threads), 16 GB RAM, Fedora Linux. A browser and other work were running. Load average before each run was about 1.
- The gateway and the load generator (k6 2.3.0) ran on the same machine. k6 uses a large part of the CPU at high rates, so the gateway numbers at the top are limited by the shared machine, not only by the gateway.
- Gateway settings: 1 worker, 1 ONNX Runtime thread, `max_wait=5ms`, queue 1024, request timeout 5 s, model v1 (int8). Only `max_batch` changes (1 or 16).
- k6 `constant-arrival-rate`: the generator sends at a fixed rate whatever the answers are, so slow answers cannot hide as fewer requests. "Not sent by k6" is the number of arrivals k6 could not start because its virtual users were all busy. If this is not 0, the generator or the gateway was saturated.
- 20 s per point below 2000 req/s, 15 s at and above 2000 req/s. One run per point. There is no repeat or confidence interval, so small differences are noise.
- gRPC load uses k6's gRPC client. `ghz` is not packaged in Fedora, so it was not used.
- Raw data: `docs/perf-data/*.jsonl`. Script: `scripts/perf.sh`.

## Result in one paragraph

On this machine the model is very fast (about 33 microseconds for one row), so at low and medium load batching only adds latency: p50 rises from about 0.4 ms to about 4 to 6 ms, because each request waits for `max_wait` or for company. Throughput is the same, since both settings keep up. Batching starts to pay near saturation: at a 12,000 req/s target the gateway served 10,553 req/s (HTTP) with `max_batch=1` and 11,229 req/s with `max_batch=16` (+6 percent), with p95 of 50.6 ms against 36.3 ms. For gRPC the same point gave 9,156 against 10,236 req/s (+12 percent), p95 65.6 ms against 50.0 ms. These are single runs. The gain is small because most of the time at that load goes to the HTTP or gRPC stack and to k6, not to the model.

The expected result "batching raises throughput at the cost of a few ms queue wait" held, but only at saturation. Below it, the cost shows and the gain does not.

## Model alone (Go benchmark, one ONNX call per batch, 1 thread)

`go test -run XXX -bench BenchmarkRun ./internal/model`

```
goos: linux
goarch: amd64
cpu: Intel(R) Core(TM) i7-4770HQ CPU @ 2.20GHz
BenchmarkRun/batch=1-8         	   68060	     32923 ns/op	     32923 ns/row
BenchmarkRun/batch=2-8         	   64380	     37630 ns/op	     18815 ns/row
BenchmarkRun/batch=4-8         	   51356	     46372 ns/op	     11593 ns/row
BenchmarkRun/batch=8-8         	   39428	     61254 ns/op	      7657 ns/row
BenchmarkRun/batch=16-8        	   26637	     95216 ns/op	      5951 ns/row
BenchmarkRun/batch=32-8        	   16574	    147009 ns/op	      4594 ns/row
BenchmarkRun/batch=64-8        	    8890	    255310 ns/op	      3989 ns/row
```

Cost per row falls from 32.9 microseconds at batch 1 to 6.0 at batch 16 (5.5 times less) and 4.0 at batch 64. This is the effect batching exploits. In the end-to-end test it is mostly hidden behind the network stack.

## HTTP (POST /v1/classify)

| Target req/s | max_batch | Achieved req/s | Not sent by k6 | p50 ms | p95 ms | p99 ms | 429s |
|---|---|---|---|---|---|---|---|
| 100 | 1 | 100 | 0 | 0.61 | 0.78 | 1.24 | 0 |
| 100 | 16 | 100 | 0 | 5.93 | 6.09 | 6.2 | 0 |
| 200 | 1 | 200 | 0 | 0.58 | 0.69 | 0.87 | 0 |
| 200 | 16 | 200 | 0 | 5.73 | 6.62 | 6.77 | 0 |
| 400 | 1 | 400 | 0 | 0.42 | 0.62 | 0.78 | 0 |
| 400 | 16 | 399.9 | 0 | 3.78 | 6.47 | 6.68 | 0 |
| 800 | 1 | 800 | 0 | 0.39 | 0.51 | 0.76 | 0 |
| 800 | 16 | 799.9 | 0 | 3.85 | 6.39 | 6.79 | 0 |
| 2000 | 1 | 2000 | 0 | 0.38 | 0.58 | 1.12 | 0 |
| 2000 | 16 | 1999.3 | 0 | 3.99 | 6.36 | 6.95 | 0 |
| 4000 | 1 | 3999.7 | 0 | 0.37 | 0.9 | 2.41 | 0 |
| 4000 | 16 | 3997.8 | 6 | 3.05 | 5 | 5.97 | 0 |
| 6000 | 1 | 5949.2 | 738 | 0.6 | 8.16 | 17.43 | 0 |
| 6000 | 16 | 5994.4 | 47 | 2.37 | 4.31 | 6.75 | 0 |
| 8000 | 1 | 7989.4 | 151 | 0.63 | 5.3 | 8.97 | 0 |
| 8000 | 16 | 7960.8 | 546 | 2.21 | 6.17 | 10.56 | 0 |
| 12000 | 1 | 10553.2 | 21430 | 26.25 | 50.61 | 67.77 | 0 |
| 12000 | 16 | 11229.1 | 11347 | 6.04 | 36.33 | 57.85 | 0 |

## gRPC (Classify)

| Target req/s | max_batch | Achieved req/s | Not sent by k6 | p50 ms | p95 ms | p99 ms |
|---|---|---|---|---|---|---|
| 100 | 1 | 100 | 0 | 0.69 | 0.81 | 1.19 |
| 100 | 16 | 100 | 0 | 6.03 | 6.21 | 6.36 |
| 200 | 1 | 200 | 0 | 0.58 | 0.76 | 0.98 |
| 200 | 16 | 200 | 0 | 5.88 | 6.67 | 6.79 |
| 400 | 1 | 400 | 0 | 0.45 | 0.59 | 0.87 |
| 400 | 16 | 399.9 | 0 | 4.07 | 6.32 | 6.66 |
| 800 | 1 | 799.9 | 0 | 0.42 | 0.56 | 0.97 |
| 800 | 16 | 800 | 0 | 4.38 | 6.59 | 6.84 |
| 2000 | 1 | 2000 | 0 | 0.39 | 0.69 | 1.62 |
| 2000 | 16 | 1999.3 | 0 | 3.79 | 6.48 | 7.26 |
| 4000 | 1 | 3998.2 | 25 | 0.42 | 2 | 5.14 |
| 4000 | 16 | 3998 | 12 | 3.03 | 5.07 | 6.27 |
| 6000 | 1 | 5951.4 | 725 | 0.67 | 8.57 | 15.59 |
| 6000 | 16 | 5965.5 | 473 | 2.43 | 6.52 | 11.62 |
| 8000 | 1 | 7899.9 | 1382 | 2.09 | 15.64 | 24.25 |
| 8000 | 16 | 7875.4 | 1815 | 2.65 | 15.05 | 24.48 |
| 12000 | 1 | 9156 | 42411 | 33.1 | 65.6 | 86.66 |
| 12000 | 16 | 10236 | 26001 | 14.17 | 50.01 | 74.15 |

## Batch statistics

From `/metrics` after the low-rate series with `max_batch=16`: 60,056 requests ran in 21,338 model calls, an average batch of 2.8. After the high-rate series: 919,820 requests in 59,404 calls, an average of 15.5, close to the cap of 16.

## Overload (load shedding)

`scripts/demo.sh`, queue size 4, `max_batch=1`, target 15,000 req/s for 6 s: 77,492 requests completed or refused, 29,251 got 200 and 48,241 got 429. The gateway stayed up and answered the accepted ones with p50 9.5 ms. See `docs/DEMO.md`.

## Kubernetes load tests (kind on this laptop)

The e2e script ran k6 inside the cluster against the Service. These are invariant checks, not benchmarks (see the e2e report in `docs/DEMO.md`).

## Limits of these numbers

- One machine, shared with the load generator and other programs.
- One run per point.
- Short runs (15 to 20 s).
- A small model. A larger model has a bigger per-call cost, so batching would matter more.
- No GPU. Do not read this as GPU serving performance.
