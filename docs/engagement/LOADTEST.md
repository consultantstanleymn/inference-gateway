# Load test: k6 and Locust

This page compares two load tools on the same gateway. All numbers were measured on one laptop (MacBook Pro mid 2015, 8 logical CPUs). The load generator and the gateway share the machine.

## Method

- Gateway: model v1 (int8), 1 worker, max-batch 16, max-wait 5 ms, ONNX Runtime threads 1, queue 1024, request timeout 5 s. It runs on the host on port 25480.
- k6: `loadtest/http.js`. It uses a constant arrival rate. It sends at the target rate even when answers are slow.
- Locust: `loadtest/locustfile.py`. Each simulated user sends 10 requests per second (`constant_throughput`). The user count is the target rate divided by 10. The text comes from `data/test.csv`. A fixed seed (7) sets the text order.
- Target rates: 2000 and 4000 requests per second. Each run lasts 60 s.
- "Same band" means: the Locust achieved rate is within 10 percent of the k6 achieved rate.
- Command: `flock heavy.lock nice -n 10 scripts/locust-vs-k6.sh`. The load average before the run was 1.40 and after it was 1.92. No other heavy job held the lock.

## Result

| Target rps | Tool | Achieved rps | p50 ms | p95 ms | Failures |
|---|---|---|---|---|---|
| 2000 | k6 | 1999.9 | 3.79 | 6.28 | 0 |
| 2000 | Locust | 1923.3 | 25 | 41 | 0 |
| 4000 | k6 | 3999.7 | 2.81 | 4.74 | 0 |
| 4000 | Locust | 3189.0 | 65 | 83 | 0 |

Locust achieved rate is the request count divided by 60 s. It includes the short ramp-up.

## Reading the result

- At 2000 requests per second, Locust reached 3.8 percent less than k6. This is inside the 10 percent margin.
- At 4000 requests per second, Locust reached 20.3 percent less than k6. This is outside the margin. Criterion I-4 is met at 2000 and not met at 4000.
- The Locust latency is higher (p50 25 ms and 65 ms against 3 to 4 ms). Locust is a Python tool and needs more CPU per request than k6. On a shared laptop, the load generator is the limit, not the gateway. The gateway latency seen by k6 stayed low at both rates.
- So Locust is useful here for user-style traffic with a fixed text mix, and k6 is the better tool for the throughput limit.
- No number was tuned. One run per point. There is no confidence interval.

Raw output: `evidence/locust-vs-k6.txt`. Locust reports: `evidence/locust-2000.html.gz` and `evidence/locust-4000.html.gz` (compressed to keep the repository small; run `gunzip` to open them).
