#!/usr/bin/env bash
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
RESULTS=()
FAILED=0

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

run "go vet" go vet ./...
run "go test race" go test -race -count=1 ./...
run "manifest examples validate" bash -c 'for f in spec/ats/examples/*.yaml; do go run ./cmd/rungated validate "$f" || exit 1; done'
run "bench regression gate" bash -c 'go run ./cmd/rungated bench run --gate'

if [ -n "${ORT_LIB:-}" ]; then
  run "tier1 onnx tests" env CGO_ENABLED=1 go test -tags onnx -race -count=1 ./internal/detect/onnx
  run "tier1 onnx embed test" bash -c 'cp internal/detect/onnx/testdata/tiny/{classifier,ner}.onnx internal/detect/onnx/testdata/tiny/{tokenizer,labels}.json internal/detect/onnx/models/ && CGO_ENABLED=1 go test -tags "onnx onnx_embed" -count=1 -run Embedded ./internal/detect/onnx; rc=$?; rm -f internal/detect/onnx/models/*.onnx internal/detect/onnx/models/*.json; exit $rc'
else
  skip "tier1 onnx tests" "set ORT_LIB to libonnxruntime.so"
fi

if command -v python3 >/dev/null 2>&1 && python3 -c "import pytest" >/dev/null 2>&1; then
  run "ml tests" bash -c 'cd ml && PYTHONPATH=. python3 -m pytest -q tests'
else
  skip "ml tests" "pytest not installed"
fi

if [ -x sdk/python/.venv/bin/python ]; then
  run "python sdk tests" bash -c 'cd sdk/python && .venv/bin/python -m pytest -q'
else
  skip "python sdk tests" "run make sdk-py"
fi

if [ -d sdk/typescript/node_modules ]; then
  run "typescript sdk tests" bash -c 'cd sdk/typescript && npx vitest run'
else
  skip "typescript sdk tests" "run make sdk-ts"
fi

run "attack demo" ./scripts/demo-local.sh

if command -v docker >/dev/null 2>&1; then
  run "compose config" bash -c 'cd deploy/compose && docker compose config -q && docker compose --profile full config -q && docker compose --profile attack config -q'
else
  skip "compose config" "docker not installed"
fi

if command -v helm >/dev/null 2>&1; then
  run "helm lint" helm lint deploy/helm/rungated
  run "helm template" bash -c 'helm template rungated deploy/helm/rungated >/dev/null'
else
  skip "helm lint" "helm not installed"
fi

echo
printf '%s\n' "${RESULTS[@]}"
exit $FAILED
