# Docker

```bash
docker run --rm -p 8080:8080 -p 8081:8081 \
  -v rungated-data:/data \
  ghcr.io/amareshhebbar/rungated:latest serve --data /data
```

## Compose demo

```bash
cd deploy/compose
docker compose --profile default up --build -d
docker compose --profile default --profile attack run --rm run-attacks
```

The Console is at [http://localhost:8080](http://localhost:8080). Use `--profile full` to run against Postgres.

## Images

* `runtime`: static distroless image.
* `runtime-onnx`: includes the ONNX runtime for the Tier 1 model. Build with `--build-arg ONNX=true`.
