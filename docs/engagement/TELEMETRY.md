# Telemetry and cardinality budget

This document lists the gateway metrics, the allowed label values, and the series budget per instance.
It also records the gaps found in the current telemetry and the rules that keep label sets bounded.

## Metric list and budget

A histogram series counts one series per bucket, plus `_sum` and `_count`.
The latency histogram has 11 buckets plus `+Inf`, so one label set is 14 series.

| Metric | Type | Labels and allowed values | Max series |
|---|---|---|---|
| `gateway_requests_total` | counter | `transport` in {http, grpc}; `outcome` in {ok, shed, timeout, cancelled, invalid, closed, error} | 2 x 7 = 14 |
| `gateway_request_duration_seconds` | histogram | `transport` in {http, grpc} | 2 x 14 = 28 |
| `gateway_queue_wait_seconds` | histogram | none | 14 |
| `gateway_batch_size` | histogram | none | 10 |
| `gateway_batch_run_seconds` | histogram | none | 14 |
| `gateway_shed_total` | counter | none | 1 |
| `gateway_cancelled_total` | counter | none | 1 |
| `gateway_queue_depth` | gauge | none | 1 |
| `gateway_model_info` | gauge | `version` from a fixed set (at most 2 versions) | 2 |

Total for `gateway_*` families: 85 series now.
The budget is under 140 series for `gateway_*` families.

Planned change: add `model_version` to `gateway_request_duration_seconds` with at most 2 versions.
That raises the latency family to 2 x 2 x 14 = 56 series.
The `gateway_*` total then stays under 140 series.

With the Go and process runtime metrics, the whole instance stays under 200 series.

## Rules

- No request ids in labels.
- No raw paths in labels.
- No request or response text in labels.
- The model version comes from a fixed set. Do not use free text.
- The `outcome` label uses the fixed set above. An unknown error maps to `error`.
- A new label needs a row in the table above and a new budget number.

## How to check

Run the bound test:

```
go test -count=1 ./internal/telemetry/
```

The test drives every allowed label value and 50 random request-like strings through the HTTP and gRPC paths.
It then gathers the registry and checks each family against the budget table in the test.
It also checks that no label value is outside the allowed set.

## Known gaps found

1. The registry has no Go or process collectors. `process_cpu_seconds_total` does not exist, so CPU saturation has no metric.
2. Request latency has no `model_version` label. A latency change cannot be split by model version.
3. No exemplars link latency to traces. A slow bucket cannot be opened in the trace view.

These gaps are fixed in the next step.

## Repair story

To be completed after the repair.
