# Capacity review: line-by-line correction

This file reviews `docs/engagement/capacity-review/unreviewed-draft.md`. The draft is kept unchanged. Each row gives the draft claim, a verdict, the corrected statement with its number, and the evidence files.

Verdicts used: supported, partly supported, unsupported, false.

| Claim | Verdict | Correct statement with the number | Evidence file |
|---|---|---|---|
| 1. Sizing uses the daily average of about 174 requests per second. | false | The average is 173.6 rps. The peak hour is 520.8 rps (x3). The launch peak is 5208.3 rps (x10). Size for the peak, not the average. | `docs/engagement/CAPACITY.md` |
| 2. The request p95 is the sum of the stage p95 values, about 15 ms. | false | Percentiles do not add. Measure the whole request. The measured end-to-end p95 is 3.61 ms at 6000 rps. | `docs/engagement/evidence/experiments/workers-1.json` |
| 3. Throughput scales linearly with cores past the knee. | false | At 6000 rps the gateway limited to 2, 4 and 6 CPUs all reached about 5997 to 5999 rps. Cores used were 0.71, 0.88 and 1.02. Rps per core fell from 8445.8 to 5881.7 as CPUs rose. The gateway was below saturation, so more cores gave no more throughput. | `docs/engagement/evidence/experiments/cpus-2.json`, `docs/engagement/evidence/experiments/cpus-4.json`, `docs/engagement/evidence/experiments/cpus-6.json` |
| 4. Batching always raises throughput, by about 4 times at any load. | false | At 6000 rps both max-batch 1 and max-batch 16 reached 6000 (6000 and 5996.9 rps). Batching lowered CPU (1.51 cores at max-batch 1 against 1.19 at 16) but raised latency (p50 0.27 ms against 1.94 ms, and 3.55 ms at max-batch 32). Batching raised throughput only near saturation, by a few percent (about 10.5k against 11.2k rps). | `docs/engagement/evidence/experiments/max-batch-1.json`, `docs/engagement/evidence/experiments/max-batch-16.json`, `docs/engagement/evidence/experiments/max-batch-32.json`, `docs/PERF.md` |
| 5. Queue length needs no model. `QUEUE_SIZE` is 256 from a smoke test. | partly supported | 256 is not wrong by luck: Little's law gives 18.75 requests in the system at the launch peak (p95 latency), far below 256. But the value must come from Little's law and the request timeout, not from a smoke test. | `docs/engagement/CAPACITY.md` |
| 6. The load generator ran on the same machine, and the numbers are clean server numbers. | false | The load tools use CPU on the same machine as the gateway. Locust reached 3189 rps for a target of 4000, 20.3 percent below k6. All numbers here are relative and measured on one laptop. | `docs/engagement/evidence/locust-vs-k6.txt`, `docs/PERF.md` |
| 7. Garbage collection causes the tail latency, and no profile data is needed. | unsupported | There were 3587 GC cycles in 61 s. Stop-the-world time was 601 ms in total (0.98 percent of wall time, 0.168 ms per cycle). GOGC=400 changed p99 from 4.09 ms to 3.83 ms and cores from 1.19 to 0.93. GC costs CPU (8.66 percent of samples), but pauses are much shorter than the p99. | `docs/engagement/evidence/experiments/gc-check.txt` |
| 8. One replica with 4 cores is enough for 10x, covering about 1740 requests per second. | false | The launch peak is 5208.3 rps, not 1740. Cores needed is 2 (central). Replicas with N+1 and 2 cores per replica: 2 central, 3 high. One replica has no spare. | `docs/engagement/CAPACITY.md` |
| 9. The int8 model has the same accuracy as the fp32 model, so quality is unchanged. | partly supported | The claim needs the gate data. The gate allows a macro-F1 drop of at most 1.0 point. Measured v2: fp32 macro-F1 0.86367, int8 0.86288 (a drop of 0.08 points). v1: 0.86363 and 0.86359. So the result is within the gate, not unchanged. | `train/gate.json`, `models/v2/metrics.json`, `models/v1/metrics.json` |
| 10. 15 million per day equals 1,500 per second. | false | 15,000,000 / 86,400 = 173.6 per second. | `docs/engagement/CAPACITY.md` |
| 11. Plan CPU utilization at 95 percent. | unsupported | The plan uses 60 percent. No experiment ran at 95 percent, so this correction is a design rule, not a measurement: near full load a queue grows fast and a replica has no room for a rolling upgrade. | `docs/engagement/CAPACITY.md` |
| 12. A laptop benchmark is server-class, at 900 requests per second per replica. | false | Every performance number is measured on one 2015 laptop. The draft number of 900 rps per replica is not in any data file here. The measured median is 5214.3 rps per core at 6000 rps, an estimate for planning only. | `docs/engagement/evidence/capacity-model-output.txt` |

## Corrected summary

1. The average rate is 173.6 rps, the peak hour is 520.8 rps and the launch peak is 5208.3 rps, so size for the peak.
2. Percentiles do not add; the measured end-to-end p95 is 3.61 ms at 6000 rps.
3. At 6000 rps the gateway reached about 5997 to 5999 rps on 2, 4 and 6 CPUs, and rps per core fell from 8445.8 to 5881.7, so more cores gave no more throughput below saturation.
4. Batching lowered CPU but raised latency, and it raised throughput only near saturation, by a few percent.
5. Little's law gives 18.75 requests in the system at the launch peak, far below a queue size of 256, but the queue size must come from Little's law and the request timeout.
6. The load tools share the machine with the gateway, and Locust reached 3189 rps for a target of 4000, 20.3 percent below k6, so all numbers are relative.
7. GC pauses are much shorter than the p99: 0.98 percent of wall time in total, and GOGC=400 moved p99 from 4.09 ms to 3.83 ms.
8. The launch peak is 5208.3 rps, and the plan needs 2 cores central with 2 replicas central and 3 high, so one replica has no spare.
9. The int8 model is within the gate, with a v2 macro-F1 drop of 0.08 points against a gate of at most 1.0 point.
10. Plan CPU at 60 percent, and treat the measured median of 5214.3 rps per core as a planning estimate only, because it comes from one 2015 laptop.
