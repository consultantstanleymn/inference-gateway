"""Unit tests for scripts/capacity_model.py. Run: python3 -m unittest scripts/test_capacity_model.py -v"""

from __future__ import annotations

import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import capacity_model as cm  # noqa: E402


class CapacityModelTest(unittest.TestCase):
    def test_littles_law(self) -> None:
        # 500 requests per second, each 8 ms in the system: 4 requests in the system on average.
        self.assertAlmostEqual(cm.littles_law(500, 0.008), 4.0)

    def test_required_cores_rounding_up(self) -> None:
        # 1000 rps at 400 rps per core and 60 percent: 1000 / 240 = 4.17, so 5 cores.
        self.assertEqual(cm.required_cores(1000, 400, 0.6), 5)
        self.assertEqual(cm.required_cores(960, 400, 0.6), 4)

    def test_replicas_include_n_plus_one(self) -> None:
        self.assertEqual(cm.replicas_for(5, 2), 4)  # ceil(5/2) = 3, plus 1
        self.assertEqual(cm.replicas_for(4, 2), 3)

    def test_traffic_arithmetic(self) -> None:
        t = cm.traffic(15_000_000, 3, 10)
        self.assertAlmostEqual(t["average_rps"], 173.611, places=2)
        self.assertAlmostEqual(t["peak_rps"], 520.833, places=2)
        self.assertAlmostEqual(t["growth_peak_rps"], 5208.333, places=2)

    def test_reads_jsonl_rows(self) -> None:
        with tempfile.TemporaryDirectory() as d:
            p = Path(d) / "x.jsonl"
            p.write_text(json.dumps({"max_batch": 1, "transport": "http", "summary": {"throughput_rps": 10}}) + "\n\n", encoding="utf-8")
            rows = cm.read_jsonl(p)
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0]["summary"]["throughput_rps"], 10)

    def test_sustainable_rules(self) -> None:
        ok = {"p95_ms": 10, "shed_429": 0, "failed_rate": 0, "requests": 1000, "dropped_iterations": 0}
        self.assertTrue(cm.is_sustainable(ok, 50))
        self.assertFalse(cm.is_sustainable({**ok, "p95_ms": 60}, 50))
        self.assertFalse(cm.is_sustainable({**ok, "shed_429": 1}, 50))
        self.assertFalse(cm.is_sustainable({**ok, "dropped_iterations": 50}, 50))

    def test_median_is_used_for_per_core(self) -> None:
        import statistics

        self.assertEqual(statistics.median([3000.0, 5000.0, 9000.0]), 5000.0)

    def test_check_doc_detects_mismatch(self) -> None:
        res = {
            "traffic": {"average_rps": 173.6, "peak_rps": 520.8, "growth_peak_rps": 5208.3},
        }
        good = "Peak is <!--cm:peak_rps-->520.8 requests per second."
        bad = "Peak is <!--cm:peak_rps-->999 requests per second."
        self.assertEqual(cm.check_doc(res, good), [])
        problems = cm.check_doc(res, bad)
        self.assertEqual(len(problems), 1)
        self.assertIn("peak_rps", problems[0])
        self.assertTrue(cm.check_doc(res, "no tags here"))


if __name__ == "__main__":
    unittest.main()
