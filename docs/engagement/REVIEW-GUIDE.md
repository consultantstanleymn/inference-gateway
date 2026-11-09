# Review guide

A 60-minute walkthrough of the engagement work.

## 5-minute pitch

CommunityCU is a credit union. Its gateway classifies text into 77 intents for chat turns, emails and in-app search. It handles about 15 million calls per day, with a peak hour at 3 times the average. A mobile-app launch may bring 10 times the traffic. The customer asked two questions: how many gateway replicas and CPU cores do we need, and where does latency come from? This work answers both with measurements, a capacity model and a review of an unreviewed draft.

## Demo steps

| Step | Command | Time |
|---|---|---|
| 1 | `podman compose -f deploy/compose/compose.yaml config` | 1 min |
| 2 | `make image` then `scripts/diag-run.sh` | 5 min |
| 3 | `python3 tools/check_dashboard.py deploy/compose/grafana/dashboards/golden-signals.json` | 1 min |
| 4 | `python3 -m unittest scripts/test_capacity_model.py` | 1 min |
| 5 | `python3 scripts/capacity_model.py --check-doc docs/engagement/CAPACITY.md` | 1 min |
| 6 | `go test -race ./internal/canary/` | 2 min |
| 7 | `scripts/canary-run.sh fail` | 2 min |
| 8 | `podman run --rm -v "$PWD/deploy/compose/prometheus/rules:/r:ro,z" --entrypoint promtool docker.io/prom/prometheus:v2.55.1 test rules /r/slo.rules.test.yml` | 1 min |

Step 2 needs the image from `make image`. Step 7 exits with code 3 on the forced rollback.

## Likely questions

1. **What throughput per core did you measure?** Median 5214.3 rps per core at 6000 rps, measured on one laptop. See `docs/engagement/CAPACITY.md`.
2. **What is the peak load?** Peak 520.8 rps. The 10x launch peak is 5208.3 rps. See `docs/engagement/CAPACITY.md`.
3. **How many replicas for 10x?** 2 to 3 replicas of 2 cores. This is an estimate with a range. See `docs/engagement/CAPACITY.md`.
4. **Where does CPU time go?** At 6000 rps the HTTP serving path is 68 percent of CPU samples and the model batch run is 3.9 percent. See `docs/engagement/evidence/experiments/`.
5. **Did GC cause the latency?** No. GC stop-the-world is 0.168 ms per cycle. See `docs/engagement/evidence/experiments/`.
6. **Does Locust agree with k6?** Yes at 2000 rps, within 3.8 percent. No at 4000 rps, 20.3 percent apart. See `docs/engagement/LOADTEST.md`.
7. **What happened in the canary pass?** All four steps promoted. See `docs/engagement/evidence/canary-pass.txt`.
8. **What happened in the forced failure?** Rollback at the first step, with canary error rate 0.3186. See `docs/engagement/evidence/canary-fail-rollback.txt`.
9. **What is the latency SLO?** 99 percent of requests under 50 ms over 28 days, with burn rates 14.4 and 6. See `docs/engagement/SLO.md`.
10. **How do you link latency to traces?** Exemplars link latency buckets to traces. See `docs/engagement/TELEMETRY.md`.
11. **Is the metric cardinality bounded?** Yes. The budget is under 200 series and the live scrape had 101 lines. See `docs/engagement/TELEMETRY.md`.
12. **What spans does one request have?** Four: `POST /v1/classify`, `tokenize`, `queue.wait`, `batch.run`. See `docs/engagement/evidence/jaeger-request-trace.json`.
13. **Is the gateway authenticated?** No. There is no authentication and no TLS. See `docs/engagement/RISK-DECISION.md`.
14. **Is GPU serving in scope?** No. GPU is ADR only. See `docs/engagement/adr/0003-gpu-and-fine-tuning.md`.
15. **How was the capacity draft checked?** Every wrong claim got a verdict, a correct number and an evidence file. See `docs/engagement/capacity-review/CORRECTION.md`.
16. **What does the dashboard show?** Latency p50, p95, p99; traffic; errors and sheds; queue depth and CPU. See `deploy/compose/grafana/dashboards/golden-signals.json`.
17. **How is the SLO rule tested?** With a promtool test that exits 0. See `deploy/compose/prometheus/rules/slo.rules.test.yml`.
18. **What is in the release manifest?** The image SBOM and the model file hashes. See `release/manifest.json`.
19. **How do you capture a profile?** Start the gateway with `GATEWAY_ADMIN_ADDR=127.0.0.1:25406` and use `go tool pprof -top`. See `docs/engagement/RUNBOOK.md`.
20. **What is the first step on a shed spike?** Scale out. Do not raise the queue size first. See `docs/engagement/RUNBOOK.md`.

## Remaining risks

- New CI jobs are not proven on GitHub. The past security runs failed in `deps` (CI used Go 1.26.0) and `image` (exceptions not passed to Trivy). Both causes are fixed, and the run after the fix was not checked here. One `ci` run failed in the Go and Python parity test on one runner CPU.
- The release push and the cosign signing step are authored, not run.
- No GPU work was done.
- All numbers come from one laptop, with the load generator on the same machine.
- The experiments ran below saturation.
- `security/exceptions.json` has a libssl3 exception.
- The Locust reports are compressed.
