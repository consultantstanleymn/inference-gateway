# LEARN: how this project works, in plain language

Read this top to bottom once. Then read the code in the order of the "Code tour". Then answer the questions at the end out loud.

## The one-minute story

A bank gets customer messages like "my card has not arrived". We want a program that reads a message and says which of 77 topics it is about (card arrival, lost card, exchange rate, and so on). That is a classifier.

The classifier is a small neural network. A Python script trains it. A second Python script converts it to a standard file format (ONNX) and shrinks it (int8 quantization). A Go program loads that file and answers requests over HTTP and gRPC. Go is used because the serving part is about concurrency, latency and clean shutdown, and Go is good at that.

The interesting part is not the network. It is how the Go program serves it: it groups requests (batching), refuses extra work when it is overloaded (load shedding), stops cleanly, and reports what it is doing (metrics and traces). A Helm chart then runs it in Kubernetes and a test proves that you can swap the model while traffic keeps flowing.

## Words you need

- **Classifier.** Text in, one label out, with a probability for each label.
- **Token and feature.** We cut the text into lowercase words. Each word, and each pair of neighbouring words, becomes a feature.
- **Hashing trick.** Instead of a dictionary that maps every word to a number, we run the word through a hash function (FNV-1a) and keep the result modulo 262,144. Two words can collide. With a model this size that costs little accuracy and we never need to store a vocabulary.
- **Embedding.** A row of 32 numbers for each hash bucket. The model learns these rows. A message becomes the average of the rows of its features.
- **ONNX.** A file format for trained models. Python writes it. ONNX Runtime (a C++ library) reads it from Python or from Go.
- **Quantization (int8).** Store the weights as 8-bit integers instead of 32-bit floats. The file is 4 times smaller and math can be faster. A tiny loss of quality is the price. We measure that loss and gate on it.
- **Macro-F1.** An accuracy score that gives every one of the 77 classes the same weight. Good when classes are not equal in size.
- **Micro-batching.** Collect requests for a few milliseconds, run the model once on all of them, hand each caller its own answer. One call for 16 rows is cheaper than 16 calls for 1 row each.
- **Load shedding.** When the waiting line is full, say "no, try later" at once (HTTP 429, gRPC RESOURCE_EXHAUSTED) instead of letting everybody wait forever.
- **Probes.** Kubernetes asks the program two questions. Liveness: are you stuck? If yes, restart. Readiness: can you take traffic now? If no, send no traffic.
- **HPA.** Horizontal Pod Autoscaler. Adds pods when CPU use is high.
- **PDB.** Pod Disruption Budget. A promise "keep at least N pods up" that Kubernetes respects when it moves pods around on purpose (node drain, upgrade).
- **Rolling update.** Replace pods one by one: start a new one, wait until it is ready, then stop an old one.
- **OpenTelemetry.** A standard way to describe what happened inside a request as a tree of timed steps (spans). **Prometheus** is the counters and histograms you scrape from `/metrics`.

## Code tour (read in this order)

1. `train/common.py`. The tokenizer (`words`, `features`) and the model class. Notice the model uses only plain operations (embedding, mask, mean, two linear layers, softmax). That is why it exports to ONNX cleanly.
2. `train/train.py`. Fixed seed, one thread, 12 passes over the data, then accuracy and macro-F1 on the official test split.
3. `train/export.py`. Export to ONNX, quantize, re-test both versions, compare ONNX to PyTorch on 200 samples, write the checksum, write test fixtures for Go.
4. `train/gate.py` and `train/gate.json`. The rule "int8 macro-F1 at least 0.84 and at most 1 point below fp32".
5. `internal/tokenize/tokenize.go`. The same tokenizer in Go. `tokenize_test.go` checks it against 500 texts from Python.
6. `internal/model/model.go`. Checks the SHA-256 of the model, opens an ONNX Runtime session, pads a batch to equal length, runs it, copies out the probabilities.
7. `internal/batcher/batcher.go`. The heart. `Submit` puts a request in a bounded channel or refuses it. `loop` and `collect` build a batch. `process` skips cancelled requests, runs the model once, sends each answer back.
8. `internal/server/service.go`. Tokenize, submit, rank the top 3, add trace spans. `server.go` maps errors to status codes and sets up shutdown.
9. `cmd/gateway/main.go`. Wires everything, warms the model up, turns readiness on, waits for SIGTERM, shuts down in order.
10. `deploy/helm/inference-gateway/`. Deployment (probes, limits, security settings), Service, HPA, PDB, ConfigMap.
11. `scripts/kind-e2e.sh`. The Kubernetes tests.

## Why each design choice exists (say these in an interview)

