#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

GATEWAY_PORT="${GATEWAY_PORT:-8080}"
EGRESS_PORT="${EGRESS_PORT:-8081}"
DATA_DIR="$(mktemp -d)"
PIDS=()

cleanup() {
  for pid in "${PIDS[@]:-}"; do
    [ -n "$pid" ] && kill "$pid" 2>/dev/null || true
  done
  rm -rf "$DATA_DIR"
}
trap cleanup EXIT

if [ ! -x bin/rungated ] || [ -n "$(find cmd internal pkg -newer bin/rungated -name '*.go' -print -quit)" ]; then
  go build -o bin/rungated ./cmd/rungated
fi

PY="${PYTHON:-}"
if [ -z "$PY" ]; then
  if [ ! -x sdk/python/.venv/bin/python ]; then
    python3 -m venv sdk/python/.venv
    sdk/python/.venv/bin/pip install -q -e "sdk/python[dev]"
  fi
  PY="sdk/python/.venv/bin/python"
fi

wait_for() {
  for _ in $(seq 1 100); do
    if curl -fsS "$1" >/dev/null 2>&1; then return 0; fi
    sleep 0.1
  done
  echo "timeout waiting for $1" >&2
  exit 1
}

bin/rungated demo mock-llm --listen 127.0.0.1:9300 2>/dev/null & PIDS+=($!)
bin/rungated demo poisoned-mcp --listen 127.0.0.1:9301 2>/dev/null & PIDS+=($!)
bin/rungated demo evil-web --listen 127.0.0.1:9302 2>/dev/null & PIDS+=($!)

RUNGATED_OPENAI_BASE_URL="http://127.0.0.1:9300/v1" \
RUNGATED_EGRESS_LISTEN="127.0.0.1:${EGRESS_PORT}" \
OPENAI_API_KEY="demo" \
bin/rungated serve --dev --listen "127.0.0.1:${GATEWAY_PORT}" --data "$DATA_DIR" 2>/dev/null & PIDS+=($!)

wait_for "http://127.0.0.1:9300/healthz"
wait_for "http://127.0.0.1:9301/healthz"
wait_for "http://127.0.0.1:9302/healthz"
wait_for "http://127.0.0.1:${GATEWAY_PORT}/v1/health"

export DEMO_GATEWAY="http://127.0.0.1:${GATEWAY_PORT}"
export DEMO_EGRESS="http://127.0.0.1:${EGRESS_PORT}"

set +e
"$PY" demo/run_attacks.py "$@"
STATUS=$?
set -e
exit $STATUS
