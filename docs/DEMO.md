# Demo (real output)

Everything below was pasted from real runs on 2026-11-07 on a 2015 MacBook Pro (CPU only). Nothing is edited except line wrapping of long JSON in the e2e section.
Ports are random on each run.

## 1. Local session (`scripts/demo.sh`)

```text
$ python train/gate.py models/v1 models/v2
PASS models/v1: int8 macro-F1 0.8636 >= 0.84
PASS models/v1: int8 drop vs fp32 0.004 points <= 1.0
PASS models/v1: onnx vs torch max diff 5.07e-07 < 0.0001
PASS models/v2: int8 macro-F1 0.8629 >= 0.84
PASS models/v2: int8 drop vs fp32 0.079 points <= 1.0
PASS models/v2: onnx vs torch max diff 5.36e-07 < 0.0001

$ bin/gateway -http-addr 127.0.0.1:35875 -grpc-addr 127.0.0.1:39653

$ curl -XPOST /v1/classify
{"intent":"transfer_fee_charged","probability":0.99982005,"model_version":"v1","top":[{"intent":"transfer_fee_charged","probability":0.99982005},{"intent":"card_payment_fee_charged","probability":0.000055975488},{"intent":"declined_transfer","probability":0.000050555896}]}


$ bin/probe grpc
{
  "intent": "transfer_fee_charged",
  "probability": 0.99982005,
  "modelVersion": "v1",
  "top": [
    {
      "intent": "transfer_fee_charged",
      "probability": 0.99982005
    },
    {
      "intent": "card_payment_fee_charged",
      "probability": 0.000055975488
    },
    {
      "intent": "declined_transfer",
      "probability": 0.000050555896
    }
  ]
}

$ curl -XPOST /v1/classify -d {"text":""}
{"error":"text is required"}
 (HTTP 400)

$ k6 burst of 800 req/s for 8 s against max_batch=16
K6SUMMARY {"rate_target":800,"requests":6401,"throughput_rps":800,"failed_rate":0,"shed_429":0,"v1":6401,"v2":0,"dropped_iterations":0,"avg_ms":3.66,"p50_ms":3.86,"p95_ms":6.39,"p99_ms":6.76,"max_ms":7.83}

$ curl /metrics | grep gateway_
gateway_batch_size_sum 6454
gateway_batch_size_count 1245
gateway_model_info{version="v1"} 1
gateway_queue_depth 0
gateway_requests_total{outcome="invalid",transport="http"} 1
gateway_requests_total{outcome="ok",transport="grpc"} 1
gateway_requests_total{outcome="ok",transport="http"} 6402
gateway_shed_total 0

$ kill -TERM <gateway>
exit code 0
2026/11/07 22:33:39 INFO model loaded version=v1 dir=models/v1
2026/11/07 22:33:39 INFO listening http=127.0.0.1:35875 grpc=127.0.0.1:39653
2026/11/07 22:33:39 INFO ready
2026/11/07 22:33:47 INFO shutdown started
2026/11/07 22:33:47 INFO shutdown done

== overload demo: queue-size 4, max-batch 1, 15000 req/s target (more than this laptop can serve)
K6SUMMARY {"rate_target":15000,"requests":77492,"throughput_rps":12877.4,"failed_rate":0.6225287771640944,"shed_429":48241,"v1":29251,"v2":0,"dropped_iterations":12511,"avg_ms":14.25,"p50_ms":9.49,"p95_ms":43.34,"p99_ms":64.53,"max_ms":145.84}
gateway_requests_total{outcome="ok",transport="http"} 29251
gateway_requests_total{outcome="shed",transport="http"} 48241
```

Reading it: the model gate passes for both versions. A normal call returns the top 3 intents and the model version. An empty text gets 400. A burst of 800 req/s with `max_batch=16` shows p95 6.4 ms and no errors. SIGTERM gives a clean exit with code 0. In the overload run the queue held 4 requests, so 48,241 requests got 429 while 29,251 got 200, and the process stayed healthy.

## 2. Kubernetes end-to-end (`scripts/kind-e2e.sh`, kind on rootless podman)

Run with `ROLLING_REPEATS=3` (three upgrades in a row: v1 to v2, v2 to v1, v1 to v2). The report file is `docs/perf-data/e2e-report.txt`.

