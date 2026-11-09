# Risk decision: no authentication on the gateway

## Decision

Accept: no authentication on the gateway. It must sit behind a private network or a mesh with mTLS.

## Risk

The gateway has no authentication and no TLS. Any caller that reaches a port can send classify requests. A caller can read traffic on the wire. A caller can fill the bounded queue and cause sheds.

## Options

1. Add a token check in the gateway. This is simple, but it adds a shared secret to every caller and a check on the hot path.
2. Use mTLS in a mesh or sidecar. The mesh handles identity and encryption. The gateway stays simple.
3. Private network only. No extra code. The network is the control.
4. Accept as is for a lab setup. The setup is local, all ports bind to 127.0.0.1, and no customer text leaves the host.

## Decision

Accept with conditions. The gateway runs only behind a private network or a mesh with mTLS. It does not listen on a public interface. The conditions below must hold.

## Owner

Stanley Sujith Nelavala

## Review date

2027-02-07

## Compensating controls

- All compose ports bind to 127.0.0.1.
- Request size limit is 64 KiB.
- Load shedding returns 429 when the queue is full.
- The container is distroless and runs as non-root. The Helm chart sets a read-only root file system.
- The security gate in `docs/engagement/SECURITY-GATE.md` must pass.
- The pprof listener is off by default and stays on loopback when on.

## What would change the decision

- Any non-loopback exposure of a gateway port.
- Any customer text in logs.
