#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MODE="${1:-}"

machine() {
  echo "host:   $(hostname)"
  echo "cpu:    $(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2 | sed 's/^ //') x $(nproc)"
  echo "memory: $(free -g 2>/dev/null | awk '/Mem:/{print $2" GB"}')"
  echo "kernel: $(uname -r)"
}

case "$MODE" in
  server)
    KEY="${API_KEY:?set API_KEY}"
    DATA="${DATA:-/tmp/rungated-load}"
    machine
    cd "$ROOT"
    [ -x bin/rungated ] || go build -o bin/rungated ./cmd/rungated
    rm -rf "$DATA"
    ulimit -n 65535 2>/dev/null || true
    echo "gateway on :8080, stop with Ctrl C"
    RUNGATED_API_KEY="$KEY" exec ./bin/rungated serve --listen 0.0.0.0:8080 --data "$DATA" ${CONFIG:+--config "$CONFIG"}
    ;;
  client)
    TARGET="${BASE_URL:?set BASE_URL, for example http://10.0.0.5:8080}"
    KEY="${API_KEY:?set API_KEY}"
    RATES="${RATES:-250 500 1000 2000 4000}"
    DURATION="${DURATION:-60s}"
    command -v k6 >/dev/null || { echo "install k6: https://grafana.com/docs/k6/latest/set-up/install-k6/"; exit 1; }
    curl -fsS "$TARGET/v1/health" >/dev/null || { echo "gateway not reachable at $TARGET"; exit 1; }
    STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
    OUT="$ROOT/bench/results/load-$STAMP"
    mkdir -p "$OUT"
    machine > "$OUT/client-machine.txt"
    VERSION="$(curl -fsS "$TARGET/v1/version")"
    for r in $RATES; do
      echo "==== $r requests per second for $DURATION"
      BASE_URL="$TARGET" API_KEY="$KEY" RATE="$r" DURATION="$DURATION" MAX_VUS=$(( r < 400 ? 400 : r )) \
        k6 run --quiet --summary-export "$OUT/rate-$r.json" "$ROOT/bench/k6/decide.js" || true
    done
    python3 - "$OUT" "$VERSION" "$TARGET" "$DURATION" <<'PY'
import glob, json, os, sys
out, version, target, duration = sys.argv[1:5]
rows = []
for f in sorted(glob.glob(os.path.join(out, "rate-*.json")), key=lambda p: int(p.rsplit("-", 1)[1][:-5])):
    m = json.load(open(f))["metrics"]
    rate = int(f.rsplit("-", 1)[1][:-5])
    d = m.get("http_req_duration", {})
    dl = m.get("decision_latency_ms", {})
    reqs = m.get("http_reqs", {})
    fail = m.get("http_req_failed", {}).get("value", 0)
    rows.append((rate, reqs.get("rate", 0), d.get("p(95)", 0), d.get("p(99)", 0), dl.get("p(95)", 0), fail * 100))
lines = ["# " + os.environ.get("LOAD_LABEL", "Load test, separate machines"), "", "Gateway: %s at %s. Duration per step: %s." % (version, target, duration), "", "| Target rps | Achieved rps | HTTP p95 ms | HTTP p99 ms | Decision p95 ms | Errors % |", "| --- | --- | --- | --- | --- | --- |"]
for r in rows:
    lines.append("| %d | %.0f | %.2f | %.2f | %.2f | %.2f |" % r)
lines += ["", "Client machine:", "", "```", open(os.path.join(out, "client-machine.txt")).read().strip(), "```", "", "Add the server machine details from the server terminal output."]
open(os.path.join(out, "README.md"), "w").write("\n".join(lines) + "\n")
print("\n".join(lines))
PY
    echo
    echo "results: $OUT"
    ;;
  *)
    echo "usage:"
    echo "  on the gateway machine:  API_KEY=secret scripts/load-remote.sh server"
    echo "  on the load machine:     BASE_URL=http://GATEWAY_IP:8080 API_KEY=secret scripts/load-remote.sh client"
    exit 1
    ;;
esac
