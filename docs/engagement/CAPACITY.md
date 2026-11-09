# Capacity: how many replicas and cores

This page answers the question of CommunityCU (a fictional customer): how many gateway replicas and CPU cores do we need, and where does latency come from? Every measured number was taken on one laptop (MacBook Pro mid 2015). Every other number is an estimate. A number with a tag like `<!--cm:name-->` is checked by `python3 scripts/capacity_model.py --check-doc docs/engagement/CAPACITY.md`.

## 1. Traffic model

| Item | Arithmetic | Value |
|---|---|---|
| Calls per day | given | 15,000,000 |
| Average rate | 15,000,000 / 86,400 s | <!--cm:average_rps-->173.6 requests per second |
| Peak hour rate | average x 3 | <!--cm:peak_rps-->520.8 requests per second |
| Launch peak (10x) | peak x 10 | <!--cm:growth_peak_rps-->5208.3 requests per second |

Size for the peak hour, not for the average. Sizing for the average would leave no room for the peak (claim 1 of the unreviewed draft).

## 2. Measured capacity

Source: eleven different gateway settings, each run at 6000 requests per second for 60 s (see `evidence/experiments/README.md`). In every run the gateway kept up: no shed, no failure, p95 at or under 7 ms. The p95 target is 50 ms.

- CPU used by the gateway was measured from `/proc` for each run.
- Throughput per core = achieved requests per second / cores used.
- The median over the eleven settings is <!--cm:rps_per_core-->5214.3 requests per second per core. The lowest is 3973.5 (max-batch 1) and the highest is 8445.8 (gateway limited to 2 CPUs).

Limits of this number:

- The gateway was not saturated at 6000 requests per second. The number is a cost per request at that load, not a maximum.
- The load generator ran on the same laptop and used part of the CPU.
- One run per setting. Differences of a few percent are noise.

## 3. Queue math (Little's law)

Little's law says L = lambda x W. L is the number of requests in the system. lambda is the arrival rate. W is the time a request spends in the system.

| Case | Rate | Requests in system, average W | Requests in system, p95 W |
|---|---|---|---|
| Peak hour | 520.8 per second | 1.05 | <!--cm:peak_in_system_p95-->1.88 |
| Launch peak | 5208.3 per second | 10.52 | <!--cm:growth_peak_in_system_p95-->18.75 |

W comes from the measured average and p95 latency of the setting closest to the median (max-wait 10 ms). The queue size of 1024 is far above 19. A longer queue is not needed. A longer queue would only hide overload as delay.

## 4. Utilization target

The target is 60 percent of the measured CPU rate per core. Reasons: a queue grows fast near full load, the measured number is for one laptop, and a replica needs room for a rolling upgrade and for noisy neighbors. A p95 under 50 ms at 60 percent is a safe plan. The model cannot prove it for other hardware.

## 5. Cores and replicas

Cores needed = ceil(rate / (rate per core x 0.6)). Replicas = ceil(cores / 2) + 1. The spare replica is the N+1 rule. A replica has 2 cores in this plan.

| Case | Cores needed | Replicas of 2 cores |
|---|---|---|
| Peak hour | <!--cm:peak_cores-->1 | <!--cm:peak_replicas-->2 |
| Launch peak (10x), central | <!--cm:cores_central-->2 | <!--cm:replicas_central-->2 |

## 6. The 10x estimate as a range

| Case | Rate per core | Cores | Replicas of 2 cores |
|---|---|---|---|
| Low | 8445.8 (best measured) | <!--cm:cores_low-->2 | <!--cm:replicas_low-->2 |
| Central | 5214.3 (median) | <!--cm:cores_central-->2 | <!--cm:replicas_central-->2 |
| High | 2607.2 (half of the median) | <!--cm:cores_high-->4 | <!--cm:replicas_high-->3 |

Range: 2 to 3 replicas of 2 cores for the 10x launch peak of 5208.3 requests per second. This is an estimate with low confidence for other hardware, and medium confidence for this laptop class.

Assumptions:

- The text length mix stays like `data/test.csv` (median 45 characters).
- The model stays the same (v1, int8).
- The CPU class of the target is not much slower than this 2015 laptop. The high case allows half the work per core.
- The load generator is not the limit.
- Requests come through HTTP with the current settings (1 worker, max-batch 16, max-wait 5 ms).

What would break the estimate:

- A larger model or longer texts. The cost per request would rise.
- A slower CPU class or noisy neighbors. Use the high case.
- Traffic that is burstier than the peak hour model (for example a push notification). Queue depth would rise before CPU does. Watch `gateway_queue_depth` and the shed rate.
- TLS or authentication in front of the gateway. This work is not in the measurement.

What to measure on real hardware first:

1. The same one-factor run (`scripts/experiment.sh`) at the knee rate, with the load generator on a separate machine.
2. CPU per request with TLS on.
3. p95 at 60 percent utilization for 30 minutes.

## 7. Where latency comes from

At 6000 requests per second the CPU profile shows the HTTP serving path at 68 percent (cumulative), the model batch run at 3.9 percent, GC at 8.7 percent and the tokenizer at 1.1 percent. See the bottleneck statement in `evidence/experiments/README.md`. Batching adds latency at this load: p50 was 0.27 ms with max-batch 1 and 1.94 ms with max-batch 16.
