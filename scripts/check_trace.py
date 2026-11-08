"""Read OTLP JSON lines (collector file exporter) and check for one span tree.

Pass: some trace has a root `request` span with `queue.wait` and `batch.run` children,
and batch.run carries a batch.size attribute.
"""
import json
import sys


def spans(lines):
    for line in lines:
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            doc = json.loads(line)
        except json.JSONDecodeError:
            continue
        for rs in doc.get("resourceSpans", []):
            for ss in rs.get("scopeSpans", []):
                yield from ss.get("spans", [])


def main() -> int:
    by_trace = {}
    for s in spans(sys.stdin):
        by_trace.setdefault(s["traceId"], []).append(s)
    for tid, ss in by_trace.items():
        root = [s for s in ss if s["name"] == "request"]
        if not root:
            continue
        rid = root[0]["spanId"]
        kids = {s["name"]: s for s in ss if s.get("parentSpanId") == rid}
        if "queue.wait" in kids and "batch.run" in kids:
            attrs = {a["key"]: a["value"] for a in kids["batch.run"].get("attributes", [])}
            if "batch.size" in attrs:
                print(f"PASS trace {tid}: request -> queue.wait, batch.run (batch.size={attrs['batch.size']})")
                return 0
    print(f"FAIL no complete span tree among {len(by_trace)} traces")
    return 1


if __name__ == "__main__":
    sys.exit(main())
