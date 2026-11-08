.PHONY: setup ort test lint proto run model-v1 model-v2 gate image e2e

PY ?= .venv/bin/python

setup:           ## Python venv for training and export (CPU PyTorch)
	python3.12 -m venv .venv
	.venv/bin/pip install --extra-index-url https://download.pytorch.org/whl/cpu -r train/requirements.txt

ort:             ## Download the pinned ONNX Runtime library (checksum verified)
	scripts/fetch-onnxruntime.sh

test: ort        ## Go tests with the race detector
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...
	go vet ./...

proto:           ## Regenerate message types (needs protoc and protoc-gen-go)
	protoc -I proto --go_out=. --go_opt=module=github.com/consultantstanleymn/inference-gateway proto/inference/v1/inference.proto

run: ort         ## Run the gateway on :8080 (HTTP) and :9090 (gRPC)
	go run ./cmd/gateway

model-v1:        ## Train, export and gate model v1 (about 6 minutes on a 2015 laptop)
	$(PY) train/train.py --seed 1 --epochs 12 --out models/v1 --version v1
	$(PY) train/export.py --dir models/v1 --fixtures
	$(PY) train/gate.py models/v1
	$(PY) train/model_card.py models/v1

model-v2:
	$(PY) train/train.py --seed 2 --epochs 15 --out models/v2 --version v2
	$(PY) train/export.py --dir models/v2
	$(PY) train/gate.py models/v2
	$(PY) train/model_card.py models/v2

gate:
	$(PY) train/gate.py models/v1 models/v2

image:
	podman build -t localhost/inference-gateway:dev -f Containerfile .

e2e:             ## Kind cluster checks (rootless podman here, Docker in CI)
	CONTAINER_TOOL=podman scripts/kind-e2e.sh