```text
== helm lint and kubeconform
==> Linting deploy/helm/inference-gateway
[INFO] Chart.yaml: icon is recommended
1 chart(s) linted, 0 chart(s) failed
SKIPPED kubeconform (not installed locally, CI runs it)
== kind cluster (podman)
== metrics-server
== helm install model v1
deployment "inference-gateway" successfully rolled out
== smoke over HTTP
{"intent":"lost_or_stolen_card","probability":0.9993979,"model_version":"v1","top":[{"intent":"lost_or_stolen_card","probability":0.9993979},{"intent":"compromised_card","probability":0.0002639438},{"intent":"card_not_working","probability":0.00017615563}]}
== smoke over gRPC
{
  "intent": "lost_or_stolen_card",
  "probability": 0.9993979,
  "modelVersion": "v1",
  "top": [
    {
      "intent": "lost_or_stolen_card",
      "probability": 0.9993979
    },
    {
      "intent": "compromised_card",
      "probability": 0.0002639438
    },
    {
      "intent": "card_not_working",
      "probability": 0.00017615563
    }
  ]
}
== HPA scale-up under load
HPA before load: running 1, desired 0, min 2
HPA desired replicas 4 (min 2) after 36 s, attempt 1
PASS HPA
== rolling upgrade v1 -> v2 under load (10 VUs, readiness watched)
k6 under upgrade: {"requests":122106,"non_2xx":0,"failed_rate":0,"v1":77812,"v2":44294,"p95_ms":6.57}
readiness samples (running but not ready): 43, of which receiving traffic: 0
responses moved v1 -> v2
PASS rolling upgrade v1 -> v2: zero non-2xx, no traffic before ready
== rolling upgrade v2 -> v1 under load (10 VUs, readiness watched)
k6 under upgrade: {"requests":124689,"non_2xx":0,"failed_rate":0,"v1":56119,"v2":68570,"p95_ms":6.37}
readiness samples (running but not ready): 35, of which receiving traffic: 0
responses moved v2 -> v1
PASS rolling upgrade v2 -> v1: zero non-2xx, no traffic before ready
== rolling upgrade v1 -> v2 under load (10 VUs, readiness watched)
k6 under upgrade: {"requests":124613,"non_2xx":0,"failed_rate":0,"v1":68009,"v2":56604,"p95_ms":6.32}
readiness samples (running but not ready): 36, of which receiving traffic: 0
responses moved v1 -> v2
PASS rolling upgrade v1 -> v2: zero non-2xx, no traffic before ready
== trace check with OpenTelemetry Collector (file exporter)
PASS trace cb7563e2d0b92db179b4bcdd3319fab3: POST /v1/classify -> queue.wait, batch.run (batch.size=1)
== pdb and probes present
NAME                MIN AVAILABLE   MAX UNAVAILABLE   ALLOWED DISRUPTIONS   AGE
inference-gateway   1               N/A               3                     5m49s
{
  "readinessProbe": {
    "failureThreshold": 2,
    "httpGet": {
      "path": "/readyz",
      "port": "http",
      "scheme": "HTTP"
    },
    "periodSeconds": 2,
    "successThreshold": 1,
    "timeoutSeconds": 1
  },
  "livenessProbe": {
    "failureThreshold": 3,
    "httpGet": {
      "path": "/healthz",
      "port": "http",
      "scheme": "HTTP"
    },
    "periodSeconds": 10,
    "successThreshold": 1,
    "timeoutSeconds": 1
  },
  "resources": {
    "limits": {
      "cpu": "1",
      "memory": "512Mi"
    },
    "requests": {
      "cpu": "250m",
      "memory": "256Mi"
    }
  },
  "securityContext": {
    "allowPrivilegeEscalation": false,
    "capabilities": {
      "drop": [
        "ALL"
      ]
    },
    "readOnlyRootFilesystem": true
  }
}
ALL REQUESTED STEPS FINISHED
```

What each check proves:

- **Smoke**: the same pod answers HTTP and gRPC through the Service.
- **HPA**: before load the autoscaler had no scale-up pending. Under k6 load (60 VUs) the desired replica count rose from the minimum of 2 to the maximum of 4 within about 40 s. The Deployment had 1 replica at that moment (the Deployment default before the HPA acts), so this is "desired rose above the minimum", not a clean 2-to-4 trace.
- **Rolling upgrade**: k6 ran 10 virtual users inside the cluster for 75 s while `helm upgrade` switched the model. Every upgrade had zero non-2xx responses, answers moved from one `model_version` to the other, and no pod that was running but not ready was ever in the ready endpoints (sampled every 0.5 s).
- **Trace**: one trace has the HTTP server span `POST /v1/classify` with children `queue.wait` and `batch.run` (with `batch.size`).

### A failure found on the way

An earlier run of the rolling-upgrade check failed: 4 of 122,069 requests got `EOF` in the same second, and one readiness sample looked wrong. The cause of the 4 errors was a keep-alive race: a client reused an idle connection at the moment a terminating pod closed it. The fix is in `internal/server/server.go`: while the server is not ready it sends `Connection: close`, so clients stop reusing connections before the pod goes away (there is a unit test). The readiness sample was a race in the test script (two reads that are not atomic), and the script now rechecks the pod before it counts a violation. After the fix, three upgrades in a row passed with zero errors. Three passes show the fix works in these runs. They do not prove that errors can never happen.

## 3. Go tests (`go test -race -count=1 -v ./...`)

```text
--- PASS: TestSIGTERMDrains (3.75s)
PASS
ok  	github.com/consultantstanleymn/inference-gateway/cmd/gateway	4.789s
--- PASS: TestBatchNeverExceedsMax (0.03s)
--- PASS: TestLoneRequestWaitsAtMostMaxWait (0.03s)
--- PASS: TestFullQueueSheds (0.00s)
--- PASS: TestCancelledRequestsAreNotRun (0.00s)
--- PASS: TestCloseDrainsQueueAndRejectsNew (0.05s)
PASS
ok  	github.com/consultantstanleymn/inference-gateway/internal/batcher	1.148s
--- PASS: TestParityWithPython (0.24s)
    model_test.go:108: top intent unchanged in a batch of 200: 198 of 200
--- PASS: TestBatchCompositionEffect (0.11s)
--- PASS: TestChecksumMismatchFails (0.10s)
PASS
ok  	github.com/consultantstanleymn/inference-gateway/internal/model	1.498s
--- PASS: TestHTTPAndGRPCAnswer (0.02s)
--- PASS: TestOverloadReturns429AndResourceExhausted (0.11s)
--- PASS: TestReadyz (0.01s)
--- PASS: TestShutdownFinishesInFlight (0.37s)
PASS
ok  	github.com/consultantstanleymn/inference-gateway/internal/server	1.535s
--- PASS: TestGolden (0.06s)
--- PASS: TestLimits (0.00s)
PASS
ok  	github.com/consultantstanleymn/inference-gateway/internal/tokenize	1.078s
```

## 4. Model

See `models/v1/model_card.md` and `models/v2/model_card.md`.
