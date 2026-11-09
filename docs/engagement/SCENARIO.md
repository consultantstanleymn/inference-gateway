Fictional customer. Any match with a real company is not intended.

# Scenario: CommunityCU intent gateway

## Customer

CommunityCU is a credit union. It runs a text classification gateway. The gateway
classifies text into 77 intents. The text comes from chat turns, emails and
in-app search.

## The question

"How many gateway replicas and CPU cores do we need, and where does latency
come from?"

## Traffic assumptions

These numbers are assumptions for planning. They are not customer data.

- Calls per day: 15,000,000.
- Average rate: 15,000,000 calls / 86,400 s = 173.6 calls per second.
- Peak hour factor: 3 times the average.
- Peak rate: 173.6 x 3 = 520.8 calls per second.
- Mobile-app launch: 10 times the traffic.
- Launch peak rate: 520.8 x 10 = 5,208 calls per second.

## Text length mix

The mix comes from `data/test.csv`. The file has 3,080 rows and 77 classes.
Characters per text:

| Measure | Value |
|---|---|
| Median | 45 |
| Mean | 54.2 |
| 95th percentile | 124 |
| Maximum | 368 |

These values are assumptions for load profiles. They are not customer data.

## Scope

In scope:

- Capacity model: replicas and CPU cores for the average, peak and launch rates.
- Latency attribution: where time goes on one request path.
- Telemetry repair: latency split by model version, exemplars that link
  latency to traces, and a bounded label budget.
- Load test comparison: a second load tool next to the k6 scripts.
- Bottleneck experiments: one factor changes at a time.
- Canary with automatic rollback.
- One latency SLO with an alert rule and a test.
- A decision record for GPU serving and fine-tuning.
- Release manifest and SBOM.

Out of scope:

- Model training and model quality changes.
- Changes to the 77 intent classes.
- Production deployment and production data.
- Cost of cloud hardware beyond the core count estimate.

## Constraints

- The gateway runs as a Go service with an ONNX Runtime library.
- The local image is `localhost/inference-gateway:dev`.
- The load tools run against a local or kind cluster.
- All data in this engagement is synthetic or from the repo.
