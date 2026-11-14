#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
HOST="$(hostname -s 2>/dev/null || echo host)"
OUT="${OUT:-bench/results/$STAMP-$HOST}"
DURATION="${DURATION:-20s}"
CONCURRENCY="${CONCURRENCY:-1,8,32,64,128}"
CONFIG="${CONFIG:-}"
TAGS="${TAGS:-}"
mkdir -p "$OUT"

BIN="$OUT/rungated"
if [ -n "$TAGS" ]; then
  CGO_ENABLED=1 go build -tags "$TAGS" -o "$BIN" ./cmd/rungated
else
  go build -o "$BIN" ./cmd/rungated
fi

CFGFLAG=()
if [ -n "$CONFIG" ]; then CFGFLAG=(--config "$CONFIG"); fi

{
  echo "# Environment"
  echo
  echo "| Item | Value |"
  echo "| --- | --- |"
  echo "| Recorded at | $STAMP |"
  echo "| Host | $HOST |"
  echo "| Kernel | $(uname -sr) |"
  echo "| CPU | $(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2- | xargs || sysctl -n machdep.cpu.brand_string 2>/dev/null || echo unknown) |"
  echo "| Logical CPUs | $(getconf _NPROCESSORS_ONLN 2>/dev/null || nproc) |"
  echo "| Memory | $(awk '/MemTotal/ {printf "%.1f GiB", $2/1048576}' /proc/meminfo 2>/dev/null || echo unknown) |"
  echo "| Go | $(go version | cut -d' ' -f3) |"
  echo "| Build tags | ${TAGS:-none} |"
  echo "| Config | ${CONFIG:-defaults} |"
  echo "| Commit | $(git rev-parse --short HEAD 2>/dev/null || echo none) |"
  echo "| Version | $("$BIN" version 2>/dev/null | head -1) |"
} > "$OUT/environment.md"

"$BIN" bench run "${CFGFLAG[@]}" --report "$OUT/detection.json" --md "$OUT/detection.md" > /dev/null
"$BIN" bench load "${CFGFLAG[@]}" --duration "$DURATION" --concurrency "$CONCURRENCY" --report "$OUT/load.json" --md "$OUT/load.md" > /dev/null

if [ -x sdk/python/.venv/bin/python ]; then
  RUNGATED_BIN="$BIN" sdk/python/.venv/bin/python scripts/outage_test.py > "$OUT/outage.txt" 2>&1 || true
elif command -v python3 >/dev/null 2>&1; then
  RUNGATED_BIN="$BIN" python3 scripts/outage_test.py > "$OUT/outage.txt" 2>&1 || true
fi

rm -f "$BIN"
{
  echo "# Benchmark record $STAMP"
  echo
  cat "$OUT/environment.md"
  echo
  cat "$OUT/detection.md"
  echo
  cat "$OUT/load.md"
  if [ -f "$OUT/outage.txt" ]; then
    echo
    echo "# Gateway failure test"
    echo
    echo '```'
    cat "$OUT/outage.txt"
    echo '```'
  fi
} > "$OUT/REPORT.md"

echo "$OUT/REPORT.md"
