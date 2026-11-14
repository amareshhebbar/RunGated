#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
IMAGE="${IMAGE:-rungated:local}"
KEY="${API_KEY:-load-local-key}"
PORT="${PORT:-18300}"
GW_CPUS="${GW_CPUS:-2}"
GW_MEM="${GW_MEM:-1g}"
NAME="rungated-load-$$"
TOTAL="$(nproc)"

command -v docker >/dev/null || { echo "docker not installed: make tools-docker"; exit 1; }
command -v k6 >/dev/null || { echo "k6 not installed: make tools-k6"; exit 1; }
[ "$TOTAL" -ge $(( GW_CPUS + 2 )) ] || { echo "need at least $(( GW_CPUS + 2 )) cpu cores, this machine has $TOTAL"; exit 1; }

GW_SET="0-$(( GW_CPUS - 1 ))"
K6_SET="$GW_CPUS-$(( TOTAL - 1 ))"

docker image inspect "$IMAGE" >/dev/null 2>&1 || make -C "$ROOT" image

cleanup() {
  docker rm -f "$NAME" >/dev/null 2>&1 || true
  docker volume rm -f "$NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker run -d --name "$NAME" \
  --cpuset-cpus "$GW_SET" --cpus "$GW_CPUS" --memory "$GW_MEM" \
  --ulimit nofile=65535:65535 \
  --read-only --tmpfs /tmp --cap-drop ALL --security-opt no-new-privileges \
  -p "127.0.0.1:$PORT:8080" -v "$NAME:/data" \
  -e RUNGATED_API_KEY="$KEY" \
  "$IMAGE" serve >/dev/null

for _ in $(seq 1 100); do
  curl -fsS --noproxy '*' "http://127.0.0.1:$PORT/v1/health" >/dev/null 2>&1 && break
  sleep 0.2
done
curl -fsS --noproxy '*' "http://127.0.0.1:$PORT/v1/health" >/dev/null || { docker logs "$NAME" | tail -20; exit 1; }

echo "gateway in docker: cores $GW_SET, $GW_CPUS cpus, $GW_MEM memory"
echo "k6 on cores $K6_SET"

LOAD_LABEL="Load test, one machine, gateway in Docker on cores $GW_SET ($GW_CPUS cpus, $GW_MEM), k6 on cores $K6_SET" \
NO_PROXY="*" BASE_URL="http://127.0.0.1:$PORT" API_KEY="$KEY" \
RATES="${RATES:-250 500 1000 2000}" DURATION="${DURATION:-30s}" \
  taskset -c "$K6_SET" bash "$ROOT/scripts/load-remote.sh" client

docker stats --no-stream --format "gateway at end: cpu {{.CPUPerc}} memory {{.MemUsage}}" "$NAME" || true
