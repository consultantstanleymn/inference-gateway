"""Quality gate. Exit code 1 if a model misses the thresholds in gate.json."""
import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent


def main() -> int:
    gate = json.loads((ROOT / "train" / "gate.json").read_text())
    bad = 0
    for d in sys.argv[1:] or ["models/v1"]:
        m = json.loads((ROOT / d / "metrics.json").read_text())
        f1_int8, f1_fp32 = m["int8_test_macro_f1"], m["fp32_test_macro_f1"]
        drop = (f1_fp32 - f1_int8) * 100
        checks = [
            (f1_int8 >= gate["min_int8_macro_f1"], f"int8 macro-F1 {f1_int8:.4f} >= {gate['min_int8_macro_f1']}"),
            (drop <= gate["max_drop_points_vs_fp32"], f"int8 drop vs fp32 {drop:.3f} points <= {gate['max_drop_points_vs_fp32']}"),
            (m["onnx_vs_torch_max_abs_diff_200"] < gate["max_onnx_vs_torch_abs_diff"],
             f"onnx vs torch max diff {m['onnx_vs_torch_max_abs_diff_200']:.2e} < {gate['max_onnx_vs_torch_abs_diff']}"),
        ]
        for ok, msg in checks:
            print(("PASS " if ok else "FAIL ") + d + ": " + msg)
            bad += not ok
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
