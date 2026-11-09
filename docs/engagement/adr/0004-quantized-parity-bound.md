# ADR 0004: Parity bound for the int8 model

- Status: accepted
- Date: 2026-11-09
- Scope: the Go test `TestParityWithPython` in `internal/model/model_test.go`

## Context

The test compares the Go result with the Python ONNX Runtime result for 200 texts. The model is an int8 dynamic-quantized ONNX model. The old test had one rule: every probability must differ by at most 5e-4.

One CI run (run 42611746329, commit b673686) failed this rule. One text differed by 1.53e-3. The runs before and after passed on the same code. The cause is the runner CPU, not the code.

## Why int8 kernels differ by CPU

- ONNX Runtime picks an int8 matrix kernel at run time. The choice depends on the CPU features (for example AVX2, AVX-512 VNNI or AMX).
- The kernels add integer products in different orders and with different rounding.
- Dynamic quantization finds the activation scale from the data at run time. A tiny change in one value can move a scale. The next layer then rounds some values to a different integer.
- The float32 softmax at the end turns a small logit change into a small probability change. The step is at most about 1e-3 for this model.

The numbers differ in a small and bounded way. The ranking of intents must not change.

## Options

| Option | Result |
|---|---|
| A | Keep 5e-4 for every value. It fails on some CPUs. Rejected. |
| B | Widen the limit for every value to a large number. It hides real faults. Rejected. |
| C | Measure the right things: a cap on the largest difference, a tight bound on the 99th percentile, and full agreement on the top intents. Accepted. |

## Decision

The test now requires all of these:

1. **Cap.** No probability differs by more than 5e-3. This is 3.3 times the largest cross-CPU difference seen (1.53e-3).
2. **99th percentile.** Over all 15400 values (200 texts, 77 classes), the 99th percentile of the difference is at most 5e-4. This keeps the old tight bound for nearly all values.
3. **Top-1.** The top intent from Go equals the top intent from Python for 100 percent of the 200 texts.
4. **Top-3.** The top-3 set from Go equals the top-3 set from Python. A class can differ only when its Python probability is within the cap of the Python third place. That case is a near tie.

## Measured numbers

Measured on one laptop (Intel Core i7-4770HQ, 8 threads, Go 1.26.10), the same machine that made the fixture:

| Measure | Value |
|---|---|
| Largest difference, 15400 values | 5.96e-08 |
| 99th percentile, all values | 4.94e-08 |
| Largest difference per text, median | 4.97e-08 |
| Top-1 agreement | 200 of 200 |

The local spread is tiny because the same CPU made both results. It is not a bound for other CPUs. The only cross-CPU data point is the CI failure: 1.53e-3, from one run. No other runner was measured, so the cap rests on that one value and a margin of 3.3 times.

## Why the new bound is safe

- The cap is 5e-3. A wrong model, a wrong tokenizer or a changed weight moves probabilities by much more than this. A test with one fixture value moved by 0.01 failed at the cap.
- The top-1 and top-3 checks catch a change in the ranking, which is what the service returns to a caller.
- The 99th percentile bound stays at 5e-4. A broad drift in many values fails it even when each value is below the cap.
- A check with the top-1 index shifted by one failed the test.

## Consequences

- The test now passes on CPUs with different int8 kernels, if the difference stays below the cap.
- If a future runner exceeds 5e-3, the test fails. Treat that as a real signal. Read the failing text and class first. Do not widen the cap without new measurements from several runners.
- The batch effect is a separate matter. See `TestBatchCompositionEffect` and `LEARN.md`.
- The fixture `parity.json` is unchanged. The Python generator (`make model-v1`, step `train/export.py --fixtures`) was not run for this change. It retrains the model and takes about 6 minutes.
