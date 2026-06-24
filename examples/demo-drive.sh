#!/usr/bin/env bash
# Animate a LIVE punchcard board for a demo recording / sanity check.
# Posts a handful of tasks and walks them todo -> in_progress -> in_review (+PR)
# -> done (+proof) on a timer, so you can screen-record the board moving in real time.
# This drives the real API the same way the Engineer loop does — it just doesn't run agents.
#
# Usage:  run `punch serve` in one terminal, then in another:  examples/demo-drive.sh
# Knobs:  STEP=<seconds-between-stages> (default 1.4), PUNCH_URL / PUNCH_TOKEN as usual.
set -euo pipefail
: "${PUNCH_URL:=http://127.0.0.1:8080}"
STEP="${STEP:-1.4}"
HERE="$(cd "$(dirname "$0")" && pwd)"
PROOF="$HERE/proof.png"

command -v punch >/dev/null || { echo "error: 'punch' not on PATH" >&2; exit 1; }
jid(){ python3 -c "import sys,json;print(json.load(sys.stdin)['id'])"; }
pr(){ echo "https://github.com/acme/app/pull/$((40+$1))"; }

TITLES=(
  "Add CSV export to reports"
  "Rate-limit the public API"
  "Fix flaky login redirect on Safari"
  "Add a /health endpoint"
  "Dark mode toggle"
  "Cache npm deps in CI"
)
IDS=()
for i in "${!TITLES[@]}"; do
  IDS+=("$(punch add --title "${TITLES[$i]}" --priority $(( (i % 3) + 1 )) | jid)")
done
echo "seeded ${#IDS[@]} tasks; animating (Ctrl-C to stop)…"

# Pipeline: task i enters in_progress at step i, in_review at i+1, done at i+2.
for f in $(seq 0 $(( ${#IDS[@]} + 1 ))); do
  for i in "${!IDS[@]}"; do
    id="${IDS[$i]}"
    if   [ "$f" -eq "$i" ];        then punch update "$id" --status in_progress >/dev/null
    elif [ "$f" -eq $(( i + 1 )) ]; then punch update "$id" --status in_review --pr "$(pr $(( i + 1 )))" >/dev/null
    elif [ "$f" -eq $(( i + 2 )) ]; then
      punch update "$id" --status done >/dev/null
      [ -f "$PROOF" ] && [ $(( i % 3 )) -eq 0 ] && punch attach "$id" "$PROOF" >/dev/null
    fi
  done
  sleep "$STEP"
done
echo "done — board fully shipped."
