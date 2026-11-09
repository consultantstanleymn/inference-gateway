#!/usr/bin/env python3
"""Check the security exceptions file.

Each item must have the fields id, tool, reason, owner, expires and review_note.
The owner must be Stanley Sujith Nelavala. The expires date must be an ISO date,
must not be in the past and must be at most 90 days ahead.

The script uses the Python standard library only. It exits 0 when the file is
valid and 1 when it finds a problem.
"""

from __future__ import annotations

import json
import sys
from datetime import date, datetime, timedelta, timezone
from pathlib import Path

REQUIRED_FIELDS = ("id", "tool", "reason", "owner", "expires", "review_note")
OWNER = "Stanley Sujith Nelavala"
MAX_DAYS_AHEAD = 90


def main(argv: list[str]) -> int:
    if len(argv) != 2:
        print("usage: check_security_exceptions.py <path-to-exceptions.json>")
        return 1

    path = Path(argv[1])
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        print(f"FAIL: file not found: {path}")
        return 1
    except json.JSONDecodeError as error:
        print(f"FAIL: invalid JSON: {error}")
        return 1

    if not isinstance(data, list):
        print("FAIL: the top level value must be a list")
        return 1

    today = datetime.now(timezone.utc).date()
    problems = 0

    for index, item in enumerate(data):
        label = f"item {index}"
        if not isinstance(item, dict):
            print(f"FAIL: {label} is not an object")
            problems += 1
            continue

        if isinstance(item.get("id"), str) and item["id"].strip():
            label = f"item {index} ({item['id']})"

        for field in REQUIRED_FIELDS:
            value = item.get(field)
            if not isinstance(value, str) or not value.strip():
                print(f"FAIL: {label} is missing field '{field}'")
                problems += 1

        owner = item.get("owner")
        if isinstance(owner, str) and owner.strip() and owner != OWNER:
            print(f"FAIL: {label} owner must be '{OWNER}'")
            problems += 1

        expires = item.get("expires")
        if isinstance(expires, str) and expires.strip():
            try:
                expiry = date.fromisoformat(expires)
            except ValueError:
                print(f"FAIL: {label} expires is not an ISO date: {expires}")
                problems += 1
            else:
                if expiry < today:
                    print(f"FAIL: {label} expired on {expiry.isoformat()}")
                    problems += 1
                elif expiry > today + timedelta(days=MAX_DAYS_AHEAD):
                    print(
                        f"FAIL: {label} expires more than {MAX_DAYS_AHEAD} days ahead: "
                        f"{expiry.isoformat()}"
                    )
                    problems += 1

    if problems:
        print(f"{problems} problem(s) found in {path}")
        return 1

    print(f"OK: {len(data)} exception(s) in {path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
