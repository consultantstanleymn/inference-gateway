"""Locust profile for POST /v1/classify. It sits next to the k6 scripts and aims at the same band.

Run it with the helper: scripts/locust-vs-k6.sh
Settings (environment):
  LOCUST_RPS_PER_USER  requests per second that each simulated user sends (default 10)
Run with: locust -f loadtest/locustfile.py --headless -u <users> -r <spawn rate> -t <time> --host http://127.0.0.1:25480
The texts come from data/test.csv. A fixed seed (7) gives the same text order in every run.
"""

from __future__ import annotations

import csv
import os
import random
from pathlib import Path

from locust import FastHttpUser, constant_throughput, task

_TEXTS_FILE = Path(__file__).resolve().parent.parent / "data" / "test.csv"
_RPS_PER_USER = float(os.environ.get("LOCUST_RPS_PER_USER", "10"))


def _load_texts() -> list[str]:
    with _TEXTS_FILE.open(newline="", encoding="utf-8") as f:
        return [row["text"] for row in csv.DictReader(f)]


_TEXTS = _load_texts()


class ClassifyUser(FastHttpUser):
    """One simulated caller. constant_throughput keeps a fixed rate per user."""

    wait_time = constant_throughput(_RPS_PER_USER)

    def on_start(self) -> None:
        # Each user has its own generator, seeded from the fixed seed and the user count.
        ClassifyUser._count = getattr(ClassifyUser, "_count", 0) + 1
        self._rng = random.Random(7 + ClassifyUser._count)

    @task
    def classify(self) -> None:
        text = self._rng.choice(_TEXTS)
        self.client.post("/v1/classify", json={"text": text}, name="/v1/classify")
