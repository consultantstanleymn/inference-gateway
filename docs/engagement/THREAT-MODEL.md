# Threat model

The full STRIDE analysis with a data-flow diagram lives in the console project of the same engagement. This page is a short pointer. It lists the known gaps and the control each gap needs.

## Known gaps

These gaps come from `SECURITY.md`:

- No authentication.
- No TLS.
- No per-caller rate limit.
- No input sanitising beyond size and encoding checks.
- Model files are baked into the image, with no signed registry.
- Plaintext OTLP inside the test setup.
- The pprof admin listener is off by default and must stay on loopback.

## Gap table

| Gap | Effect | Control today | Control needed |
|---|---|---|---|
| No authentication | Any caller that reaches a port can send requests | Ports bound to 127.0.0.1 in compose | mTLS or a token check at a mesh or ingress |
| No TLS | Traffic is readable on the wire | Local only, loopback ports | mTLS in a mesh, or TLS at the ingress |
| No per-caller rate limit | One caller can fill the queue | Bounded queue, load shedding with 429 | Per-caller quota at the ingress or mesh |
| No input sanitising beyond size and encoding | Malformed text reaches the tokenizer | 64 KiB request size limit, encoding check | Schema validation at the edge |
| Model files baked into the image, no signed registry | Image content is not verified at pull time | Image scan in CI | Signed registry and image signature check |
| Plaintext OTLP in the test setup | Traces are readable on the wire | Collector on loopback | mTLS to the collector in any shared network |
| pprof admin listener | CPU and memory profiles are exposed when on | Off by default, loopback only | Keep off in production, or gate behind auth |

## Security gate

The security gate is described in `docs/engagement/SECURITY-GATE.md`. A release must pass that gate before it leaves the lab.

## Deployment condition

The gateway must sit behind a private network or a mesh with mTLS. See `docs/engagement/RISK-DECISION.md`.
