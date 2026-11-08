"""Export to ONNX, quantize to int8, re-evaluate, write artifacts and test fixtures."""
import argparse
import hashlib
import json
import os
import random

import numpy as np
import onnxruntime as ort
import torch
from onnxruntime.quantization import QuantType, quantize_dynamic
from sklearn.metrics import f1_score

import common


def session(path):
    o = ort.SessionOptions()
    o.intra_op_num_threads = 1
    return ort.InferenceSession(str(path), o, providers=["CPUExecutionProvider"])


def run(sess, feats, bs=256):
    out = []
    for i in range(0, len(feats), bs):
        ids, mask = common.pad(feats[i : i + bs])
        out.append(sess.run(None, {"ids": ids.numpy(), "mask": mask.numpy()})[0])
    return np.concatenate(out)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dir", required=True)
    ap.add_argument("--fixtures", action="store_true", help="write Go test fixtures from this model")
    a = ap.parse_args()
    d = common.ROOT / a.dir
    labels = json.loads((d / "labels.json").read_text())
    metrics = json.loads((d / "metrics.json").read_text())
    idx = {l: i for i, l in enumerate(labels)}

    model = common.Classifier(len(labels), hidden=metrics["hidden"])
    model.load_state_dict(torch.load(d / "model.pt"))
    model.eval()

    fp32 = d / "model.fp32.onnx"
    ids, mask = common.pad([[1, 2, 3], [4, 5]])
    torch.onnx.export(
        model, (ids, mask), str(fp32), input_names=["ids", "mask"], output_names=["probs"],
        dynamic_axes={"ids": {0: "batch", 1: "len"}, "mask": {0: "batch", 1: "len"}, "probs": {0: "batch"}},
        opset_version=17, dynamo=False,
    )
    int8 = d / "model.onnx"
    quantize_dynamic(str(fp32), str(int8), weight_type=QuantType.QInt8)

    te_x, te_y = common.load_split("test")
    y = np.array([idx[l] for l in te_y])
    fte = [common.features(t) for t in te_x]
    res = {}
    for name, path in (("fp32_onnx", fp32), ("int8_onnx", int8)):
        p = run(session(path), fte).argmax(-1)
        res[name] = (float((p == y).mean()), float(f1_score(y, p, average="macro")))

    # parity: ONNX fp32 equals PyTorch on 200 samples
    pf = [fte[i] for i in range(200)]
    ids, mask = common.pad(pf)
    with torch.no_grad():
        ref = model(ids, mask).numpy()
    diff = float(np.abs(run(session(fp32), pf) - ref).max())

    metrics.update({
        "onnx_fp32_test_accuracy": res["fp32_onnx"][0], "onnx_fp32_test_macro_f1": res["fp32_onnx"][1],
        "int8_test_accuracy": res["int8_onnx"][0], "int8_test_macro_f1": res["int8_onnx"][1],
        "onnx_vs_torch_max_abs_diff_200": diff,
        "fp32_onnx_bytes": os.path.getsize(fp32), "int8_onnx_bytes": os.path.getsize(int8),
    })
    (d / "metrics.json").write_text(json.dumps(metrics, indent=2))
    sha = hashlib.sha256(int8.read_bytes()).hexdigest()
    (d / "meta.json").write_text(json.dumps(
        {"version": metrics["version"], "sha256": sha, "labels": labels, "max_features": common.MAX_FEATURES}, indent=1))
    print(json.dumps(metrics, indent=2))

    if a.fixtures:
        write_fixtures(int8, idx, te_x)


def write_fixtures(int8, idx, te_x):
    rng = random.Random(7)
    edge = ["", "   ", "Hello, WORLD!!", "Why was my card declined at the ATM?", "café über 中文 100% sure",
            "a-b_c d.e", "UPPER lower MiXeD 123 4567", "x " * 100, "I’m locked out"]
    texts = edge + rng.sample(te_x, 491)
    tok = [{"text": t, "ids": common.features(t)} for t in texts]
    tdir = common.ROOT / "internal" / "tokenize" / "testdata"
    tdir.mkdir(parents=True, exist_ok=True)
    (tdir / "golden.json").write_text(json.dumps(tok, ensure_ascii=True))
    sample = rng.sample(te_x, 200)
    # batch size 1: dynamic int8 picks activation scales per batch, so only same-batch runs match to 1e-4
    probs = run(session(int8), [common.features(t) for t in sample], bs=1)
    par = [{"text": t, "top": int(p.argmax()), "probs": [round(float(v), 7) for v in p]} for t, p in zip(sample, probs)]
    pdir = common.ROOT / "internal" / "model" / "testdata"
    pdir.mkdir(parents=True, exist_ok=True)
    (pdir / "parity.json").write_text(json.dumps(par, ensure_ascii=True))
    print("fixtures written:", len(tok), "golden,", len(par), "parity")


if __name__ == "__main__":
    main()
