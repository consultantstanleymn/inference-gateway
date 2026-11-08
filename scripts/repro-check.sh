#!/usr/bin/env bash
# Train v1 again with the same seed and compare macro-F1 with the committed run (limit 0.5 points).
set -euo pipefail
cd "$(dirname "$0")/.."
PY=${PYTHON:-.venv/bin/python}
nice -n 10 "$PY" train/train.py --seed 1 --epochs 12 --out out/repro --version v1 | tail -1
"$PY" - <<'PY'
import json, sys
a = json.load(open("models/v1/metrics.json"))["fp32_test_macro_f1"]
b = json.load(open("out/repro/metrics.json"))["fp32_test_macro_f1"]
d = abs(a - b) * 100
print(f"committed {a:.5f} rerun {b:.5f} difference {d:.4f} points")
sys.exit(0 if d <= 0.5 else 1)
PY
