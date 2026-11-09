#!/usr/bin/env python3
"""Check a Grafana dashboard JSON for required fields and known metric names.

Usage: check_dashboard.py <dashboard.json> [metrics-doc.md]

The metrics doc defaults to docs/engagement/TELEMETRY.md. Every metric name
found in a panel expression must appear in the doc text.
"""

import json
import re
import sys

DEFAULT_DOC = "docs/engagement/TELEMETRY.md"

METRIC_RE = re.compile(r"\b(?:gateway_|process_|go_)[a-zA-Z0-9_]+")
SUFFIXES = ("_bucket", "_sum", "_count")


def strip_suffix(name):
    for suffix in SUFFIXES:
        if name.endswith(suffix):
            return name[: -len(suffix)]
    return name


def collect_exprs(panel):
    exprs = []
    for target in panel.get("targets", []):
        expr = target.get("expr")
        if isinstance(expr, str) and expr.strip():
            exprs.append(expr)
    return exprs


def main(argv):
    if len(argv) < 2 or len(argv) > 3:
        print("usage: check_dashboard.py <dashboard.json> [metrics-doc.md]")
        return 2
    path = argv[1]
    doc_path = argv[2] if len(argv) == 3 else DEFAULT_DOC

    problems = []

    try:
        with open(path, "r", encoding="utf-8") as handle:
            dashboard = json.load(handle)
    except (OSError, json.JSONDecodeError) as exc:
        print("cannot load dashboard: %s" % exc)
        return 1

    try:
        with open(doc_path, "r", encoding="utf-8") as handle:
            doc_text = handle.read()
    except OSError as exc:
        print("cannot load metrics doc: %s" % exc)
        return 1

    if not dashboard.get("title"):
        problems.append("dashboard has no title")
    if not dashboard.get("uid"):
        problems.append("dashboard has no uid")

    panels = dashboard.get("panels")
    if not isinstance(panels, list) or not panels:
        problems.append("dashboard has no panels")
        panels = []

    metric_names = set()
    for index, panel in enumerate(panels):
        label = "panel %d" % index
        if not panel.get("title"):
            problems.append("%s has no title" % label)
        if not panel.get("type"):
            problems.append("%s has no type" % label)
        exprs = collect_exprs(panel)
        if not exprs:
            problems.append("%s has no target with an expr" % label)
        for expr in exprs:
            for raw in METRIC_RE.findall(expr):
                metric_names.add(strip_suffix(raw))

    for name in sorted(metric_names):
        if name not in doc_text:
            problems.append("metric %s is not in %s" % (name, doc_path))

    if problems:
        for problem in problems:
            print("PROBLEM: %s" % problem)
        return 1

    print("OK %d panels, %d metric names" % (len(panels), len(metric_names)))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