**Why must the Go tokenizer match the Python one exactly?** The model learned embeddings for specific bucket numbers. If Go hashes "card" to bucket 1234 and Python hashed it to 5678 during training, the model looks at the wrong row and the answer is nonsense. It would still return an answer, with no error. So we test it: Python writes 500 texts and the bucket ids it computes. Go must produce the same ids. The texts include accents, Chinese characters and an empty string. To make this possible we limited the tokenizer to ASCII and work on bytes, so no language-specific Unicode rules can differ.

**What does batching buy, and what does it cost?** One model call has a fixed overhead plus a cost per row. With 16 rows in one call the overhead is shared, so total throughput goes up. The cost is waiting: the first request in a batch waits for others. `max_wait` (5 ms) is the cap on that waiting, and `max_batch` (16) is the cap on batch size. Under light traffic batching adds up to 5 ms and gains nothing. Under heavy traffic it adds little and gains a lot. See `docs/PERF.md` for what we measured, including where it did not help.

**Why a bounded queue and 429?** An unbounded queue hides overload: nothing fails, but every request gets slower until all of them time out. A bounded queue turns overload into a quick, clear refusal. The caller can retry or back off. Requests that are accepted keep a predictable latency.

**What do liveness and readiness protect against?** Readiness protects callers: a new pod is not sent traffic until the model is loaded and warm. Liveness protects the system from a stuck process: Kubernetes restarts it. Do not make liveness depend on the model or on other services, or a slow start gets the pod killed in a loop. Here liveness only says "the process answers" and readiness says "the model is loaded and warm".

**Why does a PDB matter during upgrades?** Rolling updates and node drains remove pods on purpose. Without a budget Kubernetes may take down too many at once. With `minAvailable: 1` it waits until a replacement is ready. The Deployment also uses `maxUnavailable: 0` and `maxSurge: 1`: start one extra pod first, then remove an old one.

**Why the `shutdownDelay`?** When a pod gets SIGTERM, Kubernetes starts removing it from the Service, but that takes a moment, and for that moment new requests can still arrive. If the program closed its port at once, those requests would fail. So the gateway first says "not ready", waits 5 seconds, and then stops accepting and finishes what it has. That is why the rolling upgrade test sees zero errors.

**What does int8 quantization change, and how do we limit the damage?** Weights are stored as 8-bit integers with a scale. The math has small rounding errors. We compare macro-F1 before and after on the test split and fail CI if int8 is more than 1 point worse or below the absolute floor. In this project the loss was under 0.1 point.

**A real wrinkle we found.** Dynamic int8 chooses the scale for the inputs of each layer from the batch it sees. So the same text can get slightly different probabilities depending on which other texts share its batch. We saw differences of about 0.001 and the top answer changed for 2 of 200 texts in one test. That is why the Go-versus-Python parity test runs one text per call, and why a separate test measures the batch effect. A calibrated static quantization would remove it. Knowing this and saying it plainly is a good interview answer.

## What is simplified (be honest about this)

- CPU only. No GPU, CUDA, vLLM or Triton. Batching is the same idea GPU servers use, shown on CPU.
- A small word-hash model, not a transformer. Accuracy is 86 percent, not state of the art.
- The model file is baked into the container image and chosen by config. A real team would load a signed model from a registry.
- Kubernetes runs only in kind (a local test cluster). No managed cluster, no service mesh, no real production operations.
- The HPA uses CPU only.
- There is no authentication and no TLS. See `SECURITY.md`.
- The gRPC stub file is hand-written because the code generator is not packaged in Fedora.
- Trace export was tested against an OpenTelemetry Collector with a file exporter, not a real tracing backend.
- Performance numbers come from a 2015 laptop that was also running other work. Read them as relative, not absolute.
- GitHub Actions has not run yet (see README "Verification status").

## Questions to answer out loud

1. A request arrives. Walk through every step until the response leaves. (HTTP handler, tokenize, queue, batch, model, rank, response, metrics, spans.)
2. What happens if 5,000 requests arrive in one second and the model handles 800 per second?
3. A caller times out after 100 ms. What does the gateway do with its request?
4. Why is `/readyz` different from `/healthz`?
5. You deploy model v2. Which mechanism prevents dropped requests?
6. Python says macro-F1 is 0.863 and the Go service gives different answers for the same text. List three things you check.
7. Why did we hash words instead of using a vocabulary file?
8. What would you change to serve a transformer on a GPU, and what would stay the same?

## Commands to try while learning

```bash
go test -race -v ./internal/batcher          # watch the batcher tests
curl -s localhost:8080/metrics | grep -E 'batch_size|queue_wait'
go run ./cmd/gateway -max-batch 1            # compare with the default 16
python3 -c "import sys; sys.path.insert(0,'train'); import common; print(common.features('My card has not arrived'))"
```
