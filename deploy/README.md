# deploy

- docker: multi stage Dockerfile. `--target runtime` is a static distroless image. `--target runtime-onnx --build-arg ONNX=true` builds with CGO and ships libonnxruntime (verify with scripts/verify-docker-onnx.sh).
- compose: `docker compose --profile default up --build` runs the gateway, Console, mock LLM, poisoned MCP server and evil web server. `--profile full` swaps the gateway for one backed by Postgres. `--profile attack` adds the attack runner.
- helm: `helm install rungated deploy/helm/rungated`, values documented in deploy/helm/rungated/values.yaml.

Attack demo with compose:

```
cd deploy/compose
docker compose --profile default up --build -d
docker compose --profile default --profile attack run --rm run-attacks
```

Console at http://localhost:8080.
