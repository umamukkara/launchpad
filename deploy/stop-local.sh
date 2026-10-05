#!/usr/bin/env bash
# stop-local.sh
#
# Stops everything started by run-local.sh (engine, missioncontrol,
# prometheus, grafana), using the PIDs it recorded.

set -euo pipefail

cd "$(dirname "$0")/.."   # repo root
PID_FILE="deploy/.local-stack/pids.txt"

if [ ! -f "$PID_FILE" ]; then
  echo "No $PID_FILE found — nothing to stop (or run-local.sh was never run)."
  exit 0
fi

STOPPED=0
while read -r pid; do
  [ -z "$pid" ] && continue
  if kill "$pid" 2>/dev/null; then
    echo "Stopped PID $pid"
    STOPPED=$((STOPPED + 1))
  fi
done < "$PID_FILE"

echo "Stopped $STOPPED process(es)."
: > "$PID_FILE"
