#!/usr/bin/env python3
"""Capacity model for the gateway. Python standard library only.

It reads measured runs and answers: how much throughput per core, how long is the queue,
how many cores and replicas for a traffic target.

Inputs
  docs/engagement/evidence/experiments/*.json   one-factor runs (scripts/experiment.sh)
  docs/perf-data/*.jsonl                        earlier runs, one JSON object per line

Method
  1. A run is sustainable when p95 is at or under the target, no request is shed, nothing fails,
     and the load generator did not drop more than 1 percent of its arrivals.
  2. Throughput per core = achieved requests per second / CPU cores that the gateway used
     (measured from /proc during the run). Only experiment runs carry that measurement.
     The model uses the median over the sustainable runs.
  3. Little's law: L = lambda * W. L is the number of requests in the system.
  4. Cores needed = ceil(lambda / (throughput per core * utilization target)).
  5. Replicas = ceil(cores needed / cores per replica) + 1 (N+1: one spare replica).
All numbers come from one laptop. They are estimates for planning, not a promise for other hardware.
"""

from __future__ import annotations

import argparse
import json
import math
import re
import statistics
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SECONDS_PER_DAY = 86_400


def littles_law(rate_per_s: float, seconds_in_system: float) -> float:
    """L = lambda * W: the average number of requests in the system."""
    return rate_per_s * seconds_in_system


def required_cores(rate_per_s: float, rps_per_core: float, utilization: float) -> int:
    """Whole cores needed to carry the rate at the utilization target. Always rounds up."""
    if rps_per_core <= 0 or not 0 < utilization <= 1:
        raise ValueError("rps_per_core must be positive and utilization must be in (0, 1]")
    return math.ceil(rate_per_s / (rps_per_core * utilization))


def replicas_for(cores: int, cores_per_replica: int) -> int:
    """Replicas for the cores needed, plus one spare (N+1)."""
    return math.ceil(cores / cores_per_replica) + 1


def traffic(calls_per_day: float, peak_factor: float, growth: float) -> dict[str, float]:
    avg = calls_per_day / SECONDS_PER_DAY
    peak = avg * peak_factor
    return {"average_rps": avg, "peak_rps": peak, "growth_peak_rps": peak * growth}


def read_jsonl(path: Path) -> list[dict]:
    rows = []
    for line in path.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if line:
            rows.append(json.loads(line))
    return rows


def load_runs(root: Path = ROOT) -> list[dict]:
    """Return one flat record per run: name, source, settings, summary, cores_used (or None)."""
    runs: list[dict] = []
    for p in sorted((root / "docs" / "engagement" / "evidence" / "experiments").glob("*.json")):
        d = json.loads(p.read_text(encoding="utf-8"))
        runs.append(
            {
                "name": p.stem,
                "source": "experiment",
                "settings": d.get("settings", {}),
                "summary": d["summary"],
                "cores_used": (d.get("measured") or {}).get("gateway_cpu_cores_used"),
            }
        )
    for p in sorted((root / "docs" / "perf-data").glob("*.jsonl")):
        for i, row in enumerate(read_jsonl(p)):
            runs.append(
                {
                    "name": f"{p.stem}#{i}",
                    "source": "perf-data",
                    "settings": {"max_batch": row.get("max_batch"), "transport": row.get("transport")},
                    "summary": row["summary"],
                    "cores_used": None,
                }
            )
    return runs


def is_sustainable(summary: dict, p95_target_ms: float) -> bool:
    requests = max(summary.get("requests", 0), 1)
    dropped = summary.get("dropped_iterations", 0) / requests
    return (
        summary.get("p95_ms", float("inf")) <= p95_target_ms
        and summary.get("shed_429", 0) == 0
        and summary.get("failed_rate", 0) == 0
        and dropped <= 0.01
    )


