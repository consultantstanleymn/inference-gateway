# Security

This is a portfolio project. It is not hardened for production. This page says what exists and what does not.

## What is in place

- The container runs as a non-root user (distroless `nonroot`, uid 65532) with a read-only root file system, no extra Linux capabilities, no privilege escalation and the `RuntimeDefault` seccomp profile.
- The Helm chart sets `automountServiceAccountToken: false`. The gateway does not call the Kubernetes API.
- The model file is checked against a SHA-256 in `meta.json` at start. A changed model file does not load (tested).
- The ONNX Runtime library download is pinned to one version and checked against a SHA-256 in `scripts/fetch-onnxruntime.sh`. The checksum was recorded when the file was first downloaded from the official release page. It is not signed by a second source.
- Request bodies are limited to 64 KiB. Each request has a deadline. A full queue returns 429, so a burst cannot grow memory without limit.
- CI scans the image with Trivy and fails on critical findings that have a fix. This was not run in CI yet (see README, "Verification status").
- No secrets are in the repository. The gateway needs none.

## What is not in place

- **No authentication or authorization.** Anyone who can reach the port can call the API. The design assumes a network boundary. Add mTLS or a token check before any real use.
- **No TLS.** HTTP and gRPC are plaintext.
- **No rate limit per caller.** Load shedding protects the service as a whole, not one caller from another.
- **No input sanitising beyond size and encoding.** The model only sees hashed word features, but logs and traces could contain customer text if you add that. The code does not log request text.
- **No supply-chain controls** beyond Go module checksums (`go.sum`) and pinned Python versions. There is no SBOM or signature on the image.
- **Model files are baked into the image.** There is no signed model registry.
- The OpenTelemetry Collector in the e2e test uses plaintext OTLP inside the test cluster.

## Data

BANKING77 is public customer-support text with no personal data. No real customer data is used.

## Reporting

Open an issue on the repository. There is no bug bounty.
