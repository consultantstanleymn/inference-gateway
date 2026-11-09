# ADR 0003: GPU serving and fine-tuning

- Status: accepted
- Date: 2026-11-09
- Scope: decision level only

## Context

The gateway classifies text into 77 intents. The current model is a small hashed-feature model. It runs as an int8 ONNX model on ONNX Runtime on CPU. The measured numbers come from one laptop. See [`docs/PERF.md`](../../PERF.md) for the throughput and latency data.

The customer question is about replicas and CPU cores. See [`docs/engagement/CAPACITY.md`](../CAPACITY.md) for the capacity model. The latency SLO in the scenario is p95 under 50 ms.

This ADR records a decision. No GPU, Triton or vLLM was built, run or measured for this engagement. All numbers below are formulas or targets, not measurements of those options.

## Options

| Option | What it is | Where it runs |
|---|---|---|
| A | CPU ONNX int8, the current setup | CPU |
| B | Triton Inference Server | GPU |
| C | vLLM for a generative model | GPU |
| D | Fine-tuned small transformer classifier, in place of the hashed-feature model | CPU or GPU |

Option A is in production today. Options B, C and D are not built.

## Criteria

1. **Latency target.** The p95 must stay under 50 ms, as in the SLO. A change must hold this target at the peak load, not only at the average.
2. **Cost per million calls.** Use this formula only. Do not use vendor prices here.

   ```
   cost_per_million = (instance_price_per_hour / calls_per_hour) x 1,000,000
   ```

   Fill in the instance price and the calls per hour from the customer contract and from the capacity model. Compare the options on the same basis.
3. **Quality gate delta.** A change must raise the held-out accuracy gate in `train/gate.json` by a margin that the customer names. The p95 target must still hold after the change. A change that does not clear the named margin does not justify the work.
4. **Operations load.** Count the drivers, images, scheduling and patching that each option adds. A GPU option adds a driver stack, a GPU image and GPU scheduling. A new model adds a training pipeline and a model release process.

## Decision

Stay on option A: CPU ONNX int8.

Reasons:

- Option A meets the latency target on the measured data.
- Option A has the lowest operations load. It needs no GPU driver, no GPU image and no GPU scheduling.
- No quality gap is open today that would justify option D.
- The product does classification, not generation. Option C does not fit the task.

## Reopen trigger

Reopen this ADR when any one of these is true:

1. The quality gate cannot be met by the hashed-feature model.
2. The measured CPU cost per million calls, at the 10x estimate in [`CAPACITY.md`](../CAPACITY.md), is higher than the customer budget.
3. The product needs generation, not classification.

## Consequences

- The capacity plan stays on CPU replicas and cores. See [`CAPACITY.md`](../CAPACITY.md).
- The team does not carry a GPU driver stack, a GPU image or GPU scheduling.
- If a reopen trigger fires, run a measured comparison of the options before any change. Use the criteria above.
- The cost formula needs the customer instance price and the calls per hour before it can give a number.
