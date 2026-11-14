#!/usr/bin/env bash
set -uo pipefail

IMAGE="${1:?usage: scripts/verify-image.sh IMAGE [EXPECTED_VERSION]}"
EXPECT="${2:-}"
NAME="rg-verify-$$"
VOL="rg-verify-data-$$"
PORT="${PORT:-18480}"
EPORT="${EPORT:-18481}"
KEY="verify-admin-key"
BASE="http://127.0.0.1:$PORT"
FAIL=0

ok() { echo "PASS $1"; }
bad() { echo "FAIL $1  $2"; FAIL=1; }

cleanup() {
  docker rm -f "$NAME" >/dev/null 2>&1
  docker volume rm -f "$VOL" >/dev/null 2>&1
}
trap cleanup EXIT

start() {
  docker run -d --name "$NAME" \
    --read-only --tmpfs /tmp \
    --cap-drop ALL --security-opt no-new-privileges \
    -p "127.0.0.1:$PORT:8080" -p "127.0.0.1:$EPORT:8081" \
    -v "$VOL:/data" \
    -e RUNGATED_API_KEY="$KEY" \
    "$IMAGE" serve >/dev/null
  for _ in $(seq 1 100); do
    curl -fsS "$BASE/v1/health" >/dev/null 2>&1 && return 0
    sleep 0.2
  done
  docker logs "$NAME" 2>&1 | tail -20
  return 1
}

docker image inspect "$IMAGE" >/dev/null 2>&1 || docker pull "$IMAGE" >/dev/null || { echo "FAIL cannot pull $IMAGE"; exit 1; }

USER_ID="$(docker image inspect -f '{{.Config.User}}' "$IMAGE")"
case "$USER_ID" in
  ""|root|0|0:0) bad "image runs as non root" "user is '${USER_ID:-root}'" ;;
  *) ok "image runs as non root user $USER_ID" ;;
esac

SIZE_MB=$(( $(docker image inspect -f '{{.Size}}' "$IMAGE") / 1048576 ))
ok "image size ${SIZE_MB} MB"

if start; then ok "starts with read only root, no capabilities and a fresh named volume"; else bad "start" "gateway did not become healthy"; exit 1; fi

VER="$(curl -fsS "$BASE/v1/version")"
if [ -n "$EXPECT" ]; then
  echo "$VER" | grep -q "\"$EXPECT\"" && ok "version is $EXPECT" || bad "version" "$VER"
else
  ok "version endpoint: $VER"
fi

CODE=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/v1/agents")
[ "$CODE" = "401" ] && ok "admin API refuses requests without a key" || bad "admin auth" "HTTP $CODE"

REG="$(docker exec "$NAME" /rungated validate /spec/ats/examples/finance-agent.yaml 2>&1)"
echo "$REG" | grep -q "^ok" && ok "bundled example manifest validates inside the image" || bad "validate in image" "$REG"

REG="$(docker run --rm --entrypoint /rungated "$IMAGE" version 2>&1)"
[ -n "$REG" ] && ok "binary runs as a one shot command: $REG" || bad "one shot command" ""

MANIFEST="$(cd "$(dirname "$0")/.." && pwd)/spec/ats/examples/finance-agent.yaml"
RESP="$(curl -fsS -X POST "$BASE/v1/agents" -H "Authorization: Bearer $KEY" --data-binary @"$MANIFEST")"
AKEY="$(printf '%s' "$RESP" | python3 -c 'import sys,json; d=json.load(sys.stdin); print(d.get("agent_key") or d.get("key") or "")')"
[ -n "$AKEY" ] && ok "agent registered through the API" || bad "register" "$RESP"

decide() {
  curl -s -X POST "$BASE/v1/decide" -H 'content-type: application/json' \
    -H "X-Rungated-Agent: finance-agent" -H "X-Rungated-Key: $AKEY" -d "$1" \
    | python3 -c 'import sys,json; print(json.load(sys.stdin).get("verdict",""))'
}

V=$(decide '{"agent_id":"finance-agent","kind":"tool.call","tool":"get_invoice","server":"finance-mcp","args":{"invoice_id":"INV-000123"}}')
[ "$V" = "allow" ] && ok "benign tool call allowed" || bad "benign call" "$V"
V=$(decide '{"agent_id":"finance-agent","kind":"tool.call","tool":"wipe_database"}')
case "$V" in block|contain) ok "unknown tool $V";; *) bad "unknown tool" "$V";; esac

curl -fsS "$BASE/metrics" -H "Authorization: Bearer $KEY" | grep -q "rungated_" && ok "metrics served" || bad "metrics" ""

CODE=$(curl -s -o /dev/null -w '%{http_code}' --noproxy '' -x "http://finance-agent:$AKEY@127.0.0.1:$EPORT" http://169.254.169.254/latest/meta-data/)
case "$CODE" in 403|407) ok "egress proxy listening and refuses cloud metadata (HTTP $CODE)";; *) bad "egress proxy" "HTTP $CODE";; esac

curl -fsS -X POST "$BASE/v1/contain" -H "Authorization: Bearer $KEY" -H 'content-type: application/json' -d '{"agent_id":"finance-agent","reason":"verify"}' >/dev/null
sleep 0.5
docker restart "$NAME" >/dev/null
for _ in $(seq 1 100); do curl -fsS "$BASE/v1/health" >/dev/null 2>&1 && break; sleep 0.2; done
C="$(curl -fsS "$BASE/v1/containments" -H "Authorization: Bearer $KEY")"
echo "$C" | python3 -c 'import sys,json; c=json.load(sys.stdin)["containments"]; sys.exit(0 if any(x["agent_id"]=="finance-agent" and not x.get("released_at") for x in c) else 1)' \
  && ok "state on the volume survives a container restart" || bad "persistence" "$C"
A="$(curl -fsS "$BASE/v1/audit/verify" -H "Authorization: Bearer $KEY")"
echo "$A" | grep -q '"ok":true' && ok "audit chain verifies after restart" || bad "audit" "$A"

docker stop -t 10 "$NAME" >/dev/null
EXIT=$(docker inspect -f '{{.State.ExitCode}}' "$NAME")
[ "$EXIT" = "0" ] && ok "clean shutdown on SIGTERM" || bad "shutdown" "exit code $EXIT"

echo
[ "$FAIL" = "0" ] && echo "IMAGE OK: $IMAGE" || echo "IMAGE CHECK FAILED: $IMAGE"
exit $FAIL