def best_by_config(runs: list[dict], p95_target_ms: float) -> list[dict]:
    """For each configuration, the sustainable run with the highest throughput."""
    best: dict[str, dict] = {}
    for r in runs:
        if r["settings"].get("transport", "http") != "http" or not is_sustainable(r["summary"], p95_target_ms):
            continue
        key = json.dumps(r["settings"], sort_keys=True)
        if key not in best or r["summary"]["throughput_rps"] > best[key]["summary"]["throughput_rps"]:
            best[key] = r
    return sorted(best.values(), key=lambda r: -r["summary"]["throughput_rps"])


def build(args: argparse.Namespace) -> dict:
    runs = load_runs()
    configs = best_by_config(runs, args.p95_target_ms)
    with_cores = [r for r in configs if r["cores_used"]]
    # Throughput per core of CPU used, for every sustainable run that has a CPU measurement.
    per_run = sorted((r["summary"]["throughput_rps"] / float(r["cores_used"]), r["name"]) for r in with_cores)
    per_core = None
    best_core_run = None
    if per_run:
        values = [v for v, _ in per_run]
        per_core = statistics.median(values)
        # The run closest to the median gives the latency numbers for Little's law.
        best_core_run = min(with_cores, key=lambda r: abs(r["summary"]["throughput_rps"] / float(r["cores_used"]) - per_core))
    t = traffic(args.calls_per_day, args.peak_factor, args.growth)
    out: dict = {
        "inputs": {
            "calls_per_day": args.calls_per_day,
            "peak_factor": args.peak_factor,
            "growth": args.growth,
            "p95_target_ms": args.p95_target_ms,
            "utilization_target": args.util,
            "cores_per_replica": args.cores_per_replica,
        },
        "traffic": {k: round(v, 1) for k, v in t.items()},
        "runs_read": len(runs),
        "sustainable_configs": [
            {
                "name": r["name"],
                "source": r["source"],
                "settings": r["settings"],
                "throughput_rps": r["summary"]["throughput_rps"],
                "p95_ms": r["summary"]["p95_ms"],
                "cores_used": r["cores_used"],
            }
            for r in configs[:12]
        ],
    }
    if per_core:
        w_p95 = best_core_run["summary"]["p95_ms"] / 1000.0
        w_avg = best_core_run["summary"]["avg_ms"] / 1000.0
        out["per_core"] = {
            "rps_per_core": round(per_core, 1),
            "method": "median over sustainable runs",
            "runs_used": len(per_run),  # distinct configurations (the best sustainable run of each)
            "lowest_rps_per_core": round(per_run[0][0], 1),
            "highest_rps_per_core": round(per_run[-1][0], 1),
            "latency_from_run": best_core_run["name"],
        }
        est = {}
        for label, rate in (("peak", t["peak_rps"]), ("growth_peak", t["growth_peak_rps"])):
            cores = required_cores(rate, per_core, args.util)
            est[label] = {
                "rate_rps": round(rate, 1),
                "cores_needed": cores,
                "replicas_n_plus_1": replicas_for(cores, args.cores_per_replica),
                "requests_in_system_avg": round(littles_law(rate, w_avg), 2),
                "requests_in_system_p95": round(littles_law(rate, w_p95), 2),
            }
        out["estimate"] = est
        # 10x range. Low: the best measured rate per core. Central: the median. High: half the median,
        # a cautious allowance for a core on other hardware (an assumption, not a measurement).
        cases = {"low": per_run[-1][0], "central": per_core, "high": per_core / 2}
        rng = {}
        for name, rpc in cases.items():
            c = required_cores(t["growth_peak_rps"], rpc, args.util)
            rng[f"cores_{name}"] = c
            rng[f"replicas_{name}"] = replicas_for(c, args.cores_per_replica)
            rng[f"rps_per_core_{name}"] = round(rpc, 1)
        rng["note"] = "high case assumes a core on the target hardware does half the work of the median measured core"
        out["growth_range"] = rng
    return out


