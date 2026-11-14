# Contributing

Thanks for helping make AI agents safer.

## Before you start

Open an issue first for anything bigger than a small fix, so we can agree on the approach before you spend time on it.

## Setup

```bash
go build ./...
go test ./...
make demo
```

Go 1.24 or newer. The Console needs Node 22 and pnpm only if you change `console/`. The ONNX path needs ONNX Runtime: `make tools-onnx` on Fedora, then `make test-onnx`.

## Pull requests

* Keep each pull request to one change.
* Add or update tests. `go test ./...` and `gofmt -l .` must be clean.
* New detections need cases in `bench/corpus.jsonl` and must not raise the false positive rate in `make bench-gate`.
* Never commit secrets, real customer data or model weights.

## License

By contributing you agree that your contribution is licensed under the Apache License, Version 2.0.
