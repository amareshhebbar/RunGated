.PHONY: docs-build init-smoke dist quickstart load-docker bump k6-smoke tools-k6 tools tools-docker tools-onnx test-full image verify-image kind e2e examples test-all build test race bench bench-gate bench-load outage live record schema verify fuzz vet smoke console sdk-py sdk-ts sdk-test demo build-onnx test-onnx

build:
	go build -o bin/rungated ./cmd/rungated

test:
	go test ./...

race:
	go test -race ./...

bench:
	go test ./internal/policy ./internal/detect -run XXX -bench . -benchtime 2000x

bench-gate:
	go run ./cmd/rungated bench run --gate

bench-load:
	go run ./cmd/rungated bench load

outage: build
	sdk/python/.venv/bin/python scripts/outage_test.py

record:
	bash scripts/record-benchmarks.sh

live: build
	bash scripts/live-check.sh

schema:
	python3 scripts/validate-schema.py

verify:
	bash scripts/verify-all.sh

fuzz:
	go test ./internal/vault -run XXX -fuzz FuzzRedactRestore -fuzztime 60s
	go test ./internal/sse -run XXX -fuzz FuzzSSE -fuzztime 30s
	go test ./internal/llmproxy -run XXX -fuzz FuzzExtractRequest -fuzztime 30s
	go test ./internal/mcpproxy -run XXX -fuzz FuzzParse -fuzztime 30s

vet:
	go vet ./...

smoke: build
	python3 scripts/sdk_smoke.py

console:
	cd console && PNPM="$$(command -v pnpm || echo npx --yes pnpm@9)" && $$PNPM install --frozen-lockfile && $$PNPM build
	rm -rf internal/consoleui/assets && cp -r console/out internal/consoleui/assets

sdk-py:
	cd sdk/python && python3 -m venv .venv && .venv/bin/pip install -e ".[dev]"

sdk-ts:
	cd sdk/typescript && npm install && npm run build

sdk-test: build
	cd sdk/python && .venv/bin/python -m pytest -q
	cd sdk/typescript && npx vitest run

demo: build
	bash scripts/demo-local.sh

build-onnx:
	CGO_ENABLED=1 go build -tags onnx -o bin/rungated ./cmd/rungated

test-onnx:
	go test -race ./internal/tokenizer
	CGO_ENABLED=1 go test -tags onnx -race ./internal/detect/onnx

PY ?= sdk/python/.venv/bin/python

docs:
	mkdocs serve -f mkdocs.yml -a 127.0.0.1:8000

docs-build:
	mkdocs build --strict -f mkdocs.yml

e2e: build
	$(PY) -m pytest -q tests/e2e

examples: build
	bash examples/acme-support/run.sh
	bash examples/quickstart/run.sh

quickstart: build
	bash examples/quickstart/run.sh

init-smoke: build
	bash scripts/init-smoke.sh

dist:
	rm -rf dist && mkdir -p dist
	for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do \
	  os=$${t%/*}; arch=$${t#*/}; ext=""; [ "$$os" = windows ] && ext=".exe"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "-s -w -X main.version=$$(./bin/rungated version 2>/dev/null || echo dev)" -o dist/rungated-$$os-$$arch$$ext ./cmd/rungated || exit 1; \
	done
	cp scripts/install.sh dist/install.sh
	cd dist && sha256sum rungated-* > SHA256SUMS

test-all:
	bash tests/run_all.sh

tools:
	bash scripts/install-tools-fedora.sh

tools-docker:
	bash scripts/install-tools-fedora.sh docker

tools-k6:
	bash scripts/install-tools-fedora.sh k6

k6-smoke: build
	bash scripts/k6-smoke.sh

load-docker:
	bash scripts/load-docker.sh

bump:
	bash scripts/bump-version.sh $(V)

tools-onnx:
	bash scripts/install-tools-fedora.sh onnx

test-full:
	RUN_KIND=1 RUN_LOAD=1 bash tests/run_all.sh

image:
	docker build -f deploy/docker/Dockerfile --target runtime --build-arg VERSION=$$(./bin/rungated version 2>/dev/null || echo dev) -t rungated:local .

verify-image: image
	bash scripts/verify-image.sh rungated:local

kind:
	bash scripts/kind-smoke.sh
