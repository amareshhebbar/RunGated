#!/usr/bin/env bash
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
BIN="${RUNGATED_BIN:-$ROOT/bin/rungated}"
[ -x "$BIN" ] || (cd "$ROOT" && go build -o bin/rungated ./cmd/rungated)
ONLY="${1:-all}"

WORK="$(mktemp -d)"
PIDS=()
cleanup() {
  for p in "${PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done
  rm -rf "$WORK"
}
trap cleanup EXIT

GW_PORT="${GW_PORT:-28180}"
RUNGATED_API_KEY=quickstart-admin "$BIN" serve --listen "127.0.0.1:$GW_PORT" --data "$WORK/data" >"$WORK/gateway.log" 2>&1 & PIDS+=($!)
for _ in $(seq 1 100); do
  curl -fsS --noproxy '*' "http://127.0.0.1:$GW_PORT/v1/health" >/dev/null 2>&1 && break
  sleep 0.1
done
REG="$(curl -fsS --noproxy '*' -X POST "http://127.0.0.1:$GW_PORT/v1/agents" -H "Authorization: Bearer quickstart-admin" -H "Content-Type: application/yaml" --data-binary @"$HERE/agent.yaml")"
KEY="$(printf '%s' "$REG" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d.get("agent_key") or d.get("key") or d.get("api_key"))')"
export RUNGATED_GATEWAY="http://127.0.0.1:$GW_PORT" RUNGATED_AGENT_KEY="$KEY" NO_PROXY="127.0.0.1,localhost"

FAIL=0
if [ "$ONLY" = all ] || [ "$ONLY" = python ]; then
  python3 -m venv "$WORK/venv"
  "$WORK/venv/bin/pip" install -q "$ROOT/sdk/python" "langgraph>=0.3" "langchain-core>=0.3" "openai-agents>=0.2"
  "$WORK/venv/bin/python" "$HERE/langgraph_example.py" || FAIL=1
  "$WORK/venv/bin/python" "$HERE/openai_agents_example.py" || FAIL=1
fi
if [ "$ONLY" = all ] || [ "$ONLY" = node ]; then
  if command -v node >/dev/null 2>&1; then
    (cd "$ROOT/sdk/typescript" && { [ -d node_modules ] || npm ci --silent; } && npm run --silent build >/dev/null)
    mkdir -p "$WORK/js/node_modules/@rungated"
    ln -s "$ROOT/sdk/typescript" "$WORK/js/node_modules/@rungated/sdk"
    cp "$HERE/vercel_ai_example.mjs" "$WORK/js/"
    node "$WORK/js/vercel_ai_example.mjs" || FAIL=1
  else
    echo "SKIP vercel-ai: node not installed"
  fi
fi
exit $FAIL
