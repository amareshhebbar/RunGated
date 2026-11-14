#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="${BIN:-$ROOT/bin/rungated}"
[ -x "$BIN" ] || (cd "$ROOT" && go build -o bin/rungated ./cmd/rungated)

PORT=$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])')
EPORT=$(python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])')
DATA=$(mktemp -d)
GW="http://127.0.0.1:$PORT"
KEY=init-smoke-key

RUNGATED_UNKNOWN_AGENTS=observe RUNGATED_API_KEY=$KEY RUNGATED_EGRESS_LISTEN=127.0.0.1:$EPORT \
  "$BIN" serve --dev --listen 127.0.0.1:$PORT --data "$DATA" >"$DATA/server.log" 2>&1 &
PID=$!
trap 'kill $PID 2>/dev/null || true; wait $PID 2>/dev/null || true; rm -rf "$DATA"' EXIT

for _ in $(seq 1 50); do
  curl -sf "$GW/v1/health" >/dev/null 2>&1 && break
  sleep 0.1
done

decide() {
  curl -sf -X POST "$GW/v1/decide" -H "X-Rungated-Agent: learn-bot" -H 'Content-Type: application/json' -d "$1"
}

for i in $(seq 1 25); do
  decide "{\"kind\":\"tool.call\",\"server\":\"billing\",\"tool\":\"get_invoice\",\"args\":{\"invoice_id\":\"INV-$((10000+i))\"}}" >/dev/null
  decide "{\"kind\":\"tool.call\",\"server\":\"billing\",\"tool\":\"refund_payment\",\"args\":{\"invoice_id\":\"INV-$((20000+i))\",\"amount\":$((i*4))}}" >/dev/null
done
decide '{"kind":"llm.request","provider":"openai","model":"gpt-4o-mini-2024-07-18","texts":[{"source":"user","text":"hello"}]}' >/dev/null
sleep 1

"$BIN" init --id learn-bot --mode auto_contain --from-gateway "$GW" --api-key $KEY --out "$DATA/learned.yaml" 2>"$DATA/init.log"
"$BIN" validate "$DATA/learned.yaml" >/dev/null
grep -q 'pattern: \^INV-\[0-9\]{5}\$' "$DATA/learned.yaml" || { echo "pattern not learned"; cat "$DATA/learned.yaml"; exit 1; }
grep -q 'gpt-4o-mini\*' "$DATA/learned.yaml" || { echo "model not learned"; exit 1; }

"$BIN" init --id learn-bot --from-events "$DATA/state/events.jsonl" --out "$DATA/offline.yaml" 2>/dev/null
"$BIN" validate "$DATA/offline.yaml" >/dev/null

"$BIN" init --id starter-bot --out "$DATA/starter.yaml" 2>/dev/null
"$BIN" validate "$DATA/starter.yaml" >/dev/null

"$BIN" agents register "$DATA/learned.yaml" --gateway "$GW" --api-key $KEY >/dev/null

verdict() {
  decide "$1" | python3 -c 'import json,sys;print(json.load(sys.stdin)["verdict"])'
}

fail=0
expect() {
  local want="$1" body="$2" got
  got=$(verdict "$body")
  if [ "$got" = "$want" ]; then
    echo "PASS  $3 -> $got"
  else
    echo "FAIL  $3 -> $got (want $want)"
    fail=1
  fi
}

expect allow '{"kind":"tool.call","server":"billing","tool":"refund_payment","args":{"invoice_id":"INV-20001","amount":60}}' "refund inside learned range"
expect block '{"kind":"tool.call","server":"billing","tool":"refund_payment","args":{"invoice_id":"INV-20001","amount":9000}}' "refund far over learned range"
expect block '{"kind":"tool.call","server":"billing","tool":"refund_payment","args":{"invoice_id":"../etc/passwd","amount":10}}' "argument with the wrong shape"
expect block '{"kind":"tool.call","server":"billing","tool":"delete_customer","args":{"id":"c1"}}' "tool never seen"

exit $fail
