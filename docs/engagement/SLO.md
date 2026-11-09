# Latency SLO

## The SLI

The service level indicator is the share of requests that finish in under 50 ms.

```
sum(rate(gateway_request_duration_seconds_bucket{le="0.05"}[W]))
/
sum(rate(gateway_request_duration_seconds_count[W]))
```

The histogram has the labels `transport` and `model_version`. The `sum` removes both labels, so the ratio covers all transports and all model versions. The bucket `le="0.05"` exists in the histogram.

## Target and window

- Target: 99 percent of requests under 50 ms.
- Window: 28 days, rolling.

## Error budget

- Total seconds in 28 days: 28 x 24 x 3600 = 2,419,200 s.
- Error budget: 1 percent of the requests in that window may be slower than 50 ms.
- Budget as a fraction: 0.01.

## Recording rules

The file `deploy/compose/prometheus/rules/slo.rules.yml` records the good ratio for four windows:

| Rule | Window |
|---|---|
| `gateway:latency_good_ratio:rate5m` | 5 minutes |
| `gateway:latency_good_ratio:rate30m` | 30 minutes |
| `gateway:latency_good_ratio:rate1h` | 1 hour |
| `gateway:latency_good_ratio:rate6h` | 6 hours |

The error ratio is `1 - good ratio`. The burn rate is the error ratio divided by the budget (0.01).

## Alerts

| Alert | Burn rate | Windows | For | Severity |
|---|---|---|---|---|
| `GatewayLatencySLOFastBurn` | 14.4 | 1h and 5m | 2m | page |
| `GatewayLatencySLOSlowBurn` | 6 | 6h and 30m | 15m | ticket |

Both windows must be above the burn rate before an alert fires. The short window catches a new problem. The long window proves the problem is real and not a spike.

## How to read an alert

- Fast burn: the budget will be gone in about 2 days at this rate. Page someone now.
- Slow burn: the budget will be gone in about 5 days at this rate. Open a ticket and fix it in working hours.
- Both alerts point to `docs/engagement/RUNBOOK.md` for the first steps.

## What this does not cover

- No multi-region setup. One Prometheus instance runs on one laptop.
- The numbers come from a laptop, not from production traffic.
- The 50 ms target is a local choice for this engagement, not a customer SLA.
- The alert rules do not cover errors or sheds. Those have their own signals on the golden signals dashboard.

## Test

The file `deploy/compose/prometheus/rules/slo.rules.test.yml` holds four promtool cases:

1. Healthy traffic at 99.9 percent under 50 ms: no alert.
2. Sharp slowdown at 70 percent under 50 ms: fast burn fires.
3. Mild slowdown at 94 percent under 50 ms: slow burn fires, fast burn stays quiet.
4. Short spike then recovery: no alert at the end.

Run the test with:

```
podman run --rm -v "$PWD/deploy/compose/prometheus/rules:/r:ro,z" --entrypoint promtool docker.io/prom/prometheus:v2.55.1 test rules /r/slo.rules.test.yml
```

The idea is the same multiwindow burn-rate pattern used in other SLO setups. These files are written from scratch for this repo.
