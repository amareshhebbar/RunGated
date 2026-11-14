# bench

Attack corpus, regression gate and load test.

```
rungated bench gen                       regenerate bench/corpus.jsonl (deterministic, seed 1)
rungated bench run                       evaluate the corpus on an in process gateway
rungated bench run --gate                fail if recall, false positives or latency regress against bench/baseline.json
rungated bench run --update-baseline     accept the current results as the new baseline
rungated bench run --config tier1.yaml   same corpus with Tier 1 models enabled (build with -tags onnx)
rungated bench run --gateway URL --agent ID   evaluate a running gateway
rungated bench load                      throughput and latency at several concurrency levels
rungated bench load --gateway URL --agent ID   load a running gateway on its own host
```

The corpus has attack classes (direct and indirect injection, tool result and MCP resource injection, multilingual injection in 12 languages, tool description poisoning, secret leaks, PII leaks, egress exfiltration, tool abuse), 14 mutations (case, leetspeak, homoglyphs, zero width characters, fullwidth, spaced letters, base64, hex, ROT13, HTML comments, markdown hiding, Unicode tags, split across parts) and benign traffic including hard negatives such as security articles that mention injection.

Important limits of this measurement:

- The corpus is first party and was written next to the Tier 0 rules, so Tier 0 recall here is an upper bound, not a generalisation estimate. Its job is to catch regressions.
- The right way to claim detection quality is to evaluate Tier 1 on public held out data with ml/ (rungated-ml eval) and to add real incidents to the corpus over time.
- The gate compares against bench/baseline.json. After an intentional improvement, run with --update-baseline and commit the file.

Held out data for honest detection claims: bench/heldout/README.md. Recorded runs with machine details: bench/results/README.md. k6 script: bench/k6/README.md.
