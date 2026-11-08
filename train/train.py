"""Train the classifier on CPU, evaluate on the official test split, write metrics."""
import argparse
import json
import random
import time

import numpy as np
import torch
from sklearn.metrics import f1_score
from torch import nn

import common


def encode(texts):
    return [common.features(t) for t in texts]


def evaluate(model, feats, y, bs=512):
    model.eval()
    preds = []
    with torch.no_grad():
        for i in range(0, len(feats), bs):
            ids, mask = common.pad(feats[i : i + bs])
            preds.append(model.logits(ids, mask).argmax(-1))
    p = torch.cat(preds).numpy()
    return float((p == y).mean()), float(f1_score(y, p, average="macro"))


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--seed", type=int, default=1)
    ap.add_argument("--epochs", type=int, default=12)
    ap.add_argument("--hidden", type=int, default=128)
    ap.add_argument("--out", required=True)
    ap.add_argument("--version", required=True)
    a = ap.parse_args()

    random.seed(a.seed)
    np.random.seed(a.seed)
    torch.manual_seed(a.seed)
    torch.set_num_threads(1)  # one thread keeps runs repeatable and neighbour friendly

    tr_x, tr_y = common.load_split("train")
    te_x, te_y = common.load_split("test")
    labels = sorted(set(tr_y))
    idx = {l: i for i, l in enumerate(labels)}
    ytr = np.array([idx[l] for l in tr_y])
    yte = np.array([idx[l] for l in te_y])
    ftr, fte = encode(tr_x), encode(te_x)

    model = common.Classifier(len(labels), hidden=a.hidden)
    opt = torch.optim.Adam(model.parameters(), lr=2e-3)
    lossf = nn.CrossEntropyLoss()
    t0 = time.time()
    order = np.arange(len(ftr))
    rng = np.random.RandomState(a.seed)
    for ep in range(a.epochs):
        model.train()
        rng.shuffle(order)
        total = 0.0
        for i in range(0, len(order), 64):
            b = order[i : i + 64]
            ids, mask = common.pad([ftr[j] for j in b])
            opt.zero_grad()
            loss = lossf(model.logits(ids, mask), torch.from_numpy(ytr[b]))
            loss.backward()
            opt.step()
            total += loss.item() * len(b)
        print(f"epoch {ep + 1} loss {total / len(order):.4f}", flush=True)
    secs = time.time() - t0
    acc, f1 = evaluate(model, fte, yte)
    print(f"test accuracy {acc:.4f} macro_f1 {f1:.4f} train_seconds {secs:.0f}")

    out = common.ROOT / a.out
    out.mkdir(parents=True, exist_ok=True)
    torch.save(model.state_dict(), out / "model.pt")
    (out / "labels.json").write_text(json.dumps(labels))
    metrics = {
        "version": a.version, "seed": a.seed, "epochs": a.epochs, "hidden": a.hidden,
        "fp32_test_accuracy": acc, "fp32_test_macro_f1": f1,
        "train_seconds_cpu_1_thread": round(secs),
        "train_examples": len(ftr), "test_examples": len(fte),
    }
    (out / "metrics.json").write_text(json.dumps(metrics, indent=2))


if __name__ == "__main__":
    main()
