# Canary rollout

This page explains the canary proxy, the gate values, and how the same gate
would run in production. The proxy here is a test tool. It is not a production
router. No Argo Rollouts and no Flagger were used.

## How the canary works

The canary proxy sits in front of two backends: a stable gateway and a canary
gateway. It sends a share of requests to the canary and the rest to the stable
backend. The share is the canary weight.

The controller steps the weight up through 10, 25, 50 and 100 percent. Each
step holds for a fixed window (`-step-duration`, default 30s). At the end of
each window the controller reads the proxy counters and checks the gate.

The gate has four parts:

- Minimum requests: the step needs at least `-min-requests` canary requests
  before the gate can pass. A small sample gives no signal.
- Maximum error rate: the canary error rate must stay at or below
  `-max-error-rate`.
- Margin over stable: the canary error rate must not exceed the stable error
  rate by more than `-margin`. This catches a canary that is worse than stable
  even when both are under the absolute limit.
- Maximum p95: the canary p95 latency must stay at or below `-max-p95`.

When every check passes, the controller moves to the next step. When a check
fails, the controller rolls back.

## Gate values and why

| Flag | Default | Why |
|---|---|---|
| `-max-error-rate` | 0.02 | Two percent is the error budget for a chat turn. Above this, users see failures. |
| `-max-p95` | 50ms | The gateway p95 budget for a classify call. The model run and the queue wait must fit inside it. |
| `-min-requests` | 100 | A step with fewer requests cannot separate a real fault from noise. |
| `-margin` | 0.01 | One point of slack over stable absorbs normal jitter between two model versions. |

The defaults are starting points. Tune them against the SLO and the load test
data for the release.

## What rollback does

On a gate failure the controller sets the canary weight to 0 at once. All
traffic goes to the stable backend. The controller prints a JSON line with
`"result":"rolled_back"` and the reason, then exits with code 3. The proxy
keeps serving at weight 0 until it gets a signal, so the load test can finish
and the client error rate stays visible.

On a clean run the controller prints `"result":"promoted"` with the final
weight and exits 0 after a signal.

## Running it locally

```
scripts/canary-run.sh pass
scripts/canary-run.sh fail
```

`pass` mode starts gateway v1 on 127.0.0.1:25480 and gateway v2 on
127.0.0.1:25482, puts the canary proxy on 127.0.0.1:25481, and runs the k6
script against the proxy at a low steady rate.

`fail` mode adds a test helper (`cmd/stubfail`) on 127.0.0.1:25483 in front of
v2. The helper returns 500 for a set share of requests. The canary upstream
becomes the helper, so the gate sees errors and rolls back.

The script stops every process by PID in a trap. It never stops processes by
name.

## The existing kind-based rolling upgrade

`scripts/kind-e2e.sh` already runs a rolling upgrade on a kind cluster. It
replaces pods one at a time and waits for readiness. That script exists and was
not changed by this work. It covers the pod replacement path, not the weighted
traffic split.

## How production would use the same gate

The gate logic is the same in production. Only the traffic split changes.

- Service mesh: a mesh (for example Istio or Linkerd) can split traffic by
  weight between two Services. The controller sets the weight through the mesh
  API, reads error rate and p95 from the mesh metrics, and applies the same
  gate. On failure it sets the weight to 0.
- Two Deployments: run a stable Deployment and a canary Deployment behind one
  Service. A weighted route (an ingress or a gateway resource) sets the split.
  The controller patches the weight and reads metrics from Prometheus.

In both cases the gate values, the step list and the rollback rule stay the
same. The proxy in this repo is only a local stand-in for that weighted route.
