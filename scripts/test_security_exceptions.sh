#!/usr/bin/env bash
# Checks the exceptions checker with a good file, an expired file and a file with a missing field.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
d="$(mktemp -d)"
trap 'rm -rf "$d"' EXIT
future="$(date -u -d '+30 days' +%F)"
printf '[{"id":"x","tool":"t","reason":"r","owner":"Stanley Sujith Nelavala","expires":"%s","review_note":"n"}]' "$future" > "$d/good.json"
printf '[{"id":"x","tool":"t","reason":"r","owner":"Stanley Sujith Nelavala","expires":"2020-01-01","review_note":"n"}]' > "$d/expired.json"
printf '[{"id":"x","tool":"t","reason":"r","owner":"Stanley Sujith Nelavala","expires":"%s"}]' "$future" > "$d/missing.json"
python3 scripts/check_security_exceptions.py "$d/good.json" >/dev/null
echo "good file: accepted"
if python3 scripts/check_security_exceptions.py "$d/expired.json" >/dev/null; then echo "expired file: WRONGLY accepted"; exit 1; fi
echo "expired file: rejected"
if python3 scripts/check_security_exceptions.py "$d/missing.json" >/dev/null; then echo "missing field: WRONGLY accepted"; exit 1; fi
echo "missing field: rejected"
