#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CLUSTER="${CLUSTER:-rungated-smoke}"
IMAGE="${IMAGE:-rungated:kind}"
NS="${NS:-rungated}"
KEY="kind-admin-key"
PORT="${PORT:-18580}"
KEEP="${KEEP:-0}"
CREATED=0
PF=""

for t in docker kind helm kubectl; do
  command -v "$t" >/dev/null || { echo "missing $t"; exit 1; }
done

cleanup() {
  [ -n "$PF" ] && kill "$PF" 2>/dev/null || true
  if [ "$KEEP" != "1" ] && [ "$CREATED" = "1" ]; then kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT

if [ "${SKIP_BUILD:-0}" != "1" ]; then
  docker build -f "$ROOT/deploy/docker/Dockerfile" --target runtime --build-arg VERSION=kind -t "$IMAGE" "$ROOT"
fi

if ! kind get clusters | grep -qx "$CLUSTER"; then
  kind create cluster --name "$CLUSTER" --wait 120s
  CREATED=1
fi
kind load docker-image "$IMAGE" --name "$CLUSTER"

REPO="${IMAGE%:*}"
TAG="${IMAGE##*:}"
helm lint "$ROOT/deploy/helm/rungated"
helm upgrade --install rungated "$ROOT/deploy/helm/rungated" \
  --namespace "$NS" --create-namespace \
  --set image.repository="$REPO" --set image.tag="$TAG" --set image.pullPolicy=Never \
  --set secrets.apiKey="$KEY" \
  --set manifests.includeExamples=true \
  --wait --timeout 180s

kubectl -n "$NS" rollout status deploy/rungated --timeout=120s
kubectl -n "$NS" get pods -o wide

kubectl -n "$NS" port-forward svc/rungated "$PORT:8080" >/dev/null 2>&1 &
PF=$!
BASE="http://127.0.0.1:$PORT"
for _ in $(seq 1 60); do curl -fsS "$BASE/v1/health" >/dev/null 2>&1 && break; sleep 1; done

FAIL=0
ok() { echo "PASS $1"; }
bad() { echo "FAIL $1  $2"; FAIL=1; }

curl -fsS "$BASE/v1/health" >/dev/null && ok "service healthy through the cluster" || bad "health" ""
CODE=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/v1/agents")
[ "$CODE" = "401" ] && ok "admin API requires the key" || bad "auth" "HTTP $CODE"
AGENTS="$(curl -fsS "$BASE/v1/agents" -H "Authorization: Bearer $KEY")"
echo "$AGENTS" | grep -q finance-agent && ok "manifest from the chart config map is registered" || bad "manifests" "$AGENTS"
curl -fsS "$BASE/metrics" -H "Authorization: Bearer $KEY" | grep -q rungated_ && ok "metrics with bearer key" || bad "metrics" ""

POD="$(kubectl -n "$NS" get pod -l app.kubernetes.io/name=rungated -o jsonpath='{.items[0].metadata.name}')"
USER_ID="$(kubectl -n "$NS" get pod "$POD" -o jsonpath='{.spec.securityContext.runAsUser}')"
[ "$USER_ID" = "65532" ] && ok "pod runs as user 65532" || bad "pod user" "$USER_ID"
RO="$(kubectl -n "$NS" get pod "$POD" -o jsonpath='{.spec.containers[0].securityContext.readOnlyRootFilesystem}')"
[ "$RO" = "true" ] && ok "read only root filesystem" || bad "read only root" "$RO"

curl -fsS -X POST "$BASE/v1/contain" -H "Authorization: Bearer $KEY" -H 'content-type: application/json' -d '{"agent_id":"finance-agent","reason":"kind"}' >/dev/null
sleep 1
kill "$PF" 2>/dev/null || true
kubectl -n "$NS" delete pod "$POD" --wait=true
kubectl -n "$NS" rollout status deploy/rungated --timeout=120s
kubectl -n "$NS" port-forward svc/rungated "$PORT:8080" >/dev/null 2>&1 &
PF=$!
for _ in $(seq 1 60); do curl -fsS "$BASE/v1/health" >/dev/null 2>&1 && break; sleep 1; done
C="$(curl -fsS "$BASE/v1/containments" -H "Authorization: Bearer $KEY")"
echo "$C" | python3 -c 'import sys,json; c=json.load(sys.stdin)["containments"]; sys.exit(0 if any(x["agent_id"]=="finance-agent" and not x.get("released_at") for x in c) else 1)' \
  && ok "state survives pod replacement on the persistent volume" || bad "persistence" "$C"
curl -fsS "$BASE/v1/audit/verify" -H "Authorization: Bearer $KEY" | grep -q '"ok":true' && ok "audit chain verifies after pod replacement" || bad "audit" ""

echo
[ "$FAIL" = "0" ] && echo "KIND SMOKE OK" || { echo "KIND SMOKE FAILED"; kubectl -n "$NS" logs deploy/rungated --tail=50 || true; }
exit $FAIL