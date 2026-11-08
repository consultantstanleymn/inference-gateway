# Model card: banking intent classifier v2

## What it does
Maps one customer support message (English) to one of 77 banking intents.

## Data
BANKING77 (PolyAI), official split: 10003 train and 3080 test examples.
License CC BY 4.0. Credit: PolyAI Ltd. See `data/README.md`.

## Model
Hashed unigram and bigram features (2^18 buckets, FNV-1a), mean of 32-dim embeddings, one hidden layer
(128 units, ReLU), softmax. Trained on CPU with PyTorch, seed 2, 15 epochs, 1 thread, 454 s.
Exported to ONNX (opset 17), then dynamic int8 quantization with ONNX Runtime.

## Results on the official test split
| Variant | Accuracy | Macro-F1 | File size |
|---|---|---|---|
| PyTorch fp32 | 0.8630 | 0.8637 | n/a |
| ONNX fp32 | 0.8630 | 0.8637 | 33.6 MB |
| ONNX int8 (served) | 0.8623 | 0.8629 | 8.4 MB |

ONNX fp32 vs PyTorch, 200 samples: max absolute difference 5.36e-07.

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
