# One-factor experiments

All runs: HTTP on `POST /v1/classify`, model v1 (int8), k6 constant arrival rate of 6000 requests per second for 60 s, load generator on the same laptop (measured on one laptop). Fixed settings unless a table says otherwise: workers 1, max-batch 16, max-wait 5ms, 8 CPUs (no pinning), ONNX Runtime threads 1, queue 1024, request timeout 5s. Each run changes one factor. One run per point, so a small difference is noise. A CPU profile of 20 s was taken from the admin port during each run. "Cores used" is the CPU time of the gateway process from `/proc` divided by the wall time of the k6 run. "Dropped" is arrivals that k6 could not start.

Raw files: `<factor>-<value>.json` (settings, k6 summary, cores used), `.pb.gz` (pprof profile), `.top.txt` (pprof top 25 as text). `graphviz` is not installed, so only text output exists.

## Workers

| value | achieved rps | p50 ms | p95 ms | p99 ms | shed | dropped | cores used | top 5 functions by flat time |
|---|---|---|---|---|---|---|---|---|
| 1 | 5995.7 | 1.94 | 3.61 | 4.09 | 0 | 231 | 1.19 | internal/runtime/syscall/linux.Syscall6 15.94%; runtime.futex 6.65%; runtime.cgocall 3.54%; internal/server.(*Service).rank.func1 1.63%; runtime.nextFreeFast (inline) 1.63% |
| 2 | 5999.5 | 3.11 | 5.7 | 6.43 | 0 | 2 | 1.16 | internal/runtime/syscall/linux.Syscall6 16.05%; runtime.futex 6.36%; runtime.cgocall 4.00%; runtime.nextFreeFast (inline) 1.64%; runtime.tryDeferToSpanScan 1.64% |
| 4 | 5998.9 | 3.88 | 6.85 | 7.73 | 0 | 39 | 1.3 | internal/runtime/syscall/linux.Syscall6 16.79%; runtime.futex 6.37%; runtime.cgocall 5.87%; internal/server.(*Service).rank.func1 1.90%; runtime.tryDeferToSpanScan 1.53% |

## Max batch

| value | achieved rps | p50 ms | p95 ms | p99 ms | shed | dropped | cores used | top 5 functions by flat time |
|---|---|---|---|---|---|---|---|---|
| 1 | 6000 | 0.27 | 0.6 | 1.38 | 0 | 0 | 1.51 | internal/runtime/syscall/linux.Syscall6 14.31%; runtime.cgocall 13.61%; runtime.futex 6.08%; runtime.nextFreeFast (inline) 1.30%; runtime.stealWork 1.27% |
| 8 | 5999.5 | 1.04 | 2.21 | 2.6 | 0 | 0 | 1.22 | internal/runtime/syscall/linux.Syscall6 14.32%; runtime.futex 6.56%; runtime.cgocall 6.21%; runtime.memmove 1.60%; internal/server.(*Service).rank.func1 1.46% |
| 16 | 5996.9 | 1.94 | 3.63 | 4.13 | 0 | 161 | 1.19 | internal/runtime/syscall/linux.Syscall6 17.27%; runtime.futex 5.99%; runtime.cgocall 3.14%; internal/server.(*Service).rank.func1 1.52%; runtime.nextFreeFast (inline) 1.52% |
| 32 | 5999.4 | 3.55 | 6.22 | 6.9 | 0 | 18 | 1.13 | internal/runtime/syscall/linux.Syscall6 15.76%; runtime.futex 5.12%; runtime.cgocall 4.38%; runtime.nextFreeFast (inline) 2.04%; internal/server.(*Service).rank.func1 1.99% |

## Max wait

