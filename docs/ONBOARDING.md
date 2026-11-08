# Onboarding

Goal: run the gateway on your machine in about 10 minutes, then change something.

## You need

- Linux x86-64 with `gcc` (cgo needs it), Go 1.26, Python 3.12.
- For the Kubernetes part: `podman` (or Docker), `kind`, `kubectl`, `helm`.
- For load tests: `k6`.

Fedora: `sudo dnf install golang gcc kind kubernetes1.35-client helm golangci-lint protobuf-compiler golang-google-protobuf`.

## Run it

```bash
scripts/fetch-onnxruntime.sh      # downloads libonnxruntime.so 1.31.0 and checks its SHA-256
go test -race ./...               # unit, golden, parity and SIGTERM tests
go run ./cmd/gateway              # HTTP :8080, gRPC :9090, model models/v1
```

In a second terminal:

```bash
curl -s -XPOST localhost:8080/v1/classify -d '{"text":"My card has not arrived yet"}'
go run ./cmd/probe grpc -addr 127.0.0.1:9090 -text "My card has not arrived yet"
curl -s localhost:8080/metrics | grep gateway_
```

Use other ports if these are busy: `go run ./cmd/gateway -http-addr :18080 -grpc-addr :19090`.

## Retrain a model (optional, about 6 minutes)

```bash
make setup        # venv with CPU PyTorch
make model-v1     # train, export to ONNX, quantize, gate, model card, test fixtures
```

`model-v1` rewrites the Go test fixtures. Run `go test ./...` afterwards. If you change the tokenizer, change `train/common.py` and `internal/tokenize/tokenize.go` together and regenerate the fixtures.

## Map of the code

| Path | What it does |
|---|---|
| `train/` | Training, ONNX export, quantization, quality gate, model card |
| `models/v1`, `models/v2` | int8 ONNX model, labels, metrics, model card (the fp32 and PyTorch files are not committed) |
| `internal/tokenize` | Hashed-feature tokenizer, identical to Python |
| `internal/model` | Loads ONNX with a checksum check, runs a padded batch |
| `internal/batcher` | Bounded queue, micro-batching, shedding, cancellation |
| `internal/server` | HTTP and gRPC front ends, one shared `Service` |
| `internal/telemetry` | Prometheus metrics and OpenTelemetry setup |
| `cmd/gateway`, `cmd/probe` | The service and a small client |
| `deploy/helm/inference-gateway` | Helm chart |
| `scripts/kind-e2e.sh` | Kubernetes checks |
| `loadtest/` | k6 scripts |

## Tests and what they prove

| Test | File |
|---|---|
| Go tokenizer equals Python on 500 texts | `internal/tokenize/tokenize_test.go` |
| Go inference equals Python ONNX Runtime on 200 texts | `internal/model/model_test.go` |
| Batch never above max, lone request waits at most `max_wait`, full queue returns 429, cancelled requests do not run | `internal/batcher/batcher_test.go` |
| HTTP 429 and gRPC RESOURCE_EXHAUSTED, in-flight request finishes during shutdown | `internal/server/server_test.go` |
| Real binary: SIGTERM while 40 requests run, all succeed, readiness drops, exit code 0 | `cmd/gateway/signal_test.go` |
