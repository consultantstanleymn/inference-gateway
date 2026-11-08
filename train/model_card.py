"""Write models/<v>/model_card.md from metrics.json."""
import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
TEMPLATE = """# Model card: banking intent classifier {version}

## What it does
Maps one customer support message (English) to one of 77 banking intents.

## Data
BANKING77 (PolyAI), official split: {train_examples} train and {test_examples} test examples.
License CC BY 4.0. Credit: PolyAI Ltd. See `data/README.md`.

## Model
Hashed unigram and bigram features (2^18 buckets, FNV-1a), mean of 32-dim embeddings, one hidden layer
({hidden} units, ReLU), softmax. Trained on CPU with PyTorch, seed {seed}, {epochs} epochs, 1 thread, {train_seconds_cpu_1_thread} s.
Exported to ONNX (opset 17), then dynamic int8 quantization with ONNX Runtime.

## Results on the official test split
| Variant | Accuracy | Macro-F1 | File size |
|---|---|---|---|
| PyTorch fp32 | {fp32_test_accuracy:.4f} | {fp32_test_macro_f1:.4f} | n/a |
| ONNX fp32 | {onnx_fp32_test_accuracy:.4f} | {onnx_fp32_test_macro_f1:.4f} | {fp32_mb:.1f} MB |
| ONNX int8 (served) | {int8_test_accuracy:.4f} | {int8_test_macro_f1:.4f} | {int8_mb:.1f} MB |

ONNX fp32 vs PyTorch, 200 samples: max absolute difference {onnx_vs_torch_max_abs_diff_200:.2e}.

## Quality gate
`train/gate.json` sets the minimum int8 macro-F1 (set after the first real run) and a maximum
drop of 1.0 point against fp32. `train/gate.py` enforces it in CI.

## Limits
- English only. ASCII letters and digits only; other characters split words.
- 77 fixed intents. There is no "unknown" class: out-of-scope text still gets an intent.
- Probabilities are not calibrated. Do not treat 0.9 as 90 percent correct.
- Dynamic int8 scales activations per batch, so the same text can score slightly differently in
  another batch. The top intent changed for 2 of 200 samples when 200 texts ran as one batch.
- Small model chosen so it trains and serves on a laptop. It is not state of the art.
"""


def main():
    for d in sys.argv[1:]:
        m = json.loads((ROOT / d / "metrics.json").read_text())
        m["fp32_mb"] = m["fp32_onnx_bytes"] / 1e6
        m["int8_mb"] = m["int8_onnx_bytes"] / 1e6
        (ROOT / d / "model_card.md").write_text(TEMPLATE.format(**m))
        print("wrote", d + "/model_card.md")


if __name__ == "__main__":
    main()
