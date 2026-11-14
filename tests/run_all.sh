#!/usr/bin/env bash
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
RESULTS=()
FAILED=0
PY="${PY:-$ROOT/sdk/python/.venv/bin/python}"

run() {
  local name="$1"
  shift
  echo "=== $name"
  if "$@"; then
    RESULTS+=("PASS  $name")
  else
    RESULTS+=("FAIL  $name")
    FAILED=1
  fi
}
skip() { RESULTS+=("SKIP  $1  ($2)"); }

if [ ! -x "$PY" ]; then
  python3 -m venv sdk/python/.venv && sdk/python/.venv/bin/pip install -q -e "sdk/python[dev]" jsonschema
fi

run "build" go build -o bin/rungated ./cmd/rungated
run "go vet" go vet ./...
run "go test race" go test -race -count=1 ./...
run "manifest examples validate" bash -c 'for f in spec/ats/examples/*.yaml examples/*/rungated.agent.yaml; do ./bin/rungated validate "$f" >/dev/null || exit 1; done'
run "schema" "$PY" scripts/validate-schema.py
run "bench regression gate" ./bin/rungated bench run --gate

run "python sdk tests" bash -c 'cd sdk/python && ../../'"${PY#$ROOT/}"' -m pytest -q'
if command -v npx >/dev/null 2>&1 && [ -d sdk/typescript/node_modules ]; then
  run "typescript sdk tests" bash -c 'cd sdk/typescript && npx vitest run'
else
  skip "typescript sdk tests" "run npm install in sdk/typescript"
fi

run "sdk smoke" "$PY" scripts/sdk_smoke.py
run "e2e suite" "$PY" -m pytest -q tests/e2e
run "live check" bash scripts/live-check.sh
run "outage and restart" "$PY" scripts/outage_test.py
run "attack demo" bash scripts/demo-local.sh
run "external project examples/acme-support" bash examples/acme-support/run.sh
run "quickstart examples" bash examples/quickstart/run.sh
run "init and learn rulebook" bash scripts/init-smoke.sh

if [ -f mkdocs.public.yml ] && command -v mkdocs >/dev/null 2>&1; then
  run "docs public strict build" mkdocs build --strict -f mkdocs.public.yml
  run "docs internal strict build" mkdocs build --strict -f mkdocs.internal.yml
elif [ -f mkdocs.public.yml ]; then
  skip "docs build" "pip install -r docs/requirements.txt"
fi
DOCS_DIR=docs/mintlify
[ -f docs/docs.json ] && DOCS_DIR=docs
if [ -f "$DOCS_DIR/docs.json" ] && command -v mint >/dev/null 2>&1; then
  run "mintlify docs links" bash -c "cd $DOCS_DIR && mint broken-links"
elif [ -f "$DOCS_DIR/docs.json" ]; then
  skip "mintlify docs links" "npm i -g mint"
fi

if [ -z "${ORT_LIB:-}" ] && [ -f "$HOME/.local/share/rungated/onnxruntime/lib/libonnxruntime.so" ]; then
  export ORT_LIB="$HOME/.local/share/rungated/onnxruntime/lib/libonnxruntime.so"
fi
if [ -n "${ORT_LIB:-}" ]; then
  run "tier1 onnx tests" env CGO_ENABLED=1 go test -tags onnx -race -count=1 ./internal/detect/onnx
else
  skip "tier1 onnx tests" "set ORT_LIB to libonnxruntime.so"
fi
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  run "docker image build and check" make verify-image
else
  skip "docker image" "docker not installed or daemon not running"
fi
if command -v helm >/dev/null 2>&1; then
  run "helm lint" helm lint deploy/helm/rungated
else
  skip "helm lint" "helm not installed"
fi
if [ "${RUN_KIND:-0}" = "1" ]; then
  run "helm on a kind cluster" bash scripts/kind-smoke.sh
else
  skip "helm on a kind cluster" "set RUN_KIND=1, needs docker, kind, helm and kubectl"
fi
if command -v k6 >/dev/null 2>&1; then
  run "k6 load smoke" bash scripts/k6-smoke.sh
  if [ "${RUN_LOAD:-0}" = "1" ] && command -v docker >/dev/null 2>&1; then
    run "k6 load in docker" bash scripts/load-docker.sh
  fi
else
  skip "k6 load" "k6 not installed"
fi

echo
echo "==== summary"
printf '%s\n' "${RESULTS[@]}"
exit $FAILED
