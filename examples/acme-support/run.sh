#!/usr/bin/env bash
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
BIN="${RUNGATED_BIN:-$ROOT/bin/rungated}"
[ -x "$BIN" ] || (cd "$ROOT" && go build -o bin/rungated ./cmd/rungated)

WORK="$(mktemp -d)"
PIDS=()
cleanup() {
  for p in "${PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done
  rm -rf "$WORK"
}
trap cleanup EXIT

python3 -m venv "$WORK/venv"
"$WORK/venv/bin/pip" install -q "$ROOT/sdk/python" "$HERE"

LLM_PORT="${LLM_PORT:-29300}"
GW_PORT="${GW_PORT:-28080}"
"$BIN" demo mock-llm --listen "127.0.0.1:$LLM_PORT" >/dev/null 2>&1 & PIDS+=($!)
RUNGATED_OPENAI_BASE_URL="http://127.0.0.1:$LLM_PORT/v1" OPENAI_API_KEY=demo RUNGATED_API_KEY=admin-secret \
  "$BIN" serve --listen "127.0.0.1:$GW_PORT" --data "$WORK/data" >/dev/null 2>&1 & PIDS+=($!)

for _ in $(seq 1 100); do
  curl -fsS "http://127.0.0.1:$GW_PORT/v1/health" >/dev/null 2>&1 && break
  sleep 0.1
done
for _ in $(seq 1 100); do
  curl -fsS "http://127.0.0.1:$LLM_PORT/healthz" >/dev/null 2>&1 && break
  sleep 0.1
done

REG="$(curl -fsS -X POST "http://127.0.0.1:$GW_PORT/v1/agents" -H "Authorization: Bearer admin-secret" --data-binary @"$HERE/rungated.agent.yaml")"
KEY="$(printf '%s' "$REG" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d.get("agent_key") or d.get("key") or d.get("api_key"))')"

export RUNGATED_GATEWAY="http://127.0.0.1:$GW_PORT" RUNGATED_AGENT_KEY="$KEY" MOCK_LLM="http://127.0.0.1:$LLM_PORT"
"$WORK/venv/bin/python" "$HERE/check.py"
