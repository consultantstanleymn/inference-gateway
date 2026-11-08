# Decisions

Each entry says what was chosen, why, and what it costs.

## 1. A small hashed-feature model, not a transformer
Choice: hashed unigrams and bigrams, mean embedding, one hidden layer (fastText style).
Why: it trains in about 5 minutes on one CPU thread of a 2015 laptop and serves in about a millisecond. The point of the project is serving, not model quality.
Cost: 86.4 percent accuracy on BANKING77 test. Fine-tuned transformers reach above 93 percent. We do not claim state of the art.

## 2. ASCII-only tokenizer working on bytes
Choice: lowercase A-Z, keep runs of `[a-z0-9]`, split on every other byte, hash with FNV-1a.
Why: Go and Python then give the same ids for any input. Unicode lowercasing and word rules differ between languages, so a unicode-aware tokenizer would need a shared library or a long list of edge cases.
Cost: non-English text is mostly dropped. The golden test (500 texts, including accents, Chinese characters and an empty string) proves both sides agree.

## 3. ONNX Runtime through cgo
Choice: `github.com/yalue/onnxruntime_go` with the official CPU library, pinned to 1.31.0 on both the Go and the Python side.
Why: one model file runs in both languages. Matching library versions avoid parity noise.
Cost: cgo. The binary needs glibc and libstdc++, so the image uses distroless `cc`, not `static`. The fallback in the plan (a Python worker behind the gateway) was not needed.

## 4. Dynamic int8 quantization
Choice: `quantize_dynamic` with int8 weights, including the embedding table.
Why: the file shrinks from 33.6 MB to 8.4 MB. Macro-F1 moved by under 0.1 point.
Cost: dynamic quantization picks activation scales for each batch. The same text can get slightly different probabilities in a different batch (up to about 1e-3 here). The top intent changed for 2 of 200 texts when run as one batch of 200. Go-versus-Python parity is therefore tested one row per call, and a second test measures the batch effect. A static (calibrated) quantization would remove this. It is listed as a next step.

## 5. Probabilities come out of the ONNX graph
Choice: softmax is part of the exported graph.
Why: Go only has to rank. No numeric code to keep in sync.

## 6. Batcher design
Choice: one bounded channel, workers that take one item, then wait up to `max_wait` or until `max_batch`.
Why: simple, easy to test, and the wait is bounded. A lone request waits at most `max_wait` after a worker picks it up, plus the time it sat in the queue.
Cost: with several workers, batches are smaller. The default is one worker. Cancelled requests are dropped when a batch forms, but a request cancelled while its batch runs still costs that work.

## 7. Shedding with 429 and RESOURCE_EXHAUSTED
Choice: a full queue refuses at once. No blocking, no unbounded queue.
Why: a fast refusal keeps latency for accepted requests predictable and gives clients a clear signal to back off.

## 8. Models baked into the image, version chosen by config
Choice: the image holds `/models/v1` and `/models/v2`. A ConfigMap value picks one. A config checksum annotation rolls the pods.
Why: the rolling upgrade test needs two model versions without a registry or a second image.
Cost: this is not how a real team ships models. A real setup would pull a signed model from a registry or object store. The rollout, probe and zero-error behaviour is the same, which is what the test proves.

## 9. Shutdown order
Choice: on SIGTERM the gateway turns readiness off, waits `shutdown-delay` (5 s in the chart), then stops the listeners and drains.
Why: Kubernetes removes a pod from the Service endpoints while the pod is still receiving some requests. The delay covers that gap. Without it a rolling upgrade can drop requests.

## 10. HPA on CPU only
Choice: CPU utilisation of 50 percent of the request.
Why: simple and enough to show scale-up. Queue depth or batch latency would be a better signal. That needs a metrics adapter, which is out of scope.

## 11. gRPC stub is hand-written, load tests use k6 for gRPC
Choice: `protoc` and `protoc-gen-go` come from Fedora packages. `protoc-gen-go-grpc`, `buf` and `ghz` are not packaged there, and this project installs build tools only from the Fedora repositories. So `gen/inferencev1/inference_grpc.pb.go` is written by hand in the same shape the generator produces, and gRPC load uses k6's gRPC client instead of ghz.
Cost: `buf lint` runs in CI only. If the proto changes, the stub must be updated by hand or regenerated with `buf generate`.

## 12. `Connection: close` while draining
Choice: when the server is not ready (warm-up or shutdown), every HTTP response carries `Connection: close`.
Why: one rolling-upgrade run had 4 requests fail with `EOF`. A client reused an idle keep-alive connection just as the old pod closed it. Telling clients to drop the connection during the drain window removes most of that race. Three later upgrades had zero errors. It reduces the risk. It does not remove it for clients that ignore the header.