def markdown(res: dict) -> str:
    lines = ["| Item | Value |", "|---|---|"]
    t = res["traffic"]
    lines += [f"| Average rate | {t['average_rps']} rps |", f"| Peak hour rate | {t['peak_rps']} rps |", f"| Peak with growth | {t['growth_peak_rps']} rps |"]
    if "per_core" in res:
        lines.append(f"| Throughput per core | {res['per_core']['rps_per_core']} rps (median of {res["per_core"]["runs_used"]} distinct configurations; lowest {res['per_core']['lowest_rps_per_core']}, highest {res['per_core']['highest_rps_per_core']}) |")
        for label, e in res["estimate"].items():
            lines.append(f"| {label}: cores needed | {e['cores_needed']} |")
            lines.append(f"| {label}: replicas (N+1) | {e['replicas_n_plus_1']} |")
            lines.append(f"| {label}: requests in system (avg, p95) | {e['requests_in_system_avg']}, {e['requests_in_system_p95']} |")
        g = res["growth_range"]
        lines.append(f"| 10x cores: low, central, high | {g['cores_low']}, {g['cores_central']}, {g['cores_high']} |")
        lines.append(f"| 10x replicas (N+1): low, central, high | {g['replicas_low']}, {g['replicas_central']}, {g['replicas_high']} |")
    return "\n".join(lines)


def flatten(res: dict) -> dict[str, float]:
    """Numbers that a document may quote with a tag like <!--cm:peak_rps-->123."""
    flat: dict[str, float] = {
        "average_rps": res["traffic"]["average_rps"],
        "peak_rps": res["traffic"]["peak_rps"],
        "growth_peak_rps": res["traffic"]["growth_peak_rps"],
    }
    if "per_core" in res:
        flat["rps_per_core"] = res["per_core"]["rps_per_core"]
        for label, e in res["estimate"].items():
            flat[f"{label}_cores"] = e["cores_needed"]
            flat[f"{label}_replicas"] = e["replicas_n_plus_1"]
            flat[f"{label}_in_system_p95"] = e["requests_in_system_p95"]
        g = res["growth_range"]
        for k in ("cores_low", "cores_central", "cores_high", "replicas_low", "replicas_central", "replicas_high"):
            flat[k] = g[k]
    return flat


TAG = re.compile(r"<!--cm:([a-z0-9_]+)-->\s*([0-9][0-9,]*\.?[0-9]*)")


def check_doc(res: dict, doc_text: str) -> list[str]:
    """Compare every <!--cm:name-->value tag in the document with the model. Return the problems."""
    flat = flatten(res)
    problems = []
    found = TAG.findall(doc_text)
    if not found:
        problems.append("no <!--cm:name-->value tags found in the document")
    for name, value in found:
        if name not in flat:
            problems.append(f"unknown tag {name}")
            continue
        got = float(value.replace(",", ""))
        if abs(got - float(flat[name])) > 1e-9:
            problems.append(f"{name}: document says {got}, model says {flat[name]}")
    return problems


def parser() -> argparse.ArgumentParser:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--calls-per-day", type=float, default=15_000_000)
    ap.add_argument("--peak-factor", type=float, default=3)
    ap.add_argument("--growth", type=float, default=10)
    ap.add_argument("--p95-target-ms", type=float, default=50)
    ap.add_argument("--util", type=float, default=0.6)
    ap.add_argument("--cores-per-replica", type=int, default=2)
    ap.add_argument("--check-doc", type=Path, help="compare tagged numbers in this Markdown file with the model")
    return ap


def main(argv: list[str] | None = None) -> int:
    args = parser().parse_args(argv)
    res = build(args)
    if args.check_doc:
        problems = check_doc(res, args.check_doc.read_text(encoding="utf-8"))
        for p in problems:
            print("MISMATCH:", p)
        print("doc check:", "FAILED" if problems else "ok")
        return 1 if problems else 0
    print(json.dumps(res, indent=2, sort_keys=True))
    print()
    print(markdown(res))
    return 0


if __name__ == "__main__":
    sys.exit(main())
