#!/usr/bin/env python3
"""Write release/manifest.json: hashes that tie a release to its inputs.

The manifest holds the git commit, the image id, the SHA-256 of each model file,
the quality gate file, the pinned ONNX Runtime library, the Go version and go.sum.
It uses the Python standard library only. Run it from anywhere inside the repo.

Usage: scripts/release_manifest.py [--image localhost/inference-gateway:dev] [--out release/manifest.json]
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def run(cmd: list[str]) -> str:
    try:
        out = subprocess.run(cmd, cwd=ROOT, check=True, capture_output=True, text=True)
    except (OSError, subprocess.CalledProcessError):
        return ""
    return out.stdout.strip()


def ort_pin() -> dict[str, str]:
    text = (ROOT / "scripts" / "fetch-onnxruntime.sh").read_text(encoding="utf-8")
    version = re.search(r"^VERSION=(\S+)", text, re.M)
    sha = re.search(r"^SHA256=(\S+)", text, re.M)
    return {
        "version": version.group(1) if version else "",
        "archive_sha256": sha.group(1) if sha else "",
    }


def models() -> list[dict[str, str]]:
    rows = []
    for model in sorted((ROOT / "models").glob("*/model.onnx")):
        d = model.parent
        meta = d / "meta.json"
        rows.append(
            {
                "name": d.name,
                "model_onnx_sha256": sha256_file(model),
                "meta_json_sha256": sha256_file(meta) if meta.exists() else "",
                "meta_sha256_field": json.loads(meta.read_text(encoding="utf-8")).get("sha256", "") if meta.exists() else "",
            }
        )
    return rows


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--image", default="localhost/inference-gateway:dev")
    ap.add_argument("--out", default="release/manifest.json")
    args = ap.parse_args()

    image_id = run(["podman", "image", "inspect", "--format", "{{.Id}}", args.image])
    image_digest = run(["podman", "image", "inspect", "--format", "{{.Digest}}", args.image])
    gate = ROOT / "train" / "gate.json"
    lib = ROOT / "third_party" / "libonnxruntime.so"
    manifest = {
        "git_commit": run(["git", "rev-parse", "HEAD"]),
        "git_tree_clean": run(["git", "status", "--porcelain"]) == "",
        "image": {"name": args.image, "id": image_id, "digest": image_digest},
        "models": models(),
        "quality_gate_sha256": sha256_file(gate) if gate.exists() else "",
        "onnxruntime": {**ort_pin(), "library_sha256": sha256_file(lib) if lib.exists() else ""},
        "go": {"version": run(["go", "env", "GOVERSION"]), "go_sum_sha256": sha256_file(ROOT / "go.sum")},
    }
    out = ROOT / args.out
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(f"wrote {args.out}: {len(manifest['models'])} models")
    if not image_id:
        print("note: no image id found; build the image first", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