| value | achieved rps | p50 ms | p95 ms | p99 ms | shed | dropped | cores used | top 5 functions by flat time |
|---|---|---|---|---|---|---|---|---|
| 1ms | 5999.8 | 1.96 | 2.6 | 3.05 | 0 | 0 | 1.2 | internal/runtime/syscall/linux.Syscall6 16.58%; runtime.futex 4.54%; runtime.cgocall 4.00%; internal/server.(*Service).rank.func1 1.44%; runtime.(*mspan).writeHeapBitsSmall 1.35% |
| 5ms | 5999 | 1.94 | 3.6 | 4.07 | 0 | 30 | 1.15 | internal/runtime/syscall/linux.Syscall6 17.46%; runtime.futex 6.89%; runtime.cgocall 2.77%; runtime.tryDeferToSpanScan 1.53%; runtime.typePointers.next 1.39% |
| 10ms | 5996.5 | 1.94 | 3.6 | 4.06 | 0 | 151 | 1.15 | internal/runtime/syscall/linux.Syscall6 18.10%; runtime.futex 5.83%; runtime.cgocall 3.27%; internal/server.(*Service).rank.func1 1.52%; runtime.usleep 1.47% |

## CPU limit (taskset, number of CPUs for the gateway)

| value | achieved rps | p50 ms | p95 ms | p99 ms | shed | dropped | cores used | top 5 functions by flat time |
|---|---|---|---|---|---|---|---|---|
| 2 | 5996.5 | 2.18 | 3.64 | 4.33 | 0 | 189 | 0.71 | internal/runtime/syscall/linux.Syscall6 20.58%; runtime.cgocall 4.26%; internal/server.(*Service).rank.func1 2.70%; runtime.futex 2.34%; [libc.so.6] 1.92% |
| 4 | 5996.8 | 1.93 | 3.58 | 4 | 0 | 158 | 0.88 | internal/runtime/syscall/linux.Syscall6 18.63%; runtime.futex 4.41%; runtime.cgocall 3.25%; sort.partition_func 1.51%; runtime.tryDeferToSpanScan 1.28% |
| 6 | 5999.4 | 1.93 | 3.6 | 4.01 | 0 | 0 | 1.02 | internal/runtime/syscall/linux.Syscall6 18.30%; runtime.futex 4.25%; runtime.cgocall 2.61%; internal/server.(*Service).rank.func1 1.85%; runtime.(*mspan).writeHeapBitsSmall 1.23% |

## Alternative causes

GC and the tokenizer were checked with data. Raw files: `gc-check.txt`, `tokenizer-bench.txt`, `gc-gctrace/`, `gc-gogc400/`.

| Cause | Data | Verdict |
|---|---|---|
| GC | The gctrace run counted 3587 GC cycles in 61 s. Total stop-the-world time was 601 ms (0.98 percent of wall time), 0.168 ms per cycle. Background mark workers used 8.66 percent of CPU samples. With GOGC=400 the gateway used 0.93 cores instead of 1.19, and p99 went from 4.09 ms to 3.83 ms. | GC costs CPU but does not explain the tail. A pause of 0.17 ms is much smaller than a p99 of about 4 ms. |
| Tokenizer | `tokenize.Features` used 0.22 s of 20.89 s of samples (1.05 percent), about 1.8 microseconds per request. The model needs 4018 ns per row at batch 16. | Ruled out. |
| ONNX Runtime batch run | `model.(*Model).Run` used 0.82 s (3.93 percent). | Small at this rate. |

## Bottleneck statement

At 6000 requests per second on one laptop, the gateway used about 1.2 cores and kept up in every run (achieved rate within 0.1 percent of the target, no shed). No setting changed throughput. CPU time sits mostly in the HTTP serving path: `net/http.(*conn).serve` is 68 percent of the samples (cumulative) and system calls are 16 percent flat. The model batch run is under 4 percent. So the first limit on this machine is the network and request handling path, together with the load generator that shares the machine, not the model (workers-1 profile, `workers-1.top.txt`). The settings change latency, not throughput: max-batch 1 gave the lowest latency (p50 0.27 ms) at the cost of more CPU (1.51 cores), and larger batches and more workers added latency (max-batch 32: p50 3.55 ms; workers 4: p50 3.88 ms). This is measured on one laptop. It is not a statement about server hardware.

Not tested: a rate above the knee. At 6000 requests per second the gateway was not saturated, so these runs say where CPU goes, not what the maximum is. The maximum from the earlier runs is in `docs/PERF.md`.
