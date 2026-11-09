# Diagnostics compose stack

This stack runs the gateway with metrics, traces and dashboards.
It is for local diagnostics only.

## Services

| Service | Image | Host port | Purpose |
|---|---|---|---|
| gateway | localhost/inference-gateway:dev | 127.0.0.1:25480 (HTTP), 127.0.0.1:25490 (gRPC) | Inference gateway |
| otel-collector | opentelemetry-collector-contrib:0.120.0 | none | Receives OTLP traces and forwards them to Jaeger |
| jaeger | jaegertracing/all-in-one:1.66.0 | 127.0.0.1:25486 | Trace UI |
| prometheus | prom/prometheus:v2.55.1 | 127.0.0.1:25409 | Metrics store and rule evaluation |
| grafana | grafana/grafana-oss:11.4.0 | 127.0.0.1:25430 | Dashboards |

The gateway has a CPU limit of 2.0 cores.
The collector and Jaeger OTLP ports are not published to the host.

## Start

Build the image first if it is not present:

```
make image
```

Then start the stack:

```
podman compose -f deploy/compose/compose.yaml up -d
```

## Check

```
podman compose -f deploy/compose/compose.yaml config >/dev/null
docker compose -f deploy/compose/compose.yaml config -q
podman run --rm -v "$PWD/deploy/compose/prometheus:/p:ro,z" --entrypoint promtool docker.io/prom/prometheus:v2.55.1 check config /p/prometheus.yml
```

## URLs

| What | URL |
|---|---|
| Gateway health | http://127.0.0.1:25480/healthz |
| Gateway readiness | http://127.0.0.1:25480/readyz |
| Gateway metrics | http://127.0.0.1:25480/metrics |
| Jaeger UI | http://127.0.0.1:25486 |
| Prometheus | http://127.0.0.1:25409 |
| Grafana | http://127.0.0.1:25430 |

The Grafana admin user is `admin`.
The password comes from `GRAFANA_ADMIN_PASSWORD`.
The default is `local-test-only-admin`.
Anonymous access is off.

## Stop

```
podman compose -f deploy/compose/compose.yaml down
```

## Notes

- Prometheus scrapes `gateway:8080/metrics` every 5 s.
- Prometheus runs with `--enable-feature=exemplar-storage`.
- The rules directory `prometheus/rules` is mounted at `/etc/prometheus/rules`.
- The canary proxy job is added in the canary step.
- CPU saturation comes from `process_cpu_seconds_total` of the gateway.
  There is no cAdvisor service.
- There is no terraform here.
- Every host port binds to 127.0.0.1 only.
  Nothing in this stack is exposed beyond the loopback interface.
