"""Shared pieces for training and export: tokenizer, data loading, model."""
import csv
import pathlib

import torch
from torch import nn

ROOT = pathlib.Path(__file__).resolve().parent.parent
BUCKETS = 1 << 18
MAX_WORDS = 64
MAX_FEATURES = 2 * MAX_WORDS - 1  # unigrams plus bigrams of the kept words

FNV_OFFSET = 2166136261
FNV_PRIME = 16777619


def fnv1a32(data: bytes) -> int:
    h = FNV_OFFSET
    for b in data:
        h ^= b
        h = (h * FNV_PRIME) & 0xFFFFFFFF
    return h


def words(text: str) -> list[bytes]:
    """ASCII only: lowercase A-Z, keep runs of [a-z0-9], every other byte splits.

    Working on raw UTF-8 bytes keeps Go and Python identical for any input.
    """
    out, cur = [], bytearray()
    for b in text.encode("utf-8"):
        if 65 <= b <= 90:
            b += 32
        if 97 <= b <= 122 or 48 <= b <= 57:
            cur.append(b)
        elif cur:
            out.append(bytes(cur))
            cur = bytearray()
    if cur:
        out.append(bytes(cur))
    return out[:MAX_WORDS]


def features(text: str) -> list[int]:
    """Hashed unigram buckets, then hashed bigram buckets, in text order."""
    w = words(text)
    ids = [fnv1a32(x) % BUCKETS for x in w]
    ids += [fnv1a32(a + b" " + b) % BUCKETS for a, b in zip(w, w[1:])]
    return ids


def load_split(name: str):
    path = ROOT / "data" / f"{name}.csv"
    with open(path, newline="", encoding="utf-8") as f:
        rows = list(csv.DictReader(f))
    return [r["text"] for r in rows], [r["category"] for r in rows]


def pad(batch_ids: list[list[int]]):
    """Pad to the longest row. Returns ids [B, L] int64 and mask [B, L] float32."""
    length = max(1, max(len(x) for x in batch_ids))
    ids = torch.zeros((len(batch_ids), length), dtype=torch.int64)
    mask = torch.zeros((len(batch_ids), length), dtype=torch.float32)
    for i, row in enumerate(batch_ids):
        if row:
            ids[i, : len(row)] = torch.tensor(row, dtype=torch.int64)
            mask[i, : len(row)] = 1.0
    return ids, mask


class Classifier(nn.Module):
    """Masked mean of hashed embeddings, one hidden layer, softmax."""

    def __init__(self, n_classes: int, dim: int = 32, hidden: int = 128, dropout: float = 0.2):
        super().__init__()
        self.emb = nn.Embedding(BUCKETS, dim)
        nn.init.normal_(self.emb.weight, std=0.1)
        self.fc1 = nn.Linear(dim, hidden)
        self.drop = nn.Dropout(dropout)
        self.fc2 = nn.Linear(hidden, n_classes)

    def logits(self, ids, mask):
        e = self.emb(ids) * mask.unsqueeze(-1)
        mean = e.sum(dim=1) / mask.sum(dim=1, keepdim=True).clamp(min=1.0)
        return self.fc2(self.drop(torch.relu(self.fc1(mean))))

    def forward(self, ids, mask):
        """Probabilities. This is the graph that gets exported to ONNX."""
        return torch.softmax(self.logits(ids, mask), dim=-1)
