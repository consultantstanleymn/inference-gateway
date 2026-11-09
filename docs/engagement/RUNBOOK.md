# Engagement runbook

Main runbook: [docs/RUNBOOK.md](../../RUNBOOK.md). It holds the signal table, the standard checks and the day-to-day procedures. This page adds the engagement procedures for the SLO, the canary, shedding and profiling.

## 1. Latency SLO burn

Alerts: `GatewayLatencySLOFastBurn` and `GatewayLatencySLOSlowBurn` in `deploy/compose/prometheus/rules/slo.rules.yml`. The SLO numbers are explained in `docs/engagement/SLO.md`.

1. Confirm the alert is firing in Prometheus.
   ```
   curl -s http://127.0.0.1:9090/api/v1/alerts | jq '.data.alerts[] | select(.labels.alertname | startswith("GatewayLatencySLO"))'
   ```
2. Open the Grafana dashboard `inf-golden` and read p95 by model version.
   ```
   open http://127.0.0.1:3000/d/inf-golden
   ```
3. On the same dashboard, read queue depth, shed rate and CPU.
4. Check if the burn started after a deploy.
   ```
   podman ps --format '{{.Image}} {{.CreatedAt}}'
   ```
5. If a deploy came first, roll back the image.
   ```
   podman compose -f deploy/compose/compose.yaml up -d --force-recreate gateway
   ```
6. If the canary is active, set the v2 weight to 0.
   ```
   scripts/canary-run.sh fail
   ```
7. Record the alert name, the start time and the action in the incident note.

## 2. Canary rollback

The gate values are in `docs/engagement/ROLLOUT.md`. The proxy is a test tool, not a production component.

1. Run the canary in pass mode or fail mode.
   ```
   scripts/canary-run.sh pass
   scripts/canary-run.sh fail
   ```
2. Read the controller JSON lines in the output. Each line shows the step, the weight, the error rate and p95.
3. On a gate breach the controller sets the v2 weight to 0 at once.
4. The canary command exits with code 3 on a rollback. Check the exit code.
   ```
   echo $?
   ```
5. Confirm the weight is 0.
   ```
   curl -s http://127.0.0.1:8081/weight
   ```
6. Keep the transcript. The pass and fail transcripts live in `docs/engagement/evidence/`.

## 3. Shedding spike

1. Check the shed counter.
   ```
   curl -s http://127.0.0.1:8080/metrics | grep 'gateway_requests_total{.*outcome="shed"'
   ```
2. Check queue depth and CPU on the `inf-golden` dashboard.
3. Scale out first.
   ```
   podman compose -f deploy/compose/compose.yaml up -d --scale gateway=3
   ```
4. Do not raise the queue size first. A larger queue adds wait time and hides the overload.
5. If shedding continues after scale out, check the batch settings against `docs/RUNBOOK.md`.
6. Record the shed rate before and after the change.

## 4. pprof capture

1. Start the gateway with the admin address on loopback only.
   ```
   GATEWAY_ADMIN_ADDR=127.0.0.1:25406 ./bin/gateway
   ```
2. Capture a 20 second CPU profile.
   ```
   curl -o cpu.pb.gz 'http://127.0.0.1:25406/debug/pprof/profile?seconds=20'
   ```
3. Read the top of the profile as text.
   ```
   go tool pprof -top -nodecount=25 cpu.pb.gz
   ```
4. Keep the `cpu.pb.gz` file with the experiment notes.
5. Never bind the admin address to a public interface.
6. graphviz is not installed, so use text output (`-top`, `-list`, `-traces`). Do not use `-svg`.
