# Recorded results

`scripts/record-benchmarks.sh` writes one directory per run here, named by UTC time and host: environment, detection results, load results at several concurrency levels, and the gateway failure test. Commit the directory so every number you quote has its setup next to it.

    scripts/record-benchmarks.sh
    TAGS=onnx CONFIG=tier1.yaml DURATION=30s scripts/record-benchmarks.sh

Quote latency like this: hardware, concurrency, p50 and p95 and p99, error rate, and whether the load generator shared the machine with the gateway. The in process mode shares it, so for a published number run the gateway on its own host and pass `--gateway` to `rungated bench load`.
