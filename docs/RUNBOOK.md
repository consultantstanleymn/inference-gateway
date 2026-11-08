# Runbook

For whoever runs the gateway. Metrics are at `/metrics` on the HTTP port.

## Signals to watch

| Metric | Meaning | Worry when |
|---|---|---|
| `gateway_requests_total{outcome="shed"}` | requests refused (429) | rising for more than a minute |
| `gateway_requests_total{outcome="timeout"}` | requests past the deadline | any sustained rate |
| `gateway_queue_depth` | requests waiting now | stays near `queue_size` |
| `gateway_queue_wait_seconds` | time waiting for a batch | p95 above `max_wait` plus one batch run |
| `gateway_batch_size` | requests per model call | always 1 under load (batching not working) |
| `gateway_batch_run_seconds` | one model call | grows with batch size faster than expected |
| `gateway_cancelled_total` | callers that left before inference | high rate means callers time out earlier than the gateway |
| `gateway_model_info{version}` | loaded model | differs between pods after an upgrade |

## Problems

**Pods are not Ready.** `/readyz` is 200 only after the model loads and the warm-up finishes.
Run `kubectl logs deploy/inference-gateway`. Typical causes: "model checksum mismatch" (the model file and `meta.json` disagree), "init onnxruntime" (the library path or version is wrong), or a ConfigMap that points at a model version that is not in the image.

**Many 429 responses.** The queue is full. The model cannot keep up with the arrival rate.
1. Check `kubectl get hpa`. If it is at max replicas, raise `hpa.maxReplicas` or the CPU limit.
2. If CPU is low but the queue is full, raise `gateway.maxBatch` (larger batches give more throughput per CPU second, at some latency cost).
3. Do not raise `queueSize` first. A longer queue only moves the problem into latency and timeouts.

**Many 504 responses (timeouts).** Requests wait longer than `requestTimeout`. Same causes as 429, but the queue is long enough to hide it. Lower `queueSize` so overload turns into fast 429 responses instead.

**Latency is high at low traffic.** Each lone request can wait up to `maxWait` (5 ms by default). Lower `gateway.maxWait` if the 5 ms matters more than throughput.

**Errors during a rolling upgrade.** Check that `gateway.shutdownDelay` is above the time the endpoints take to update (5 s in the chart) and that `terminationGracePeriodSeconds` (30 s) is larger than `shutdownDelay` plus the longest request.

## Change the model version

```bash
helm upgrade ig deploy/helm/inference-gateway -f deploy/helm/inference-gateway/values-model-v2.yaml \
  --set image.repository=localhost/inference-gateway --set image.tag=dev
kubectl rollout status deploy/inference-gateway
```

Roll back: `helm rollback ig` or upgrade with `values-model-v1.yaml`. The response field `model_version` tells you which model answered.

## Add a model version

1. Train and export into `models/vN` (`train/train.py`, `train/export.py`), run `train/gate.py models/vN`.
2. Add `COPY models/vN/...` to the `Containerfile` and a `values-model-vN.yaml`.
3. Build, load into the cluster, `helm upgrade`.

## Disruptions

The PodDisruptionBudget keeps at least one pod available during node drains. With `replicaCount: 1` and the HPA off the budget blocks a drain. Keep at least two replicas.
