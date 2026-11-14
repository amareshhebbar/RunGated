#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PORT="${PORT:-18200}"
KEY="${API_KEY:-k6-smoke-admin-key}"
RATE="${RATE:-200}"
DURATION="${DURATION:-15s}"
K6="${K6:-k6}"
DATA="$(mktemp -d)"
LOG="$DATA/gateway.log"

command -v "$K6" >/dev/null 2>&1 || { echo "k6 not installed: make tools-k6"; exit 1; }
cd "$ROOT"
[ -x bin/rungated ] || go build -o bin/rungated ./cmd/rungated

PID=""
cleanup() {
  [ -n "$PID" ] && kill "$PID" >/dev/null 2>&1 || true
  [ -n "$PID" ] && wait "$PID" 2>/dev/null || true
  rm -rf "$DATA"
}
trap cleanup EXIT

ulimit -n 65535 2>/dev/null || true
RUNGATED_API_KEY="$KEY" ./bin/rungated serve --listen "127.0.0.1:$PORT" --data "$DATA/data" >"$LOG" 2>&1 &
PID=$!

for _ in $(seq 1 50); do
  if curl -fsS --noproxy '*' "http://127.0.0.1:$PORT/v1/health" >/dev/null 2>&1; then break; fi
  sleep 0.2
done
curl -fsS --noproxy '*' "http://127.0.0.1:$PORT/v1/health" >/dev/null || { echo "gateway did not start"; cat "$LOG"; exit 1; }

echo "k6 at $RATE requests per second for $DURATION against the local gateway"
BASE_URL="http://127.0.0.1:$PORT" API_KEY="$KEY" RATE="$RATE" DURATION="$DURATION" MAX_VUS=400 NO_PROXY="*" \
  "$K6" run --quiet "$ROOT/bench/k6/decide.js"
echo "K6 SMOKE OK"
